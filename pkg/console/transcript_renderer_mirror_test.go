package console

import (
	"strings"
	"testing"
)

// The turn renderer prints its rows straight to stdout rather than through
// WriteTranscript, so it mirrors them itself. It is the largest writer in the
// session: if its rows were missing, a resize repaint would erase the answer
// the user is looking at.
func TestRendererRowsEnterTheTranscriptMirror(t *testing.T) {
	mirrorTestFooter(t, 30, 20)
	r := NewAssistantTurnRenderer(30, NewMarkdownFormatter(false, false))

	captureRendererStdout(t, func() {
		r.WriteChunk("the quick brown fox jumps over the lazy dog\n")
	})

	lines, partial, _, ok := TranscriptRepaint()
	if !ok {
		t.Fatal("the renderer wrote nothing the mirror knows about")
	}
	if partial != "" {
		t.Fatalf("a newline-terminated chunk left a partial line %q", partial)
	}
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "  the quick brown fox jumps") ||
		!strings.Contains(joined, "  over the lazy dog") {
		t.Fatalf("the wrapped rows are not both mirrored:\n%q", lines)
	}
}
