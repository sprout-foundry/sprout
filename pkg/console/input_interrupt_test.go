package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInterruptLine_MidSearchEndsTheSearchAndAbandonsTheLine(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.history = []string{"older entry"}
	ir.line, ir.cursorPos = "draft", len("draft")
	ir.enterSearchMode()
	ir.searchQuery = "old"

	var keepReading bool
	captureStdout(t, func() { keepReading = ir.interruptLine() })

	require.True(t, keepReading, "typed text is abandoned, not the whole read")
	require.False(t, ir.searchMode, "the search must not outlive the Ctrl-C")
	require.Empty(t, ir.searchQuery)
	require.Empty(t, ir.preSearchLine, "Esc must have nothing to restore")
	require.Empty(t, ir.line)
	require.Equal(t, "draft", ir.history[len(ir.history)-1], "the line the search started from goes to history")
}

func TestInterruptLine_EmptyLineMidSearchEndsTheRead(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.history = []string{"older entry"}
	ir.enterSearchMode()

	var keepReading bool
	captureStdout(t, func() { keepReading = ir.interruptLine() })

	require.False(t, keepReading)
	require.False(t, ir.searchMode)
}

func TestRedrawAfterResume_PinnedBoxPrintsNoInlinePrompt(t *testing.T) {
	ir, _, _ := testInputReaderWithFooter()
	ir.line, ir.cursorPos = "half typed", len("half typed")
	out := captureStdout(t, ir.redrawAfterResume)
	require.NotContains(t, out, "> ", "the pinned box draws its own label")

	inline := NewInputReader("> ")
	inline.terminalWidth = 80
	out = captureStdout(t, inline.redrawAfterResume)
	require.True(t, strings.Contains(out, "> "), "inline input still reprints its prompt: %q", out)
}
