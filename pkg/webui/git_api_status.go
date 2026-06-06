//go:build !js

package webui

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// handleAPIGitStatus handles git status requests
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	status, err := ws.getGitStatusForWorkspace(ws.getWorkspaceRootForRequest(r))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get git status: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"message":     "success",
		"status":      status,
		"files":       getAllGitFiles(status), // Backward compatibility
		"in_git_repo": status.InGitRepo,
	})
}
