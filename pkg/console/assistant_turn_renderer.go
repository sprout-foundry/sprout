package console

import (
	"fmt"
	"os"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/envutil"
	"golang.org/x/term"
)

// AssistantTurnRenderer wraps the streaming-callback path for one assistant
// turn. It does two things:
//
//  1. Indents every emitted line of assistant prose with a configurable
//     prefix (default "  ") so the model's text visually separates from
//     chrome (tool-log lines, agent messages, system info).
//  2. Buffers the *current* prose segment so it can be re-rendered with
//     markdown formatting at the end of the turn.
//
// A "segment" is a contiguous run of stream chunks with no interleaved
// non-prose terminal output between them. Tool logs and any other
// writeTerminalMessage call must notify the renderer via OnExternalWrite
// to finalize the current segment (no re-render of older segments) and
// start a fresh one. At turn end, FinalizeAtTurnEnd potentially re-renders
// the final segment with markdown formatting — clearing the streamed
// version via ANSI cursor manipulation and emitting the colorized version
// in its place.
//
// The re-render only fires if (a) stdout is a TTY, (b) the segment
// contains markdown features worth formatting, and (c) a usable terminal
// width is available. Otherwise the streamed raw version stays — fail-safe
// rather than risk a scrollback-destroying cursor glitch on non-TTY
// targets.
type AssistantTurnRenderer struct {
	mu sync.Mutex

	// Current segment buffer. Reset on segment boundary.
	seg strings.Builder

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
}

// NewAssistantTurnRenderer constructs a renderer with the given terminal
// width snapshot and markdown formatter. width <= 0 disables soft-wrap
// accounting and the post-stream re-render path (the indent still works).
func NewAssistantTurnRenderer(width int, formatter *MarkdownFormatter) *AssistantTurnRenderer {
	return &AssistantTurnRenderer{
		atLineStart:   true,
		terminalWidth: width,
		formatter:     formatter,
		indent:        "  ",
	}
}

// WriteReasoningChunk consumes one chunk of reasoning/thinking output
// from the streaming pipeline and renders the collapsed form. On the
// FIRST chunk of a reasoning segment it prints a single dim "▽ Thinking…"
// header; on subsequent chunks it only accumulates the byte count so the
// terminal stays clean even when the model emits tens of KiB of internal
// monologue. The header is finalized into "▽ Thinking · N kB (~N tokens)"
// by the next prose chunk (via WriteChunk) or by FinalizeAtTurnEnd.
//
// No-op when the chunk is empty. Safe to call concurrently with other
// renderer methods — internal mutex guards the state.
func (r *AssistantTurnRenderer) WriteReasoningChunk(chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.reasoningActive {
		// Print the header on a fresh row so it sits cleanly between
		// the assistant header and the prose that follows. Dim color
		// reads as "ambient detail" rather than active output.
		fmt.Printf("%s%s%s▽ Thinking…%s\n", r.indent, ColorDim, ColorItalic, ColorReset)
		r.reasoningActive = true
		// The header consumed exactly one physical row regardless of
		// terminal width (it's well under any reasonable width); track
		// it so the prose segment's clear-and-rerender at turn end
		// doesn't accidentally walk over the header.
		r.physicalLines++
	}
	r.reasoningBytes += len(chunk)
}

// endReasoningLocked finalizes the collapsed header in place, rewriting
// "▽ Thinking…" to "▽ Thinking · 1.2 kB (~310 tokens)". Called with the
// mutex held. Idempotent — no-op when no reasoning was streamed this
// turn. Token estimate uses the common rule of thumb (1 token ≈ 4
// bytes); it's a hint, not an accounting source.
func (r *AssistantTurnRenderer) endReasoningLocked() {
	if !r.reasoningActive {
		return
	}
	r.reasoningActive = false
	bytes := r.reasoningBytes
	r.reasoningBytes = 0

	// Step one row up onto the header, clear it, reprint the summary.
	fmt.Print("\033[1A\r\033[K")
	fmt.Printf("%s%s%s▽ Thinking · %s · ~%d tokens%s\n",
		r.indent, ColorDim, ColorItalic,
		formatBytesShort(bytes), bytes/4, ColorReset)
}

// EndReasoning is the exported counterpart of endReasoningLocked for
// callers that drive the lifecycle directly (e.g. an explicit "end of
// thinking" event). The CLI today doesn't need it — WriteChunk and
// FinalizeAtTurnEnd both call the locked form — but it's exposed for
// completeness and tests.
func (r *AssistantTurnRenderer) EndReasoning() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.endReasoningLocked()
}

// WriteChunk emits a chunk of assistant text to stdout, prefixing each line
// with the configured indent. The chunk is also appended to the current
// segment buffer for potential post-segment re-render.
func (r *AssistantTurnRenderer) WriteChunk(chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// Prose has arrived — finalize any pending reasoning header so the
	// "▽ Thinking…" line collapses to the summary before the first
	// prose row lands.
	r.endReasoningLocked()

	r.seg.WriteString(chunk)
	indentRunes := len([]rune(r.indent))

	for _, ch := range chunk {
		if r.atLineStart {
			fmt.Print(r.indent)
			r.atLineStart = false
			r.curLineRunes = indentRunes
		}
		fmt.Print(string(ch))
		if ch == '\n' {
			r.physicalLines += physicalRows(r.curLineRunes, r.terminalWidth)
			r.atLineStart = true
			r.curLineRunes = 0
		} else {
			r.curLineRunes++
		}
	}
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

// FinalizeAtTurnEnd is called once the assistant's turn has completed (after
// the spinner stops, after any post-turn book-keeping). If the current
// segment has substantial markdown content and stdout is a TTY, the
// streamed raw text is cleared and the markdown-formatted version is
// emitted in its place. Otherwise the streamed text is left as-is.
func (r *AssistantTurnRenderer) FinalizeAtTurnEnd() {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Catch a "reasoning-only" turn: the model emitted thinking but
	// no prose response (rare, but happens when a tool call follows
	// directly). Without this the "▽ Thinking…" header would stay
	// on screen as a stale ellipsis instead of resolving to the
	// final byte/token count.
	r.endReasoningLocked()

	text := r.seg.String()
	if text == "" {
		r.resetSegment()
		return
	}
	if !shouldReformat(text, r.terminalWidth) {
		r.resetSegment()
		return
	}
	if !isStdoutTTY() {
		r.resetSegment()
		return
	}

	// Compute how many rows we need to walk back through. If the stream
	// ended mid-line (no trailing \n), the in-progress line's rows haven't
	// been counted in physicalLines — add them now.
	upRows := r.physicalLines
	if !r.atLineStart {
		upRows += physicalRows(r.curLineRunes, r.terminalWidth) - 1
	}

	// Hide the cursor for the duration of the clear-and-reprint dance.
	// Without this, the terminal cursor visibly jumps to the top of the
	// segment and then back down as the formatted text renders, which
	// reads as a "blink" — especially noticeable for tall code blocks.
	// `\033[?25l` hides; `\033[?25h` restores. Restoration is wrapped in
	// a defer-equivalent so an early return inside `emitFormatted`
	// doesn't leave the user with an invisible cursor for the rest of
	// the session.
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	// Cursor to column 0, then up to the first row of the streamed segment,
	// then clear from cursor to end of screen.
	fmt.Print("\r")
	if upRows > 0 {
		fmt.Printf("\033[%dA", upRows)
	}
	fmt.Print("\033[J")

	formatted := r.formatter.Format(text)
	// Emit formatted text with the same indent as the live stream.
	r.emitFormatted(formatted)
	r.resetSegment()
}

// emitFormatted prints `text` line-by-line with the configured indent. Each
// "physical line" (\n-terminated) gets one indent. The trailing newline is
// preserved so the next output (turn footer) lands on a fresh row.
func (r *AssistantTurnRenderer) emitFormatted(text string) {
	for _, line := range strings.SplitAfter(text, "\n") {
		if line == "" {
			continue
		}
		fmt.Print(r.indent)
		fmt.Print(line)
	}
	if !strings.HasSuffix(text, "\n") {
		fmt.Println()
	}
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
	r.seg.Reset()
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

// shouldReformat decides whether the segment has enough markdown signal to
// justify the cursor-clear + re-render dance. A plain one-line "Yes." has
// nothing to gain; a multi-paragraph response with headings + code blocks
// has plenty.
func shouldReformat(text string, width int) bool {
	if width <= 0 {
		return false
	}
	if !envutil.ResolveColorPreference(true) {
		// In no-color mode the formatter strips markers — re-rendering
		// would just produce a duplicate (and the clear+reprint flash is
		// a regression with no visual upside).
		return false
	}
	// Look for any markdown feature worth styling.
	markers := []string{
		"\n# ", "\n## ", "\n### ", "\n#### ",
		"\n- ", "\n* ", "\n+ ",
		"\n> ",
		"```", "**", "__",
	}
	for _, m := range markers {
		if strings.Contains(text, m) {
			return true
		}
	}
	// Leading markers (start-of-buffer; the \n-prefixed checks above
	// won't catch the very first line).
	if strings.HasPrefix(text, "# ") || strings.HasPrefix(text, "## ") ||
		strings.HasPrefix(text, "- ") || strings.HasPrefix(text, "* ") {
		return true
	}
	// Inline code spans (single backticks) — only count if there are at
	// least two so we know it's a real code span rather than a stray.
	if strings.Count(text, "`") >= 2 {
		return true
	}
	return false
}

func isStdoutTTY() bool {
	return term.IsTerminal(int(os.Stdout.Fd()))
}
