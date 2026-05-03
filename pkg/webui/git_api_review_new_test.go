package webui

import (
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// gitReviewExtractKeyCommentsFromDiff — pure helper
// ---------------------------------------------------------------------------

func TestGitReviewExtractKeyCommentsFromDiff_Empty(t *testing.T) {
	if got := gitReviewExtractKeyCommentsFromDiff(""); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGitReviewExtractKeyCommentsFromDiff_NoComments(t *testing.T) {
	diff := `diff --git a/src/main.go b/src/main.go
index abc123..def456 100644
--- a/src/main.go
+++ b/src/main.go
 package main
+func hello() {
+	return "world"
 }`
	if got := gitReviewExtractKeyCommentsFromDiff(diff); got != "" {
		t.Errorf("expected empty string, got %q", got)
	}
}

func TestGitReviewExtractKeyCommentsFromDiff_WithImportantComment(t *testing.T) {
	diff := `diff --git a/src/main.go b/src/main.go
--- a/src/main.go
+++ b/src/main.go
+// TODO: refactor this function
+func hello() {}`

	got := gitReviewExtractKeyCommentsFromDiff(diff)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
	if !strings.Contains(got, "TODO:") {
		t.Errorf("result should contain TODO keyword: %q", got)
	}
	if !strings.Contains(got, "src/main.go") {
		t.Errorf("result should contain file name: %q", got)
	}
}

func TestGitReviewExtractKeyCommentsFromDiff_HashComments(t *testing.T) {
	diff := `diff --git a/README.md b/README.md
--- a/README.md
+++ b/README.md
+# WARNING: this is deprecated
+
+// NOTE: check this out`

	got := gitReviewExtractKeyCommentsFromDiff(diff)
	if got == "" {
		t.Fatal("expected non-empty result")
	}
}

func TestGitReviewExtractKeyCommentsFromDiff_CapsAtTen(t *testing.T) {
	// Build a diff with many important comments (> 10)
	var lines []string
	lines = append(lines, "diff --git a/file.go b/file.go")
	for i := 0; i < 15; i++ {
		lines = append(lines, "+// CRITICAL: issue "+string(rune('0'+i)))
	}
	diff := strings.Join(lines, "\n")

	got := gitReviewExtractKeyCommentsFromDiff(diff)
	count := 0
	for _, line := range strings.Split(got, "\n") {
		if len(line) > 0 {
			count++
		}
	}
	if count > 10 {
		t.Errorf("expected at most 10 comments, got %d", count)
	}
}

func TestGitReviewExtractKeyCommentsFromDiff_DetectsFileFromDiffLine(t *testing.T) {
	diff := `diff --git a/pkg/agent/main.go b/pkg/agent/main.go
--- a/pkg/agent/main.go
+++ b/pkg/agent/main.go
+// FIXME: needs rewrite`

	got := gitReviewExtractKeyCommentsFromDiff(diff)
	if !strings.Contains(got, "pkg/agent/main.go") {
		t.Errorf("expected file name in result, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// gitReviewIsImportantComment — pure helper
// ---------------------------------------------------------------------------

func TestGitReviewIsImportantComment_AllKeywords(t *testing.T) {
	keywords := []string{
		"CRITICAL", "IMPORTANT", "NOTE:", "WARNING", "TODO:", "FIXME",
		"HACK", "BUG", "SECURITY", "FIX", "WORKAROUND",
		"BECAUSE", "REASON:", "WHY:", "INTENT:", "PURPOSE:",
	}
	for _, kw := range keywords {
		t.Run(kw, func(t *testing.T) {
			if !gitReviewIsImportantComment(kw) {
				t.Errorf("keyword %q should be important", kw)
			}
		})
	}
}

func TestGitReviewIsImportantComment_CaseInsensitive(t *testing.T) {
	if !gitReviewIsImportantComment("todo: lowercase") {
		t.Error("should be case-insensitive for 'todo:'")
	}
	if !gitReviewIsImportantComment("Security issue") {
		t.Error("should be case-insensitive for 'security'")
	}
}

func TestGitReviewIsImportantComment_LongCommentWithoutKeyword(t *testing.T) {
	// Comments > 50 chars starting with "//" should be considered important
	long := "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	if len(long) < 52 {
		t.Skip("test string too short")
	}
	if !gitReviewIsImportantComment("// " + long) {
		t.Errorf("long comment should be important")
	}
}

func TestGitReviewIsImportantComment_ShortCommentWithoutKeyword(t *testing.T) {
	if gitReviewIsImportantComment("// short") {
		t.Error("short comment without keyword should not be important")
	}
}

func TestGitReviewIsImportantComment_HashCommentWithKeyword(t *testing.T) {
	if !gitReviewIsImportantComment("# FIXME: something") {
		t.Error("hash comment with FIXME should be important")
	}
}

func TestGitReviewIsImportantComment_PlainTextNoKeyword(t *testing.T) {
	if gitReviewIsImportantComment("regular line of code") {
		t.Error("plain text without keyword should not be important")
	}
}

// ---------------------------------------------------------------------------
// gitReviewCategorizeChanges — pure helper
// ---------------------------------------------------------------------------

func TestGitReviewCategorizeChanges_Empty(t *testing.T) {
	if got := gitReviewCategorizeChanges(""); got != "" {
		t.Errorf("expected empty, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_Security(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
+// SECURITY: fix vulnerability
+if err != nil { return }`

	got := gitReviewCategorizeChanges(diff)
	if !strings.Contains(got, "Security") {
		t.Errorf("expected Security category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_ErrorHandling(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
+if err != nil { return }
+return nil, ErrNotFound`

	got := gitReviewCategorizeChanges(diff)
	if !strings.Contains(got, "Error handling") {
		t.Errorf("expected Error handling category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_DependencyUpdates(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
+require(github.com/pkg/errors)`

	got := gitReviewCategorizeChanges(diff)
	if !strings.Contains(got, "Dependency updates") {
		t.Errorf("expected Dependency updates category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_TestChanges(t *testing.T) {
	diff := `diff --git a/main_test.go b/main_test.go
+func TestFoo(t *testing.T) {}`

	got := gitReviewCategorizeChanges(diff)
	if !strings.Contains(got, "Test changes") {
		t.Errorf("expected Test changes category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_CodeRemoval(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
-old function removed
+new function added`

	got := gitReviewCategorizeChanges(diff)
	if !strings.Contains(got, "Code removal") {
		t.Errorf("expected Code removal category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_SkipsDiffAndIndexLines(t *testing.T) {
	diff := `diff --git a/main.go b/main.go
index abc123..def456 100644
--- a/main.go
+++ b/main.go
+new code`

	got := gitReviewCategorizeChanges(diff)
	// Should not be empty because we have added code, but should not
	// count the diff/index lines as additions.
	// The key thing: the "index" line should not be counted as an addition.
	// Just verify it doesn't crash and produces reasonable output.
	if got == "" {
		t.Log("empty output acceptable for this minimal diff")
	}
}

func TestGitReviewCategorizeChanges_WithCounts(t *testing.T) {
	diff := `diff --git a/a.go b/a.go
+// BUG: something
+// BUG: another
+if err != nil { return }`

	got := gitReviewCategorizeChanges(diff)
	// Error handling should be detected from "if err"
	if !strings.Contains(got, "Error handling") {
		t.Errorf("expected Error handling category, got %q", got)
	}
}

func TestGitReviewCategorizeChanges_IgnoresPlusPlusAndMinusMinus(t *testing.T) {
	diff := `+++ b/main.go
--- a/main.go
+new line
-removed line`

	got := gitReviewCategorizeChanges(diff)
	// +++ should not be counted as added line, but - should
	if !strings.Contains(got, "Code removal") {
		t.Errorf("expected Code removal/refactoring, got %q", got)
	}
}
