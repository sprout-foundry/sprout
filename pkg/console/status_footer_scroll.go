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
		rows, _, _ := WrapSteerLayout(f.steerLine, 0, cols, 0)
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

// hintRowCount returns 1 when the keyboard shortcut hint row is active,
// 0 otherwise. SP-115. Nil-safe so callers can use it on optional
// footer pointers without guarding.
func (f *StatusFooter) hintRowCount() int {
	if f == nil {
		return 0
	}
	if f.showKeymapHint {
		return 1
	}
	return 0
}

// reservedRows returns the number of bottom-pinned rows the footer is
// holding. Always at least 2 (rule + content). When the steer input is
// active, additional rows are reserved above the rule — one row per
// visual line of the steer buffer. When the keymap hint is active,
// one extra row is reserved above the rule for the shortcut hint.
func (f *StatusFooter) reservedRows() int {
	return 2 + f.steerRowCount() + f.hintRowCount()
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
}
