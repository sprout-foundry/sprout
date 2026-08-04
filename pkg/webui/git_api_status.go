//go:build !js

package webui

import (
	"fmt"
	"net/http"
)

// handleAPIGitStatus handles git status requests
func (ws *ReactWebServer) handleAPIGitStatus(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodGet) {
		return
	}

	status, err := ws.getGitStatusForWorkspace(ws.getWorkspaceRootForRequest(r))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to get git status: %v", err), http.StatusInternalServerError)
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":     "success",
		"status":      status,
		"files":       getAllGitFiles(status), // Backward compatibility
		"in_git_repo": status.InGitRepo,
	})
}
