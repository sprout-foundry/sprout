package console

// hasEditedHistory resets the history navigation cursor after a buffer
// mutation so subsequent up-arrow recall starts fresh. Must be called
// with r.mu held.
func (r *SteerInputReader) hasEditedHistory() {
	r.historyIndex = -1
	r.pendingBuffer = nil
}

// recallHistory walks the steer-history index by delta. Negative steps
// backward (toward older), positive steps forward (toward newer / the
// live buffer). At the live-buffer boundary it restores the pending
// buffer the user was typing when they started navigating.
func (r *SteerInputReader) recallHistory(delta int) {
	r.mu.Lock()
	if len(r.history) == 0 {
		r.mu.Unlock()
		return
	}

	if r.historyIndex == -1 && delta < 0 {
		// First Up while at live buffer — snapshot current text so we
		// can return to it on later Down.
		snap := make([]byte, len(r.buffer))
		copy(snap, r.buffer)
		r.pendingBuffer = snap
	}

	newIdx := r.historyIndex + delta
	switch {
	case newIdx < 0:
		// Already at oldest — clamp.
		newIdx = len(r.history) - 1
		if r.historyIndex < 0 {
			// First Up press from the live buffer.
			newIdx = 0
		}
	case newIdx >= len(r.history):
		// Walked past the newest entry → back to the live buffer.
		newIdx = -1
	}

	if newIdx == -1 {
		// Restore the pending buffer the user was typing.
		if r.pendingBuffer != nil {
			r.buffer = append(r.buffer[:0], r.pendingBuffer...)
		} else {
			r.buffer = r.buffer[:0]
		}
	} else {
		// history is ordered oldest→newest. UI walks newest-first, so
		// index `i` maps to history[len-1-i].
		entry := r.history[len(r.history)-1-newIdx]
		r.buffer = append(r.buffer[:0], entry...)
	}
	r.cursorPos = len(r.buffer)
	r.historyIndex = newIdx
	r.resetCompletionCycleLocked()
	r.mu.Unlock()
	r.renderLine()
}

// appendHistory pushes a submitted message onto the history ring,
// deduplicating consecutive repeats and capping at SteerHistoryCap.
func (r *SteerInputReader) appendHistory(text string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if text == "" {
		return
	}
	if n := len(r.history); n > 0 && r.history[n-1] == text {
		return // consecutive dup
	}
	r.history = append(r.history, text)
	if over := len(r.history) - SteerHistoryCap; over > 0 {
		r.history = r.history[over:]
	}
	r.historyIndex = -1
	r.pendingBuffer = nil
}
