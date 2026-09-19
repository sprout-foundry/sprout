//go:build !js

package webui

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// fileRevision is a file's on-disk revision (§7a): modification time in unix
// seconds plus the sha256 of its content. A missing/unreadable file yields an
// error; the caller decides how to report it.
type fileRevision struct {
	mtime int64
	hash  string
}

func fileRevisionFor(path string) (fileRevision, error) {
	// path is the canonicalizePath-verified resolution of the request path —
	// the same value handleFileRead/handleFileWrite use directly and
	// unsuppressed. gosec's interprocedural taint analysis cannot see the
	// sanitizer across this helper's boundary, hence the two annotations.
	info, err := os.Stat(path) //nolint:gosec // G703: canonical, sanitizer-verified path
	if err != nil {
		return fileRevision{}, err
	}
	data, err := os.ReadFile(path) //nolint:gosec // G703: canonical, sanitizer-verified path
	if err != nil {
		return fileRevision{}, err
	}
	sum := sha256.Sum256(data)
	return fileRevision{mtime: info.ModTime().Unix(), hash: hex.EncodeToString(sum[:])}, nil
}

// handleAPIFile handles API requests for file operations (read/write)
func (ws *ReactWebServer) handleAPIFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleFileRead(w, r)
	case http.MethodPost:
		ws.handleFileWrite(w, r)
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
	}
}

// handleFileRead handles file read operations
func (ws *ReactWebServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "File path is required")
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, false)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_file_path", fmt.Sprintf("Invalid file path: %v", err))
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(canonicalPath)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, "file_not_found", fmt.Sprintf("File not found: %v", err))
		return
	}

	if info.IsDir() {
		writeJSONErr(w, http.StatusBadRequest, "path_is_directory", "Path is a directory")
		return
	}

	if info.Size() > maxFileReadSize {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]interface{}{
			"error":    "file too large to open in editor",
			"size":     info.Size(),
			"max_size": maxFileReadSize,
		})
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !isAppConfigPath(canonicalPath) {
		// Unified session allowlist (filesystem perms work): if the
		// active chat agent has the file's folder on its session
		// allowlist (because the user previously approved it via the
		// agent's filesystem dialog), skip the token check. This
		// makes browser file opens consistent with agent file reads:
		// one approval covers both surfaces.
		if a := ws.getActiveAgentForRequest(r); a != nil && a.IsFolderSessionAllowed(canonicalPath) {
			// Allowlisted — fall through and serve the file.
		} else {
			consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
			if consentToken == "" {
				consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
			}
			if !fileConsents.consume(consentToken, canonicalPath, "read") {
				ws.writeExternalPathConsentRequired(w, canonicalPath, "read")
				return
			}
		}
	}

	// Read file content
	content, err := os.ReadFile(canonicalPath)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_read_file", fmt.Sprintf("Failed to read file: %v", err))
		return
	}

	// Last-Modified lets safe-write clients (SP-140-7 §7a) echo this value
	// back as baseMtime on the POST, turning blind overwrites into
	// revision-checked writes with no extra round-trip.
	w.Header().Set("Last-Modified", info.ModTime().UTC().Format(http.TimeFormat))

	// Determine content type
	// First, try to detect content type from the file content (magic bytes)
	contentType := http.DetectContentType(content)

	// Fallback to extension-based detection for types http.DetectContentType can't reliably detect
	// or for types that need specific MIME types (like .js, .svg)
	ext := strings.ToLower(filepath.Ext(canonicalPath))
	switch ext {
	case ".json":
		contentType = "application/json"
	case ".js":
		contentType = "application/javascript"
	case ".css":
		contentType = "text/css"
	case ".html":
		contentType = "text/html"
	case ".svg":
		contentType = "image/svg+xml"
	case ".png":
		contentType = "image/png"
	case ".jpg", ".jpeg":
		contentType = "image/jpeg"
	case ".gif":
		contentType = "image/gif"
	case ".webp":
		contentType = "image/webp"
	case ".bmp":
		contentType = "image/bmp"
	case ".ico":
		contentType = "image/x-icon"
	}

	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(content)))
	w.Write(content)
}

// handleFileWrite handles file write operations
func (ws *ReactWebServer) handleFileWrite(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		writeJSONErr(w, http.StatusBadRequest, "path_required", "File path is required")
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, true)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "invalid_file_path", fmt.Sprintf("Invalid file path: %v", err))
		return
	}

	if !isWithinWorkspace(canonicalPath, workspaceRoot) && !isAppConfigPath(canonicalPath) {
		if a := ws.getActiveAgentForRequest(r); a != nil && a.IsFolderSessionAllowed(canonicalPath) {
			// Allowlisted — fall through and write the file.
		} else {
			consentToken := strings.TrimSpace(r.Header.Get(consentTokenHeader))
			if consentToken == "" {
				consentToken = strings.TrimSpace(r.URL.Query().Get("consent_token"))
			}
			if !fileConsents.consume(consentToken, canonicalPath, "write") {
				ws.writeExternalPathConsentRequired(w, canonicalPath, "write")
				return
			}
		}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxFileWriteBodySize)
	// Read request body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, "failed_to_read_request_body", fmt.Sprintf("Failed to read request body: %v", err))
		return
	}

	// Parse JSON to extract content field
	var requestData struct {
		Content string `json:"content"`
		// SP-140-7 §7a safe-write seam (opt-in): when either guard is
		// supplied, the write is conditional on the file still being at the
		// revision the caller last read — baseMTime against the on-disk mtime
		// (unix seconds) and/or baseHash against the sha256 of the on-disk
		// bytes. A mismatch returns 409 with the current revision and writes
		// nothing. Omitted guards preserve the historical unconditional
		// behavior, so existing callers (editor saves, the design surfaces
		// predating the seam) are untouched.
		BaseMTime *int64  `json:"baseMtime,omitempty"`
		BaseHash  *string `json:"baseHash,omitempty"`
	}
	if err := json.Unmarshal(body, &requestData); err != nil {
		writeJSONErr(w, http.StatusBadRequest, "failed_to_parse_json", fmt.Sprintf("Failed to parse JSON: %v", err))
		return
	}

	// §7a: evaluate the guards BEFORE any write. The stat/read race window
	// after this point is the same one the unconditional path always had; the
	// guard shrinks lost-update exposure from "since first read" to
	// "since this check".
	if requestData.BaseMTime != nil || requestData.BaseHash != nil {
		// Compute the file's current revision once: mtime + content hash.
		// canonicalPath is the canonicalizePath-verified resolution (the same
		// sanitizer the read/write paths below use); hashing here keeps the
		// guard logic in one place.
		currentRev, revErr := fileRevisionFor(canonicalPath)
		if revErr != nil {
			// The file the caller read is gone (or unreadable): that is a
			// conflict — an unconditional write would silently re-create it.
			// The message distinguishes deletion from other stat failures.
			reason := "The file was deleted after it was read; nothing was written."
			if !os.IsNotExist(revErr) {
				reason = "The file could not be read to verify the base revision; nothing was written."
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":   "base_file_missing",
				"message": reason,
				"path":    canonicalPath,
			})
			return
		}
		conflict := func() {
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":        "revision_conflict",
				"message":      "The file changed after it was read; nothing was written.",
				"path":         canonicalPath,
				"currentMtime": currentRev.mtime,
				"currentHash":  currentRev.hash,
			})
		}
		if requestData.BaseMTime != nil && currentRev.mtime != *requestData.BaseMTime {
			conflict()
			return
		}
		if requestData.BaseHash != nil && *requestData.BaseHash != "" && currentRev.hash != *requestData.BaseHash {
			conflict()
			return
		}
	}

	content := []byte(requestData.Content)

	// Create directory if it doesn't exist
	dir := filepath.Dir(canonicalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_create_directory", fmt.Sprintf("Failed to create directory: %v", err))
		return
	}

	// Write file
	if err := os.WriteFile(canonicalPath, content, 0644); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_write_file", fmt.Sprintf("Failed to write file: %v", err))
		return
	}

	// Publish file change event
	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "write", string(content)))

	// Stat the file to get actual filesystem mtime for the client
	modTime := int64(0)
	if info, err := os.Stat(canonicalPath); err == nil {
		modTime = info.ModTime().Unix()
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":  true,
		"message":  "File saved successfully",
		"path":     canonicalPath,
		"size":     len(content),
		"mod_time": modTime,
	})
}
