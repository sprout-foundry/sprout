// Reasoning (collapsed "Thinking" header) methods for AssistantTurnRenderer,
// split from assistant_turn_renderer.go. The reasoning path prints a single
// dim "▽ Thinking…" header on the first reasoning chunk, counts bytes
// silently, then rewrites the header in place into a byte/token summary
// when prose starts or the turn ends. The core streaming-chunk path
// (WriteChunk / flush / segment handling) stays in assistant_turn_renderer.go.
package console

import (
	"fmt"
)

// WriteReasoningChunk consumes one chunk of reasoning/thinking output
// from the streaming pipeline and renders the collapsed form. On the
// FIRST chunk of a reasoning segment it prints a single dim "▽ Thinking…"
// header; on subsequent chunks it only accumulates the byte count so the
// terminal stays clean even when the model emits tens of KiB of internal
// monologue. The header is finalized into "▽ Thinking · N kB (~N tokens)"
// by the next prose chunk (via WriteChunk) or by FinalizeAtTurnEnd.
//
// The header is printed WITHOUT a trailing newline so that
// endReasoningLocked can rewrite it in-place on the same row using
// `\r\033[K` + summary + `\n`. This avoids DEC save/restore (`\0337`/`\0338`)
// entirely — those sequences collide with concurrent writers (activity
// indicator, status footer, InputReader) that use `\r\033[K` and can
// corrupt the cursor position on many terminals.
//
// No-op when the chunk is empty. Safe to call concurrently with other
// renderer methods — internal mutex guards the state.
func (r *AssistantTurnRenderer) WriteReasoningChunk(chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	LockOutput()
	defer UnlockOutput()

	if !r.reasoningActive {
		// Print the header on the current row WITHOUT a trailing newline.
		// The activity indicator's Stop() already cleared the row with
		// `\r\033[K` and left the cursor at column 0, so we can write
		// directly. Both this method and Indicator.render() hold outputMu,
		// so no concurrent writer can interleave.
		fmt.Printf("%s%s%s▽ Thinking…%s", r.indent, ColorDim, ColorItalic, ColorReset)
		r.reasoningActive = true
		// We're mid-line on the header row. Track the visual width so
		// subsequent WriteChunk calls indent correctly.
		r.atLineStart = false
		r.curLineRunes = displayWidth(r.indent) + displayWidth("▽ Thinking…")
		// physicalLines is NOT incremented here — the header occupies the
		// same row that the spinner's Stop() already cleared. It will be
		// incremented in endReasoningLocked when the summary line gets
		// its trailing \n.
	}
	r.reasoningBytes += len(chunk)
}

// endReasoningLocked finalizes the collapsed header in place, rewriting
// "▽ Thinking…" to "▽ Thinking · 1.2 kB (~310 tokens)". Called with the
// mutex held. Idempotent — no-op when no reasoning was streamed this
// turn. Token estimate uses the common rule of thumb (1 token ≈ 4
// bytes); it's a hint, not an accounting source.
//
// The header was printed without a trailing newline by WriteReasoningChunk,
// so we rewrite it in-place: `\r\033[K` clears the current row, then we
// print the summary + `\n` to advance to the next row. No cursor save/restore
// needed — we're already on the correct row because both this path and the
// indicator/footer hold outputMu for serialization.
func (r *AssistantTurnRenderer) endReasoningLocked() {
	if !r.reasoningActive {
		return
	}
	r.reasoningActive = false
	bytes := r.reasoningBytes
	r.reasoningBytes = 0

	// Rewrite the header row in-place. `\r` returns to column 0,
	// `\033[K` clears to end of line, then we print the summary.
	fmt.Print("\r\033[K")
	fmt.Printf("%s%s%s▽ Thinking · %s · ~%d tokens%s\n",
		r.indent, ColorDim, ColorItalic,
		formatBytesShort(bytes), bytes/4, ColorReset)

	// The summary line (with its trailing \n) consumed exactly one physical
	// row. The cursor is now at the start of the next row.
	r.physicalLines++
	r.atLineStart = true
	r.curLineRunes = 0
}

// EndReasoning is the exported counterpart of endReasoningLocked for
// callers that drive the lifecycle directly (e.g. an explicit "end of
// thinking" event). The CLI today doesn't need it — WriteChunk and
// FinalizeAtTurnEnd both call the locked form — but it's exposed for
// completeness and tests.
func (r *AssistantTurnRenderer) EndReasoning() {
	r.mu.Lock()
	defer r.mu.Unlock()
	LockOutput()
	defer UnlockOutput()
	r.endReasoningLocked()
}

// CursorOnFreshRow reports whether the renderer is currently sitting at
// the start of an untouched row (column 0, no in-progress text). True
// after endReasoningLocked (which advances past the summary's \n) and
// after each completed newline in WriteChunk. Used by the CLI's
// streaming callback to decide whether to inject a separator \n before
// the first prose chunk: when false, the cursor is mid-line (the
// indicator's cleared residue) and the \n is required to escape it;
// when true, the cursor is already on a fresh row and the \n would add
// a spurious blank line — notably when reasoning ran first.
func (r *AssistantTurnRenderer) CursorOnFreshRow() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.atLineStart
}

// ReasoningActive reports whether a reasoning header ("▽ Thinking…") is
// currently printed on the renderer's row waiting to be finalized in
// place by endReasoningLocked. The streaming callback uses this to
// suppress the separator \n on the first prose chunk: when reasoning is
// active, the cursor is mid-line on the header row, and endReasoningLocked
// will rewrite that exact row via \r\033[K. Injecting a \n first would
// advance past the header row, leaving "▽ Thinking…" orphaned and
// placing the summary on the wrong row.
func (r *AssistantTurnRenderer) ReasoningActive() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.reasoningActive
}

// formatBytesShort returns a compact human-readable size. Used in the
// reasoning collapsed header where horizontal space is at a premium:
// "1234" → "1.2 kB", "1234567" → "1.2 MB". Plain "B" under 1 kB.
func formatBytesShort(n int) string {
	switch {
	case n < 1024:
		return fmt.Sprintf("%d B", n)
	case n < 1024*1024:
		return fmt.Sprintf("%.1f kB", float64(n)/1024.0)
	default:
		return fmt.Sprintf("%.1f MB", float64(n)/(1024.0*1024.0))
	}
}
