package console

// handleEvent dispatches a parsed InputEvent (produced by the shared
// EscapeParser) to the appropriate steer reader action. This replaces
// the former hand-rolled escape-sequence / UTF-8 / CSI parsing that
// lived in this file.
func (r *SteerInputReader) handleEvent(event *InputEvent) {
	switch event.Type {
	case EventChar:
		r.insertAtCursor([]byte(event.Data))
	case EventBackspace:
		r.handleBackspace()
	case EventDelete:
		r.deleteForward()
	case EventEnter:
		r.handleSubmit()
	case EventTab:
		r.toggleSubmitMode()
	case EventUp:
		r.recallHistory(-1)
	case EventDown:
		r.recallHistory(1)
	case EventLeft:
		r.moveCursorBackward()
	case EventRight:
		r.moveCursorForward()
	case EventHome:
		r.moveCursorStart()
	case EventEnd:
		r.moveCursorEnd()
	case EventWordLeft:
		r.moveWord(-1)
	case EventWordRight:
		r.moveWord(1)
	case EventDeleteWordBackward:
		r.deleteWordBackward()
	case EventEscape:
		r.clearBuffer()
	case EventInterrupt:
		r.handleInterrupt()
	case EventPasteStart:
		r.beginPaste()
	case EventPasteEnd:
		r.endPaste()
	case EventMouse:
		// Mouse events are not supported in steer mode — swallow.
	}
}

// clearBuffer clears the steer buffer (plain ESC key).
func (r *SteerInputReader) clearBuffer() {
	r.mu.Lock()
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.mu.Unlock()
	r.renderLine()
}

// handleInterrupt fires the user-provided callback (typically wired to
// chatAgent.TriggerInterrupt) and clears the buffer. Stays active —
// the user can keep typing or hit Ctrl+C again.
func (r *SteerInputReader) handleInterrupt() {
	r.mu.Lock()
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.historyIndex = -1
	r.pendingBuffer = nil
	cb := r.interruptFn
	r.mu.Unlock()
	r.renderLine()
	if cb != nil {
		cb()
	}
}

// handleSubmit fires the appropriate callback for the current submit
// mode and clears the line. Empty submissions are dropped (no-op) so
// users can hit Enter on an empty line without sending noise to the
// agent. Non-empty submissions are appended to the steer history ring
// regardless of mode so up-arrow recall works across both.
func (r *SteerInputReader) handleSubmit() {
	r.mu.Lock()
	text := string(r.buffer)
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	mode := r.submitMode
	submit := r.submitFn
	queue := r.queueFn
	r.mu.Unlock()
	r.renderLine()
	if text == "" {
		return
	}
	r.appendHistory(text)
	if mode == SteerSubmitModeQueue && queue != nil {
		queue(text)
		return
	}
	if submit != nil {
		submit(text)
	}
}

// toggleSubmitMode flips between STEER and QUEUE modes (Tab key).
// No-op when no queueFn is wired — the reader is built without queue
// support in that case and the user shouldn't see a Tab affordance.
func (r *SteerInputReader) toggleSubmitMode() {
	r.mu.Lock()
	if r.queueFn == nil {
		r.mu.Unlock()
		return
	}
	if r.submitMode == SteerSubmitModeNow {
		r.submitMode = SteerSubmitModeQueue
	} else {
		r.submitMode = SteerSubmitModeNow
	}
	r.mu.Unlock()
	r.renderLine()
}
