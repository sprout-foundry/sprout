//go:build !js

package webui

// git_api_review_heuristics.go — thin adapters over the shared heuristic
// diff analysis in pkg/codereview (diff_context.go). The heuristics live
// there so the CLI review command and these deep-review endpoints share one
// implementation; these wrappers keep the project-folder-rooted call sites
// unchanged.
import (
	"github.com/sprout-foundry/sprout/pkg/codereview"
)

func gitReviewExtractKeyCommentsFromDiff(diff string) string {
	return codereview.ExtractKeyCommentsFromDiff(diff)
}

func gitReviewIsImportantComment(comment string) bool {
	return codereview.IsImportantComment(comment)
}

func gitReviewCategorizeChanges(diff string) string {
	return codereview.CategorizeChanges(diff)
}
