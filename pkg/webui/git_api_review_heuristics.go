//go:build !js

package webui

// git_api_review_heuristics.go — thin adapters over the shared heuristic
// diff analysis in pkg/codereview (diff_context.go). The heuristics live
// there so the CLI review command and these deep-review endpoints share one
// implementation; these wrappers keep the project-folder-rooted call sites
// unchanged.
import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/codereview"
)

func (ws *ReactWebServer) gitReviewDetectProjectType(projectFolder string) string {
	return codereview.DetectProjectType(projectFolder)
}

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	return codereview.ExtractKeyCommentsFromDiff(diff)
}

func (ws *ReactWebServer) gitReviewExtractStagedChangesSummary(projectFolder string) string {
	cmd := ws.gitCommandForProjectFolder(projectFolder, "diff", "--cached", "--stat")
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

func (ws *ReactWebServer) gitReviewExtractFileContextForChanges(projectFolder, diff string) string {
	return codereview.ExtractFileContextForChanges(projectFolder, diff)
}
