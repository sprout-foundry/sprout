package console

// InputReader tab-completion. The shared cycle state machine and the
// CompletionProvider type live in completion.go (SP-078 Phase 2). This
// file is the InputReader-specific glue: it holds the per-reader
// completionCycle field, the SetCompleter install hook, and the
// handleTabCompletion dispatch that fires when the user presses Tab at
// the REPL prompt.
//
// The implementation predates SP-078 and has been preserved verbatim
// from input_completion.go@<pre-SP-078>. The refactor in Phase 2
// delegates the cycle bookkeeping to completion.go's CycleCompletion
// so the same logic is reusable from SteerInputReader (Ctrl-] binding).

// SetCompleter installs a completion provider that is invoked when the user
// presses Tab. Pass nil to disable completion. The provider receives the
// current buffer + cursor position and returns ordered candidate replacements.
func (ir *InputReader) SetCompleter(c CompletionProvider) {
	ir.completer = c
}

// SetRichCompleter installs a structured completion provider that
// returns candidates with descriptions. When set, the live autocomplete
// dropdown prefers this over the plain completer.
func (ir *InputReader) SetRichCompleter(rc RichCompletionProvider) {
	ir.richCompleter = rc
}

// resetCompletionCycle clears the active completion cycle so the next
// Tab press starts fresh from the current buffer. Mirrors the SteerInputReader's
// resetCompletionCycleLocked. Should be called whenever the buffer is mutated
// by typing, deleting, history navigation, paste, or at the start of ReadLine.
func (ir *InputReader) resetCompletionCycle() {
	if ir.completionCycle != nil {
		ir.completionCycle.Reset()
	}
}

// handleTabCompletion either starts a new completion cycle or advances
// the existing one. A cycle is "live" as long as the buffer still
// matches the last completion we applied; any other edit (typing,
// arrow keys) leaves the buffer different and the next Tab starts
// fresh.
//
// SP-078: delegates to CycleCompletion in completion.go. The cycle
// struct is allocated on first apply; subsequent Tab presses while the
// buffer still matches `lastApplied` advance through candidates.
func (ir *InputReader) handleTabCompletion() {
	if ir.completionCycle == nil {
		ir.completionCycle = &CompletionCycle{}
	}
	newLine, newCursorPos, ok := CycleCompletion(ir.completionCycle, ir.line, ir.cursorPos, ir.completer)
	if !ok {
		return
	}
	ir.line = newLine
	ir.cursorPos = newCursorPos
	ir.hasEditedLine = true
	ir.historyIndex = -1
	ir.completionCycle.Advance(ir.line)
	ir.Refresh()
}
