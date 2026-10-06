package console

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRenderer_WordWrapsWithinIndent(t *testing.T) {
	r := NewAssistantTurnRenderer(30, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("the quick brown fox jumps over the lazy dog\n")
	})
	require.Equal(t, "  the quick brown fox jumps\n  over the lazy dog\n", out)
}

func TestRenderer_WrappedListItemHangsUnderText(t *testing.T) {
	r := NewAssistantTurnRenderer(30, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("- second item with a much longer description\n")
	})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	require.Greater(t, len(lines), 1, "expected a wrap: %q", out)
	require.True(t, strings.HasPrefix(lines[0], "  - second"), "first row: %q", lines[0])
	for _, l := range lines[1:] {
		require.True(t, strings.HasPrefix(l, "    ") && l[4] != ' ', "continuation should hang under the text: %q", l)
	}
	for _, l := range lines {
		require.LessOrEqual(t, len(l), 30, "row overflows the terminal: %q", l)
	}
}

func TestRenderer_ShortLinesAndNarrowTerminalsPassThrough(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() { r.WriteChunk("short line\n") })
	require.Equal(t, "  short line\n", out)

	narrow := NewAssistantTurnRenderer(12, NewMarkdownFormatter(false, false))
	long := "a line far wider than this tiny terminal"
	out = captureRendererStdout(t, func() { narrow.WriteChunk(long + "\n") })
	require.Equal(t, "  "+long+"\n", out, "below minWrapColumns the terminal wraps")
}

func TestRenderer_HardBreaksOverlongWords(t *testing.T) {
	r := NewAssistantTurnRenderer(30, NewMarkdownFormatter(false, false))
	word := strings.Repeat("x", 40)
	out := captureRendererStdout(t, func() { r.WriteChunk(word + "\n") })
	for _, l := range strings.Split(strings.TrimSuffix(out, "\n"), "\n") {
		require.True(t, strings.HasPrefix(l, "  "), "row lost the indent: %q", l)
		require.LessOrEqual(t, len(l), 30)
	}
}

func TestRenderer_TablesAndRulesFitInsideIndent(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	const width = 40
	r := NewAssistantTurnRenderer(width, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("| area | what is there |\n|---|---|\n| CLI | " + strings.Repeat("cobra commands ", 6) + "|\n\n---\n")
		r.FinalizeAtTurnEnd()
	})
	for _, l := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		require.LessOrEqual(t, displayWidth(stripANSIEscapeCodes(l)), width, "row overflows the terminal: %q", l)
	}
}

func TestHangingIndent(t *testing.T) {
	cases := map[string]string{
		"plain text":               "",
		"- item":                   "  ",
		"  * nested":               "    ",
		"12. numbered":             "    ",
		"│ code":                   "  ",
		"\x1b[32m-\x1b[0m colored": "  ",
	}
	for in, want := range cases {
		require.Equal(t, want, hangingIndent(in), "hangingIndent(%q)", in)
	}
}

func TestRenderer_ToolAfterReasoningFinalizesHeader(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk(strings.Repeat("x", 400))
		r.OnExternalWrite() // a tool starts before any prose
	})
	require.Contains(t, out, "▽ Thinking · ", "the header must be rewritten to its summary")
	require.True(t, strings.HasSuffix(out, "tokens"+ColorReset+"\n"), "the summary must end its row: %q", out)
}
