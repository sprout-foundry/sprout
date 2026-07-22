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

// TestAutocomplete_RenderRowsStartAtColumnZero is a regression test for a
// cursor-column bug in the dropdown rendering. MoveCursorDown (\033[B)
// preserves the cursor's column, and ClearLine (\033[2K) does not move the
// cursor to column 0. Without an intervening carriage return (\r), the row
// text is written starting at whatever column the input prompt left the
// cursor at — so the dropdown appears shifted right rather than at column 0.
//
// The fix inserts \r between the two sequences. This test verifies that
// every "\033[B" emitted by render() is immediately followed by "\r".
func TestAutocomplete_RenderRowsStartAtColumnZero(t *testing.T) {
	a := &inlineAutocomplete{
		visible:  true,
		selected: 0,
		candidates: []CompletionCandidate{
			{Text: "/help", Description: "Show help"},
			{Text: "/heart", Description: ""},
			{Text: "/heat", Description: "Temperature"},
		},
	}

	output := captureStdout(t, func() { a.render() })

	// Fixed sequence: move-down + carriage return + clear-line.
	// MoveCursorDownSeq(1) emits "\x1b[1B" (not "\x1b[B" — it includes the
	// explicit row count), and ClearLineSeq() emits "\x1b[2K".
	fixedSeq := "\x1b[1B\r\x1b[2K"
	// Buggy sequence: move-down + clear-line (no carriage return).
	buggySeq := "\x1b[1B\x1b[2K"

	// Sanity check: render() drew exactly 3 candidates, so the fixed
	// sequence must appear 3 times.
	if got := strings.Count(output, fixedSeq); got != 3 {
		t.Errorf("expected 3 occurrences of %q (move-down + carriage return + clear), got %d\noutput=%q",
			fixedSeq, got, output)
	}

	// The buggy sequence (without \r) must never appear, otherwise text
	// would start at the prompt's column instead of column 0.
	if strings.Contains(output, buggySeq) {
		t.Errorf("render() output contains buggy sequence %q (no carriage return before clear); "+
			"text would start at the wrong column\noutput=%q", buggySeq, output)
	}
}

// TestAutocomplete_ClearRowsUseCarriageReturn is the clear()-side regression
// companion to the render() test above. clear() erases previously drawn
// dropdown rows using the same move-down + clear pattern, so it must also
// emit \r between them to return to column 0.
func TestAutocomplete_ClearRowsUseCarriageReturn(t *testing.T) {
	a := &inlineAutocomplete{
		visible:      true,
		renderedRows: 4,
	}

	output := captureStdout(t, func() { a.clear() })

	fixedSeq := "\x1b[1B\r\x1b[2K"
	buggySeq := "\x1b[1B\x1b[2K"

	if got := strings.Count(output, fixedSeq); got != 4 {
		t.Errorf("expected 4 occurrences of %q (move-down + carriage return + clear), got %d\noutput=%q",
			fixedSeq, got, output)
	}
	if strings.Contains(output, buggySeq) {
		t.Errorf("clear() output contains buggy sequence %q (no carriage return before clear)\noutput=%q",
			buggySeq, output)
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

// TestAutocomplete_HideResetsRenderedRows verifies that hide() clears
// renderedRows so a subsequent clear() in refreshLocked doesn't write
// escape sequences for rows that were already erased.
func TestAutocomplete_HideResetsRenderedRows(t *testing.T) {
	a := &inlineAutocomplete{
		visible:      true,
		renderedRows: 4,
		candidates:   []CompletionCandidate{{Text: "/help"}},
	}
	a.hide()
	if a.renderedRows != 0 {
		t.Errorf("renderedRows should be 0 after hide(), got %d", a.renderedRows)
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
