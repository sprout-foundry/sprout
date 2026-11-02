package console

// hold stops the spinner and keeps it stopped while a prompt owns the
// terminal. Events from work still running underneath the prompt (a parallel
// tool's ToolStart, a subagent's activity) call Start, which would otherwise
// draw the spinner over the question the user is answering; while held, Start
// only records the message and Stop clears it. release hands back what the
// spinner should show once the prompt is gone.
//
// Returns the message and active state from before the hold.
func (a *ActivityIndicator) hold() (string, bool) {
	if a == nil {
		return "", false
	}
	msg, wasActive := a.ActiveMessage()
	a.Stop()
	a.mu.Lock()
	a.held = true
	a.heldMsg, a.heldWant = msg, wasActive
	a.mu.Unlock()
	return msg, wasActive
}

// release ends a hold and reports the message the spinner should resume
// with: the latest Start since the hold (or the pre-hold spinner), unless a
// Stop came after it.
func (a *ActivityIndicator) release() (string, bool) {
	if a == nil {
		return "", false
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	a.held = false
	msg, want := a.heldMsg, a.heldWant
	a.heldMsg, a.heldWant = "", false
	return msg, want
}
