package codereview

import "strings"

// importantCommentKeywords mark review comments worth surfacing to the
// user. Shared by the staged-review CLIs and the WebUI review endpoint —
// previously copy-pasted in three packages.
var importantCommentKeywords = []string{
	"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
	"HACK", "XXX", "BUG", "SECURITY", "FIX", "WORKAROUND",
	"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
}

// IsImportantComment reports whether a review comment contains a keyword
// that marks it as important enough to highlight. Long // comments are
// treated as important regardless of content.
func IsImportantComment(comment string) bool {
	commentUpper := strings.ToUpper(comment)
	for _, keyword := range importantCommentKeywords {
		if strings.Contains(commentUpper, keyword) {
			return true
		}
	}
	return strings.HasPrefix(comment, "//") && len(comment) > 50
}
