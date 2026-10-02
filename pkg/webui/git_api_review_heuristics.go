//go:build !js

package webui

// git_api_review_heuristics.go — thin adapters over the shared heuristic
// diff analysis in pkg/codereview (diff_context.go). The heuristics live
// there so the CLI review command and these deep-review endpoints share one
// implementation; these wrappers keep the workspace-rooted call sites
// unchanged.
import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/codereview"
)

func (ws *ReactWebServer) gitReviewDetectProjectType(workspaceRoot string) string {
	return codereview.DetectProjectType(workspaceRoot)
}

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	return codereview.ExtractKeyCommentsFromDiff(diff)
}

func (ws *ReactWebServer) gitReviewExtractStagedChangesSummary(workspaceRoot string) string {
	cmd := ws.gitCommandForWorkspace(workspaceRoot, "diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && strings.TrimSpace(statLines[0]) != "" {
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}
	return ""
}

func gitReviewIsImportantComment(comment string) bool {
	return codereview.IsImportantComment(comment)
}

func gitReviewCategorizeChanges(diff string) string {
	return codereview.CategorizeChanges(diff)
}

func (ws *ReactWebServer) gitReviewExtractFileContextForChanges(workspaceRoot, diff string) string {
	return codereview.ExtractFileContextForChanges(workspaceRoot, diff)
}
