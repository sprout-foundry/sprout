package console

import (
	"testing"
	"unicode/utf8"

	"github.com/sprout-foundry/sprout/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func newEditIR(line string) *InputReader {
	return &InputReader{prompt: "> ", terminalWidth: 80, editBuffer: editBuffer{line: line, cursorPos: len(line)}}
}

func TestInputReader_CursorStepsByRune(t *testing.T) {
	ir := newEditIR("日本語テスト")
	testutil.CaptureStdout(t, func() {
		ir.MoveCursor(-3)
		ir.InsertChar("X")
	})
	require.Equal(t, "日本語Xテスト", ir.line)

	ir = newEditIR("ok 🙂🙂 end")
	testutil.CaptureStdout(t, func() {
		ir.MoveCursor(-5)
		ir.InsertChar("X")
	})
	require.Equal(t, "ok 🙂X🙂 end", ir.line)
	require.True(t, utf8.ValidString(ir.line))
}

func TestInputReader_BackspaceAndDeleteRemoveWholeRunes(t *testing.T) {
	ir := newEditIR("café")
	testutil.CaptureStdout(t, func() { ir.Backspace() })
	require.Equal(t, "caf", ir.line)

	ir = newEditIR("🙂ab")
	testutil.CaptureStdout(t, func() {
		ir.SetCursor(0)
		ir.Delete()
	})
	require.Equal(t, "ab", ir.line)
}

func TestInputReader_KillAndYank(t *testing.T) {
	ir := newEditIR("hello world foo")
	testutil.CaptureStdout(t, func() {
		ir.DeleteWordBackward()
		ir.SetCursor(0)
		ir.DeleteWordForward()
	})
	require.Equal(t, " world ", ir.line)
	testutil.CaptureStdout(t, func() { ir.Yank() })
	require.Equal(t, "hello world ", ir.line, "Ctrl-Y restores the most recent kill")
}

func TestWordBoundary(t *testing.T) {
	line := "  héllo   wörld  "
	require.Equal(t, len("  "), wordBoundary(line, len("  héllo"), -1))
	require.Equal(t, len("  héllo"), wordBoundary(line, 0, 1))
	require.Equal(t, len("  héllo   wörld"), wordBoundary(line, len("  héllo"), 1))
}

func TestShouldCollapsePaste(t *testing.T) {
	require.False(t, shouldCollapsePaste("./main.go"))
	require.False(t, shouldCollapsePaste("a\nb\nc"))
	require.True(t, shouldCollapsePaste("1\n2\n3\n4\n5"))
	require.Equal(t, "[pasted 5 lines]", pastePlaceholder("1\n2\n3\n4\n5"))
}

func TestHasWordMotionModifier(t *testing.T) {
	for seq, want := range map[string]bool{
		"\x1b[1;3": true, // Option/Alt
		"\x1b[1;5": true, // Ctrl
		"\x1b[1;7": true, // Ctrl+Alt
		"\x1b[1;2": false,
		"\x1b[":    false,
	} {
		require.Equal(t, want, hasWordMotionModifier([]byte(seq)), "%q", seq)
	}
}
