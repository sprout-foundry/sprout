package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEscapeParser_PageKeys(t *testing.T) {
	for seq, want := range map[string]InputEventType{"\x1b[5~": EventPageUp, "\x1b[6~": EventPageDown} {
		ep := NewEscapeParser()
		var got *InputEvent
		for i := 0; i < len(seq); i++ {
			if ev := ep.Parse(seq[i]); ev != nil {
				got = ev
			}
		}
		require.NotNil(t, got, "%q", seq)
		require.Equal(t, want, got.Type, "%q", seq)
	}
}

// Moving down and back up through a word-wrapped layout returns to where it
// started, keeping the column — and never sticks on a soft-wrap boundary,
// which belongs to the earlier row.
func TestCaretRowMove_RoundTripsThroughWrappedRows(t *testing.T) {
	text := "steer › alpha beta gamma delta epsilon zeta eta theta iota kappa"
	pos := composerCaretPositions(text, 16)
	start := strings.Index(text, "beta") + 2
	b := start
	var ok bool
	for i := 0; i < 3; i++ {
		b, ok = caretRowMove(pos, b, 1, false)
		require.True(t, ok, "down %d", i)
	}
	for i := 0; i < 3; i++ {
		b, ok = caretRowMove(pos, b, -1, false)
		require.True(t, ok, "up %d", i)
	}
	require.Equal(t, start, b)
}

func TestCaretRowMove_EdgesAndClamp(t *testing.T) {
	text := "one\ntwo\nthree"
	pos := composerCaretPositions(text, 40)
	_, ok := caretRowMove(pos, 1, -1, false)
	require.False(t, ok, "Up on the first row falls through (to history)")
	_, ok = caretRowMove(pos, len(text), 1, false)
	require.False(t, ok, "Down on the last row falls through")

	b, ok := caretRowMove(pos, len(text), -10, true)
	require.True(t, ok)
	require.Equal(t, 3, b, "a page past the top clamps to the first row, column kept as near as it fits")
	_, ok = caretRowMove(pos, 0, -10, true)
	require.False(t, ok, "already on the first row: the caller jumps to the start")
}

func TestInlineCaretPositions_FollowTerminalWrap(t *testing.T) {
	// "> " prompt, 10 columns: "> abcdefgh" fills row 0, "ij" is row 1.
	pos := inlineCaretPositions("abcdefghij", 10, 2)
	b, ok := caretRowMove(pos, len("abcdefghij"), -1, false)
	require.True(t, ok, "a single wrapped line can be walked by rows")
	require.Equal(t, 0, b, "column 2 on row 1 → column 2 on row 0, the first byte after the prompt")
}

func TestInputReader_RowsAndPagesOverAWrappedLine(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 10
	ir.line = strings.Repeat("x", 38) // rows: 8 + 10 + 10 + 10
	ir.cursorPos = len(ir.line)
	ir.hasEditedLine = true
	captureStdout(t, func() { ir.NavigateVertically(-1) })
	require.Equal(t, 28, ir.cursorPos, "Up moves one screen row of a wrapped line")
	captureStdout(t, func() { ir.pageRows(-1) })
	require.Equal(t, 8, ir.cursorPos, "Page Up past the top lands on the first row, column kept")
	captureStdout(t, func() { ir.pageRows(-1) })
	require.Equal(t, 0, ir.cursorPos, "Page Up on the first row goes to the start")
	captureStdout(t, func() { ir.pageRows(1) })
	require.NotZero(t, ir.cursorPos)
}

// A collapsed paste is one placeholder on screen; landing inside it puts the
// caret at the end of the paste, not inside text the user cannot see.
func TestInputReader_RawOffsetForDisplay(t *testing.T) {
	ir := NewInputReader("> ")
	paste := strings.Repeat("p\n", 20)
	ir.line = "ab" + paste + "cd"
	ir.collapsedPastes = []pasteSpan{{start: 2, end: 2 + len(paste)}}
	label := pastePlaceholder(paste)
	require.Equal(t, 1, ir.rawOffsetForDisplay(1))
	require.Equal(t, 2+len(paste), ir.rawOffsetForDisplay(2+len(label)/2))
	require.Equal(t, 2+len(paste)+1, ir.rawOffsetForDisplay(2+len(label)+1))
}
