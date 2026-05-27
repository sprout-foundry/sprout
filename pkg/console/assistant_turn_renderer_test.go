package console

import (
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// captureRendererStdout redirects os.Stdout for the duration of fn and returns
// whatever was written. We can't intercept fmt.Print's destination
// otherwise — the renderer uses fmt.Print directly so the stream order
// matches what a real terminal would see.
func captureRendererStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	rd, wr, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = wr

	done := make(chan string, 1)
	go func() {
		var b strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := rd.Read(buf)
			if n > 0 {
				b.Write(buf[:n])
			}
			if err != nil {
				done <- b.String()
				return
			}
		}
	}()

	fn()
	wr.Close()
	os.Stdout = old
	return <-done
}

func TestRenderer_IndentsEachLine(t *testing.T) {
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("hello\nworld\n")
	})
	require.Equal(t, "  hello\n  world\n", out)
}

func TestRenderer_IndentsAcrossChunkBoundaries(t *testing.T) {
	// "hel" + "lo\nwor" + "ld\n" — a line break sits across chunks.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("hel")
		r.WriteChunk("lo\nwor")
		r.WriteChunk("ld\n")
	})
	require.Equal(t, "  hello\n  world\n", out)
}

func TestRenderer_FinalizeNoOpWhenNoMarkdownFeatures(t *testing.T) {
	// Plain prose: should NOT re-render at finalize, so the streamed
	// version is the final output (no cursor clear, no duplicate).
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("Just a plain sentence.\n")
		r.FinalizeAtTurnEnd()
	})
	// Only the streamed-with-indent should be present — no ANSI cursor
	// movement (which would indicate an attempted re-render).
	require.Equal(t, "  Just a plain sentence.\n", out)
	require.NotContains(t, out, "\033[", "no ANSI sequences should be emitted for plain prose")
}

func TestRenderer_OnExternalWriteResetsSegment(t *testing.T) {
	// Even with markdown features, an external write between segments
	// means FinalizeAtTurnEnd should not be able to re-render older
	// content. We can't directly observe the buffer, but we can verify
	// that after OnExternalWrite + a small final segment with NO
	// markdown features, Finalize is a no-op (i.e., it considers only
	// the post-external segment).
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(true, true))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("# Heading\n- bullet\n") // would trigger re-render
		r.OnExternalWrite()                   // breaks segment
		r.WriteChunk("done.\n")               // plain prose
		r.FinalizeAtTurnEnd()
	})
	// First segment streamed with indent, then nothing (external write
	// happened externally — the renderer doesn't emit anything for it),
	// then the second segment streamed with indent.
	require.Equal(t, "  # Heading\n  - bullet\n  done.\n", out)
	require.NotContains(t, out, "\033[", "no re-render should fire — final segment has no markdown features")
}

func TestPhysicalRowsMath(t *testing.T) {
	// width=80, len=80 → fits exactly in 1 row
	require.Equal(t, 1, physicalRows(80, 80))
	// width=80, len=81 → spills to 2 rows
	require.Equal(t, 2, physicalRows(81, 80))
	// width=80, len=160 → exactly 2 rows
	require.Equal(t, 2, physicalRows(160, 80))
	// width=80, len=161 → 3 rows
	require.Equal(t, 3, physicalRows(161, 80))
	// width=0 → fallback to 1
	require.Equal(t, 1, physicalRows(500, 0))
	// len=0 → at least 1
	require.Equal(t, 1, physicalRows(0, 80))
}

func TestShouldReformat(t *testing.T) {
	// Plain prose — no.
	require.False(t, shouldReformat("Hello world.\n", 80))
	// Headings — yes.
	require.True(t, shouldReformat("# Big heading\nsome text\n", 80))
	require.True(t, shouldReformat("intro\n## Sub heading\nbody\n", 80))
	// Bullets — yes.
	require.True(t, shouldReformat("intro\n- item one\n- item two\n", 80))
	// Code block — yes.
	require.True(t, shouldReformat("see this:\n```go\nx := 1\n```\n", 80))
	// Bold — yes.
	require.True(t, shouldReformat("this is **important** text\n", 80))
	// Inline code (>=2 backticks) — yes.
	require.True(t, shouldReformat("call `foo()` then `bar()`\n", 80))
	// Single backtick (likely stray) — no.
	require.False(t, shouldReformat("apostrophe ' that's it\n", 80))
	// width=0 — no (post-stream re-render needs terminal width).
	require.False(t, shouldReformat("# heading\n", 0))
}
