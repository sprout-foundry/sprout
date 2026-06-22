package console

// CompletionProvider returns candidate completions for the current input
// state. It receives the current line and cursor position and should return
// a list of full-line replacements ordered by likelihood. An empty result
// means "no completion available."
type CompletionProvider func(line string, cursorPos int) []string

// completionCycle tracks an in-progress Tab cycle. When the user presses
// Tab repeatedly without typing other characters, we step through
// candidates in order. When the buffer changes (typing, arrow keys, etc.),
// the next Tab starts a fresh cycle automatically because lastApplied no
// longer matches the current line.
type completionCycle struct {
	candidates  []string // ordered candidate replacements
	index       int      // current candidate
	lastApplied string   // buffer after applying candidates[index]
}

// SetCompleter installs a completion provider that is invoked when the user
// presses Tab. Pass nil to disable completion. The provider receives the
// current buffer + cursor position and returns ordered candidate replacements.
func (ir *InputReader) SetCompleter(c CompletionProvider) {
	ir.completer = c
}

// handleTabCompletion either starts a new completion cycle or advances the
// existing one. A cycle is "live" as long as the buffer still matches the
// last completion we applied; any other edit (typing, arrow keys) leaves
// the buffer different and the next Tab starts fresh.
func (ir *InputReader) handleTabCompletion() {
	if ir.completer == nil {
		return
	}

	// Continue an existing cycle if the buffer is exactly what we last set.
	if ir.completionCycle != nil && ir.line == ir.completionCycle.lastApplied {
		c := ir.completionCycle
		c.index = (c.index + 1) % len(c.candidates)
		ir.applyCompletion(c.candidates[c.index])
		return
	}

	// Start a fresh cycle from the current buffer.
	candidates := ir.completer(ir.line, ir.cursorPos)
	if len(candidates) == 0 {
		// No matches — clear any stale cycle and silently do nothing.
		ir.completionCycle = nil
		return
	}

	ir.completionCycle = &completionCycle{
		candidates: candidates,
		index:      0,
	}
	ir.applyCompletion(candidates[0])
}

// applyCompletion replaces the buffer with the given completion, moves the
// cursor to the end, refreshes the rendering, and records the applied value
// in the active cycle so subsequent Tabs can detect that we're still cycling.
func (ir *InputReader) applyCompletion(s string) {
	ir.line = s
	ir.cursorPos = len(s)
	ir.hasEditedLine = true
	ir.historyIndex = -1
	if ir.completionCycle != nil {
		ir.completionCycle.lastApplied = s
	}
	ir.Refresh()
}
