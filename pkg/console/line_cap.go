package console

import (
	"fmt"
	"strings"
)

// LineCapWriter wraps a sink (typically the streaming-output `fmt.Print`)
// and clamps each *visual* line to a maximum character count. When a line
// exceeds the cap, the writer emits the head, then a single
// `… [+N chars]` truncation marker, then swallows further bytes on that
// line until the next newline.
//
// This exists because the LLM sometimes streams a tool result that
// contains a single very long line (minified JS/JSON, a base64 blob, an
// unbroken log line) and terminals soft-wrap that into hundreds of
// visual rows, blowing up the user's scrollback. The LLM's view of the
// content is unchanged — only what the terminal renders is clipped.
//
// Not goroutine-safe; callers are expected to serialize writes (the
// streaming callback path naturally does this — one chunk at a time
// from a single producer).
type LineCapWriter struct {
	// CharLimit is the maximum characters allowed on one line before
	// truncation kicks in. Zero or negative disables capping.
	CharLimit int

	// Sink receives all bytes that are not clipped.
	Sink func(string)

	// charsInLine is the character count since the last newline.
	charsInLine int

	// suppressing is true once the current line has hit CharLimit and
	// we've already emitted the truncation marker. Bytes are dropped
	// until we see a newline.
	suppressing bool

	// suppressedCount is how many characters we've dropped on the
	// current line — reported in the truncation marker once we know
	// the final tally (i.e. on the next newline).
	suppressedCount int
}

// NewLineCapWriter constructs a writer with the given char limit and
// downstream sink. Use SinkFromPrintf for the typical stdout case.
func NewLineCapWriter(charLimit int, sink func(string)) *LineCapWriter {
	return &LineCapWriter{CharLimit: charLimit, Sink: sink}
}

// SinkFromPrintf is a convenience that writes to stdout via fmt.Print.
// It exists so callsites don't have to spell out the closure.
func SinkFromPrintf() func(string) {
	return func(s string) { fmt.Print(s) }
}

// Write processes one streaming chunk. The chunk can contain any mix of
// newlines and long runs; the writer tracks position across calls.
func (w *LineCapWriter) Write(chunk string) {
	if w.Sink == nil || chunk == "" {
		return
	}
	if w.CharLimit <= 0 {
		// Capping disabled — pass through verbatim.
		w.Sink(chunk)
		return
	}

	// Walk the chunk segment-by-segment, splitting at newlines.
	for {
		nl := strings.IndexByte(chunk, '\n')
		if nl < 0 {
			// No newline in remainder — entire chunk is mid-line.
			w.consumePartialLine(chunk)
			return
		}
		// Process up to (and including) the newline.
		w.consumePartialLine(chunk[:nl])
		w.flushTruncationMarker()
		w.Sink("\n")
		w.charsInLine = 0
		w.suppressing = false
		w.suppressedCount = 0
		chunk = chunk[nl+1:]
		if chunk == "" {
			return
		}
	}
}

// consumePartialLine handles a piece of the current line (no newlines).
// It emits up to CharLimit characters; further characters are counted
// but not emitted, and a truncation marker is queued for the newline.
func (w *LineCapWriter) consumePartialLine(segment string) {
	if segment == "" {
		return
	}

	if w.suppressing {
		w.suppressedCount += len(segment)
		return
	}

	remaining := w.CharLimit - w.charsInLine
	if remaining <= 0 {
		// Already at the cap from a prior chunk — start suppressing.
		w.startSuppressing(segment)
		return
	}
	if len(segment) <= remaining {
		w.Sink(segment)
		w.charsInLine += len(segment)
		return
	}

	// Emit the head, then start suppressing the tail.
	head := segment[:remaining]
	tail := segment[remaining:]
	w.Sink(head)
	w.charsInLine += len(head)
	w.startSuppressing(tail)
}

// startSuppressing transitions to the suppressed state and records how
// many chars were dropped at the transition.
func (w *LineCapWriter) startSuppressing(dropped string) {
	w.suppressing = true
	w.suppressedCount = len(dropped)
}

// flushTruncationMarker emits the `… [+N chars]` notice when a
// suppressed line is about to terminate.
func (w *LineCapWriter) flushTruncationMarker() {
	if !w.suppressing {
		return
	}
	w.Sink(fmt.Sprintf(" … [+%d chars]", w.suppressedCount))
}

// Flush is intended for end-of-stream cleanup. If the stream ends
// without a trailing newline on a suppressed line, the marker still
// needs to land so the user sees what was dropped.
func (w *LineCapWriter) Flush() {
	if w.suppressing {
		w.flushTruncationMarker()
		w.suppressing = false
		w.suppressedCount = 0
	}
}
