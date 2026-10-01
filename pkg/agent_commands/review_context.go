package commands

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/codereview"
)

// review_context.go — thin CLI adapters over the shared heuristic diff
// analysis in pkg/codereview (diff_context.go). The heuristics live there so
// the WebUI deep-review endpoints share one implementation; these wrappers
// keep the CLI's cwd-based call sites unchanged.

func detectProjectType() string {
	return codereview.DetectProjectType(".")
}

func extractStagedChangesSummary() string {
	cmd := gitCommand("diff", "--cached", "--stat")
	output, err := cmd.Output()
	if err != nil {
		return ""
	}

	statLines := strings.Split(string(output), "\n")
	if len(statLines) > 0 && statLines[0] != "" {
		return fmt.Sprintf("Staged changes summary: %s", strings.TrimSpace(statLines[0]))
	}

	return ""
}

func extractKeyCommentsFromDiff(diff string) string {
	return codereview.ExtractKeyCommentsFromDiff(diff)
}

func isImportantComment(comment string) bool {
	return codereview.IsImportantComment(comment)
}

func categorizeChanges(diff string) string {
	return codereview.CategorizeChanges(diff)
}

func extractFileContextForChanges(diff string) string {
	return codereview.ExtractFileContextForChanges(".", diff)
}

func shouldSkipFileForContext(filePath string) bool {
	return codereview.ShouldSkipFileForContext(filePath)
}

func isValidRepoFilePath(filePath string) bool {
	return codereview.IsValidRepoFilePath(".", filePath)
}
