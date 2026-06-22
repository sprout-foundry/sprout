package history

import (
	"strings"
	"testing"

	"github.com/sergi/go-diff/diffmatchpatch"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// stripAllColor
// ---------------------------------------------------------------------------

func TestStripAllColor_RemovesSingleColorPair(t *testing.T) {
	input := RedColor + "deleted" + ResetColor
	got := stripAllColor(input)
	assert.Equal(t, "deleted", got)
}

func TestStripAllColor_RemovesBoldAndMultipleColors(t *testing.T) {
	input := BoldStyle + "file.go" + ResetColor + " " + GreenColor + "+++4" + ResetColor + " " + RedColor + "---4" + ResetColor
	got := stripAllColor(input)
	assert.Equal(t, "file.go +++4 ---4", got)
}

func TestStripAllColor_NoColorCodesReturnsInput(t *testing.T) {
	input := "plain text with no escapes"
	got := stripAllColor(input)
	assert.Equal(t, input, got)
}

func TestStripAllColor_EmptyString(t *testing.T) {
	assert.Equal(t, "", stripAllColor(""))
}

func TestStripAllColor_RemovesNestedAndComplexSequences(t *testing.T) {
	// Bold + color + numeric params like [1;31m
	input := "\x1b[1;31mbold red\x1b[0m normal \x1b[32mgreen\x1b[0m"
	got := stripAllColor(input)
	assert.Equal(t, "bold red normal green", got)
}

// ---------------------------------------------------------------------------
// containsDeletionColor / containsAdditionColor / containsColorChange
// ---------------------------------------------------------------------------

func TestContainsDeletionColor_RedPresent(t *testing.T) {
	assert.True(t, containsDeletionColor(RedColor+"error"+ResetColor))
}

func TestContainsDeletionColor_GreenOnly(t *testing.T) {
	assert.False(t, containsDeletionColor(GreenColor+"ok"+ResetColor))
}

func TestContainsDeletionColor_NoColor(t *testing.T) {
	assert.False(t, containsDeletionColor("plain text"))
}

func TestContainsAdditionColor_GreenPresent(t *testing.T) {
	assert.True(t, containsAdditionColor(GreenColor+"new code"+ResetColor))
}

func TestContainsAdditionColor_RedOnly(t *testing.T) {
	assert.False(t, containsAdditionColor(RedColor+"old code"+ResetColor))
}

func TestContainsColorChange_EitherColorPresent(t *testing.T) {
	assert.True(t, containsColorChange(RedColor+"x"+ResetColor))
	assert.True(t, containsColorChange(GreenColor+"x"+ResetColor))
}

func TestContainsColorChange_NoColor(t *testing.T) {
	assert.False(t, containsColorChange("  context line"))
}

// ---------------------------------------------------------------------------
// removeColoredPart
// ---------------------------------------------------------------------------

func TestRemoveColoredPart_RemovesGreenSegment(t *testing.T) {
	line := "before " + GreenColor + "added" + ResetColor + " after"
	// Removing the green (addition) part leaves the "before" state.
	got := removeColoredPart(line, GreenColor, ResetColor)
	assert.Equal(t, "before  after", got)
}

func TestRemoveColoredPart_RemovesRedSegment(t *testing.T) {
	line := "before " + RedColor + "deleted" + ResetColor + " after"
	got := removeColoredPart(line, RedColor, ResetColor)
	assert.Equal(t, "before  after", got)
}

func TestRemoveColoredPart_NoMatchReturnsUnchanged(t *testing.T) {
	line := "plain text with no color"
	got := removeColoredPart(line, GreenColor, ResetColor)
	assert.Equal(t, line, got)
}

func TestRemoveColoredPart_MultipleSegments(t *testing.T) {
	line := GreenColor + "a" + ResetColor + " mid " + GreenColor + "b" + ResetColor
	got := removeColoredPart(line, GreenColor, ResetColor)
	assert.Equal(t, " mid ", got)
}

// ---------------------------------------------------------------------------
// calculateChanges
// ---------------------------------------------------------------------------

func TestCalculateChanges_OnlyInserts(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffInsert, Text: "abc"},
		{Type: diffmatchpatch.DiffInsert, Text: "de"},
	}
	add, del := calculateChanges(diffs)
	assert.Equal(t, 5, add)
	assert.Equal(t, 0, del)
}

func TestCalculateChanges_OnlyDeletes(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffDelete, Text: "xyz"},
	}
	add, del := calculateChanges(diffs)
	assert.Equal(t, 0, add)
	assert.Equal(t, 3, del)
}

func TestCalculateChanges_MixedDiffs(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffEqual, Text: "shared"},   // not counted
		{Type: diffmatchpatch.DiffInsert, Text: "added"},   // 5 chars
		{Type: diffmatchpatch.DiffDelete, Text: "removed"}, // 7 chars
	}
	add, del := calculateChanges(diffs)
	assert.Equal(t, 5, add)
	assert.Equal(t, 7, del)
}

func TestCalculateChanges_Empty(t *testing.T) {
	add, del := calculateChanges(nil)
	assert.Equal(t, 0, add)
	assert.Equal(t, 0, del)
}

// ---------------------------------------------------------------------------
// getStatsFromDiff
// ---------------------------------------------------------------------------

func TestGetStatsFromDiff_AdditionsAndDeletions(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffInsert, Text: "add"},
		{Type: diffmatchpatch.DiffDelete, Text: "del"},
	}
	got := getStatsFromDiff(diffs, "main.go")
	assert.Contains(t, got, "main.go")
	assert.Contains(t, got, "+++3")
	assert.Contains(t, got, "---3")
	assert.True(t, strings.HasSuffix(got, "\n"), "should end with newline")
}

func TestGetStatsFromDiff_AdditionsOnly(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffInsert, Text: "new"},
	}
	got := getStatsFromDiff(diffs, "file.go")
	assert.Contains(t, got, "+++3")
	assert.NotContains(t, got, "---", "should not show deletion stat when none")
}

func TestGetStatsFromDiff_DeletionsOnly(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffDelete, Text: "old"},
	}
	got := getStatsFromDiff(diffs, "file.go")
	assert.Contains(t, got, "---3")
	assert.NotContains(t, got, "+++", "should not show addition stat when none")
}

func TestGetStatsFromDiff_NoChanges(t *testing.T) {
	diffs := []diffmatchpatch.Diff{
		{Type: diffmatchpatch.DiffEqual, Text: "same"},
	}
	got := getStatsFromDiff(diffs, "unchanged.go")
	assert.Contains(t, got, "unchanged.go")
	assert.NotContains(t, got, "+++")
	assert.NotContains(t, got, "---")
}

// ---------------------------------------------------------------------------
// normalizeDiffText
// ---------------------------------------------------------------------------

func TestNormalizeDiffText_NoColorUnchanged(t *testing.T) {
	input := "line1\nline2\nline3"
	got := normalizeDiffText(input)
	assert.Equal(t, input, got)
}

func TestNormalizeDiffText_SingleLineColorAlreadyNormalized(t *testing.T) {
	// A single line with its own color start and reset is already normalized.
	input := RedColor + "deleted" + ResetColor
	got := normalizeDiffText(input)
	assert.Equal(t, input, got)
}

func TestNormalizeDiffText_MultilineColorBlock(t *testing.T) {
	// Color block spanning two lines: color starts on line1 and resets on line2.
	input := RedColor + "line1\nline2" + ResetColor
	got := normalizeDiffText(input)

	lines := strings.Split(got, "\n")
	require.Len(t, lines, 2)

	// Each line should have its own color start and reset.
	assert.Contains(t, lines[0], RedColor)
	assert.Contains(t, lines[0], ResetColor)
	assert.Contains(t, lines[1], RedColor)
	assert.Contains(t, lines[1], ResetColor)

	// The visible text on each line should be correct.
	assert.Equal(t, "line1", stripAllColor(lines[0]))
	assert.Equal(t, "line2", stripAllColor(lines[1]))
}

func TestNormalizeDiffText_GreenMultilineBlock(t *testing.T) {
	input := GreenColor + "added1\nadded2\nadded3" + ResetColor
	got := normalizeDiffText(input)

	lines := strings.Split(got, "\n")
	require.Len(t, lines, 3)

	for i, line := range lines {
		assert.Contains(t, line, GreenColor, "line %d missing green color", i)
		assert.Contains(t, line, ResetColor, "line %d missing reset", i)
	}

	assert.Equal(t, "added1", stripAllColor(lines[0]))
	assert.Equal(t, "added2", stripAllColor(lines[1]))
	assert.Equal(t, "added3", stripAllColor(lines[2]))
}

func TestNormalizeDiffText_MixedColorsOnDifferentLines(t *testing.T) {
	// Red on line1, green on line2 — each self-contained.
	input := RedColor + "del" + ResetColor + "\n" + GreenColor + "add" + ResetColor
	got := normalizeDiffText(input)
	assert.Equal(t, input, got)
}

func TestNormalizeDiffText_EmptyString(t *testing.T) {
	got := normalizeDiffText("")
	assert.Equal(t, "", got)
}

// ---------------------------------------------------------------------------
// checkPython
// ---------------------------------------------------------------------------

func TestCheckPython_DoesNotPanicAndSetsAvailability(t *testing.T) {
	// checkPython uses sync.Once, so it may have already been called.
	// We can't reset it, but we can verify it doesn't panic and that
	// the pythonAvailable global is set to a definitive value.
	assert.NotPanics(t, func() {
		checkPython()
	})
	// After checkPython runs, pythonAvailable should be deterministically
	// true or false (not in an undefined state).
	_ = pythonAvailable // just ensure it's readable without panic
}

// ---------------------------------------------------------------------------
// GetDiff — end-to-end integration
// ---------------------------------------------------------------------------

func TestGetDiff_ShowsAdditionsAndDeletions(t *testing.T) {
	diff := GetDiff("main.go", "hello world", "hello earth")
	require.NotEmpty(t, diff)

	// Stats header should include the filename.
	assert.Contains(t, diff, "main.go")

	stripped := stripAllColor(diff)
	// The deleted original and inserted new text should appear.
	assert.Contains(t, stripped, "hello world")
	assert.Contains(t, stripped, "hello earth")
}

func TestGetDiff_NoChangesReturnsMinimalOutput(t *testing.T) {
	diff := GetDiff("noop.go", "same content", "same content")
	// With no changes, there are no +/- lines, just the stats header.
	stripped := stripAllColor(diff)
	assert.Contains(t, stripped, "noop.go")
	assert.NotContains(t, stripped, "+same content")
	assert.NotContains(t, stripped, "-same content")
}

func TestGetDiff_MultilineChanges(t *testing.T) {
	original := "line1\nline2\nline3"
	updated := "line1\nline3\nline4"
	diff := GetDiff("multi.go", original, updated)
	require.NotEmpty(t, diff)

	stripped := stripAllColor(diff)
	// The changed lines should appear in the diff output.
	assert.Contains(t, stripped, "line4")
}
