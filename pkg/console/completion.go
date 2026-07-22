package console

// CompletionProvider returns candidate completions for the current input
// state. It receives the current line and cursor position and should return
// a list of full-line replacements ordered by likelihood. An empty result
// means "no completion available."
//
// Shared by InputReader and SteerInputReader (SP-078 Phase 2). The
// implementation in pkg/console/input_completion.go predates this split;
// this file keeps the type definition so the same provider can be
// installed on either reader.
type CompletionProvider func(line string, cursorPos int) []string

// CompletionCycle tracks an in-progress completion cycle. When the user
// presses the completion binding repeatedly without typing other
// characters, we step through candidates in order. When the buffer
// changes (typing, arrow keys, etc.), the next binding press starts a
// fresh cycle automatically because lastApplied no longer matches the
// current line.
//
// This struct is intentionally reader-agnostic; both InputReader and
// SteerInputReader embed it via a pointer field. CycleCompletion drives
// the state machine.
type CompletionCycle struct {
	candidates  []string // ordered candidate replacements
	index       int      // current candidate
	lastApplied string   // buffer after applying candidates[index]
}

// CycleCompletion either advances the existing completion cycle or starts
// a fresh one. Returns the new (line, cursorPos) pair the caller should
// apply, plus an `ok` flag — false when there is no completer installed
// OR the completer returned zero candidates (silent no-op, matches the
// InputReader's existing behavior at input_completion.go:42-46).
//
// `cycle` must be a non-nil pointer to a CompletionCycle owned by the
// caller; CycleCompletion initializes it on the first call and reads
// from it on subsequent calls. Both InputReader.handleTabCompletion and
// SteerInputReader.handleSteerCompletion allocate the cycle lazily on
// first apply. The caller must call cycle.Advance(newLine) after a
// successful apply, and cycle.Reset() whenever the user edits the
// buffer so the next press starts fresh.
func CycleCompletion(cycle *CompletionCycle, line string, cursorPos int, completer CompletionProvider) (newLine string, newCursorPos int, ok bool) {
	if cycle == nil || completer == nil {
		return line, cursorPos, false
	}

	// Continue an existing cycle if the buffer still matches the last
	// completion we applied.
	if cycle.lastApplied != "" && line == cycle.lastApplied {
		cycle.index = (cycle.index + 1) % len(cycle.candidates)
		return cycle.candidates[cycle.index], len(cycle.candidates[cycle.index]), true
	}

	// Start a fresh cycle.
	candidates := completer(line, cursorPos)
	if len(candidates) == 0 {
		cycle.Reset()
		return line, cursorPos, false
	}

	cycle.candidates = candidates
	cycle.index = 0
	cycle.lastApplied = ""
	return candidates[0], len(candidates[0]), true
}

// Advance records that `applied` was just applied to the buffer, so the
// next CycleCompletion call with the same `line` knows to advance
// through candidates instead of starting over.
func (c *CompletionCycle) Advance(applied string) {
	if c == nil {
		return
	}
	c.lastApplied = applied
}

// Reset clears cycle state. Call this whenever the user edits the
// buffer (typing, deleting, paste, history recall) so the next
// completion press starts fresh from the new buffer content.
func (c *CompletionCycle) Reset() {
	if c == nil {
		return
	}
	c.candidates = nil
	c.index = 0
	c.lastApplied = ""
}
