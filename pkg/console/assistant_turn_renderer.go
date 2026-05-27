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

// WriteChunk emits a chunk of assistant text to stdout, prefixing each line
// with the configured indent. The chunk is also appended to the current
// segment buffer for potential post-segment re-render.
func (r *AssistantTurnRenderer) WriteChunk(chunk string) {
	if chunk == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

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
