//go:build !js

package webui

import (
	"fmt"
	"net/http"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// handleAPIGitDeepReview reviews the staged change with reviewer subagents
// (the same review_changes pipeline as /review-deep) and returns structured
// MUST_FIX/VERIFY guidance for the review tab's fix picker. It bypasses
// /api/query so the review doesn't enter chat history.
func (ws *ReactWebServer) handleAPIGitDeepReview(w http.ResponseWriter, r *http.Request) {
	if !requireMethod(w, r, http.MethodPost) {
		return
	}

	clientID := ws.resolveClientID(r)
	agentInst, err := ws.getClientAgent(clientID)
	if err != nil || agentInst == nil {
		writeJSONErr(w, http.StatusServiceUnavailable, "agent_not_available", "Agent is not available")
		return
	}

	// Exit code 1 means staged changes exist.
	workspaceRoot := ws.getWorkspaceRootForRequest(r)
	checkCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--quiet", "--exit-code")
	if err := checkCmd.Run(); err == nil {
		writeJSONErr(w, http.StatusBadRequest, "no_staged_changes", "No staged changes found")
		return
	}

	review, err := agentInst.ReviewChanges(r.Context(), agent.ReviewChangesOptions{Scope: "staged", Dir: workspaceRoot})
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "deep_review_failed", fmt.Sprintf("Review failed: %v", err))
		return
	}

	status := map[string]string{"APPROVE": "approved", "CHANGES_REQUIRED": "needs_revision"}[review.Verdict]
	if status == "" {
		status = "inconclusive"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":           "Review completed",
		"status":            status,
		"feedback":          review.Summary(),
		"detailed_guidance": review.GuidanceJSON(),
		"review_output":     review.Markdown(),
		"provider":          agentInst.GetProvider(),
		"model":             agentInst.GetModel(),
	})
}
