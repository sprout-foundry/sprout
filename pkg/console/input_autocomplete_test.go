package console

import (
	"fmt"
	"strings"
	"testing"
)

func mockRichCompleter(line string, _ int) []CompletionCandidate {
	switch line {
	case "/he":
		return []CompletionCandidate{
			{Text: "/help", Description: "Show help"},
			{Text: "/heart", Description: ""},
			{Text: "/heat", Description: "Temperature"},
		}
	case "/per":
		return []CompletionCandidate{
			{Text: "/persona", Description: "Switch persona"},
			{Text: "/persona list", Description: "List personas"},
			{Text: "/persona clear", Description: "Clear persona"},
		}
	case "/xyz":
		return nil
	default:
		return nil
	}
}

func TestAutocomplete_ShowsCandidatesForSlashPrefix(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)

	if !a.visible {
		t.Error("expected dropdown to be visible after typing /he")
	}
	if len(a.candidates) != 3 {
		t.Errorf("expected 3 candidates, got %d", len(a.candidates))
	}
	if a.candidates[0].Text != "/help" {
		t.Errorf("expected first candidate /help, got %q", a.candidates[0].Text)
	}
}

func TestAutocomplete_HidesForNonSlashInput(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("hello world", len("hello world"), nil, mockRichCompleter)

	if a.visible {
		t.Error("dropdown should not be visible for non-slash input")
	}
}

func TestAutocomplete_HidesWhenNoMatches(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/xyz", len("/xyz"), nil, mockRichCompleter)

	if a.visible {
		t.Error("dropdown should not be visible when completer returns no matches")
	}
}

func TestAutocomplete_NavigationChangesSelection(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)

	if a.selected != 0 {
		t.Errorf("expected initial selection 0, got %d", a.selected)
	}

	a.moveSelection(1)
	if a.selected != 1 {
		t.Errorf("expected selection 1 after down, got %d", a.selected)
	}

	a.moveSelection(1)
	if a.selected != 2 {
		t.Errorf("expected selection 2 after second down, got %d", a.selected)
	}

	a.moveSelection(1)
	if a.selected != 0 {
		t.Errorf("expected selection 0 after wrap, got %d", a.selected)
	}

	a.moveSelection(-1)
	if a.selected != 2 {
		t.Errorf("expected selection 2 after up-wrap, got %d", a.selected)
	}
}

func TestAutocomplete_AcceptReturnsSelectedCandidate(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)

	got := a.accept()
	if got != "/help" {
		t.Errorf("accept() = %q, want /help", got)
	}

	a.moveSelection(1)
	got = a.accept()
	if got != "/heart" {
		t.Errorf("accept() after move = %q, want /heart", got)
	}
}

func TestAutocomplete_AcceptReturnsEmptyWhenHidden(t *testing.T) {
	a := newInlineAutocomplete()
	got := a.accept()
	if got != "" {
		t.Errorf("accept() on hidden dropdown should return empty, got %q", got)
	}
}

func TestAutocomplete_HideClearsState(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)

	a.hide()
	if a.visible {
		t.Error("dropdown should not be visible after hide()")
	}
	if len(a.candidates) != 0 {
		t.Error("candidates should be cleared after hide()")
	}
}

func TestAutocomplete_UpdateResetsSelectionWhenBecomingVisible(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)
	a.moveSelection(2)

	a.update("/per", len("/per"), nil, mockRichCompleter)
	if a.selected != 0 {
		t.Errorf("selection should reset to 0 when new candidates appear, got %d", a.selected)
	}
}

func TestAutocomplete_UpdatePreservesSelectionWhenStillVisible(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)
	a.moveSelection(1)

	a.update("/he", len("/he"), nil, mockRichCompleter)
	if a.selected != 1 {
		t.Errorf("selection should be preserved at 1, got %d", a.selected)
	}
}

func TestAutocomplete_NilCompleterHidesDropdown(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", len("/he"), nil, mockRichCompleter)

	if !a.visible {
		t.Fatal("expected dropdown to be visible")
	}

	a.update("/he", len("/he"), nil, nil)
	if a.visible {
		t.Error("dropdown should hide when completer is nil")
	}
}

func TestAutocomplete_CursorNotAtEndHidesDropdown(t *testing.T) {
	a := newInlineAutocomplete()
	a.update("/he", 1, nil, mockRichCompleter)
	if a.visible {
		t.Error("dropdown should hide when cursor is not at end of line")
	}
}

func TestAutocomplete_FallsBackToPlainCompleter(t *testing.T) {
	a := newInlineAutocomplete()
	plain := func(line string, _ int) []string {
		if line == "/he" {
			return []string{"/help", "/heart"}
		}
		return nil
	}
	a.update("/he", len("/he"), plain, nil)

	if !a.visible {
		t.Fatal("expected dropdown to be visible via plain completer")
	}
	if len(a.candidates) != 2 {
		t.Errorf("expected 2 candidates from plain completer, got %d", len(a.candidates))
	}
	if a.candidates[0].Text != "/help" {
		t.Errorf("expected first candidate /help, got %q", a.candidates[0].Text)
	}
}

func TestAutocomplete_RichCompleterPreferredOverPlain(t *testing.T) {
	a := newInlineAutocomplete()
	plainCalled := false
	plain := func(line string, _ int) []string {
		plainCalled = true
		return []string{"/plain-only"}
	}
	rich := func(line string, _ int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/rich-only", Description: "Rich wins"}}
	}
	a.update("/he", len("/he"), plain, rich)

	if plainCalled {
		t.Error("plain completer should not be called when rich completer returns results")
	}
	if a.candidates[0].Text != "/rich-only" {
		t.Errorf("expected rich completer to win, got %q", a.candidates[0].Text)
	}
}

func TestAutocomplete_RichCompleterEmptyFallsBackToPlain(t *testing.T) {
	a := newInlineAutocomplete()
	plain := func(line string, _ int) []string {
		return []string{"/plain-fallback"}
	}
	rich := func(line string, _ int) []CompletionCandidate {
		return nil
	}
	a.update("/he", len("/he"), plain, rich)

	if !a.visible {
		t.Fatal("expected dropdown to be visible via plain fallback")
	}
	if a.candidates[0].Text != "/plain-fallback" {
		t.Errorf("expected plain fallback candidate, got %q", a.candidates[0].Text)
	}
}

func TestAutocomplete_MoreCountCorrect(t *testing.T) {
	a := newInlineAutocomplete()
	many := make([]CompletionCandidate, 12)
	for i := range many {
		many[i] = CompletionCandidate{Text: fmt.Sprintf("/cmd%02d", i)}
	}
	rich := func(_ string, _ int) []CompletionCandidate { return many }
	a.update("/cmd", len("/cmd"), nil, rich)

	if len(a.candidates) != 12 {
		t.Fatalf("expected 12 candidates, got %d", len(a.candidates))
	}
}

// TestAutocomplete_DropdownBlockCandidatesAboveInput verifies the
// above-input pinned rendering that replaced the old below-line
// dropdown: the combined block fed to the footer's pinned rows has the
// candidate rows ABOVE the prompt line, with the selected candidate
// marked with "▶ " and unselected rows indented.
func TestAutocomplete_DropdownBlockCandidatesAboveInput(t *testing.T) {
	candidates := []CompletionCandidate{
		{Text: "/help", Description: "Show help"},
		{Text: "/heart", Description: ""},
		{Text: "/heat", Description: "Temperature"},
	}

	full, cursorRow, cursorCol := buildDropdownBlock("> ", "/he", len("/he"), 80, candidates, 1)
	lines := strings.Split(full, "\n")

	// 3 candidates + 1 input line.
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (3 candidates + 1 input), got %d: %q", len(lines), full)
	}
	// Selected candidate (index 1) has the ▶ marker.
	if !strings.Contains(lines[1], "▶ ") {
		t.Errorf("selected candidate row should have ▶ marker, got %q", lines[1])
	}
	if strings.Contains(lines[0], "▶") || strings.Contains(lines[2], "▶") {
		t.Errorf("unselected candidate rows must not have ▶ marker")
	}
	// Last row is the input line.
	if lines[3] != "> /he" {
		t.Errorf("last row should be the input line, got %q", lines[3])
	}
	// Cursor sits on the input line (last row of the combined layout).
	if cursorRow != 3 {
		t.Errorf("cursorRow = %d, want 3 (input line)", cursorRow)
	}
	// Cursor col includes the prompt prefix width + cursor position.
	if cursorCol != len("> /he") {
		t.Errorf("cursorCol = %d, want %d", cursorCol, len("> /he"))
	}
}

// TestAutocomplete_DropdownBlockCapsAtMaxDropdownRows verifies that
// the pinned block renders at most maxDropdownRows candidates above
// the input line, mirroring the steer panel's cap.
func TestAutocomplete_DropdownBlockCapsAtMaxDropdownRows(t *testing.T) {
	candidates := make([]CompletionCandidate, 20)
	for i := range candidates {
		candidates[i] = CompletionCandidate{Text: fmt.Sprintf("/cmd%02d", i)}
	}

	full, cursorRow, _ := buildDropdownBlock("> ", "/", 1, 80, candidates, 0)
	lines := strings.Split(full, "\n")
	if len(lines) != maxDropdownRows+1 {
		t.Fatalf("expected %d lines (%d candidates + 1 input), got %d",
			maxDropdownRows+1, maxDropdownRows, len(lines))
	}
	// Cursor row accounts for the capped candidate count.
	if cursorRow != maxDropdownRows {
		t.Errorf("cursorRow = %d, want %d (capped candidates + input line)", cursorRow, maxDropdownRows)
	}
}

func TestHandleEvent_HistoryVsAutocompleteRouting(t *testing.T) {
	const slash = "/he"

	tests := []struct {
		name    string
		history []string
		// setup puts the InputReader in the state the scenario starts from
		// (historyIndex/line/hasEditedLine), returning the events to send.
		setup func(ir *InputReader) []*InputEvent
		// assert inspects the post-event state.
		assert func(t *testing.T, ir *InputReader)
	}{
		{
			name:    "Up crosses recalled slash entry to older normal entry",
			history: []string{"normal-old", slash},
			setup: func(ir *InputReader) []*InputEvent {
				return []*InputEvent{
					{Type: EventUp}, // recalls slash entry
					{Type: EventUp}, // must cross it
				}
			},
			assert: func(t *testing.T, ir *InputReader) {
				if ir.line != "normal-old" || ir.historyIndex != 0 {
					t.Errorf("line=%q historyIndex=%d, want normal-old/0", ir.line, ir.historyIndex)
				}
				if ir.autocomplete.visible {
					t.Error("autocomplete should be hidden after leaving the slash entry")
				}
			},
		},
		{
			name:    "Down crosses recalled slash entry to newer entry",
			history: []string{"older", slash, "newer"},
			setup: func(ir *InputReader) []*InputEvent {
				return []*InputEvent{
					{Type: EventUp},   // recalls "newer"
					{Type: EventUp},   // recalls slash entry
					{Type: EventDown}, // must cross to "newer"
				}
			},
			assert: func(t *testing.T, ir *InputReader) {
				if ir.line != "newer" || ir.historyIndex != 2 {
					t.Errorf("line=%q historyIndex=%d, want newer/2", ir.line, ir.historyIndex)
				}
				if ir.autocomplete.visible {
					t.Error("autocomplete should be hidden after leaving the slash entry")
				}
			},
		},
		{
			name:    "Down from newest recalled slash entry exits history",
			history: []string{"older", slash},
			setup: func(ir *InputReader) []*InputEvent {
				return []*InputEvent{
					{Type: EventUp},   // recalls newest slash entry
					{Type: EventDown}, // must exit history
				}
			},
			assert: func(t *testing.T, ir *InputReader) {
				if ir.line != "" || ir.historyIndex != -1 {
					t.Errorf("line=%q historyIndex=%d, want \"\"/-1", ir.line, ir.historyIndex)
				}
				if ir.autocomplete.visible {
					t.Error("autocomplete should be hidden after line clears")
				}
			},
		},
		{
			name:    "Up/Down on edited slash buffer navigates autocomplete",
			history: []string{"history-1", "history-2"},
			setup: func(ir *InputReader) []*InputEvent {
				ir.line = slash
				ir.cursorPos = len(ir.line)
				ir.hasEditedLine = true
				ir.Refresh()
				return []*InputEvent{
					{Type: EventUp},   // wraps 0 → 2
					{Type: EventDown}, // wraps 2 → 0
					{Type: EventDown}, // advances 0 → 1
				}
			},
			assert: func(t *testing.T, ir *InputReader) {
				if ir.autocomplete.selected != 1 {
					t.Errorf("autocomplete.selected=%d, want 1", ir.autocomplete.selected)
				}
				if ir.line != slash {
					t.Errorf("line=%q, want %q (history must not be touched)", ir.line, slash)
				}
				if ir.historyIndex != -1 {
					t.Errorf("historyIndex=%d, want -1", ir.historyIndex)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ir := NewInputReader("> ")
			ir.terminalWidth = 80
			ir.richCompleter = mockRichCompleter
			ir.SetHistory(tt.history)

			for _, ev := range tt.setup(ir) {
				ir.HandleEvent(ev)
			}
			tt.assert(t, ir)
		})
	}
}

// --- Regression tests for autocomplete fixes ---

// TestAutocomplete_HideClearsCandidates verifies that hide() resets the
// candidate state so the next update() starts fresh. Rendering is
// footer-driven now (the old below-line renderRows/clearRows are gone),
// so there is no rendered-row bookkeeping to preserve.
func TestAutocomplete_HideClearsCandidates(t *testing.T) {
	a := &inlineAutocomplete{
		visible:    true,
		selected:   1,
		candidates: []CompletionCandidate{{Text: "/help"}, {Text: "/heart"}},
	}
	a.hide()
	if a.visible {
		t.Error("hide() should clear the visible flag")
	}
	if len(a.candidates) != 0 {
		t.Errorf("hide() should clear candidates, got %d", len(a.candidates))
	}
	if a.selected != 0 {
		t.Errorf("hide() should reset selection to 0, got %d", a.selected)
	}
}

// TestAutocomplete_DropdownHiddenForHistoryRecalledLine verifies Fix 1:
// when a slash-prefixed line is recalled from history (hasEditedLine ==
// false), the dropdown does NOT appear. Arrow keys should navigate
// history, not the dropdown.
func TestAutocomplete_DropdownHiddenForHistoryRecalledLine(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.richCompleter = mockRichCompleter
	ir.SetHistory([]string{"normal", "/help"})

	// Recall the slash entry from history.
	ir.HandleEvent(&InputEvent{Type: EventUp})

	if ir.line != "/help" {
		t.Fatalf("expected /help from history, got %q", ir.line)
	}
	if ir.hasEditedLine {
		t.Error("hasEditedLine should be false after history recall")
	}
	// The dropdown should NOT be visible because hasEditedLine is false.
	// (Before the fix, the dropdown appeared but arrow keys navigated
	// history instead of the dropdown — confusing UX.)
}

// TestAutocomplete_DropdownShowsAfterTypingFromHistory verifies that
// after recalling a slash entry from history, typing a character
// (setting hasEditedLine = true) makes the dropdown appear.
func TestAutocomplete_DropdownShowsAfterTypingFromHistory(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	// Broad mock: returns results for any "/h" prefix.
	ir.richCompleter = func(line string, _ int) []CompletionCandidate {
		if strings.HasPrefix(line, "/h") {
			return []CompletionCandidate{{Text: "/help", Description: "help"}}
		}
		return nil
	}
	ir.SetHistory([]string{"normal", "/he"})

	// Recall "/he" from history.
	ir.HandleEvent(&InputEvent{Type: EventUp})
	if ir.line != "/he" {
		t.Fatalf("expected /he, got %q", ir.line)
	}
	// Dropdown should NOT be visible (hasEditedLine == false).
	if ir.autocomplete.visible {
		t.Error("dropdown should not be visible for history-recalled line")
	}

	// Type 'l' to make it "/hel" — this sets hasEditedLine = true.
	ir.InsertChar("l")
	if !ir.hasEditedLine {
		t.Error("hasEditedLine should be true after typing")
	}

	// Now the dropdown should be visible.
	if !ir.autocomplete.visible {
		t.Error("dropdown should be visible after typing on a slash line")
	}
}

// TestCompletionCycle_ResetsOnInsertChar verifies Fix 2: InsertChar
// resets the completion cycle so a stale lastApplied doesn't cause
// Tab to advance instead of starting fresh.
func TestCompletionCycle_ResetsOnInsertChar(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.completer = func(line string, _ int) []string {
		return []string{"/help", "/heart"}
	}

	// Tab to complete to "/help" and advance the cycle.
	ir.line = "/he"
	ir.cursorPos = 3
	ir.hasEditedLine = true
	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Fatalf("expected /help after first Tab, got %q", ir.line)
	}

	// Simulate backspace + retype to get back to exactly "/help".
	// Without resetCompletionCycle, lastApplied is "/help" and the
	// next Tab would advance to "/heart" from the stale cycle.
	// With the fix, InsertChar resets the cycle so Tab starts fresh
	// and applies the first candidate (/help) again.
	ir.line = "/hel"
	ir.cursorPos = 4
	ir.InsertChar("p") // makes "/help"

	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Errorf("second Tab should start fresh cycle (apply /help), got %q", ir.line)
	}
}

// TestCompletionCycle_ResetsOnTabAcceptFromDropdown verifies Fix 4:
// accepting a candidate via Tab when the dropdown is visible resets
// the completion cycle so a subsequent Tab starts fresh.
func TestCompletionCycle_ResetsOnTabAcceptFromDropdown(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	// Broad completer: returns results for any "/he" prefix, including
	// after accepting /help (so we can detect stale cycle advancement).
	ir.richCompleter = func(line string, _ int) []CompletionCandidate {
		if strings.HasPrefix(line, "/he") {
			return []CompletionCandidate{
				{Text: "/help", Description: "Show help"},
				{Text: "/heart", Description: ""},
				{Text: "/heat", Description: "Temperature"},
			}
		}
		return nil
	}
	ir.completer = func(line string, _ int) []string {
		if strings.HasPrefix(line, "/he") {
			return []string{"/help", "/heart", "/heat"}
		}
		return nil
	}

	// Type "/he" to show the dropdown.
	ir.InsertChar("/")
	ir.InsertChar("h")
	ir.InsertChar("e")

	if !ir.autocomplete.visible {
		t.Fatal("dropdown should be visible after typing /he")
	}

	// Tab accepts the selected candidate (/help).
	ir.HandleEvent(&InputEvent{Type: EventTab})

	if ir.line != "/help" {
		t.Fatalf("expected /help after Tab-accept, got %q", ir.line)
	}

	// Now Tab again — without Fix 4, lastApplied="/help" and the
	// stale cycle would advance to "/heart". With the fix,
	// lastApplied is empty so CycleCompletion starts a fresh cycle
	// and applies the first candidate (/help) again.
	ir.handleTabCompletion()
	if ir.line != "/help" {
		t.Errorf("second Tab should re-apply /help (fresh cycle), got %q — "+
			"stale cycle advanced to next candidate", ir.line)
	}
}

// TestAutocomplete_UpdateShortCircuitsOnSameLine verifies that update()
// doesn't re-invoke the completer when the line hasn't changed and the
// dropdown is already visible — avoids redundant completer calls from
// background Refresh events.
func TestAutocomplete_UpdateShortCircuitsOnSameLine(t *testing.T) {
	calls := 0
	rich := func(line string, _ int) []CompletionCandidate {
		calls++
		return []CompletionCandidate{{Text: "/help", Description: "help"}}
	}
	a := newInlineAutocomplete()

	// First update: shows the dropdown, calls completer.
	a.update("/he", len("/he"), nil, rich)
	if calls != 1 {
		t.Fatalf("expected 1 completer call, got %d", calls)
	}
	if !a.visible {
		t.Fatal("dropdown should be visible")
	}

	// Second update with same line: should short-circuit, no completer call.
	a.update("/he", len("/he"), nil, rich)
	if calls != 1 {
		t.Errorf("expected 1 completer call (short-circuited), got %d", calls)
	}
}

// TestAutocomplete_HideMarksInvisible verifies the Enter-accept /
// Escape pattern: hide() marks the dropdown invisible (state-only, no
// terminal writes). The pinned-block teardown is the footer's job
// (ClearSteerLineLocked), covered in input_render_dropdown_test.go.
func TestAutocomplete_HideMarksInvisible(t *testing.T) {
	a := &inlineAutocomplete{
		visible:    true,
		selected:   0,
		candidates: []CompletionCandidate{{Text: "/help"}},
	}

	a.hide()
	if a.visible {
		t.Error("dropdown should be invisible after hide()")
	}
	if len(a.candidates) != 0 {
		t.Errorf("hide() should clear candidates, got %d", len(a.candidates))
	}
}

// TestEnterHandler_ForceClearsDropdownAfterRefresh verifies the fix for
// the bug where autocomplete dropdown rows persisted on screen after
// Enter:
//
//  1. User types "/he" — the dropdown state machine is visible (the
//     footer renders it above the prompt line).
//  2. User presses Enter — the Enter handler accepts the candidate
//     ("/help"), hides the dropdown state, and calls Refresh().
//  3. Refresh() → refreshLocked() honors suppressAutocompleteNextRefresh
//     by skipping the update() step, so the dropdown stays hidden for
//     the accepted line instead of re-appearing (the rich completer
//     still matches "/help", e.g. for sub-commands).
//
// This test asserts:
//   - After the Enter sequence, visible=false and no dropdown block is
//     re-rendered for the accepted line.
//   - The accepted text ("/help") is on the input line.
//   - The suppression flag is consumed (cleared) by refreshLocked.
//   - The Refresh output contains no post-accept candidate text.
func TestEnterHandler_ForceClearsDropdownAfterRefresh(t *testing.T) {
	// Completer that returns matches for BOTH "/he" (typing phase) AND
	// "/help" (post-accept phase) — this is the scenario where the
	// re-rendered dropdown would stay on screen without the fix.
	rich := func(line string, _ int) []CompletionCandidate {
		switch line {
		case "/he":
			return []CompletionCandidate{
				{Text: "/help", Description: "Show help"},
				{Text: "/heart", Description: ""},
				{Text: "/heat", Description: "Temperature"},
			}
		case "/help":
			return []CompletionCandidate{
				{Text: "/help ", Description: "Show help"},
				{Text: "/help add", Description: "Add help topic"},
				{Text: "/help search", Description: "Search help"},
			}
		}
		return nil
	}

	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.richCompleter = rich

	// Set up: user has typed "/he" and the dropdown is visible.
	ir.InsertChar("/")
	ir.InsertChar("h")
	ir.InsertChar("e")
	if !ir.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible after typing /he")
	}

	// --- Simulate the Enter handler (with the fix). ---
	output := captureStdout(t, func() {
		// Step 1: accept the selected candidate.
		text := ir.autocomplete.accept()
		if text != "" {
			ir.line = text
			ir.cursorPos = len(ir.line)
		}
		// Step 2: hide the dropdown state.
		ir.autocomplete.hide()
		// Step 3: set the suppression flag so refreshLocked skips
		// the update step for the accepted line.
		ir.suppressAutocompleteNextRefresh = true
		// Step 4: Refresh redraws the input line with "/help" but
		// does NOT re-show the dropdown.
		ir.Refresh()
	})

	// After the fix: no dropdown state for the accepted line.
	if ir.autocomplete.visible {
		t.Error("dropdown should be invisible after Enter handler")
	}
	// The suppression flag must be consumed (cleared) by refreshLocked
	// so subsequent Refresh calls behave normally.
	if ir.suppressAutocompleteNextRefresh {
		t.Error("suppressAutocompleteNextRefresh should be cleared after Refresh")
	}

	// The accepted text "/help" should be on the input line.
	if ir.line != "/help" {
		t.Errorf("expected accepted text /help on input line, got %q", ir.line)
	}

	// The output must NOT contain the post-accept dropdown text —
	// "/help search" is a candidate that only appears if the dropdown
	// was re-rendered for the accepted line. The suppression flag
	// should have prevented it.
	if strings.Contains(output, "/help search") {
		t.Errorf("output should not contain post-accept dropdown candidates (/help search); "+
			"suppressAutocompleteNextRefresh was not honored\noutput=%q", output)
	}
}

// TestEnterHandler_NoDropdownStillClears verifies that the suppression
// flag is safe when there is no dropdown to suppress — i.e., when the
// user presses Enter on a non-slash line (no completer matches, no
// dropdown was visible). The flag is set and immediately consumed;
// no phantom dropdown state is created.
func TestEnterHandler_NoDropdownStillClears(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.richCompleter = mockRichCompleter

	ir.InsertChar("h")
	ir.InsertChar("i")

	if ir.autocomplete.visible {
		t.Fatal("setup: dropdown should not be visible for non-slash input")
	}

	// Simulate the Enter handler's suppression flag + Refresh.
	ir.suppressAutocompleteNextRefresh = true
	captureStdout(t, func() {
		ir.Refresh()
	})

	// Input line is preserved.
	if ir.line != "hi" {
		t.Errorf("input line should be preserved, got %q", ir.line)
	}
	if ir.autocomplete.visible {
		t.Errorf("dropdown should not become visible, got visible=true")
	}
	// Flag consumed.
	if ir.suppressAutocompleteNextRefresh {
		t.Error("suppressAutocompleteNextRefresh should be cleared after Refresh")
	}
}
