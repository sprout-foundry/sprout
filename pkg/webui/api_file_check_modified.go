package webui

import (
	"encoding/json"
	"net/http"
	"os"
)

// checkModifiedRequest is the JSON request body for the check-modified endpoint.
// Each entry maps a file path to its last-known modification time (Unix seconds).
type checkModifiedRequest struct {
	Files map[string]int64 `json:"files"` // path -> mtime (unix seconds)
}

// checkModifiedFile describes a single file whose content changed on disk.
type checkModifiedFile struct {
	Path    string `json:"path"`
	ModTime int64  `json:"mod_time"` // current mtime (unix seconds)
	Size    int64  `json:"size"`
}

// checkModifiedResponse is the JSON response body.
type checkModifiedResponse struct {
	Modified []checkModifiedFile `json:"modified"` // files that changed on disk
}

func (ws *ReactWebServer) handleAPIFileCheckModified(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	r.Body = http.MaxBytesReader(w, r.Body, 256*1024)
	var req checkModifiedRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	var modified []checkModifiedFile
	for path, knownMtime := range req.Files {
		// Use forWrite=true so canonicalizePath can handle deleted/non-existent
		// files by resolving symlinks on the nearest existing parent.
		canonicalPath, err := canonicalizePath(path, workspaceRoot, true)
		if err != nil {
			continue // skip unresolvable paths
		}

		// Only check files within the workspace (or app config paths).
		if !isWithinWorkspace(canonicalPath, workspaceRoot) && !isAppConfigPath(canonicalPath) {
			continue
		}

		info, err := os.Stat(canonicalPath)
		if err != nil {
			// File was deleted or is otherwise inaccessible — report it.
			// Return the original request path to avoid leaking the resolved
			// absolute filesystem path.
			modified = append(modified, checkModifiedFile{
				Path:    path,
				ModTime: 0,
				Size:    0,
			})
			continue
		}

		if info.IsDir() {
			continue
		}

		// Register (or refresh) this path with the file watcher so we can
		// push real-time change notifications instead of relying solely on
		// the 3-second polling interval. Only register existing files to
		// avoid wasting fsnotify watch slots on deleted/inaccessible paths.
		if ws.fileWatcher != nil {
			ws.fileWatcher.watch(canonicalPath, path)
		}

		currentMtime := info.ModTime().Unix()
		if currentMtime != knownMtime {
			modified = append(modified, checkModifiedFile{
				Path:    path,
				ModTime: currentMtime,
				Size:    info.Size(),
			})
		}
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(checkModifiedResponse{Modified: modified})
}
