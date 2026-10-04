package codereview

import (
	"context"
	"fmt"
	"strings"
)

const maxRangeCommitSubjects = 20

// RangeDiff returns the changes HEAD introduces since its merge-base with base
// (`git diff base...HEAD`) — what a pull request against base would show.
func RangeDiff(ctx context.Context, dir, base string) (string, error) {
	if base == "" || strings.HasPrefix(base, "-") {
		return "", fmt.Errorf("invalid base ref %q", base)
	}
	root := gitTopLevel(ctx, dir)
	if _, err := runGit(ctx, root, "rev-parse", "--verify", "--quiet", base+"^{commit}"); err != nil {
		return "", fmt.Errorf("unknown base ref %q", base)
	}
	return runGit(ctx, root, "diff", base+"...HEAD")
}

// BuildRangeContext is BuildStagedContext for committed work: file context is
// read from HEAD, and the commit subjects since base stand in for the message.
func BuildRangeContext(ctx context.Context, dir, base, diff string) StagedContext {
	root := gitTopLevel(ctx, dir)
	return StagedContext{
		ProjectType:      DetectProjectType(root),
		CommitMessage:    rangeCommitSummary(ctx, root, base),
		KeyComments:      ExtractKeyComments(diff),
		ChangeCategories: CategorizeChanges(diff),
		FullFileContext:  revHunkContext(ctx, root, "HEAD", diff),
	}
}

func rangeCommitSummary(ctx context.Context, root, base string) string {
	out, err := runGit(ctx, root, "log", "--format=%s", base+"..HEAD")
	if err != nil {
		return ""
	}
	subjects := strings.Split(strings.TrimSpace(out), "\n")
	if len(subjects) == 0 || subjects[0] == "" {
		return ""
	}
	if len(subjects) > maxRangeCommitSubjects {
		subjects = subjects[:maxRangeCommitSubjects]
	}
	return "Commits since " + base + ":\n- " + strings.Join(subjects, "\n- ")
}
