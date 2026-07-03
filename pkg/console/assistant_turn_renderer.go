package console

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// AssistantTurnRenderer wraps the streaming-callback path for one assistant
// turn. It formats each line of assistant prose with markdown (ANSI colors,
// syntax highlighting, tables, code blocks) AS IT STREAMS, before the text
// reaches the terminal. This replaces the old clear-and-reprint approach
// (which was disabled because the formatted output's row count never matched
// the streamed text, causing cursor clobbering).
//
// Lines are buffered until a newline arrives, then passed through the
// StreamingMarkdownFormatter and emitted with the configured indent. This
// gives ~1 line of latency (imperceptible) but eliminates all cursor
// manipulation — clobbering is structurally impossible.
//
// A "segment" is a contiguous run of stream chunks with no interleaved
// non-prose terminal output between them. Tool logs and any other
// writeTerminalMessage call must notify the renderer via OnExternalWrite
// to finalize the current segment and start a fresh one.
type AssistantTurnRenderer struct {
	mu sync.Mutex

	// seg accumulates the raw text for the current segment. Used to detect
	// empty segments and for the footer-streaming gate. Reset on segment
	// boundary.
	seg strings.Builder

	// lineBuf buffers the current line until \n so it can be formatted as
	// a unit. Cross-line state (code blocks, tables) lives in streamFmt.
	lineBuf strings.Builder

	// streamFmt applies per-line markdown formatting with cross-line state.
	// nil when the formatter is nil or colors are disabled (raw emit fallback).
	streamFmt *StreamingMarkdownFormatter

	// Streaming state — tracked across chunks.
	atLineStart   bool
	curLineRunes  int // visual length of the in-progress line (runes)
	physicalLines int // physical rows the COMPLETED lines have used

	// Reasoning-stream state. When a turn includes a thinking-model
	// reasoning block, the per-chunk text would otherwise flood the
	// terminal with lines the user almost never reads through. Instead
	// we print a single "▽ Thinking..." header on the first reasoning
	// chunk, count bytes silently after that, and finalize the header
	// to "▽ Thinking · 1.2KB (~310 tokens)" when prose starts (or at
	// turn end). The full reasoning is still published verbatim to the
	// event bus for WebUI consumers — only the terminal gets collapsed.
	reasoningActive bool
	reasoningBytes  int

	terminalWidth int
	formatter     *MarkdownFormatter
	indent        string

	// footer is the status footer to suppress during active prose
	// streaming. When non-nil, SetProseStreaming(true) is called on
	// the first WriteChunk of each segment and SetProseStreaming(false)
	// on segment end (OnExternalWrite / FinalizeAtTurnEnd).
	footer *StatusFooter
}

// SetFooter wires the status footer so the renderer can suppress its
// refresh during active prose streaming — the root cause of the
// "scattered characters" clobbering symptom (DEC save/restore cursor
// races with scroll-region content).
func (r *AssistantTurnRenderer) SetFooter(f *StatusFooter) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.footer = f
}

// NewAssistantTurnRenderer constructs a renderer with the given terminal
// width snapshot and markdown formatter. When the formatter has colors
// enabled, a StreamingMarkdownFormatter is created for per-line formatting.
// width <= 0 disables soft-wrap accounting; the indent still works.
func NewAssistantTurnRenderer(width int, formatter *MarkdownFormatter) *AssistantTurnRenderer {
	r := &AssistantTurnRenderer{
		atLineStart:   true,
		terminalWidth: width,
		formatter:     formatter,
		indent:        "  ",
	}
	if formatter != nil && formatter.enableColors {
		r.streamFmt = NewStreamingMarkdownFormatter(formatter)
	}
	return r
}

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

// WriteChunk emits a chunk of assistant text to stdout, formatting each
// complete line with markdown before it reaches the terminal. Text is
// buffered until a newline arrives, then passed through the
// StreamingMarkdownFormatter and emitted with the configured indent.
//
// When colors are disabled (streamFmt == nil), falls back to raw emit:
// each line gets the indent but no formatting.
func (r *AssistantTurnRenderer) WriteChunk(chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Serialize against the status footer / input renderer so a concurrent
	// footer redraw can't interleave with this stream and displace the
	// cursor.
	LockOutput()
	defer UnlockOutput()

	// Prose has arrived — finalize any pending reasoning header so the
	// "▽ Thinking…" line collapses to the summary before the first
	// prose row lands.
	r.endReasoningLocked()

	// Suppress footer refresh while prose is streaming. The footer's
	// DEC save/restore (\0337/\0338) races with scroll-region scrolling,
	// displacing the cursor and scattering characters.
	if r.footer != nil && r.seg.Len() == 0 {
		r.footer.SetProseStreaming(true)
	}

	r.seg.WriteString(chunk)
	r.lineBuf.WriteString(chunk)

	// Process complete lines from the buffer.
	r.flushCompleteLines()
}

// flushCompleteLines extracts complete lines (terminated by \n) from
// lineBuf, formats each through the streaming formatter, and emits the
// result with the configured indent. Partial lines remain in the buffer.
func (r *AssistantTurnRenderer) flushCompleteLines() {
	for {
		s := r.lineBuf.String()
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			if s == "" {
				// All lines flushed — cursor is at the start of a fresh row.
				r.atLineStart = true
				r.curLineRunes = 0
			} else {
				// Partial line remains in buffer — cursor is mid-line.
				r.curLineRunes = displayWidth(r.indent) + runewidth.StringWidth(s)
				r.atLineStart = false
			}
			return
		}

		line := s[:idx]
		rest := s[idx+1:]
		r.lineBuf.Reset()
		if rest != "" {
			r.lineBuf.WriteString(rest)
		}

		emitted := r.emitFormattedLine(line)
		r.physicalLines += emitted
		r.atLineStart = true
		r.curLineRunes = 0
	}
}

// emitFormattedLine formats a single line and writes it to stdout with the
// indent. When colors are enabled, the line passes through the
// StreamingMarkdownFormatter. Otherwise it's emitted raw with indent.
// The formatter output may span multiple lines (table flush); each output
// line gets the indent. Returns the number of physical output lines emitted.
func (r *AssistantTurnRenderer) emitFormattedLine(line string) int {
	var formatted string
	if r.streamFmt != nil {
		formatted = r.streamFmt.ProcessLine(line)
	} else {
		formatted = line + "\n"
	}
	if formatted == "" {
		return 0 // line was consumed (e.g. code fence boundary)
	}
	// Each output line gets the indent prefix.
	lineCount := 0
	for _, outLine := range strings.SplitAfter(formatted, "\n") {
		if outLine == "" {
			continue
		}
		fmt.Print(r.indent)
		fmt.Print(outLine)
		lineCount++
	}
	return lineCount
}

// OnExternalWrite finalizes the current segment without re-rendering it.
// Wire this into the OutputRouter's writeTerminalMessage so that tool-log
// lines, agent messages, and any other non-prose terminal output break the
// prose segment cleanly. A fresh segment begins on the next WriteChunk.
func (r *AssistantTurnRenderer) OnExternalWrite() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resetSegment()
}

// OnExternalWriteRows finalizes the current segment and advances
// physicalLines by `n` rows to account for external writes that
// consumed terminal rows (e.g. a blank-line separator or a multi-line
// todo block). This keeps the renderer's state in sync at segment
// boundaries.
//
// When n == 0 the segment is still reset (same as OnExternalWrite).
// When n > 0 the renderer treats the external write as if it had
// emitted n newline-terminated rows: physicalLines advances, the
// cursor is considered at the start of a fresh row, and the segment
// buffer resets.
func (r *AssistantTurnRenderer) OnExternalWriteRows(n int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.physicalLines += n
	r.atLineStart = true
	r.curLineRunes = 0
	r.seg.Reset()
	r.lineBuf.Reset()
	if r.streamFmt != nil {
		r.streamFmt.Reset()
	}
}

// FinalizeAtTurnEnd is called once the assistant's turn has completed (after
// the spinner stops, after any post-turn book-keeping). It flushes any
// remaining partial line through the formatter and ensures a trailing newline
// so the cursor lands on a fresh row for the next turn's output.
func (r *AssistantTurnRenderer) FinalizeAtTurnEnd() {
	r.mu.Lock()
	defer r.mu.Unlock()
	LockOutput()
	defer UnlockOutput()

	// Catch a "reasoning-only" turn: the model emitted thinking but
	// no prose response (rare, but happens when a tool call follows
	// directly). Without this the "▽ Thinking…" header would stay
	// on screen as a stale ellipsis instead of resolving to the
	// final byte/token count.
	r.endReasoningLocked()

	// Flush any remaining partial line through the formatter.
	if r.lineBuf.Len() > 0 {
		line := r.lineBuf.String()
		r.lineBuf.Reset()
		emitted := r.emitFormattedLine(line)
		r.physicalLines += emitted
	}

	// Flush any pending formatter state (buffered table rows).
	if r.streamFmt != nil {
		flushed := r.streamFmt.Flush()
		if flushed != "" {
			for _, outLine := range strings.SplitAfter(flushed, "\n") {
				if outLine == "" {
					continue
				}
				fmt.Print(r.indent)
				fmt.Print(outLine)
				r.physicalLines++
			}
		}
	}

	// Ensure a trailing newline so the cursor is on a fresh row before
	// the caller writes the turn summary / renders the next prompt.
	// Streaming prose frequently ends without a trailing \n (the model's
	// last chunk is mid-sentence or ends in a space).
	if !r.atLineStart {
		fmt.Print("\n")
	}
	r.resetSegment()
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

func (r *AssistantTurnRenderer) resetSegment() {
	// Re-enable footer refresh now that the prose segment is done.
	if r.footer != nil {
		r.footer.SetProseStreaming(false)
	}
	r.seg.Reset()
	r.lineBuf.Reset()
	if r.streamFmt != nil {
		r.streamFmt.Reset()
	}
	r.atLineStart = true
	r.curLineRunes = 0
	r.physicalLines = 0
}

// physicalRows returns the number of terminal rows a line of `visualLen`
// runes occupies on a terminal of `width` columns. width<=0 means
// "unknown" — fall back to 1.
func physicalRows(visualLen, width int) int {
	if width <= 0 {
		return 1
	}
	if visualLen <= 0 {
		return 1
	}
	return (visualLen + width - 1) / width
}

// currentStdoutWidth reads the terminal's current column count live, or 0 if it
// can't be determined (not a TTY / error). Used at turn finalize to detect a
// resize that happened while the turn was streaming.
func currentStdoutWidth() int {
	cols, _, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || cols <= 0 {
		return 0
	}
	return cols
}
