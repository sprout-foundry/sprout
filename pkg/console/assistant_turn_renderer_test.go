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

func TestRenderer_ReasoningHeaderNoCursorSaveRestore(t *testing.T) {
	// Regression: the old implementation used \0337/\0338 (DEC save/restore)
	// which collided with concurrent cursor writers (activity indicator,
	// status footer). The new implementation prints the header without a
	// trailing newline and rewrites it in-place with \r\033[K.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking step 1")
	})
	// The header should NOT contain \0337 or \0338.
	require.NotContains(t, out, "\0337", "should not use DEC cursor save")
	require.NotContains(t, "\0338", out, "should not use DEC cursor restore")
	// The header should NOT end with a newline (it's printed in-place).
	require.False(t, strings.HasSuffix(out, "\n"), "header should not end with newline")
	// The header should contain the thinking text.
	require.Contains(t, out, "▽ Thinking…")
}

func TestRenderer_ReasoningSummaryRewritesInPlace(t *testing.T) {
	// Regression: endReasoningLocked should rewrite the header row in-place
	// using \r\033[K + summary + \n, without using \0338.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking step 1")
		r.WriteReasoningChunk("thinking step 2")
		// Simulate prose arriving to trigger endReasoningLocked.
		r.WriteChunk("hello\n")
	})
	// No DEC save/restore sequences.
	require.NotContains(t, out, "\0337", "should not use DEC cursor save")
	require.NotContains(t, "\0338", "should not use DEC cursor restore")
	// The summary line should be present.
	require.Contains(t, out, "▽ Thinking ·")
	require.Contains(t, out, "tokens")
	// The prose should follow with correct indent.
	require.Contains(t, out, "  hello\n")
}

func TestRenderer_ReasoningPhysicalLinesAccounting(t *testing.T) {
	// Regression: after reasoning header + summary, physicalLines should
	// reflect exactly 1 row (the summary line), and atLineStart should
	// be true so the next prose chunk gets indented correctly.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
		r.WriteChunk("first line\nsecond line\n")
	})
	// After reasoning summary (1 row) + "first line\n" (1 row) + "second line\n" (1 row)
	// = 3 physical lines total.
	r.mu.Lock()
	defer r.mu.Unlock()
	require.Equal(t, 3, r.physicalLines, "physicalLines should count reasoning summary + 2 prose lines")
	require.True(t, r.atLineStart, "should be at line start after trailing newline")
	require.Equal(t, 0, r.curLineRunes, "curLineRunes should be 0 at line start")
}

func TestRenderer_ReasoningThenProseIndentsCorrectly(t *testing.T) {
	// Regression: after reasoning, the first prose chunk should be indented
	// correctly (not double-indented or missing indent).
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
		r.WriteChunk("prose\n")
	})
	// The summary line ends with \n, so "prose" starts on a fresh line
	// and should get exactly one indent.
	require.Contains(t, out, "  prose\n")
}

func TestRenderer_EndReasoningNoOpWhenInactive(t *testing.T) {
	// Calling EndReasoning when no reasoning was streamed should be a no-op.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.EndReasoning()
		r.WriteChunk("hello\n")
	})
	require.Equal(t, "  hello\n", out)
	require.NotContains(t, out, "Thinking")
}

func TestRenderer_FinalizeAtTurnEndReasoningOnly(t *testing.T) {
	// A reasoning-only turn (no prose) should finalize the header at
	// FinalizeAtTurnEnd without crashing.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
		r.FinalizeAtTurnEnd()
	})
	require.NotContains(t, out, "\0337", "should not use DEC cursor save")
	require.NotContains(t, "\0338", "should not use DEC cursor restore")
	require.Contains(t, out, "▽ Thinking ·")
}

func TestRenderer_CursorOnFreshRow(t *testing.T) {
	// Regression for MUST_FIX #1: after reasoning finalizes, the cursor
	// is on a fresh row (endReasoningLocked's \n advanced past it),
	// so the streaming callback's `firstProseChunk` gate must NOT
	// inject another \n. CursorOnFreshRow is the renderer's
	// authoritative answer.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	// Before any reasoning or prose, the renderer is at line start.
	require.True(t, r.CursorOnFreshRow(), "fresh renderer should be at line start")
	captureRendererStdout(t, func() {
		// First reasoning chunk prints the header — mid-line, not at
		// line start.
		r.WriteReasoningChunk("thinking")
	})
	require.False(t, r.CursorOnFreshRow(), "mid-reasoning should not be at line start")
	captureRendererStdout(t, func() {
		// First prose chunk triggers endReasoningLocked → cursor
		// advances past the summary's \n → fresh row again.
		r.WriteChunk("hello\n")
	})
	require.True(t, r.CursorOnFreshRow(), "after reasoning summary \\n + prose \\n, should be at fresh row")
}
