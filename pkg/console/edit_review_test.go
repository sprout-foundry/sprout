package console

import (
	"strings"
	"testing"
)

func TestRenderColoredDiff(t *testing.T) {
	hunk := ReviewHunk{
		ID:       "hunk-0",
		OldStart: 1,
		OldLines: 5,
		Lines: []ReviewDiffLine{
			{Type: "context", Content: "line1"},
			{Type: "remove", Content: "old"},
			{Type: "add", Content: "new"},
			{Type: "context", Content: "line4"},
		},
	}

	var sb strings.Builder
	RenderColoredDiff(&sb, hunk)
	output := sb.String()

	if !strings.Contains(output, "hunk-0") {
		t.Error("expected hunk ID in output")
	}
	if !strings.Contains(output, "- old") {
		t.Error("expected removed line in output")
	}
	if !strings.Contains(output, "+ new") {
		t.Error("expected added line in output")
	}
	// Should contain ANSI color codes
	if !strings.Contains(output, "\033[31m") {
		t.Error("expected red color code for removals")
	}
	if !strings.Contains(output, "\033[32m") {
		t.Error("expected green color code for additions")
	}
}

func TestRenderEditReview_EmptyHunks(t *testing.T) {
	var sb strings.Builder
	result := RenderEditReview(&sb, nil)

	if len(result.AcceptedHunks) != 0 {
		t.Error("expected no accepted hunks for empty input")
	}
}

func TestRenderEditReview_AcceptsAll(t *testing.T) {
	hunks := []ReviewHunk{
		{ID: "hunk-0", OldStart: 1, OldLines: 3, Lines: []ReviewDiffLine{
			{Type: "add", Content: "new"},
		}},
		{ID: "hunk-1", OldStart: 10, OldLines: 3, Lines: []ReviewDiffLine{
			{Type: "remove", Content: "old"},
		}},
	}

	var sb strings.Builder
	result := RenderEditReview(&sb, hunks)

	if len(result.AcceptedHunks) != 2 {
		t.Errorf("expected 2 accepted hunks, got %d", len(result.AcceptedHunks))
	}
	if !strings.Contains(result.AcceptedHunks[0], "hunk-0") {
		t.Error("expected hunk-0 in accepted")
	}
}

func TestFormatHunkSummary(t *testing.T) {
	hunk := ReviewHunk{
		ID:       "hunk-0",
		OldStart: 5,
		OldLines: 4,
		Lines: []ReviewDiffLine{
			{Type: "add", Content: "a1"},
			{Type: "add", Content: "a2"},
			{Type: "remove", Content: "r1"},
			{Type: "context", Content: "c1"},
		},
	}

	summary := FormatHunkSummary(hunk)

	if !strings.Contains(summary, "hunk-0") {
		t.Error("expected hunk ID in summary")
	}
	if !strings.Contains(summary, "+2") {
		t.Error("expected +2 adds in summary")
	}
	if !strings.Contains(summary, "-1") {
		t.Error("expected -1 remove in summary")
	}
}

func TestRenderColoredDiff_FilePath(t *testing.T) {
	// When FilePath is set, the header includes the path in dim color and
	// the hunk ID still appears so per-hunk grep remains possible.
	hunk := ReviewHunk{
		ID:       "hunk-0",
		FilePath: "pkg/console/edit_review.go",
		OldStart: 1,
		OldLines: 3,
		Lines: []ReviewDiffLine{
			{Type: "add", Content: "new"},
		},
	}

	var sb strings.Builder
	RenderColoredDiff(&sb, hunk)
	output := sb.String()

	if !strings.Contains(output, "pkg/console/edit_review.go") {
		t.Error("expected file path in header")
	}
	if !strings.Contains(output, "hunk-0") {
		t.Error("expected hunk ID in header alongside path")
	}
	// Header should be dim (path color) then bold (hunk ID)
	if !strings.Contains(output, "\033[2mpkg/console/edit_review.go\033[0m") {
		t.Error("expected dim path prefix before reset")
	}
}

func TestRenderColoredDiff_NoFilePath(t *testing.T) {
	// Empty FilePath → falls back to legacy header format (no dim prefix).
	hunk := ReviewHunk{
		ID:       "hunk-0",
		OldStart: 1,
		OldLines: 3,
		Lines: []ReviewDiffLine{
			{Type: "add", Content: "new"},
		},
	}

	var sb strings.Builder
	RenderColoredDiff(&sb, hunk)
	output := sb.String()

	if strings.Contains(output, "\033[2m") && strings.Contains(output, "hunk-0") {
		// If dim appears, it must be from context lines, not the header.
		headerLine := strings.SplitN(output, "\n", 2)[0]
		if strings.HasPrefix(headerLine, "\033[2m") {
			t.Error("legacy header should not start with dim color code")
		}
	}
}

func TestFormatHunkSummary_FilePath(t *testing.T) {
	hunk := ReviewHunk{
		ID:       "hunk-0",
		FilePath: "foo/bar.go",
		OldStart: 5,
		OldLines: 4,
		Lines: []ReviewDiffLine{
			{Type: "add", Content: "a1"},
		},
	}

	summary := FormatHunkSummary(hunk)
	if !strings.HasPrefix(summary, "foo/bar.go · ") {
		t.Errorf("expected path-first summary, got %q", summary)
	}
}
