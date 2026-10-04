package console

import (
	"strings"
)

const maxDropdownRows = 5

// CompletionCandidate is a single autocomplete suggestion with an
// optional description shown alongside the command name in the
// dropdown.
type CompletionCandidate struct {
	Text        string
	Description string
	// Display, when set, is shown in the dropdown in place of Text (an
	// @file candidate's Text is the whole completed line).
	Display string
}

// RichCompletionProvider returns structured candidates that include a
// description for each completion. When installed on an InputReader,
// the live dropdown uses this instead of the plain CompletionProvider
// to render richer hints.
type RichCompletionProvider func(line string, cursorPos int) []CompletionCandidate

// inlineAutocomplete manages the live slash-command dropdown state
// machine: candidates, selection, accept, hide. It is owned by both
// InputReader and SteerInputReader and activated only when the current
// line starts with "/". Rendering is footer-driven — the candidate
// rows + input line are drawn as a pinned block above the prompt
// (steer-panel style) by the StatusFooter.
type inlineAutocomplete struct {
	// visible tracks whether the dropdown is currently rendered.
	visible bool
	// candidates are the filtered completion entries currently shown.
	candidates []CompletionCandidate
	// selected is the 0-based index into candidates.
	selected int
	// lastLine tracks the input line from the previous update call.
	// When the line changes, selection resets to the top candidate.
	lastLine string
	// dismissedLine, when non-empty, suppresses the dropdown for the
	// given line. Set by Escape/Tab-accept so the dropdown doesn't
	// immediately reappear on the next render (which would re-invoke
	// the completer and show the same candidates). Cleared when the
	// line changes via any edit.
	dismissedLine string
	// fileMode is set while the candidates are @file paths rather than
	// slash commands.
	fileMode bool
}

// newInlineAutocomplete returns a zero-value manager (hidden).
func newInlineAutocomplete() *inlineAutocomplete {
	return &inlineAutocomplete{}
}

// update recomputes candidates and decides whether the dropdown should
// be visible. Called after each buffer mutation. Prefers the rich
// provider when set, falling back to the plain completer.
func (a *inlineAutocomplete) update(line string, cursorPos int, completer CompletionProvider, richCompleter RichCompletionProvider) {
	if cursorPos != len(line) {
		a.hide()
		return
	}
	if _, ok := atFileToken(line); ok {
		a.updateFiles(line)
		return
	}
	if !strings.HasPrefix(line, "/") {
		a.hide()
		return
	}

	// Suppress the dropdown if the user explicitly dismissed it
	// (Escape or Tab-accept) for this exact line. Any edit changes
	// the line, clearing the suppression.
	if a.dismissedLine == line {
		a.hide()
		return
	}

	// Short-circuit when the line hasn't changed, the dropdown is already
	// visible, and we have a completer. Assumes the completer is
	// deterministic for the same input — completers that consult external
	// state must invalidate this cache themselves.
	if a.visible && !a.fileMode && line == a.lastLine && (richCompleter != nil || completer != nil) {
		return
	}

	var candidates []CompletionCandidate

	if richCompleter != nil {
		candidates = richCompleter(line, cursorPos)
	}
	if candidates == nil && completer != nil {
		candidates = plainToCandidates(completer(line, cursorPos))
	}

	a.fileMode = false
	a.show(line, candidates)
}

// updateFiles shows the @file candidates for line.
func (a *inlineAutocomplete) updateFiles(line string) {
	if a.dismissedLine == line {
		a.hide()
		return
	}
	if a.visible && a.fileMode && line == a.lastLine {
		return
	}
	a.show(line, atFileCandidates(line))
	a.fileMode = a.visible
}

// show makes candidates the visible list for line, hiding the dropdown
// when there are none.
func (a *inlineAutocomplete) show(line string, candidates []CompletionCandidate) {
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

// formatDropdownRow renders a single autocomplete candidate as a
// dropdown row. The selected row uses a "▶" marker prefix; unselected
// rows use two spaces. This deliberately avoids embedding ANSI escape
// codes (reverse video) in the text because the footer's WrapSteerLayout
// is not ANSI-aware — ANSI bytes would be counted as visible width and
// cause incorrect wrapping/truncation.
func formatDropdownRow(c CompletionCandidate, selected bool, cols int) string {
	const marker = "▶ "
	const markerOff = "  "
	prefix := markerOff
	if selected {
		prefix = marker
	}

	label := c.Text
	if c.Display != "" {
		label = c.Display
	}
	body := " " + label
	if c.Description != "" {
		body = body + "  " + c.Description
	}
	budget := cols - displayWidth(prefix)
	if budget < 1 {
		budget = 1
	}
	body = truncateLinePreservingANSI(body, budget)
	return prefix + body
}

// hide marks the dropdown as invisible and clears candidate state.
// Does NOT tear down a rendered pinned block — the caller's
// refreshLocked detects the hidden state and calls the footer's
// ClearSteerLineLocked to release the reserved rows.
func (a *inlineAutocomplete) hide() {
	a.visible = false
	a.candidates = nil
	a.selected = 0
	a.lastLine = ""
	a.fileMode = false
}

// dismiss hides the dropdown AND suppresses it from reappearing for
// the given line. Used by Escape and Tab-accept so the dropdown doesn't
// immediately show again on the next render cycle. Any edit that
// changes the line clears the suppression.
func (a *inlineAutocomplete) dismiss(line string) {
	a.hide()
	a.dismissedLine = line
}

// settleAfterAccept closes the list after a Tab/Enter accept. A slash
// command stays dismissed for the accepted line; an @path may keep going
// (a directory lists its contents), so it is only hidden and the next
// update decides.
func (a *inlineAutocomplete) settleAfterAccept(line string, wasFileMode bool) {
	if wasFileMode {
		a.hide()
		return
	}
	a.dismiss(line)
}

// accept returns the currently selected candidate's text, or "" if none.
func (a *inlineAutocomplete) accept() string {
	if a == nil || !a.visible || a.selected < 0 || a.selected >= len(a.candidates) {
		return ""
	}
	return a.candidates[a.selected].Text
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
