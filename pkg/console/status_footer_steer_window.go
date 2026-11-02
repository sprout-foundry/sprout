package console

// status_footer_steer_window.go — which rows of a long input the pinned box
// shows. The box is at most maxSteerRows tall; past that it is a window onto
// the full word-wrapped layout that follows the caret, so moving up through a
// long message (Up, Page Up) keeps the caret on screen instead of leaving it
// above the box. Rows hidden above or below are marked on the edge rows.

// composerMoreMarker replaces the continuation marker on an edge row of the
// box when rows are hidden beyond it.
const composerMoreMarker = "⋮ "

// steerWindow is the slice of the box's layout to draw.
type steerWindow struct {
	rows         []string // the rows shown
	top          int      // index of rows[0] in the full layout
	labelRow     int      // index of the label row in the full layout, or -1
	caretRow     int      // in rows; -1 when the caret is not shown
	caretByte    int      // byte offset of the caret in rows[caretRow]
	above, below bool     // rows hidden above / below the window
}

// steerWindowLocked lays out text at the box's wrap width and picks the
// window: the one shown last frame, moved just enough to contain the caret.
// With completion candidates above the input (the label is not on the first
// row) the window stays on the newest rows, where the candidates and input
// are. Caller holds outputMu; f.mu is taken here.
func (f *StatusFooter) steerWindowLocked(text, prefix string, cols int) steerWindow {
	all, _, _ := WrapSteerLayout(text, 0, composerWrapCols(cols), 0)
	labelRow := -1
	for i, row := range all {
		if len(row) >= len(prefix) && row[:len(prefix)] == prefix {
			labelRow = i
			break
		}
	}
	n := min(len(all), maxSteerRows)

	f.mu.Lock()
	caret, caretCol, top := f.steerCursorRow, f.steerCursorCol, f.steerViewTop
	if caret < 0 || caret >= len(all) || labelRow > 0 {
		top = len(all) - n
	} else {
		top = min(max(top, caret-n+1), caret)
	}
	top = min(max(top, 0), len(all)-n)
	f.steerViewTop = top
	f.mu.Unlock()

	w := steerWindow{rows: all[top : top+n], top: top, labelRow: labelRow, caretRow: -1,
		above: top > 0, below: top+n < len(all)}
	switch r := caret - top; {
	case caret < 0 || caret >= len(all):
		// No caret given: it sits at the end of the input.
		w.caretRow, w.caretByte = n-1, -1
	case r >= 0 && r < n:
		w.caretRow, w.caretByte = r, byteIndexAtWidth(w.rows[r], caretCol)
	}
	return w
}

// marker is the gutter for shown row i: a "more" marker on an edge row with
// rows hidden beyond it, the continuation marker on any other row past the
// label, nothing on the label row and the candidate rows above it.
func (w steerWindow) marker(i int) string {
	abs := w.top + i
	if w.labelRow < 0 || abs <= w.labelRow {
		return ""
	}
	if (i == 0 && w.above) || (i == len(w.rows)-1 && w.below) {
		return composerMoreMarker
	}
	return composerContinuationMarker
}
