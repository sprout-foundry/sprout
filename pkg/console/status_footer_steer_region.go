package console

import (
	"fmt"
)

// status_footer_steer_region.go — the steer-line region of StatusFooter
// (the pinned input row above the footer), split out of status_footer.go.
// Pure move. (Distinct from status_footer_steer.go, which holds the steer
// row-rendering helpers.)

const maxSteerRows = 6

// applySteerRegionOrDefer handles the activation/row-count-change path
// shared by the steer setters: when prose is streaming, the DECSTBM
// re-apply is deferred (it would displace in-flight prose) and only
// the steer rows render; otherwise orphaned rows are cleared, the
// region is re-applied, and a full draw happens — atomically under
// outputMu, so the sequence can't interleave with prose writes.
// Returns true when the caller's follow-up draw() should be skipped
// (the full draw already happened here).
func (f *StatusFooter) applySteerRegionOrDefer(wasActive bool, prevRows, newRows int) bool {
	f.mu.Lock()
	streaming := f.proseStreaming
	f.mu.Unlock()
	if streaming {
		f.mu.Lock()
		f.pendingSteerRegion = true
		f.mu.Unlock()
		return false
	}
	LockOutput()
	defer UnlockOutput()
	if wasActive && newRows < prevRows {
		f.clearOrphanedSteerRows(prevRows, newRows)
	}
	switch {
	case !wasActive && newRows > prevRows:
		// Opening the panel (turn start, or resuming after a prompt that
		// closed it) claims the region's bottom rows, which hold the newest
		// transcript — the prompt and the user's answer — and the cursor.
		// Lift them above the panel instead of painting over them.
		f.liftRegionContentLocked()
	case newRows > prevRows:
		// Growing an open panel (typing wraps, the dropdown opens) keeps the
		// content in place so keystrokes don't push history into
		// scrollback. The rows it takes are not guaranteed blank: a wrapped
		// footer row or a stale steer frame can be sitting there.
		f.blankNewSteerRows(prevRows, newRows)
	}
	// These setters serve the mid-turn steer panel: the spinner or
	// streaming prose owns a live line, so the cursor must stay on it.
	f.applyScrollRegionKeepingCursorLocked()
	f.drawLocked()
	return true
}

// SetSteerLine reserves one or more pinned rows above the rule and
// renders the supplied text there. Newlines (`\n`) in `text` produce
// additional rows up to maxSteerRows. Called by SteerInputReader as
// the user types — each keystroke replaces the prior content. Safe to
// call repeatedly; the scroll region is re-applied only when the row
// count changes. SP-055.
//
// SP-078: also clears steerWrappedActive so a subsequent legacy
// SetSteerLine after SetSteerLineWrapped reverts to the byte-offset
// render path.
func (f *StatusFooter) SetSteerLine(text string) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	active := f.active
	newRows := f.steerBlockRows()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		// Activation OR row-count change: blank any orphaned rows from
		// the previous size before reapplying the region. Without this,
		// shrinking from 3 rows to 1 would leave the top two rows
		// stranded above the new scroll region.
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWithCursor is like SetSteerLine but also specifies the
// byte offset within text where the input caret (▏) should appear.
// Used by SteerInputReader to render a mid-buffer cursor for readline
// cursor movement (Ctrl-A/E/B/F, Alt-B/F, etc.). An offset of -1
// falls back to caret-at-end (legacy) behavior.
func (f *StatusFooter) SetSteerLineWithCursor(text string, cursorByteOffset int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = cursorByteOffset
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	active := f.active
	newRows := f.steerBlockRows()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWrapped is the SP-078 width-aware variant. text is the
// full steer buffer (already prefixed). cursorRow and cursorCol are
// 0-based indices into the VISUAL row array the footer will render
// after hard-break (\n) split + soft wrap to the terminal width.
//
// Use this when the buffer can exceed the panel width; the legacy
// SetSteerLineWithCursor path splits on \n only and overflows
// horizontally on over-wide lines. cursorRow < 0 is treated as
// "caret at end of last visible row."
//
// The footer reserves enough scroll-region rows for the visual row
// count (capped at maxSteerRows) and shifts the caret row back into
// the visible window when truncation occurs.
func (f *StatusFooter) SetSteerLineWrapped(text string, cursorRow, cursorCol int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = true
	f.steerCursorRow = cursorRow
	f.steerCursorCol = cursorCol
	active := f.active
	newRows := f.steerBlockRows()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		// Defer the scroll-region change to segment end while prose
		// streams: DECSTBM homes the cursor and re-clamps the region,
		// which races with in-flight prose writes inside the region.
		// The extra row is rendered by drawLocked regardless, and
		// SetProseStreaming(false) re-applies the region once prose
		// is done.
		if f.applySteerRegionOrDefer(wasActive, prevRows, newRows) {
			return
		}
	}
	f.draw()
}

// SetSteerLineWrappedLocked is the lock-free variant of
// SetSteerLineWrapped for callers that already hold outputMu (e.g.
// InputReader.refreshLocked runs under LockOutput). It records the
// same wrapped-mode state and re-renders via applyScrollRegionLocked +
// drawLocked instead of applyScrollRegion + draw, which would
// re-acquire the non-reentrant outputMu and deadlock.
func (f *StatusFooter) SetSteerLineWrappedLocked(text string, cursorRow, cursorCol int) {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	prevRows := f.lastSteerRows
	f.steerActive = true
	f.steerLine = text
	f.steerCursor = -1
	f.steerWrappedActive = true
	f.steerCursorRow = cursorRow
	f.steerCursorCol = cursorCol
	active := f.active
	newRows := f.steerBlockRows()
	f.mu.Unlock()
	if !active {
		return
	}
	if !wasActive || newRows != prevRows {
		if wasActive && newRows < prevRows {
			f.clearOrphanedSteerRows(prevRows, newRows)
		}
		// Defer the DECSTBM re-apply while prose streams — same
		// rationale as applySteerRegionOrDefer. This Locked variant's
		// caller already holds outputMu, so the catch-up runs via the
		// pendingSteerRegion goroutine at SetProseStreaming(false).
		f.mu.Lock()
		streaming := f.proseStreaming
		if streaming {
			f.pendingSteerRegion = true
		}
		f.mu.Unlock()
		if !streaming {
			f.shiftScrollRegionLocked()
			f.applyScrollRegionLocked()
		}
	}
	f.drawLocked()
}

// clearOrphanedSteerRows blanks rows that USED to belong to the steer
// panel but won't be rendered this frame because the panel shrank.
// Without this, deleting a `\n` would leave the previous row's text
// stranded above the now-smaller panel. Called with the mutex NOT
// held; it does its own short ANSI write.
func (f *StatusFooter) clearOrphanedSteerRows(prevRows, newRows int) {
	_, rows := f.terminalSize()
	if rows < 3 {
		return
	}
	// SP-115: hint row pushes steer panel up by hintRows.
	f.mu.Lock()
	hintRows := f.hintRowCountLocked()
	f.mu.Unlock()
	// Steer panel occupies rows (rows-1-hintRows-prevRows) .. (rows-2-hintRows).
	// After shrinking, it occupies (rows-1-hintRows-newRows) .. (rows-2-hintRows).
	// Blank the rows in the top of the old panel that the new one doesn't cover.
	fmt.Fprint(f.w, "\0337")
	// Temporarily drop the region so we can address the soon-to-be-
	// scrollable rows directly; applyScrollRegion will re-clamp it.
	fmt.Fprint(f.w, "\033[r")
	for i := 0; i < prevRows-newRows; i++ {
		row := rows - 1 - hintRows - prevRows + i
		if row < 1 {
			continue
		}
		fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
	}
	fmt.Fprint(f.w, "\0338")
}

// blankNewSteerRows blanks the rows the block newly occupies when it grows,
// before the scroll region claims them. The rows were outside the old
// region, so they can still hold a wrapped footer row or a stale steer
// frame; the region is dropped for the write and re-applied by the caller.
func (f *StatusFooter) blankNewSteerRows(prevRows, newRows int) {
	_, rows := f.terminalSize()
	if rows < 3 {
		return
	}
	f.mu.Lock()
	hintRows := f.hintRowCountLocked()
	f.mu.Unlock()
	_, _ = fmt.Fprint(f.w, "\0337")
	_, _ = fmt.Fprint(f.w, "\033[r")
	for i := prevRows; i < newRows; i++ {
		row := rows - 1 - hintRows - i
		if row < 1 {
			continue
		}
		_, _ = fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
	}
	_, _ = fmt.Fprint(f.w, "\0338")
}

// ClearSteerLine drops the steer panel and contracts the scroll region back
// to the idle row count. Called when the SteerInputReader stops (e.g.
// ProcessQuery returned). SP-055.
//
// The pinned block is a contiguous run of rows at the bottom of the
// terminal: the rule, then the text rows, then the hint row and the content
// line. Dropping the text rows and the rule together contracts that run, so
// the transcript keeps every row it had. Blanking the rows first would erase
// the block's top rows, which after the shrink are transcript rows — the
// ones holding the end of the model's reply.
func (f *StatusFooter) ClearSteerLine() {
	if f == nil || !f.isTTY {
		return
	}
	LockOutput()
	defer UnlockOutput()
	f.clearSteerLineLocked()
}

// clearSteerLineLocked is the single clear path: ClearSteerLine,
// ClearSteerLineLocked and the picker teardown all funnel here, so the
// sequence runs under exactly one outputMu acquisition. Caller MUST hold
// outputMu — every step below assumes it, and a second acquisition on this
// goroutine would wedge on the non-reentrant mutex.
func (f *StatusFooter) clearSteerLineLocked() {
	if f == nil || !f.isTTY {
		return
	}
	f.mu.Lock()
	wasActive := f.steerActive
	oldReserved := f.reservedRowsLocked()
	f.steerActive = false
	f.steerLine = ""
	f.steerCursor = -1
	f.steerWrappedActive = false
	f.steerCursorRow = -1
	f.steerCursorCol = 0
	f.lastSteerRows = 0
	active := f.active
	f.mu.Unlock()
	if !active || !wasActive {
		return
	}
	// Re-apply the margins for the smaller block, then repaint the chrome
	// over the rows the block vacated. The full draw clears only the rows
	// of the block it paints now, and above the new bottom margin the
	// vacated rows are transcript — so the pinned rows the block no longer
	// covers are blanked explicitly, whatever the streaming state.
	f.applyScrollRegionKeepingCursorLocked()
	f.drawLocked()
	f.blankVacatedSteerRows(oldReserved)
}

// blankVacatedSteerRows clears the rows the block pinned before it shrank
// and no longer covers. With `reserved` rows pinned, the margins are
// 1..rows-reserved — the bottom margin is the region's last scrolling row,
// not a pinned one — so the vacated rows are rows-oldReserved+1 through
// rows-reserved: the steer rule and text rows, now inside the region and
// still holding chrome. The cursor sat above them in the old region and the
// content did not move, so none of them holds transcript; the rows the
// smaller block still pins are the draw's.
func (f *StatusFooter) blankVacatedSteerRows(oldReserved int) {
	_, rows := f.terminalSize()
	if rows < 3 {
		return
	}
	f.mu.Lock()
	reserved := f.reservedRowsLocked()
	f.mu.Unlock()
	first, last := rows-oldReserved+1, rows-reserved
	if first < 1 || first > last {
		return
	}
	_, _ = fmt.Fprint(f.w, "\0337")
	for row := first; row <= last; row++ {
		_, _ = fmt.Fprintf(f.w, "\033[%d;1H\033[K", row)
	}
	_, _ = fmt.Fprint(f.w, "\0338")
}

// ClearSteerLineLocked is the lock-free variant of ClearSteerLine for
// callers that already hold outputMu (InputReader.refreshLocked, the
// picker's teardown). Both share clearSteerLineLocked, so the clear has
// one implementation and one lock discipline: acquiring outputMu a second
// time on a goroutine that already holds it deadlocks the REPL, which is
// what left the terminal echoing keystrokes with a dead prompt.
func (f *StatusFooter) ClearSteerLineLocked() {
	f.clearSteerLineLocked()
}

// draw renders the pinned footer rows. Always: row N-1 horizontal rule,
// row N content. When a steer line is active: row N-2 steer input,
// row N-1 rule, row N content. Uses save/restore cursor (DEC private mode
// \0337/\0338) so any in-flight prompt rendering above the footer is
// not perturbed.
