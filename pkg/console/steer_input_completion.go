package console

// SetCompleter installs a completion provider for the steer panel
// (SP-078 Phase 2). Bound to Ctrl-] (the only free completion binding
// — Tab is reserved for STEER ↔ QUEUE mode toggle). The provider
// receives the current buffer + cursor position and returns ordered
// candidate replacements. Pass nil to disable completion.
//
// Mirrors (*InputReader).SetCompleter in pkg/console/input_completion.go;
// the underlying cycle state machine is shared via completion.go.
func (r *SteerInputReader) SetCompleter(c CompletionProvider) {
	r.mu.Lock()
	r.completer = c
	if r.completionCycle != nil {
		r.completionCycle.Reset()
	}
	r.mu.Unlock()
}

// handleSteerCompletion is the Ctrl-] dispatch. It cycles through
// candidates from the configured completer (same UX as InputReader's
// Tab cycle): the first press applies the first candidate; subsequent
// presses with no intervening edit cycle through candidates; any edit
// to the buffer starts a fresh cycle. Silent no-op when no completer
// is installed or the completer returns zero candidates.
//
// Callers must NOT hold r.mu.
func (r *SteerInputReader) handleSteerCompletion() {
	r.mu.Lock()
	if r.completer == nil {
		r.mu.Unlock()
		return
	}
	if r.completionCycle == nil {
		r.completionCycle = &CompletionCycle{}
	}
	line := string(r.buffer)
	newLine, newCursorPos, ok := CycleCompletion(r.completionCycle, line, r.cursorPos, r.completer)
	if !ok {
		r.mu.Unlock()
		return
	}
	r.buffer = []byte(newLine)
	r.cursorPos = newCursorPos
	if r.cursorPos > len(r.buffer) {
		r.cursorPos = len(r.buffer)
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
	r.completionCycle.Advance(newLine)
	r.mu.Unlock()
	r.renderLine()
}

// resetCompletionCycleLocked clears the active completion cycle so
// the next Ctrl-] press starts fresh from the current buffer. Caller
// MUST hold r.mu.
func (r *SteerInputReader) resetCompletionCycleLocked() {
	if r.completionCycle != nil {
		r.completionCycle.Reset()
	}
}

// SubmitMode reports the current Enter-binding. Exposed for tests.
func (r *SteerInputReader) SubmitMode() SteerSubmitMode {
	if r == nil {
		return SteerSubmitModeNow
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.submitMode
}
