package console

import (
	"fmt"
	"strings"
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
	require.Equal(t, "[pasted 5 lines: 1 2 3 4 5]", pastePlaceholder("1\n2\n3\n4\n5"))
}

func TestPastePlaceholderShowsPreview(t *testing.T) {
	// A collapsed paste must lead with its actual text, not only a count —
	// that is what lets the user confirm they pasted the right thing.
	// Newlines and tabs flatten to single spaces so the preview stays on
	// one line; content under the preview bound is shown in full.
	require.Equal(t,
		"[pasted 6 lines: func main() {  fmt.Println(\"hi\") } more more more]",
		pastePlaceholder("func main() {\n\tfmt.Println(\"hi\")\n}\nmore\nmore\nmore"))

	// Long single-line pastes clip at pastePreviewChars with an ellipsis.
	long := strings.Repeat("x", pastePreviewChars+50)
	got := pastePlaceholder(long)
	require.Contains(t, got, strings.Repeat("x", pastePreviewChars)+"…")
	require.Contains(t, got, fmt.Sprintf("[pasted %d chars: ", pastePreviewChars+50))

	// Short content under the preview bound is shown in full, no ellipsis.
	require.Equal(t, "[pasted 12 chars: hello world!]", pastePlaceholder("hello world!"))
}

// The preview is echoed verbatim into the composer, so it must never carry
// control bytes — a paste of coloured terminal output would otherwise
// re-emit raw ANSI escapes and let the terminal interpret them.
func TestPastePlaceholderStripsControlBytes(t *testing.T) {
	got := pastePlaceholder("\x1b[31mERROR\x1b[0m something failed\nsecond line")
	require.NotContains(t, got, "\x1b")
	require.NotContains(t, got, "[31m")
	require.Contains(t, got, "ERROR")
	require.Contains(t, got, "something failed")

	// A bare BEL/backspace must not survive either.
	require.NotContains(t, pastePlaceholder("a\x07b\x08c\nd\ne\nf"), "\x07")
	require.NotContains(t, pastePlaceholder("a\x07b\x08c\nd\ne\nf"), "\x08")
}

// Truncated or unusual escape sequences must not panic or leak the ESC
// byte — the preview is echoed straight into the terminal.
func TestPastePreview_EscapeSequenceBounds(t *testing.T) {
	for _, in := range []string{"\x1b", "\x1b[", "\x1b[3", "\x1b]", "\x1b]0;title", "\x1bP", "\x1b\x1b\x1b"} {
		require.NotContains(t, pastePreview(in), "\x1b", "input %q leaked ESC", in)
	}
	// OSC terminated by ST (ESC \) and by BEL both resume after the sequence.
	require.Contains(t, pastePreview("\x1b]0;t\x1b\\visible"), "visible")
	require.Contains(t, pastePreview("\x1b]0;t\x07visible"), "visible")
	// A two-rune ESC sequence leaves the following text intact.
	require.Contains(t, pastePreview("\x1bMvisible"), "visible")
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
