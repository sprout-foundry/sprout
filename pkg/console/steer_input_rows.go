package console

// steer_input_rows.go — row-wise caret movement in the steer box: Up/Down
// through a wrapped or multi-line message before they reach history, and
// Page Up/Page Down through one longer than the box shows.

// steerPageRows is how far Page Up/Page Down move: one row less than the box
// shows, so a row of context carries over.
const steerPageRows = maxSteerRows - 1

// moveRowsLocked moves the caret delta rows through the box's own layout
// (word-wrapped at composerWrapCols, after the label). Reports whether the
// caret moved; without clamp it stays put on the edge row. Caller holds r.mu.
func (r *SteerInputReader) moveRowsLocked(delta int, clamp bool) bool {
	prefix := SteerPromptPrefix
	if r.submitMode == SteerSubmitModeQueue {
		prefix = QueuePromptPrefix
	}
	cols := 80
	if r.footer != nil {
		if c, _ := r.footer.TerminalSize(); c > 0 {
			cols = c
		}
	}
	positions := composerCaretPositions(prefix+r.line, composerWrapCols(cols))
	b, ok := caretRowMove(positions, len(prefix)+r.cursorPos, delta, clamp)
	if !ok {
		return false
	}
	return r.moveTo(max(b-len(prefix), 0))
}

// moveRowsOrRecall is Up/Down: a row within the message while there is one
// in that direction, history once the caret is on the edge row.
func (r *SteerInputReader) moveRowsOrRecall(delta int) {
	r.mu.Lock()
	moved := r.moveRowsLocked(delta, false)
	r.mu.Unlock()
	if moved {
		r.renderLine()
		return
	}
	r.recallHistory(delta)
}

// pageRows is Page Up/Page Down: a page of rows, or to the start or end of
// the message from the first or last row. Never history.
func (r *SteerInputReader) pageRows(dir int) {
	r.move(func() bool {
		if r.moveRowsLocked(dir*steerPageRows, true) {
			return true
		}
		if dir < 0 {
			return r.moveTo(0)
		}
		return r.moveTo(len(r.line))
	})
}

// clickAt moves the caret to a left-click inside the box's text rows (only
// reported when mouse_click_to_position is on).
func (r *SteerInputReader) clickAt(data string) {
	row, col, ok := leftClickAt(data)
	if !ok || r.footer == nil {
		return
	}
	b, ok := r.footer.SteerCaretAt(row, col)
	if !ok {
		return
	}
	r.move(func() bool {
		prefix := SteerPromptPrefix
		if r.submitMode == SteerSubmitModeQueue {
			prefix = QueuePromptPrefix
		}
		return r.moveTo(min(max(b-len(prefix), 0), len(r.line)))
	})
}
