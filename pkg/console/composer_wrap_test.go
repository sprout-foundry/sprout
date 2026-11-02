package console

import (
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWrapHardLine_BreaksBetweenWords(t *testing.T) {
	require.Equal(t, []string{"hello ", "world ", "foo"}, WrapHardLine("hello world foo", 8))
}

func TestWrapHardLine_LongWordBreaksInPlace(t *testing.T) {
	require.Equal(t, []string{"> /abcdef", "ghij"}, WrapHardLine("> /abcdefghij", 9),
		"a word wider than a row is broken where it is, not moved off the prompt")
}

func TestWrapHardLine_RowsReassembleTheInput(t *testing.T) {
	in := "the quick brown fox  jumps   over a lazy dog, then keeps going across the field"
	for cols := 3; cols <= 30; cols++ {
		require.Equal(t, in, strings.Join(WrapHardLine(in, cols), ""), "cols=%d", cols)
	}
}

var composerChrome = regexp.MustCompile(`^(↪ |steer › )`)

// typedTextOnScreen reads the input box back off the emulated screen and
// strips its chrome (label, continuation markers, caret glyph), leaving what
// the user typed as they would read it.
func typedTextOnScreen(scr *vtScreen, firstRow, lastRow int) string {
	var b strings.Builder
	for r := firstRow; r <= lastRow; r++ {
		row := strings.TrimRight(string(scr.grid[r-1]), " ")
		row = composerChrome.ReplaceAllString(row, "")
		// Rows end where a word wrapped; the break's space is the trailing
		// blank the trim removed.
		b.WriteString(strings.ReplaceAll(row, caretGlyph, "") + " ")
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// The regression: wrapped rows lost their last characters, replaced by "…",
// because the box wrapped at the full width and then added the continuation
// marker (and, without color, the caret glyph) to rows that were already
// full. Whatever the width and wherever the caret, every typed character must
// be on screen, in order, and no row may run past the edge.
func TestComposer_WrappedRowsKeepEveryCharacter(t *testing.T) {
	text := "the quick brown fox jumps over the lazy dog and then keeps running across the field until sunset"
	for _, noColor := range []string{"1", ""} {
		t.Setenv("NO_COLOR", noColor)
		for cols := 24; cols <= 60; cols += 3 {
			for _, cursor := range []int{0, 17, 40, len(text)} {
				scr := newVTScreen(cols, 24)
				f := &StatusFooter{w: scr, isTTY: true, active: true, steerCursor: -1,
					sizeOverride: &terminalSizeOverride{cols: cols, rows: 24}}
				f.applyScrollRegionLocked()
				f.SetComposerMode(ComposerSteer)
				full := SteerPromptPrefix + text
				row, col := composerCaret(full, len(SteerPromptPrefix)+cursor, composerWrapCols(cols))
				f.SetSteerLineWrapped(full, row, col)

				block := f.steerBlockRows() // rule + text rows
				first, last := 24-2-block+2, 24-2
				got := typedTextOnScreen(scr, first, last)
				require.Equal(t, text, got, "NO_COLOR=%q cols=%d cursor=%d\n%s", noColor, cols, cursor, scr.dump())
				require.False(t, scr.wrappedPastEdge, "a row ran past the edge: cols=%d cursor=%d\n%s", cols, cursor, scr.dump())
			}
		}
	}
}
