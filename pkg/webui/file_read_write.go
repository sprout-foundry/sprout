//go:build !js

package webui

import (
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

// handleAPIFile handles API requests for file operations (read/write)
func (ws *ReactWebServer) handleAPIFile(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		ws.handleFileRead(w, r)
	case http.MethodPost:
		ws.handleFileWrite(w, r)
	default:
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleFileRead handles file read operations
func (ws *ReactWebServer) handleFileRead(w http.ResponseWriter, r *http.Request) {
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	fileConsents := ws.getFileConsentManagerForRequest(r)
	// Get file path from query parameter
	path := r.URL.Query().Get("path")
	if path == "" {
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, false)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
		return
	}

	// Check if file exists and is not a directory
	info, err := os.Stat(canonicalPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("File not found: %v", err), http.StatusNotFound)
		return
	}

	if info.IsDir() {
		http.Error(w, "Path is a directory", http.StatusBadRequest)
		return
	}

	if info.Size() > maxFileReadSize {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusRequestEntityTooLarge)
		json.NewEncoder(w).Encode(map[string]interface{}{
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
		http.Error(w, fmt.Sprintf("Failed to read file: %v", err), http.StatusInternalServerError)
		return
	}

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
		http.Error(w, "File path is required", http.StatusBadRequest)
		return
	}

	canonicalPath, err := canonicalizePath(path, workspaceRoot, true)
	if err != nil {
		http.Error(w, fmt.Sprintf("Invalid file path: %v", err), http.StatusBadRequest)
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
		http.Error(w, fmt.Sprintf("Failed to read request body: %v", err), http.StatusBadRequest)
		return
	}

	// Parse JSON to extract content field
	var requestData struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &requestData); err != nil {
		http.Error(w, fmt.Sprintf("Failed to parse JSON: %v", err), http.StatusBadRequest)
		return
	}

	content := []byte(requestData.Content)

	// Create directory if it doesn't exist
	dir := filepath.Dir(canonicalPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		http.Error(w, fmt.Sprintf("Failed to create directory: %v", err), http.StatusInternalServerError)
		return
	}

	// Write file
	if err := os.WriteFile(canonicalPath, content, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Failed to write file: %v", err), http.StatusInternalServerError)
		return
	}

	// Publish file change event
	ws.publishClientEvent(ws.resolveClientID(r), events.EventTypeFileChanged, events.FileChangedEvent(canonicalPath, "write", string(content)))

	// Stat the file to get actual filesystem mtime for the client
	modTime := int64(0)
	if info, err := os.Stat(canonicalPath); err == nil {
		modTime = info.ModTime().Unix()
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success":  true,
		"message":  "File saved successfully",
		"path":     canonicalPath,
		"size":     len(content),
		"mod_time": modTime,
	})
}
