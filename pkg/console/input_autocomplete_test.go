package console

import (
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
