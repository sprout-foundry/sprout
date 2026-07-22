package console

import (
	"fmt"
	"strings"
)

const maxAutocompleteRows = 8

// CompletionCandidate is a single autocomplete suggestion with an
// optional description shown alongside the command name in the
// dropdown.
type CompletionCandidate struct {
	Text        string
	Description string
}

// RichCompletionProvider returns structured candidates that include a
// description for each completion. When installed on an InputReader,
// the live dropdown uses this instead of the plain CompletionProvider
// to render richer hints.
type RichCompletionProvider func(line string, cursorPos int) []CompletionCandidate

// inlineAutocomplete manages the live slash-command dropdown that
// appears below the input line as the user types. It is owned by
// InputReader and activated only when the current line starts with "/".
type inlineAutocomplete struct {
	// visible tracks whether the dropdown is currently rendered.
	visible bool
	// candidates are the filtered completion entries currently shown.
	candidates []CompletionCandidate
	// selected is the 0-based index into candidates.
	selected int
	// renderedRows is how many rows were last drawn, so clear() can
	// erase exactly that many lines before a re-render or dismissal.
	renderedRows int
	// lastLine tracks the input line from the previous update call.
	// When the line changes, selection resets to the top candidate.
	lastLine string
}

// newInlineAutocomplete returns a zero-value manager (hidden).
func newInlineAutocomplete() *inlineAutocomplete {
	return &inlineAutocomplete{}
}

// update recomputes candidates and decides whether the dropdown should
// be visible. Called after each buffer mutation. Prefers the rich
// provider when set, falling back to the plain completer.
func (a *inlineAutocomplete) update(line string, cursorPos int, completer CompletionProvider, richCompleter RichCompletionProvider) {
	if (!strings.HasPrefix(line, "/")) || cursorPos != len(line) {
		a.hide()
		return
	}

	// Short-circuit when the line hasn't changed, the dropdown is already
	// visible, and we have a completer. Assumes the completer is
	// deterministic for the same input — completers that consult external
	// state must invalidate this cache themselves.
	if a.visible && line == a.lastLine && (richCompleter != nil || completer != nil) {
		return
	}

	var candidates []CompletionCandidate

	if richCompleter != nil {
		candidates = richCompleter(line, cursorPos)
	}
	if candidates == nil && completer != nil {
		candidates = plainToCandidates(completer(line, cursorPos))
	}

	if len(candidates) == 0 {
		a.hide()
		return
	}

	// Reset selection to top when the input line changed since the
	// last update; preserve it when the line is the same (re-render).
	if line != a.lastLine {
		a.selected = 0
	}
	a.lastLine = line

	a.candidates = candidates
	if a.selected >= len(candidates) {
		a.selected = 0
	}
	a.visible = true
}

func plainToCandidates(ss []string) []CompletionCandidate {
	if len(ss) == 0 {
		return nil
	}
	out := make([]CompletionCandidate, len(ss))
	for i, s := range ss {
		out[i] = CompletionCandidate{Text: s}
	}
	return out
}

// hide marks the dropdown as invisible and clears candidate state.
// Does NOT erase rendered rows from the terminal — the caller must
// invoke clear() (or Refresh → refreshLocked → clear) to erase them.
func (a *inlineAutocomplete) hide() {
	a.visible = false
	a.candidates = nil
	a.selected = 0
	a.lastLine = ""
}

// accept returns the currently selected candidate's text, or "" if none.
func (a *inlineAutocomplete) accept() string {
	if a == nil || !a.visible || a.selected < 0 || a.selected >= len(a.candidates) {
		return ""
	}
	return a.candidates[a.selected].Text
}

// render draws the dropdown below the input cursor position. The caller
// must already hold the output lock and must have just finished drawing
// the input line via refreshInputLine().
func (a *inlineAutocomplete) render() {
	if a == nil || !a.visible || len(a.candidates) == 0 {
		return
	}

	n := len(a.candidates)
	more := 0
	if n > maxAutocompleteRows {
		more = n - maxAutocompleteRows
		n = maxAutocompleteRows
	}

	// Compute the scroll window so the selected item is always visible.
	offset := 0
	if a.selected >= n && more > 0 {
		offset = a.selected - n + 1
		if offset > more {
			offset = more
		}
	}

	// Save cursor position.
	fmt.Print("\0337")

	for i := 0; i < n; i++ {
		idx := i + offset
		if idx >= len(a.candidates) {
			break
		}
		c := a.candidates[idx]
		fmt.Printf("%s\r%s", MoveCursorDownSeq(1), ClearLineSeq())

		if idx == a.selected {
			fmt.Printf("\033[7m %s\033[27m", c.Text)
			if c.Description != "" {
				fmt.Printf(" \033[2;7m%s\033[27m", c.Description)
			}
		} else {
			fmt.Printf("\033[1m %s\033[0m", c.Text)
			if c.Description != "" {
				fmt.Printf(" \033[2m%s\033[0m", c.Description)
			}
		}
	}

	drawnRows := n

	if more > 0 {
		fmt.Printf("%s\r%s\033[2m ↓ %d more\033[0m", MoveCursorDownSeq(1), ClearLineSeq(), more)
		drawnRows++
	}

	// Restore cursor position.
	fmt.Print("\0338")
	a.renderedRows = drawnRows
}

// clear erases any previously drawn dropdown rows. The caller must hold
// the output lock.
func (a *inlineAutocomplete) clear() {
	if a == nil || a.renderedRows == 0 {
		return
	}

	fmt.Print("\0337")
	for i := 0; i < a.renderedRows; i++ {
		fmt.Printf("%s\r%s", MoveCursorDownSeq(1), ClearLineSeq())
	}
	fmt.Print("\0338")
	a.renderedRows = 0
}

// moveSelection changes the selected index by delta (-1 for up, +1 for
// down), wrapping around.
func (a *inlineAutocomplete) moveSelection(delta int) {
	if a == nil || !a.visible || len(a.candidates) == 0 {
		return
	}
	n := len(a.candidates)
	a.selected = (a.selected + delta + n) % n
}
