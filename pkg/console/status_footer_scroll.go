package console

import (
	"fmt"
	"strings"
)

// ---------------------------------------------------------------------------
// scroll region helpers
// ---------------------------------------------------------------------------

// steerRowCount returns how many footer rows the current steer buffer
// needs. 0 when steer is inactive. Otherwise:
//   - wrapped mode (SP-078): the visual row count of WrapSteerLayout,
//     capped at [1, maxSteerRows]. This is width-aware: a 200-char
//     buffer in an 80-col terminal reserves 3 rows even without any
//     embedded \n.
//   - legacy mode: 1 + (number of \n in the buffer), clamped to
//     [1, maxSteerRows]. Nil-safe so callers can use it on optional
//     footer pointers without guarding.
func (f *StatusFooter) steerRowCount() int {
	if f == nil || !f.steerActive {
		return 0
	}
	if f.steerWrappedActive {
		cols, _ := f.terminalSize()
		if cols <= 0 {
			cols = 80
		}
		// Compute visual rows without cursor mapping: cursorByte=-1
		// still produces a full layout, just with the cursor pinned to
		// the end. Use 0 so WrapSteerLayout still returns all rows.
		rows, _, _ := WrapSteerLayout(f.steerLine, 0, composerWrapCols(cols), 0)
		n := len(rows)
		if n < 1 {
			n = 1
		}
		if n > maxSteerRows {
			n = maxSteerRows
		}
		return n
	}
	lines := strings.Count(f.steerLine, "\n") + 1
	if lines < 1 {
		lines = 1
	}
	if lines > maxSteerRows {
		lines = maxSteerRows
	}
	return lines
}

// steerBlockRows is the height of the pinned input box: its text rows
// plus the dim rule drawn above them. Geometry that clears or reserves
// the box works in block rows, so the rule moves and clears with it.
func (f *StatusFooter) steerBlockRows() int {
	if n := f.steerRowCount(); n > 0 {
		return n + 1
	}
	return 0
}

// hintRowCount returns 1 when the keyboard shortcut hint row is active,
// 0 otherwise. SP-115. Nil-safe so callers can use it on optional
// footer pointers without guarding.
func (f *StatusFooter) hintRowCount() int {
	if f == nil {
		return 0
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hintRowCountLocked()
}

// hintRowCountLocked is hintRowCount for callers already holding f.mu.
// The row shows the keymap hints, or an override in their place — an
// override counts even when the keymap hints are switched off, because it
// is the prompt's own key legend.
func (f *StatusFooter) hintRowCountLocked() int {
	if f.showKeymapHint || f.hintOverride != "" {
		return 1
	}
	return 0
}

// reservedRows returns the number of bottom-pinned rows the footer is
// holding. Always at least 2 (rule + content). When the steer input is
// active, additional rows are reserved above the rule — one row per
// visual line of the steer buffer. When the keymap hint is active,
// one extra row is reserved above the rule for the shortcut hint.
//
// Self-locking so every caller gets a consistent snapshot: the steer
// fields it reads are mutated under f.mu by ClearSteerLine and the
// keystroke handlers.
func (f *StatusFooter) reservedRows() int {
	if f == nil {
		return 2
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reservedRowsLocked()
}

// reservedRowsLocked is reservedRows for callers already holding f.mu.
func (f *StatusFooter) reservedRowsLocked() int {
	return 2 + f.steerBlockRows() + f.hintRowCountLocked()
}

// canPinInput reports whether the footer can host pinned input rows
// (the steer panel or an InputReader dropdown): it is a live TTY and
// has been started. Callers use this to decide whether to route an
// input line through the pinned-block renderer.
func (f *StatusFooter) canPinInput() bool {
	if f == nil || !f.isTTY {
		return false
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.active
}

// applyScrollRegion sets the scroll region to rows 1..(rows-reserved) so the
// bottom pinned rows are excluded. Reserves 2 rows by default (rule + content),
// 3 rows when a steer input is active (steer + rule + content). No-op when
// the terminal is too short for both the footer and any usable scroll area.
func (f *StatusFooter) applyScrollRegion() {
	f.applyScrollRegionLocked()
}

// applyScrollRegionLocked is the lock-free inner body of applyScrollRegion.
// Caller must hold outputMu. Safe to call from printExternalLocked where
// the lock is already held.
func (f *StatusFooter) applyScrollRegionLocked() {
	_, rows := f.terminalSize()
	reserved := f.reservedRows()
	if rows < reserved+1 {
		return
	}
	// DECSTBM: set scroll region. After setting, cursor moves to the
	// home position of the new region (row 1, col 1) per VT100 spec.
	// We then move it just above the footer so subsequent prints land
	// where the user expects (at the bottom of the active scroll area).
	fmt.Fprintf(f.w, "\033[1;%dr", rows-reserved)
	fmt.Fprintf(f.w, "\033[%d;1H", rows-reserved)
	f.mu.Lock()
	f.appliedReserved, f.appliedRows = reserved, rows
	f.mu.Unlock()
}

// applyScrollRegionKeepingCursorLocked re-applies the margins for the
// current reserved-row count while a turn owns the cursor: the spinner or
// streaming prose is mid-line somewhere in the region. applyScrollRegionLocked
// parks the cursor on the region's last row, which strands that line
// whenever it isn't already there. The content does not scroll: the region
// is re-applied around the cursor as it stands, so writing resumes where it
// left off. Caller must hold outputMu.
func (f *StatusFooter) applyScrollRegionKeepingCursorLocked() {
	_, rows := f.terminalSize()
	newReserved := f.reservedRows()
	if rows < newReserved+1 {
		return
	}

	// Every DECRC here restores a row inside the margins in force at that
	// moment: some terminals clamp a restored cursor into the margins, so
	// growing re-saves the raised position before the margins shrink.
	//
	// The content itself does not move here. A caller that grows the block
	// over live transcript lifts it first (liftRegionContentLocked); one
	// that grows it per keystroke (the autocomplete dropdown) deliberately
	// does not, since lifting on every keystroke would push the newest
	// transcript rows into scrollback and shrink the visible history. If
	// the restored cursor ends up below the new margin, it must have been
	// lifted first, or line feeds stall on the last row. Shrinking likewise
	// leaves the vacated rows to the caller (clearOrphanedSteerRows), which
	// blanks them in place.
	var b strings.Builder
	b.WriteString("\0337")
	fmt.Fprintf(&b, "\033[1;%dr\0338", rows-newReserved)
	_, _ = fmt.Fprint(f.w, b.String())

	f.mu.Lock()
	f.appliedReserved, f.appliedRows = newReserved, rows
	f.mu.Unlock()
}

// liftRegionContentLocked scrolls the region's content up by however much
// the pinned block is about to grow, and moves the cursor up with it so a
// mid-line spinner or prose line resumes on its own row. Without the lift
// the block paints over the newest transcript rows and the DECRC in
// applyScrollRegionKeepingCursorLocked restores a cursor that now sits
// below the bottom margin — where a line feed cannot scroll, so every
// following line overwrites the last screen row.
//
// Call it after updating the state reservedRows reads and before
// applyScrollRegionKeepingCursorLocked. Caller must hold outputMu.
func (f *StatusFooter) liftRegionContentLocked() {
	_, rows := f.terminalSize()
	newReserved := f.reservedRows()
	f.mu.Lock()
	oldReserved, oldRows := f.appliedReserved, f.appliedRows
	f.mu.Unlock()
	grow := newReserved - oldReserved
	if oldReserved == 0 || oldRows != rows || grow <= 0 || rows <= newReserved {
		return
	}
	// The old margins are still in force: line feeds on their bottom row
	// scroll the region, and CUU from the restored cursor stops at the top
	// margin.
	_, _ = fmt.Fprintf(f.w, "\0337\033[%d;1H%s\0338\033[%dA",
		rows-oldReserved, strings.Repeat("\n", grow), grow)
}

// shiftScrollRegionLocked moves the scroll region's content to follow a
// change in the pinned block's height. Without it, a growing block (the
// autocomplete dropdown opening, the steer panel appearing) paints over the
// last transcript rows, and a shrinking one leaves its old rows stranded in
// the region. Growth scrolls the content up into scrollback; shrinking
// scrolls it back down so it stays adjacent to the prompt.
//
// Call it after updating the state reservedRows reads and before
// applyScrollRegionLocked. Caller must hold outputMu.
func (f *StatusFooter) shiftScrollRegionLocked() {
	_, rows := f.terminalSize()
	newReserved := f.reservedRows()
	f.mu.Lock()
	oldReserved, oldRows := f.appliedReserved, f.appliedRows
	f.mu.Unlock()
	if oldReserved == 0 || oldRows != rows || newReserved == oldReserved ||
		rows <= newReserved || rows <= oldReserved {
		return
	}
	if grow := newReserved - oldReserved; grow > 0 {
		// The old margins are still in effect, so line feeds on their
		// bottom row scroll the region up.
		_, _ = fmt.Fprintf(f.w, "\033[%d;1H%s", rows-oldReserved, strings.Repeat("\n", grow))
		return
	}
	// Widen the margins first, then reverse-index at the top row: the
	// content moves down and the rows the block vacated fall off the bottom.
	_, _ = fmt.Fprintf(f.w, "\033[1;%dr\033[1;1H%s", rows-newReserved, strings.Repeat("\033M", oldReserved-newReserved))
}

// clearScreenLocked wipes the screen, re-applies the margins and repaints
// the chrome, leaving the cursor on the first row so the transcript starts
// again from the top. Caller must hold outputMu.
func (f *StatusFooter) clearScreenLocked() {
	_, _ = fmt.Fprint(f.w, "\033[r\033[H\033[2J")
	f.applyScrollRegionLocked()
	f.drawFullLocked()
	_, _ = fmt.Fprint(f.w, "\033[1;1H")
}
