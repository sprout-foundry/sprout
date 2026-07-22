package console

// handleEvent dispatches a parsed InputEvent (produced by the shared
// EscapeParser) to the appropriate steer reader action. This replaces
// the former hand-rolled escape-sequence / UTF-8 / CSI parsing that
// lived in this file.
func (r *SteerInputReader) handleEvent(event *InputEvent) {
	// SP-078 Phase 3: when the slash-command dropdown is visible, the
	// user expects Up/Down to navigate candidates and Tab to accept
	// the selection — not recall history or toggle submit mode. We
	// gate on both `autocomplete != nil` and `autocomplete.visible`
	// so a stale "visible" flag doesn't shadow normal navigation
	// when the buffer no longer starts with "/".
	if r.autocomplete != nil && r.autocomplete.visible {
		switch event.Type {
		case EventUp:
			r.navigateDropdown(-1)
			return
		case EventDown:
			r.navigateDropdown(1)
			return
		case EventTab:
			r.acceptDropdown()
			return
		case EventEscape:
			r.hideDropdown()
			return
		}
	}
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

// navigateDropdown moves the dropdown selection by delta (-1 for up,
// +1 for down), wrapping around at the boundaries. Re-renders so
// the new selection's reverse-video highlight appears immediately.
// Caller must NOT hold r.mu.
func (r *SteerInputReader) navigateDropdown(delta int) {
	r.mu.Lock()
	if r.autocomplete != nil && r.autocomplete.visible {
		r.autocomplete.moveSelection(delta)
	}
	r.mu.Unlock()
	r.renderLine()
}

// acceptDropdown replaces the steer buffer with the currently selected
// dropdown candidate (mirrors the InputReader's Tab-accept behavior
// at input_core_event.go:EventTab). Dismisses the dropdown for the
// current line so it doesn't immediately reappear on the next render.
// No-op when the dropdown has no visible selection.
//
// Caller must NOT hold r.mu.
func (r *SteerInputReader) acceptDropdown() {
	r.mu.Lock()
	if r.autocomplete == nil || !r.autocomplete.visible {
		r.mu.Unlock()
		return
	}
	text := r.autocomplete.accept()
	if text == "" {
		r.mu.Unlock()
		return
	}
	r.buffer = []byte(text)
	r.cursorPos = len(r.buffer)
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.resetCompletionCycleLocked()
	// Dismiss for the ACCEPTED text so the dropdown doesn't reappear
	// for the same slash command the user just selected.
	r.autocomplete.dismiss(string(r.buffer))
	r.mu.Unlock()
	r.renderLine()
}

// hideDropdown dismisses the dropdown without changing the buffer.
// The dropdown stays suppressed for the current line until the user
// edits the buffer.
// Caller must NOT hold r.mu.
func (r *SteerInputReader) hideDropdown() {
	r.mu.Lock()
	if r.autocomplete != nil {
		r.autocomplete.dismiss(string(r.buffer))
	}
	r.mu.Unlock()
	r.renderLine()
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
