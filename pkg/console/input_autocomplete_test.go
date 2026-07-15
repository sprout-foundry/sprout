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
		visible: true,
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
