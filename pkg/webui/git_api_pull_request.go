//go:build !js

package webui

import (
	"encoding/json"
	"net/http"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// handleAPIGitPullRequest creates a pull request via the gh CLI or GitHub API.
func (ws *ReactWebServer) handleAPIGitPullRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req struct {
		Title string `json:"title"`
		Body  string `json:"body"`
		Base  string `json:"base"`
		Head  string `json:"head"`
		Draft bool   `json:"draft"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Title == "" {
		http.Error(w, "PR title is required", http.StatusBadRequest)
		return
	}

	workspaceRoot := ws.getWorkspaceRootForRequest(r)

	result, err := gitops.CreatePullRequest(r.Context(), workspaceRoot, gitops.PullRequestRequest{
		Title: req.Title,
		Body:  req.Body,
		Base:  req.Base,
		Head:  req.Head,
		Draft: req.Draft,
	})

	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": true,
		"url":     result.URL,
		"number":  result.Number,
		"state":   result.State,
	})
}
