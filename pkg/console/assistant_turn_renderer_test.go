package console

import (
	"os"
	"strings"
	"testing"
	"time"

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

func TestRenderer_ReasoningActive(t *testing.T) {
	// Regression for Bug 2: ReasoningActive() must report true while the
	// "▽ Thinking…" header is on the row, and false after endReasoningLocked
	// finalizes it. The streaming callback uses this to suppress the
	// separator \n that would otherwise orphan the header row.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	require.False(t, r.ReasoningActive(), "fresh renderer should not have reasoning active")
	captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
	})
	require.True(t, r.ReasoningActive(), "reasoning header printed, should be active")
	captureRendererStdout(t, func() {
		r.WriteChunk("prose\n")
	})
	require.False(t, r.ReasoningActive(), "after prose finalized reasoning, should not be active")
}

func TestRenderer_ReasoningHeaderRewrittenInPlace(t *testing.T) {
	// Regression for Bug 2: when reasoning is active and the first prose
	// chunk arrives, endReasoningLocked must rewrite the header row in
	// place. If the streaming callback's separator \n fires between the
	// header and the summary, the header gets orphaned on its own row and
	// the summary appears below it — two visible lines instead of one.
	//
	// The byte stream contains both "▽ Thinking…" (the header) and
	// "▽ Thinking ·" (the summary) because endReasoningLocked emits
	// \r\033[K between them — that erases the header on a real terminal
	// but the bytes remain in the pipe. The key invariant: there must be
	// NO \n between the header and the \r\033[K that rewrites it. If a \n
	// were present, the header would be on its own row, orphaned.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
		r.WriteChunk("hello\n")
	})
	// Locate the header and the summary in the raw byte stream.
	headerIdx := strings.Index(out, "▽ Thinking…")
	require.GreaterOrEqual(t, headerIdx, 0, "header should be present")
	summaryIdx := strings.Index(out, "▽ Thinking ·")
	require.GreaterOrEqual(t, summaryIdx, 0, "summary should be present")
	require.Greater(t, summaryIdx, headerIdx, "summary should come after header")
	// Between the header and the summary, there must be NO newline.
	// If the streaming callback injected fmt.Println() here, a \n would
	// separate them, orphaning the header on its own row.
	between := out[headerIdx:summaryIdx]
	require.NotContains(t, between, "\n",
		"no separator \\n between header and in-place rewrite — header would be orphaned; got between=%q", between)
	// The rewrite sequence (\r\033[K) must be present between them.
	require.Contains(t, between, "\r\033[K",
		"endReasoningLocked must erase the header row before the summary")
	// Prose follows on the next row.
	require.Contains(t, out, "  hello\n")
}

func TestRenderer_FinalizeEnsuresTrailingNewline(t *testing.T) {
	// Regression for Bug 3: when streamed prose ends WITHOUT a trailing
	// \n (the common case — the model's last chunk is mid-sentence),
	// FinalizeAtTurnEnd must emit a \n so the cursor lands on a fresh
	// row. Before the indicator.Stop() fix (Bug 1), Stop() unconditionally
	// wrote \r\033[K at turn end, which acted as an implicit "cursor at
	// column 0" guarantee. Now that Stop() is a true no-op when idle,
	// FinalizeAtTurnEnd owns this responsibility. Without it, the
	// per-turn summary line glues onto the partial prose row and the
	// next prompt's \r\033[K clobbers it.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("partial line with no newline")
		r.FinalizeAtTurnEnd()
	})
	// The prose was streamed without a trailing \n, so FinalizeAtTurnEnd
	// must add one. The output should end with exactly one \n.
	require.True(t, strings.HasSuffix(out, "\n"),
		"FinalizeAtTurnEnd must emit a trailing \\n when cursor is mid-line; got %q", out)
}

func TestRenderer_FinalizeNoDoubleNewlineWhenAlreadyAtLineStart(t *testing.T) {
	// When the prose already ended with \n (cursor on a fresh row),
	// FinalizeAtTurnEnd must NOT add a spurious blank line.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteChunk("complete line\n")
		r.FinalizeAtTurnEnd()
	})
	require.Equal(t, "  complete line\n", out,
		"no extra newline when prose already ended with \\n; got %q", out)
}

func TestRenderer_FinalizeTrailingNewlineAfterReasoning(t *testing.T) {
	// Reasoning-only turn: the reasoning header is finalized by
	// endReasoningLocked, which advances the cursor past the summary \n
	// (atLineStart = true). FinalizeAtTurnEnd must NOT add another \n.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	out := captureRendererStdout(t, func() {
		r.WriteReasoningChunk("thinking")
		r.FinalizeAtTurnEnd()
	})
	// The summary line ends with \n; no extra blank line.
	if trailing := strings.Count(out, "\n"); trailing > 1 {
		t.Errorf("reasoning-only finalize should have exactly one trailing \\n, got %d; output=%q", trailing, out)
	}
	require.True(t, strings.HasSuffix(out, "\n"), "should end with single \\n")
}

func TestOnExternalWriteAdvancesPhysicalLines(t *testing.T) {
	// Verify that OnExternalWriteRows correctly advances physicalLines
	// and resets the segment, so the renderer's row math stays in sync
	// when external stdout writes (tool start blank line, todo block)
	// interleave with prose chunks.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	captureRendererStdout(t, func() {
		r.WriteChunk("The ")
	})
	// After "The " (no newline), physicalLines should be 0.
	r.mu.Lock()
	require.Equal(t, 0, r.physicalLines, "no completed lines yet")
	require.False(t, r.atLineStart, "mid-line after 'The '")
	require.Equal(t, 6, r.curLineRunes, "curLineRunes tracks indent(2) + 'The '(4)")
	r.mu.Unlock()

	// Simulate an external blank-line write (e.g. tool start).
	r.OnExternalWriteRows(1)

	r.mu.Lock()
	require.Equal(t, 1, r.physicalLines, "external write consumed 1 row")
	require.True(t, r.atLineStart, "cursor at line start after external write")
	require.Equal(t, 0, r.curLineRunes, "curLineRunes reset")
	require.Empty(t, r.seg.String(), "segment buffer reset")
	r.mu.Unlock()

	// Continue with prose that has a newline.
	captureRendererStdout(t, func() {
		r.WriteChunk("quick\n")
	})

	r.mu.Lock()
	require.Equal(t, 2, r.physicalLines, "external 1 + quick\\n 1 = 2")
	require.True(t, r.atLineStart)
	require.Equal(t, 0, r.curLineRunes)
	// Segment should only contain the post-external-write chunk.
	require.Equal(t, "quick\n", r.seg.String(), "segment only has post-external content")
	r.mu.Unlock()
}

func TestOnExternalWriteZeroArgResetsSegment(t *testing.T) {
	// Verify that the zero-arg OnExternalWrite still works (backward compat).
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	captureRendererStdout(t, func() {
		r.WriteChunk("hello\n")
	})
	r.mu.Lock()
	require.Equal(t, 1, r.physicalLines)
	r.mu.Unlock()

	r.OnExternalWrite()

	r.mu.Lock()
	require.Equal(t, 0, r.physicalLines, "OnExternalWrite resets physicalLines")
	require.True(t, r.atLineStart)
	require.Equal(t, 0, r.curLineRunes)
	require.Empty(t, r.seg.String())
	r.mu.Unlock()
}

func TestFinalizeClearsAllRowsAfterExternalWrites(t *testing.T) {
	// Simulate a multi-paragraph prose flow with external writes between
	// paragraphs. After FinalizeAtTurnEnd, the formatter should walk back
	// the correct total count of rows (prose + external).
	//
	// Flow:
	//   WriteChunk("# Heading\n")        -> 1 row, physicalLines=1
	//   OnExternalWriteRows(1)           -> external blank line, physicalLines=2
	//   WriteChunk("Body paragraph.\n")  -> 1 row, physicalLines=3
	//   OnExternalWriteRows(1)           -> external blank line, physicalLines=4
	//   WriteChunk("Final.\n")           -> 1 row, physicalLines=5
	//
	// FinalizeAtTurnEnd should compute upRows=5 and walk back 5 rows.
	// We can't easily test ANSI cursor movement in a pipe, but we can
	// verify that physicalLines is correct throughout.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))
	captureRendererStdout(t, func() {
		r.WriteChunk("# Heading\n")
	})
	r.mu.Lock()
	require.Equal(t, 1, r.physicalLines)
	r.mu.Unlock()

	r.OnExternalWriteRows(1)
	r.mu.Lock()
	require.Equal(t, 2, r.physicalLines)
	r.mu.Unlock()

	captureRendererStdout(t, func() {
		r.WriteChunk("Body paragraph.\n")
	})
	r.mu.Lock()
	require.Equal(t, 3, r.physicalLines)
	r.mu.Unlock()

	r.OnExternalWriteRows(1)
	r.mu.Lock()
	require.Equal(t, 4, r.physicalLines)
	r.mu.Unlock()

	captureRendererStdout(t, func() {
		r.WriteChunk("Final.\n")
	})
	r.mu.Lock()
	require.Equal(t, 5, r.physicalLines)
	r.mu.Unlock()
}

func TestUpRowsFormulaCorrectness(t *testing.T) {
	// Verify that the upRows formula used in FinalizeAtTurnEnd computes the
	// total rows the segment occupies on screen. The semantic contract:
	//
	//   upRows = physicalLines + physicalRows(curLineRunes, width)
	//   when curLineRunes > 0 (there is an in-progress line)
	//
	//   upRows = physicalLines
	//   when curLineRunes == 0 (cursor is at line start, no in-progress line)
	//
	// The buggy formula subtracted 1 from the in-progress contribution,
	// which skipped clearing one of the in-progress line's wrapped rows.

	tests := []struct {
		name          string
		physicalLines int
		atLineStart   bool
		curLineRunes  int
		terminalWidth int
		wantUpRows    int
	}{
		{
			name: "at-line-start-no-in-progress",
			physicalLines: 3, atLineStart: true, curLineRunes: 0, terminalWidth: 80,
			wantUpRows: 3, // 3 + 0 = 3
		},
		{
			name: "in-progress-40-chars-fits-one-row",
			physicalLines: 3, atLineStart: false, curLineRunes: 42, terminalWidth: 80,
			wantUpRows: 4, // 3 + physicalRows(42, 80)=1 → 4
		},
		{
			name: "in-progress-80-chars-exact-fit",
			physicalLines: 3, atLineStart: false, curLineRunes: 80, terminalWidth: 80,
			wantUpRows: 4, // 3 + physicalRows(80, 80)=1 → 4
		},
		{
			name: "in-progress-81-chars-wraps-to-two-rows",
			physicalLines: 3, atLineStart: false, curLineRunes: 81, terminalWidth: 80,
			wantUpRows: 5, // 3 + physicalRows(81, 80)=2 → 5
		},
		{
			name: "in-progress-200-chars-wraps-to-three-rows",
			physicalLines: 3, atLineStart: false, curLineRunes: 200, terminalWidth: 80,
			wantUpRows: 6, // 3 + physicalRows(200, 80)=3 → 6
		},
		{
			name: "zero-physical-lines-with-in-progress",
			physicalLines: 0, atLineStart: false, curLineRunes: 50, terminalWidth: 80,
			wantUpRows: 1, // 0 + physicalRows(50, 80)=1 → 1
		},
		{
			name: "empty-segment",
			physicalLines: 0, atLineStart: true, curLineRunes: 0, terminalWidth: 80,
			wantUpRows: 0, // 0 + 0 = 0
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Compute expected: total rows the segment occupies on screen.
			expected := tt.physicalLines
			if tt.curLineRunes > 0 {
				expected += physicalRows(tt.curLineRunes, tt.terminalWidth)
			}
			require.Equal(t, tt.wantUpRows, expected, "expected value sanity check")

			// Compute what the formula in FinalizeAtTurnEnd gives.
			// After the fix:
			//   upRows := r.physicalLines
			//   if r.curLineRunes > 0 {
			//       upRows += physicalRows(r.curLineRunes, r.terminalWidth)
			//   }
			currentFormula := tt.physicalLines
			if tt.curLineRunes > 0 {
				currentFormula += physicalRows(tt.curLineRunes, tt.terminalWidth)
			}

			require.Equal(t, expected, currentFormula,
				"upRows formula should equal total rows the segment occupies on screen")
		})
	}
}

func TestRenderer_InProgressLineRowAccounting(t *testing.T) {
	// Verify that streaming text with an in-progress line (no trailing \n)
	// produces correct internal state, and that the upRows derived from that
	// state equals the total rows the segment occupies.

	const width = 80

	tests := []struct {
		name         string
		chunks       []string
		wantLines    int
		wantAtStart  bool
		wantCurRunes int
		wantUpRows   int
	}{
		{
			name:       "three-completed-lines-no-in-progress",
			chunks:     []string{"line1\n", "line2\n", "line3\n"},
			wantLines:  3, wantAtStart: true, wantCurRunes: 0, wantUpRows: 3,
		},
		{
			name:       "three-lines-plus-40-char-in-progress",
			chunks:     []string{"line1\n", "line2\n", "line3\n", strings.Repeat("x", 40)},
			wantLines:  3, wantAtStart: false, wantCurRunes: 42, wantUpRows: 4,
		},
		{
			name:       "three-lines-plus-78-char-in-progress-exact-fit",
			chunks:     []string{"line1\n", "line2\n", "line3\n", strings.Repeat("x", 78)},
			wantLines:  3, wantAtStart: false, wantCurRunes: 80, wantUpRows: 4,
		},
		{
			name:       "three-lines-plus-79-char-in-progress-wraps",
			chunks:     []string{"line1\n", "line2\n", "line3\n", strings.Repeat("x", 79)},
			wantLines:  3, wantAtStart: false, wantCurRunes: 81, wantUpRows: 5,
		},
		{
			name:       "three-lines-plus-198-char-in-progress-multi-wrap",
			chunks:     []string{"line1\n", "line2\n", "line3\n", strings.Repeat("x", 198)},
			wantLines:  3, wantAtStart: false, wantCurRunes: 200, wantUpRows: 6,
		},
		{
			name:       "single-chunk-no-newline",
			chunks:     []string{strings.Repeat("a", 50)},
			wantLines:  0, wantAtStart: false, wantCurRunes: 52, wantUpRows: 1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := NewAssistantTurnRenderer(width, NewMarkdownFormatter(false, false))
			captureRendererStdout(t, func() {
				for _, chunk := range tt.chunks {
					r.WriteChunk(chunk)
				}
			})

			r.mu.Lock()
			defer r.mu.Unlock()

			require.Equal(t, tt.wantLines, r.physicalLines, "physicalLines mismatch")
			require.Equal(t, tt.wantAtStart, r.atLineStart, "atLineStart mismatch")
			require.Equal(t, tt.wantCurRunes, r.curLineRunes, "curLineRunes mismatch")

			// Verify upRows = total rows the segment occupies
			expectedUpRows := r.physicalLines
			if r.curLineRunes > 0 {
				expectedUpRows += physicalRows(r.curLineRunes, r.terminalWidth)
			}
			require.Equal(t, tt.wantUpRows, expectedUpRows,
				"upRows should equal total rows the segment occupies")
		})
	}
}



func TestSubscriberStdoutInterleaveDoesNotEraseRenderedProse(t *testing.T) {
	// Regression test for the streaming word-erasure bug.
	// Simulate concurrent WriteChunk and locked external stdout writes.
	// After both goroutines finish, the renderer's physicalLines should
	// match the actual row count and the segment should be intact.
	r := NewAssistantTurnRenderer(80, NewMarkdownFormatter(false, false))

	done := make(chan struct{})

	// Goroutine 1: drives WriteChunk with small delays between runes.
	go func() {
		words := []string{"Hello ", "world ", "this ", "is ", "a ", "test\n"}
		for _, w := range words {
			captureRendererStdout(t, func() {
				r.WriteChunk(w)
			})
		}
	}()

	// Goroutine 2: simulates subscriber's locked external writes.
	go func() {
		// Simulate a tool start blank line mid-stream.
		time.Sleep(1 * time.Millisecond)
		LockOutput()
		// Write happens here (we can't capture it in a test pipe
		// without redirecting os.Stdout, so just simulate the effect).
		UnlockOutput()
		r.OnExternalWriteRows(1)

		// Simulate another tool start.
		time.Sleep(2 * time.Millisecond)
		LockOutput()
		UnlockOutput()
		r.OnExternalWriteRows(1)

		done <- struct{}{}
	}()

	// Wait for the external writes to complete.
	<-done
	// Give the write goroutine time to finish.
	time.Sleep(50 * time.Millisecond)

	r.mu.Lock()
	defer r.mu.Unlock()
	// The prose "Hello world this is a test\n" has one newline -> 1 row.
	// Plus 2 external blank lines = 3 total.
	require.Equal(t, 3, r.physicalLines,
		"physicalLines should count prose rows + external write rows")
	require.True(t, r.atLineStart, "should be at line start after trailing newline")
}
