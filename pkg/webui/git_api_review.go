//go:build !js

package webui

import (
	"fmt"
	"net/http"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/codereview"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// handleAPIGitDeepReview performs the same deep staged review flow as /review-deep,
// but without routing through /api/query so it doesn't pollute chat history.
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

	diffCmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached")
	stagedDiffBytes, err := diffCmd.Output()
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_get_staged_diff", fmt.Sprintf("Failed to get staged diff: %v", err))
		return
	}

	stagedDiff := string(stagedDiffBytes)
	stagedDiff = truncateDiffOutput(stagedDiff, 200000)
	if strings.TrimSpace(stagedDiff) == "" {
		writeJSONErr(w, http.StatusBadRequest, "no_diff_content_found", "No actual diff content found in staged changes")
		return
	}

	cfg, err := configuration.LoadOrInitConfig(true)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_load_config", fmt.Sprintf("Failed to load config: %v", err))
		return
	}

	logger := utils.GetLogger(true)
	optimizer := utils.NewDiffOptimizerForReview()
	optimizer.WorkingDir = workspaceRoot
	optimizedDiff := optimizer.OptimizeDiff(stagedDiff)

	service := codereview.NewCodeReviewService(cfg, logger)
	agentClient := service.GetDefaultAgentClient()

	activeProvider := strings.TrimSpace(agentInst.GetProvider())
	activeModel := strings.TrimSpace(agentInst.GetModel())
	if activeProvider != "" {
		if sessionClient, err := factory.CreateProviderClient(api.ClientType(activeProvider), activeModel); err == nil {
			agentClient = sessionClient
		}
	}

	if agentClient == nil {
		writeJSONErr(w, http.StatusInternalServerError, "failed_to_initialize_review_client", "Failed to initialize review client")
		return
	}

	reviewCtx := &codereview.ReviewContext{
		Diff:             optimizedDiff.OptimizedContent,
		Config:           cfg,
		Logger:           logger,
		AgentClient:      agentClient,
		ProjectType:      ws.gitReviewDetectProjectType(workspaceRoot),
		CommitMessage:    ws.gitReviewExtractStagedChangesSummary(workspaceRoot),
		KeyComments:      gitReviewExtractKeyCommentsFromDiff(stagedDiff),
		ChangeCategories: gitReviewCategorizeChanges(stagedDiff),
		FullFileContext:  ws.gitReviewExtractFileContextForChanges(workspaceRoot, stagedDiff),
	}

	if len(optimizedDiff.FileSummaries) > 0 {
		var summaryInfo strings.Builder
		summaryInfo.WriteString("\n\nLarge files optimized for review:\n")
		for file, summary := range optimizedDiff.FileSummaries {
			summaryInfo.WriteString(fmt.Sprintf("- %s: %s\n", file, summary))
		}
		reviewCtx.Diff += summaryInfo.String()
	}

	opts := &codereview.ReviewOptions{
		Type:             codereview.StagedReview,
		SkipPrompt:       true,
		RollbackOnReject: false,
	}

	reviewResponse, err := service.PerformAgenticReview(reviewCtx, opts)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, "deep_review_failed", fmt.Sprintf("Deep review failed: %v", err))
		return
	}

	reviewOutput := fmt.Sprintf("%s\n%s\n\nStatus: %s\n\nFeedback:\n%s",
		"[list] AI CODE REVIEW (DEEP PASS)",
		strings.Repeat("═", 50),
		strings.ToUpper(reviewResponse.Status),
		reviewResponse.Feedback)

	if strings.TrimSpace(reviewResponse.DetailedGuidance) != "" {
		reviewOutput += fmt.Sprintf("\n\nDetailed Guidance:\n%s", reviewResponse.DetailedGuidance)
	}
	if reviewResponse.Status == "rejected" && reviewResponse.NewPrompt != "" {
		reviewOutput += fmt.Sprintf("\n\nSuggested New Prompt:\n%s", reviewResponse.NewPrompt)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"message":              "Deep review completed",
		"status":               reviewResponse.Status,
		"feedback":             reviewResponse.Feedback,
		"detailed_guidance":    reviewResponse.DetailedGuidance,
		"suggested_new_prompt": reviewResponse.NewPrompt,
		"review_output":        reviewOutput,
		"provider":             agentInst.GetProvider(),
		"model":                agentInst.GetModel(),
		"warnings":             optimizedDiff.Warnings,
	})
}
