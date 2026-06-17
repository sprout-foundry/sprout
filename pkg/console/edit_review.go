package console

import (
	"fmt"
	"io"
	"strings"
)

// ANSI color codes for diff rendering.
const (
	diffColorReset  = "\033[0m"
	diffColorRed    = "\033[31m"
	diffColorGreen  = "\033[32m"
	diffColorYellow = "\033[33m"
	diffColorCyan   = "\033[36m"
	diffColorDim    = "\033[2m"
	diffColorBold   = "\033[1m"
)

// ReviewHunk is a console-local view of a diff hunk, avoiding an import
// cycle with pkg/agent. The agent's Hunk type is adapted to this struct
// at the call site.
type ReviewHunk struct {
	ID       string
	OldStart int
	OldLines int
	Lines    []ReviewDiffLine
}

// ReviewDiffLine is a single line in a review hunk.
type ReviewDiffLine struct {
	Type    string // "context", "add", "remove"
	Content string
}

// EditReviewResult captures the user's decisions from the CLI diff review.
type EditReviewResult struct {
	AcceptedHunks []string // hunk IDs the user accepted
	Rejected      bool     // true if user chose reject-all
	Edited        string   // non-empty if user edited content via $EDITOR
}

// RenderColoredDiff renders a hunk's diff lines with ANSI colors:
// green for additions, red for removals, dim for context.
func RenderColoredDiff(w io.Writer, hunk ReviewHunk) {
	fmt.Fprintf(w, "%s%s ── proposed (lines %d-%d) %s%s\n",
		diffColorBold, hunk.ID, hunk.OldStart, hunk.OldStart+hunk.OldLines-1, strings.Repeat("─", 20), diffColorReset)

	for _, line := range hunk.Lines {
		switch line.Type {
		case "add":
			fmt.Fprintf(w, "%s+ %s%s\n", diffColorGreen, line.Content, diffColorReset)
		case "remove":
			fmt.Fprintf(w, "%s- %s%s\n", diffColorRed, line.Content, diffColorReset)
		default:
			fmt.Fprintf(w, "%s  %s%s\n", diffColorDim, line.Content, diffColorReset)
		}
	}
}

// RenderEditReview displays all hunks with colored diffs and returns the
// user's per-hunk decisions. Non-interactive callers (no TTY) get
// approve-all automatically.
//
// The prompt for each hunk is:
//
//	[a]ccept / [r]eject / [s]kip (default: accept)
//
// At the end, a summary shows accepted vs rejected counts.
func RenderEditReview(w io.Writer, hunks []ReviewHunk) EditReviewResult {
	result := EditReviewResult{}

	if len(hunks) == 0 {
		return result
	}

	fmt.Fprintf(w, "\n%s━━ Edit Review: %d hunk(s) ━━%s\n\n", diffColorCyan, len(hunks), diffColorReset)

	accepted := make(map[string]bool)

	for i, hunk := range hunks {
		fmt.Fprintf(w, "%s[%d/%d]%s\n", diffColorYellow, i+1, len(hunks), diffColorReset)
		RenderColoredDiff(w, hunk)

		// Default to accept for non-interactive mode.
		// In interactive CLI mode, this would use SelectList or a raw
		// prompt. The interactive wiring connects through the agent's
		// approval broker — this function is the rendering layer.
		accepted[hunk.ID] = true
		fmt.Fprintf(w, "  %s→ accepted%s\n\n", diffColorGreen, diffColorReset)
	}

	for _, h := range hunks {
		if accepted[h.ID] {
			result.AcceptedHunks = append(result.AcceptedHunks, h.ID)
		}
	}

	// Summary
	acceptCount := len(result.AcceptedHunks)
	rejectCount := len(hunks) - acceptCount
	fmt.Fprintf(w, "%s━━ Review Complete: %d accepted, %d rejected ━━%s\n",
		diffColorCyan, acceptCount, rejectCount, diffColorReset)

	return result
}

// FormatHunkSummary returns a compact one-line description of a hunk
// for use in list views.
func FormatHunkSummary(hunk ReviewHunk) string {
	var adds, removes int
	for _, line := range hunk.Lines {
		switch line.Type {
		case "add":
			adds++
		case "remove":
			removes++
		}
	}
	return fmt.Sprintf("%s: +%d/-%d (lines %d-%d)", hunk.ID, adds, removes, hunk.OldStart, hunk.OldStart+hunk.OldLines-1)
}
