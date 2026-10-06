package console

// Steer/queue box editing. The text model is the shared editBuffer, so
// every edit behaves exactly as at the idle prompt; this file adds the
// reader's locking, history detach and redraw around it.

// edit applies op under r.mu and redraws when it changed the line.
// Changing edits detach from history navigation and the completion cycle.
func (r *SteerInputReader) edit(op func() bool) {
	r.mu.Lock()
	changed := op()
	if changed {
		r.historyIndex = -1
		r.pendingBuffer = nil
		r.resetCompletionCycleLocked()
	}
	r.mu.Unlock()
	if changed {
		r.renderLine()
	}
}

// move applies a cursor motion under r.mu and redraws.
func (r *SteerInputReader) move(op func() bool) {
	r.mu.Lock()
	changed := op()
	r.mu.Unlock()
	if changed {
		r.renderLine()
	}
}

// insertAtCursor inserts text at the cursor. Caller must NOT hold r.mu.
func (r *SteerInputReader) insertAtCursor(data []byte) {
	r.edit(func() bool {
		r.insertText(string(data))
		return len(data) > 0
	})
}

func (r *SteerInputReader) handleBackspace()    { r.edit(r.deleteRuneBefore) }
func (r *SteerInputReader) deleteForward()      { r.edit(r.deleteRuneAt) }
func (r *SteerInputReader) deleteWordBackward() { r.edit(r.killWordBefore) }
func (r *SteerInputReader) deleteWordForward()  { r.edit(r.killWordAfter) }
func (r *SteerInputReader) killToEnd()          { r.edit(r.editBuffer.killToEnd) }
func (r *SteerInputReader) killToStart()        { r.edit(r.editBuffer.killToStart) }
func (r *SteerInputReader) undoEdit()           { r.edit(r.undo) }
func (r *SteerInputReader) moveCursorBackward() { r.move(func() bool { return r.moveRunes(-1) }) }
func (r *SteerInputReader) moveCursorForward()  { r.move(func() bool { return r.moveRunes(1) }) }
func (r *SteerInputReader) moveCursorStart()    { r.move(func() bool { return r.moveTo(0) }) }
func (r *SteerInputReader) moveCursorEnd()      { r.move(func() bool { return r.moveTo(len(r.line)) }) }
func (r *SteerInputReader) moveWord(delta int) {
	r.move(func() bool { return r.editBuffer.moveWord(delta) })
}

// yank inserts the most recently killed text at the cursor (Ctrl-Y).
func (r *SteerInputReader) yank() {
	r.edit(func() bool {
		r.insertText(r.killBuffer)
		return r.killBuffer != ""
	})
}

// setLineLocked replaces the whole line as one undoable step. Caller MUST
// hold r.mu.
func (r *SteerInputReader) setLineLocked(text string) {
	r.replaceLine(text)
}

// repaint redraws the footer chrome and the box (Ctrl-L mid-turn).
func (r *SteerInputReader) repaint() {
	if r.footer != nil {
		r.footer.Refresh()
	}
	r.renderLine()
}
