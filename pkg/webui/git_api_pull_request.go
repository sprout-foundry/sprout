//go:build !js

package webui

import (
	"encoding/json"
	"net/http"

	gitops "github.com/sprout-foundry/sprout/pkg/git"
)

// handleAPIGitPullRequest creates a pull request via the gh CLI or GitHub API.
func (ws *ReactWebServer) handleAPIGitPullRequest(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
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
		writeJSONErr(w, http.StatusBadRequest, "invalid_json", "Invalid JSON")
		return
	}

	if req.Title == "" {
		writeJSONErr(w, http.StatusBadRequest, "pr_title_required", "PR title is required")
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
		writeJSON(w, http.StatusInternalServerError, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"url":     result.URL,
		"number":  result.Number,
		"state":   result.State,
	})
}
