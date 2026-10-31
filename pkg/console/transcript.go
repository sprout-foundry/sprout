package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync/atomic"
)

// transcript.go — the single writer for the conversation area.
//
// The status footer owns a DECSTBM scroll region (rows 1..N-reserved) and
// pins its own rows below it. Anything that writes to the terminal outside
// that region lands on the pinned rows; anything that writes while the
// region has been dropped (a child process emitting \033[r, a resize clear,
// a pager) scrolls the whole screen. Both lose transcript.
//
// Before this file, seven writers each carried their own cursor discipline
// and only some of them knew about the region. WriteTranscript is the one
// place that does: it parks the cursor on the region's last row, re-asserts
// the region if it drifted, and writes. Line-oriented callers get blank-line
// discipline for free via BlankLine.

// transcriptWriter is where transcript text goes. os.Stdout in production;
// tests swap it to capture exactly what the user would see.
var transcriptWriter io.Writer = os.Stdout

// transcriptAtLineStart records whether the last byte written to the
// transcript was a newline, so BlankLine can avoid manufacturing empty rows.
var transcriptAtLineStart atomic.Bool

// transcriptLastRowBlank records whether the row the cursor currently sits on
// is empty, i.e. the previous write ended with a blank line. SectionBreak uses
// it to collapse runs of separators.
var transcriptLastRowBlank atomic.Bool

func init() {
	// A fresh session starts at the top of the screen: the cursor is on a
	// fresh row, so a leading blank line would be wasted.
	transcriptAtLineStart.Store(true)
	transcriptLastRowBlank.Store(false)
}

// SetTranscriptWriterForTest redirects transcript output to w and returns a
// restore function. Tests must call the restore.
func SetTranscriptWriterForTest(w io.Writer) (restore func()) {
	prev := transcriptWriter
	prevAtStart := transcriptAtLineStart.Load()
	prevBlank := transcriptLastRowBlank.Load()
	transcriptWriter = w
	transcriptAtLineStart.Store(true)
	transcriptLastRowBlank.Store(false)
	return func() {
		transcriptWriter = prev
		transcriptAtLineStart.Store(prevAtStart)
		transcriptLastRowBlank.Store(prevBlank)
	}
}

// TranscriptAtLineStart reports whether the cursor sits at the start of a
// fresh transcript row, i.e. whether a blank line would actually be visible.
func TranscriptAtLineStart() bool {
	return transcriptAtLineStart.Load()
}

// WriteTranscript writes s into the conversation area.
//
// The write is atomic with respect to every other console chrome write and
// is guaranteed to land inside the scroll region:
//
//   - When the footer is live, the cursor is parked on the region's last row
//     first. Parking (rather than probing with DSR \033[6n) costs one escape
//     sequence and no round-trip: a terminal that ignores the probe would
//     stall the REPL, and a transcript write is line-oriented anyway, so the
//     cursor is going to move.
//   - When the region has been dropped by a child process or a resize, the
//     footer's applied margins no longer describe the screen. Re-applying
//     them here makes every transcript write a self-healing checkpoint, not
//     just every full footer draw.
//
// Bare "\n" is translated to "\r\n": raw mode swallows OPOST, so a lone LF
// leaves the cursor in the same column on the next row (the staircase bug).
func WriteTranscript(s string) {
	WriteTranscriptTo(transcriptWriter, s)
}

// WriteTranscriptTo is WriteTranscript with an explicit underlying terminal
// writer. The footer draws to os.Stderr; callers that print to stderr pass it
// here so the parking sequence and the text reach the same stream.
func WriteTranscriptTo(w io.Writer, s string) {
	if s == "" {
		return
	}
	LockOutput()
	defer UnlockOutput()
	writeTranscriptLocked(w, s)
}

// writeTranscriptLocked is the lock-free body of WriteTranscriptTo. Caller
// MUST hold outputMu.
func writeTranscriptLocked(w io.Writer, s string) {
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(w)
	}
	_, _ = io.WriteString(w, hardenNewlines(s))
	transcriptAtLineStart.Store(strings.HasSuffix(s, "\n"))
	// A write that is itself just a newline leaves the cursor on an empty
	// row; anything with content leaves the cursor on a row that is not.
	transcriptLastRowBlank.Store(strings.TrimRight(s, "\r\n") == "")
}

// BlankLine emits one newline, but only when the transcript is not already
// at the start of a fresh row. Use it where a blank is cosmetic — a turn that
// already ended on a fresh row should not get a second empty one.
func BlankLine() {
	LockOutput()
	defer UnlockOutput()
	blankLineLocked(transcriptWriter)
}

// SectionBreak starts a new visual section: exactly one blank row between the
// previous section and the next, whether or not the previous output already
// ended on a fresh row. That is the difference from BlankLine — at a turn
// boundary the separator is the point, not a nicety. Runs still collapse, so
// two section breaks in a row cannot produce two blank rows.
func SectionBreak() {
	LockOutput()
	defer UnlockOutput()
	if transcriptLastRowBlank.Load() {
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(transcriptWriter)
	}
	_, _ = io.WriteString(transcriptWriter, "\n")
	transcriptLastRowBlank.Store(true)
	transcriptAtLineStart.Store(true)
}

// BlankLineTo is BlankLine for an explicit terminal writer.
func BlankLineTo(w io.Writer) {
	LockOutput()
	defer UnlockOutput()
	blankLineLocked(w)
}

// blankLineLocked is the lock-free body of BlankLine. Caller MUST hold
// outputMu.
func blankLineLocked(w io.Writer) {
	if transcriptAtLineStart.Load() {
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(w)
	}
	_, _ = io.WriteString(w, "\n")
	transcriptAtLineStart.Store(true)
	transcriptLastRowBlank.Store(true)
}

// parkForTranscriptLocked moves the cursor to the last row of the scroll
// region and re-asserts the region's margins. Caller MUST hold outputMu.
//
// Re-asserting unconditionally is deliberate: the footer's appliedReserved /
// appliedRows only describe what the footer last sent, not what the screen
// currently honours. A child process, a pager, or a resize can drop the
// margins without the footer knowing, and the cost of being wrong in the
// other direction (re-sending margins that were already correct) is two
// escape sequences.
func (f *StatusFooter) parkForTranscriptLocked(w io.Writer) {
	_, rows := f.terminalSize()
	if rows <= 0 {
		return
	}
	f.mu.Lock()
	reserved := f.reservedRowsLocked()
	if reserved <= 0 {
		reserved = 2
	}
	f.mu.Unlock()
	if rows < reserved+2 {
		// Terminal too short for a region plus content: nothing sensible to
		// park on. Let the write go out at the cursor.
		return
	}
	bottom := rows - reserved
	// \033[K clears the row before writing. Without it, a line that was
	// left half-written (a streaming segment cut short by an interrupt, a
	// tool-log line truncated by a resize) survives under the new text and
	// the two interleave. Parking is a scroll-region operation, so the
	// clear only ever touches the region's last row.
	_, _ = fmt.Fprintf(w, "\033[1;%dr\033[%d;1H\033[K", bottom, bottom)
}

// hardenNewlines converts bare LF to CRLF so a transcript line starts at
// column 0 even in raw mode. Existing CRLF is left alone.
func hardenNewlines(s string) string {
	if !strings.Contains(s, "\n") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s) + 8)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			b.WriteString("\r\n")
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}
