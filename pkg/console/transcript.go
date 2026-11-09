package console

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
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

// Transcript lines are mirrored for Resize. A pinned footer forces resize
// handling to guess where the terminal's own reflow left the stale footer
// rows; a wrong guess erases conversation lines, which is what SP-171c is
// about. Re-emitting the transcript from a mirror of the conversation area
// removes the guess.
//
// The mirror holds the transcript's lines, oldest first, trimmed to what the
// conversation area can show: a line that no longer fits is a line the terminal
// just scrolled out. Content the console did not write (`!` output, a child
// that took the TTY) enters it as an unknown row, which blocks repainting while
// it is still on screen — see transcriptEntry.
//
// Locking: outputMu is held for the whole write, so the mirror's line breaks
// match the order the bytes reached the terminal; transcriptRingMu nests inside
// it and nothing takes it alone.
type transcriptEntry struct {
	line string
	// unknown marks a row the console cannot reproduce. One is counted even
	// when the child printed more: the mirror then holds MORE rows than the
	// screen does, which only ever defers a repaint, never claims a row the
	// terminal has already scrolled away.
	unknown bool
}

var (
	transcriptRingMu sync.Mutex
	transcriptRing   []transcriptEntry
	// transcriptPartial is the line in progress: bytes written since the last
	// newline (streaming prose, an interrupted line). It joins the ring only
	// when the newline arrives, but it does occupy a row until then.
	transcriptPartial string
	// transcriptFallbackCapacity stands in when no footer reports a
	// conversation area: a generous screen, not a real cap.
	transcriptFallbackCapacity = 200
)

// appendTranscriptLines records s in the mirror. Caller MUST hold outputMu.
//
// It owns transcriptAtLineStart: the mirror's capacity depends on whether the
// cursor sits on a fresh row, and the transcript has several writers (the turn
// renderer prints its rows directly rather than through WriteTranscript), so
// deriving the flag from the bytes here keeps every writer honest.
func appendTranscriptLines(s string) {
	if s == "" {
		return
	}
	transcriptAtLineStart.Store(strings.HasSuffix(s, "\n"))
	// Geometry first: transcriptCapacity reads the footer's mutex, and the
	// mirror's lock stays the innermost one in the package.
	cols, capacity := transcriptCapacity()
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	for len(s) > 0 {
		i := strings.IndexByte(s, '\n')
		if i < 0 {
			transcriptPartial += strings.TrimSuffix(s, "\r")
			break
		}
		transcriptPartial += strings.TrimSuffix(s[:i], "\r")
		transcriptRing = append(transcriptRing, transcriptEntry{line: transcriptPartial})
		transcriptPartial = ""
		s = s[i+1:]
	}
	trimTranscriptMirrorLocked(cols, capacity)
}

// markTranscriptUnmirrored records that something outside the console wrote at
// least one row into the conversation area. Those rows cannot be re-emitted, so
// a resize repaint that would cover them is refused while any of them is still
// on screen.
func markTranscriptUnmirrored() {
	cols, capacity := transcriptCapacity()
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	transcriptRing = append(transcriptRing, transcriptEntry{unknown: true})
	transcriptPartial = ""
	trimTranscriptMirrorLocked(cols, capacity)
}

// MarkTranscriptUnmirrored is the exported form, for the seams where a child
// process is handed the terminal — a `!` command, an editor escape.
//
// It takes no lock: those seams straddle outputMu (an editor escape runs with
// it, a shell command without it). A mark that lands out of order with a
// concurrent transcript write only adds an unknown row, which defers a repaint;
// it can never claim a row the terminal has already scrolled away.
func MarkTranscriptUnmirrored() { markTranscriptUnmirrored() }

// trimTranscriptMirrorToAreaLocked drops the lines the conversation area can no
// longer show after the terminal changed size. Caller MUST hold outputMu.
//
// A resize moves rows across the boundary between the screen and scrollback that
// no transcript write tells the mirror about: a shrink pushes the oldest rows off
// the top. If the mirror kept them, a later grow would re-emit them under the
// copy the terminal filed in scrollback — the same text twice. This is the same
// rule appendTranscriptLines applies after every write, applied to a geometry
// change instead of to new bytes.
func trimTranscriptMirrorToAreaLocked() {
	cols, capacity := transcriptCapacity()
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	trimTranscriptMirrorLocked(cols, capacity)
}

// trimTranscriptMirrorLocked drops the oldest rows that no longer fit in the
// conversation area. Caller MUST hold transcriptRingMu.
func trimTranscriptMirrorLocked(cols, capacity int) {
	rows := mirrorRowsLocked(cols)
	for len(transcriptRing) > 0 && rows > capacity {
		rows -= mirrorEntryRowsLocked(transcriptRing[0], cols)
		transcriptRing = transcriptRing[1:]
	}
}

func mirrorEntryRowsLocked(e transcriptEntry, cols int) int {
	if e.unknown {
		return 1
	}
	return physicalRowsForLine(e.line, cols)
}

func mirrorRowsLocked(cols int) int {
	rows := 0
	if transcriptPartial != "" {
		rows = physicalRowsForLine(transcriptPartial, cols)
	}
	for _, e := range transcriptRing {
		rows += mirrorEntryRowsLocked(e, cols)
	}
	return rows
}

// TranscriptRepaint returns the lines a resize should re-emit into the
// conversation area, oldest first, plus the line in progress and the number of
// physical rows they occupy at the terminal's CURRENT width. ok is false when
// nothing is mirrored, the area holds a row the console never wrote, or the
// lines no longer fit the area.
//
// The caller adds the condition that actually makes a repaint safe: that the
// mirror covered the WHOLE area before the resize. See
// StatusFooter.replayTranscriptLocked.
func TranscriptRepaint() (lines []string, partial string, contentRows int, ok bool) {
	cols, capacity := transcriptCapacity()
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	for _, e := range transcriptRing {
		if e.unknown {
			return nil, "", 0, false
		}
	}
	rows := mirrorRowsLocked(cols)
	if rows > capacity {
		// Lines have scrolled off the top. Re-emitting them would print them a
		// second time under the copy scrollback kept.
		return nil, "", 0, false
	}
	if len(transcriptRing) == 0 {
		return nil, "", 0, false
	}
	lines = make([]string, 0, len(transcriptRing))
	for _, e := range transcriptRing {
		lines = append(lines, e.line)
	}
	return lines, transcriptPartial, rows, true
}

// TranscriptRowsAt is how many physical rows the mirrored conversation occupies
// at a given width. Resize needs it at the OLD width to know how far down the
// block it is about to replace actually reached; counting the same lines at the
// NEW width would place the clear's top inside the old block on a narrowing, or
// above it on a widening.
func TranscriptRowsAt(cols int) (rows int, known bool) {
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	for _, e := range transcriptRing {
		if e.unknown {
			return 0, false
		}
	}
	return mirrorRowsLocked(cols), true
}

// TranscriptContentRows is how many physical rows of conversation the mirror
// believes the area is holding, and whether that is known at all (false when the
// area contains a row the console did not write, or nothing is mirrored).
func TranscriptContentRows() (rows int, known bool) {
	cols, _ := transcriptCapacity()
	return TranscriptRowsAt(cols)
}

// ResetTranscriptMirror forgets the conversation area. Use it when the screen
// is handed over wholesale — a pager, a full-screen editor — or cleared
// outright, so a later resize does not replay lines the user can no longer
// reach.
func ResetTranscriptMirror() {
	LockOutput()
	defer UnlockOutput()
	resetTranscriptMirrorLocked()
}

// resetTranscriptMirrorLocked is ResetTranscriptMirror for callers holding
// outputMu.
func resetTranscriptMirrorLocked() {
	transcriptRingMu.Lock()
	defer transcriptRingMu.Unlock()
	transcriptRing = nil
	transcriptPartial = ""
	transcriptAtLineStart.Store(true)
}

// transcriptCapacity is the conversation area's capacity in physical rows, and
// the width its lines wrap at. The row the cursor is parked on counts only when
// it carries content: a newline-terminated write leaves it blank, so the area
// shows one row fewer.
//
// The capacity must not be generous. A mirror holding lines the terminal has
// already scrolled off re-emits them on a later grow, under the copy scrollback
// kept — the same text twice on screen.
func transcriptCapacity() (cols, rows int) {
	footer := GetGlobalStatusFooter()
	if footer == nil {
		return liveTerminalWidth(), transcriptFallbackCapacity
	}
	cols, _ = footer.terminalSize()
	bottom := footer.TranscriptBottomRow()
	if bottom <= 1 {
		return cols, transcriptFallbackCapacity
	}
	if transcriptAtLineStart.Load() {
		return cols, bottom - 1
	}
	return cols, bottom
}

// physicalRowsForLine is how many screen rows a transcript line occupies at
// cols: a line wider than the terminal wraps at the column edge, and an empty
// line still takes one row.
func physicalRowsForLine(line string, cols int) int {
	return physicalRows(displayWidth(line), cols)
}

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
	WriteTranscriptAtRowTo(w, 0, s)
}

// WriteTranscriptAtRow writes s into the conversation area starting at the
// absolute screen row `row` (1-based), or at the region's last row when row
// is 0 or there is no pinned region. Use it for a block that must keep its
// first line on screen: a block taller than the region anchored at the
// bottom would otherwise scroll its own head into scrollback.
func WriteTranscriptAtRow(row int, s string) {
	WriteTranscriptAtRowTo(transcriptWriter, row, s)
}

// WriteTranscriptAtRowTo is WriteTranscriptAtRow for an explicit writer.
func WriteTranscriptAtRowTo(w io.Writer, row int, s string) {
	if s == "" {
		return
	}
	LockOutput()
	defer UnlockOutput()
	if activeSelectList != nil {
		writeTranscriptLocked(w, s) // above the open picker; no anchoring
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() && row > 0 {
		bottom := footer.TranscriptBottomRow()
		if bottom > 0 {
			if row > bottom {
				row = bottom
			}
			_, _ = fmt.Fprintf(w, "\033[1;%dr\033[%d;1H\033[K", bottom, row)
			_, _ = io.WriteString(w, hardenNewlines(s))
			transcriptLastRowBlank.Store(strings.TrimRight(s, "\r\n") == "")
			appendTranscriptLines(s)
			return
		}
	}
	writeTranscriptLocked(w, s)
}

// writeTranscriptLocked is the lock-free body of WriteTranscriptTo. Caller
// MUST hold outputMu.
func writeTranscriptLocked(w io.Writer, s string) {
	// A picker is on screen (a question, an approval): output arriving now
	// — a parallel tool's line, a subagent's block — goes above it and the
	// picker is redrawn below, as PrintExternal does. Parking at the
	// region's last row instead would paint over the picker's rows.
	if sl := activeSelectList; sl != nil {
		sl.printExternalLocked(s)
		// Not mirrored: the line sits above a picker frame that owns the rows
		// below it, and the frame is redrawn rather than replayed. The mirror
		// records the row as unknown so a resize will not erase it.
		transcriptAtLineStart.Store(true)
		transcriptLastRowBlank.Store(strings.TrimRight(s, "\r\n") == "")
		markTranscriptUnmirrored()
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(w)
	}
	_, _ = io.WriteString(w, hardenNewlines(s))
	transcriptLastRowBlank.Store(strings.TrimRight(s, "\r\n") == "")
	appendTranscriptLines(s)
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
	if transcriptLastRowBlank.Load() || activeSelectList != nil {
		return // a separator above an open picker is noise
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(transcriptWriter)
	}
	_, _ = io.WriteString(transcriptWriter, "\n")
	transcriptLastRowBlank.Store(true)
	appendTranscriptLines("\n")
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
	if transcriptAtLineStart.Load() || activeSelectList != nil {
		return
	}
	if footer := GetGlobalStatusFooter(); footer != nil && footer.canPinInput() {
		footer.parkForTranscriptLocked(w)
	}
	_, _ = io.WriteString(w, "\n")
	transcriptLastRowBlank.Store(true)
	appendTranscriptLines("\n")
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
	bottom := f.TranscriptBottomRow()
	if bottom <= 0 {
		return
	}
	// \033[K clears the row before writing. Without it, a line that was
	// left half-written (a streaming segment cut short by an interrupt, a
	// tool-log line truncated by a resize) survives under the new text and
	// the two interleave. Parking is a scroll-region operation, so the
	// clear only ever touches the region's last row.
	_, _ = fmt.Fprintf(w, "\033[1;%dr\033[%d;1H\033[K", bottom, bottom)
}

// TranscriptBottomRow is the scroll region's last row: the row the next
// transcript line lands on. 0 when the footer cannot pin rows (not a TTY,
// not started, or a terminal too short for a region plus content).
func (f *StatusFooter) TranscriptBottomRow() int {
	if f == nil || !f.isTTY {
		return 0
	}
	_, rows := f.terminalSize()
	if rows <= 0 {
		return 0
	}
	f.mu.Lock()
	reserved := f.reservedRowsLocked()
	f.mu.Unlock()
	if reserved <= 0 {
		reserved = 2
	}
	if rows < reserved+2 {
		return 0
	}
	return rows - reserved
}

// SetGeometryForTest pins the terminal geometry and marks the footer as a
// live TTY, so tests can exercise the region-relative layout without a pty.
func SetGeometryForTest(f *StatusFooter, cols, rows int) {
	if f == nil {
		return
	}
	f.isTTY = true
	f.sizeOverride = &terminalSizeOverride{cols: cols, rows: rows}
}

// SetActiveForTest marks the footer as started, so region-relative helpers
// behave as they do in a live session.
func (f *StatusFooter) SetActiveForTest() {
	if f == nil {
		return
	}
	f.mu.Lock()
	f.active = true
	f.mu.Unlock()
}

// TranscriptStartRow is the row a block of `lines` transcript lines starts
// on so that it ends on the region's last row, keeping the block's head and
// tail on screen instead of scrolling its beginning into scrollback. 0 when
// there is no pinned region, so the caller writes at the cursor.
func TranscriptStartRow(lines int) int {
	bottom := GetGlobalStatusFooter().TranscriptBottomRow()
	if bottom <= 0 || lines <= 0 {
		return 0
	}
	if start := bottom - lines + 1; start > 1 {
		return start
	}
	return 1
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
