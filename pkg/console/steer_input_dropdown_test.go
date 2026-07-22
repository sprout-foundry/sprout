// SP-078 Phase 3: live autocomplete dropdown on the steer panel.
// Mirrors the InputReader's dropdown affordance but adapted to the
// footer's pinned-rows rendering. These tests cover the dropdown's
// pure logic (candidate updates, selection navigation, accept, dismiss)
// and the rendering helpers (formatSteerDropdownRow, buildDropdownLine).
package console

import (
	"strings"
	"testing"
)

func newTestReaderWithRichCompleter(rc RichCompletionProvider) *SteerInputReader {
	r := newTestReader(nil, nil)
	r.SetRichCompleter(rc)
	return r
}

// refreshTestDropdown is a test helper: invokes the reader's
// refreshDropdownLocked helper with the current buffer + cursor.
// Most tests need to drive this since renderLine is a no-op without
// a footer.
func refreshTestDropdown(r *SteerInputReader) {
	r.mu.Lock()
	r.refreshDropdownLocked(string(r.buffer), r.cursorPos)
	r.mu.Unlock()
}

// ---------------------------------------------------------------------------
// SetRichCompleter / autocomplete field wiring
// ---------------------------------------------------------------------------

func TestSteerInputReader_SetRichCompleter_StoresProvider(t *testing.T) {
	var called bool
	rc := func(line string, cursorPos int) []CompletionCandidate {
		called = true
		return []CompletionCandidate{{Text: "/help", Description: "Show help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	// update() only invokes the completer when the line starts with "/".
	r.insertAtCursor([]byte("/"))

	refreshTestDropdown(r)

	if !called {
		t.Fatal("richCompleter should be invoked via refreshDropdownLocked when buffer starts with /")
	}
}

func TestSteerInputReader_SetRichCompleter_NilHidesDropdown(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}

	// Replace with nil → dropdown should hide.
	r.SetRichCompleter(nil)
	if r.autocomplete.visible {
		t.Fatal("SetRichCompleter(nil) should hide the dropdown")
	}
}

// ---------------------------------------------------------------------------
// Dropdown visibility gating
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_VisibleOnlyForSlashPrefix(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/help", Description: "Show help"}}
	}
	r := newTestReaderWithRichCompleter(rc)

	// No slash → no candidates.
	r.insertAtCursor([]byte("hello"))
	refreshTestDropdown(r)
	if r.autocomplete.visible {
		t.Fatal("non-slash buffer should not show dropdown")
	}

	// Buffer starts with / → candidates appear.
	r.buffer = r.buffer[:0]
	r.cursorPos = 0
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("/ prefix should show dropdown")
	}
}

func TestSteerInputReader_Dropdown_HidesWhenCursorNotAtEnd(t *testing.T) {
	// InputReader hides the dropdown when the cursor isn't at the end
	// of the buffer. Steer reader inherits the same gating so editing
	// in the middle of a slash command doesn't keep stale suggestions.
	rc := func(line string, cursorPos int) []CompletionCandidate {
		if !strings.HasPrefix(line, "/") || cursorPos != len(line) {
			return nil
		}
		return []CompletionCandidate{{Text: "/help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/he"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}

	// Move cursor to middle (cursorPos=1, before /he → /he cursor at 1).
	r.cursorPos = 1
	refreshTestDropdown(r)
	if r.autocomplete.visible {
		t.Fatal("cursor not at end should hide dropdown")
	}
}

func TestSteerInputReader_Dropdown_NoCandidatesHides(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return nil // no slash commands match
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/zz"))
	refreshTestDropdown(r)
	if r.autocomplete.visible {
		t.Fatal("no candidates should hide dropdown")
	}
}

// ---------------------------------------------------------------------------
// Dropdown navigation
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_NavigateUpDown(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{
			{Text: "/alpha"},
			{Text: "/beta"},
			{Text: "/gamma"},
		}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}
	if r.autocomplete.selected != 0 {
		t.Fatalf("initial selection should be 0, got %d", r.autocomplete.selected)
	}

	r.navigateDropdown(1)
	if r.autocomplete.selected != 1 {
		t.Fatalf("down: expected selection 1, got %d", r.autocomplete.selected)
	}

	r.navigateDropdown(-1)
	if r.autocomplete.selected != 0 {
		t.Fatalf("up: expected selection 0, got %d", r.autocomplete.selected)
	}

	// Wrap: -1 from 0 → 2 (last).
	r.navigateDropdown(-1)
	if r.autocomplete.selected != 2 {
		t.Fatalf("wrap-up: expected selection 2, got %d", r.autocomplete.selected)
	}

	// Wrap: +1 from 2 → 0.
	r.navigateDropdown(1)
	if r.autocomplete.selected != 0 {
		t.Fatalf("wrap-down: expected selection 0, got %d", r.autocomplete.selected)
	}
}

func TestSteerInputReader_Dropdown_NavigateDoesNothingWhenHidden(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return nil
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("plain"))
	refreshTestDropdown(r)
	// Dropdown not visible — navigation should be a silent no-op.
	r.navigateDropdown(1)
	r.navigateDropdown(-1)
	if r.autocomplete.visible {
		t.Fatal("navigation should not flip visibility on")
	}
}

// ---------------------------------------------------------------------------
// Tab accept
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_TabAcceptsSelected(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{
			{Text: "/help", Description: "Show help"},
			{Text: "/history", Description: "Show history"},
		}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/h"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}

	// Move to second candidate.
	r.navigateDropdown(1)

	// Tab accepts.
	r.acceptDropdown()
	if got := string(r.buffer); got != "/history" {
		t.Fatalf("accept should replace buffer with selected candidate, got %q", got)
	}
	if r.cursorPos != len("/history") {
		t.Fatalf("cursor should be at end of accepted text, got %d", r.cursorPos)
	}
	if r.autocomplete.visible {
		t.Fatal("accept should hide the dropdown")
	}
}

func TestSteerInputReader_Dropdown_AcceptEmptyIsNoop(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)

	// Force the dropdown into an invalid state (no selection).
	r.autocomplete.selected = -1
	r.autocomplete.candidates = nil

	original := string(r.buffer)
	r.acceptDropdown()
	if string(r.buffer) != original {
		t.Fatalf("accept on invalid state should not change buffer, got %q", r.buffer)
	}
}

// ---------------------------------------------------------------------------
// Escape dismiss
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_EscapeHides(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/he"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}

	original := string(r.buffer)
	r.hideDropdown()
	if r.autocomplete.visible {
		t.Fatal("hideDropdown should clear visible flag")
	}
	if string(r.buffer) != original {
		t.Fatalf("hideDropdown should not change buffer, got %q", r.buffer)
	}
}

// ---------------------------------------------------------------------------
// Event dispatch: Up/Down/Tab/Escape route to dropdown when visible
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_UpEventNavigatesDropdown(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/a"}, {Text: "/b"}, {Text: "/c"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)

	// Start at 0. Up arrow should wrap to 2.
	r.handleEvent(&InputEvent{Type: EventUp})
	if r.autocomplete.selected != 2 {
		t.Fatalf("Up should wrap to last candidate, got %d", r.autocomplete.selected)
	}
}

func TestSteerInputReader_Dropdown_DownEventNavigatesDropdown(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/a"}, {Text: "/b"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)

	r.handleEvent(&InputEvent{Type: EventDown})
	if r.autocomplete.selected != 1 {
		t.Fatalf("Down should advance to 1, got %d", r.autocomplete.selected)
	}
}

func TestSteerInputReader_Dropdown_EscapeEventHides(t *testing.T) {
	rc := func(line string, cursorPos int) []CompletionCandidate {
		return []CompletionCandidate{{Text: "/help"}}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/h"))
	refreshTestDropdown(r)
	if !r.autocomplete.visible {
		t.Fatal("setup: dropdown should be visible")
	}

	r.handleEvent(&InputEvent{Type: EventEscape})
	if r.autocomplete.visible {
		t.Fatal("Escape should hide the dropdown")
	}
}

func TestSteerInputReader_Dropdown_UpRecallsHistoryWhenHidden(t *testing.T) {
	// When the dropdown isn't visible (buffer doesn't start with /),
	// Up arrow should still recall history as before.
	var submitted []string
	r := newTestReader(&submitted, nil)
	for _, b := range []byte("entry") {
		r.insertAtCursor([]byte{b})
	}
	r.handleSubmit()

	// Empty buffer — dropdown won't be visible.
	r.buffer = r.buffer[:0]
	r.cursorPos = 0

	r.handleEvent(&InputEvent{Type: EventUp})
	if got := string(r.buffer); got != "entry" {
		t.Fatalf("Up with no dropdown should recall history, got %q", got)
	}
}

// ---------------------------------------------------------------------------
// formatSteerDropdownRow helper
// ---------------------------------------------------------------------------

func TestFormatSteerDropdownRow_SelectedHasMarker(t *testing.T) {
	row := formatSteerDropdownRow(CompletionCandidate{Text: "/help", Description: "Show help"}, true, 60)
	// Selected row starts with the ▶ marker.
	if !strings.Contains(row, "▶ ") {
		t.Fatalf("selected row should contain ▶ marker, got %q", row)
	}
	if !strings.Contains(row, "/help") {
		t.Fatalf("selected row should include command text, got %q", row)
	}
	if !strings.Contains(row, "Show help") {
		t.Fatalf("selected row should include description, got %q", row)
	}
}

func TestFormatSteerDropdownRow_UnselectedHasNoMarker(t *testing.T) {
	row := formatSteerDropdownRow(CompletionCandidate{Text: "/help", Description: "Show help"}, false, 60)
	if strings.Contains(row, "▶") {
		t.Fatalf("unselected row should NOT have ▶ marker, got %q", row)
	}
	if !strings.Contains(row, "/help") {
		t.Fatalf("unselected row should include command text, got %q", row)
	}
}

func TestFormatSteerDropdownRow_NoDescription(t *testing.T) {
	row := formatSteerDropdownRow(CompletionCandidate{Text: "/help"}, true, 60)
	if !strings.Contains(row, "/help") {
		t.Fatalf("expected command text, got %q", row)
	}
}

func TestFormatSteerDropdownRow_TruncatesLongDescription(t *testing.T) {
	long := strings.Repeat("x", 100)
	row := formatSteerDropdownRow(CompletionCandidate{Text: "/cmd", Description: long}, false, 30)
	if visibleLen(row) > 30 {
		t.Fatalf("row should fit within cols (30), got visible width %d: %q", visibleLen(row), row)
	}
}

// ---------------------------------------------------------------------------
// buildDropdownLine helper — combines candidates + input line
// ---------------------------------------------------------------------------

func TestBuildDropdownLine_LayoutHasCandidateRowsPlusInputLine(t *testing.T) {
	r := &SteerInputReader{
		autocomplete: newInlineAutocomplete(),
	}
	r.autocomplete.candidates = []CompletionCandidate{
		{Text: "/help"},
		{Text: "/history"},
	}
	r.autocomplete.selected = 0

	full, cursorRow, cursorCol := r.buildDropdownLine(SteerPromptPrefix, "/h", 2, 80, r.autocomplete.candidates, r.autocomplete.selected)
	lines := strings.Split(full, "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines (2 candidates + 1 input), got %d: %q", len(lines), full)
	}
	// Cursor should sit on the input line (last row).
	if cursorRow != 2 {
		t.Fatalf("expected cursorRow=2 (input line), got %d", cursorRow)
	}
	// CursorCol should include the prefix width + cursor byte position.
	// The exact value depends on wrappedGeometry's prefix handling,
	// so we just verify it's past the prefix.
	prefixW := displayWidth(SteerPromptPrefix)
	if cursorCol < prefixW {
		t.Fatalf("cursorCol should be ≥ prefixWidth (%d), got %d", prefixW, cursorCol)
	}
	// First line should be the selected (with ▶ marker) candidate.
	if !strings.Contains(lines[0], "▶") {
		t.Fatalf("first candidate (selected) should have ▶ marker, got %q", lines[0][:min(20, len(lines[0]))])
	}
}

func TestBuildDropdownLine_SelectedHighlight(t *testing.T) {
	r := &SteerInputReader{
		autocomplete: newInlineAutocomplete(),
	}
	r.autocomplete.candidates = []CompletionCandidate{
		{Text: "/alpha"},
		{Text: "/beta"},
	}
	r.autocomplete.selected = 1 // /beta is selected

	full, _, _ := r.buildDropdownLine(SteerPromptPrefix, "/", 1, 80, r.autocomplete.candidates, r.autocomplete.selected)
	lines := strings.Split(full, "\n")
	if !strings.HasPrefix(lines[0], "  ") {
		t.Fatalf("unselected first candidate should have '  ' prefix, got %q", lines[0][:min(20, len(lines[0]))])
	}
	if !strings.Contains(lines[1], "▶") {
		t.Fatalf("selected second candidate should have ▶ prefix, got %q", lines[1][:min(20, len(lines[1]))])
	}
	// No ANSI codes — plain text only.
	if strings.Contains(lines[1], "\033[") {
		t.Fatalf("candidate rows must not contain ANSI codes (footer is not ANSI-aware), got %q", lines[1])
	}
}

func TestBuildDropdownLine_CapsAtMaxDropdownRows(t *testing.T) {
	// More candidates than the cap → first N rendered, rest dropped.
	r := &SteerInputReader{
		autocomplete: newInlineAutocomplete(),
	}
	candidates := make([]CompletionCandidate, 20)
	for i := range candidates {
		candidates[i] = CompletionCandidate{Text: "/cmd" + string(rune('a'+i))}
	}
	r.autocomplete.candidates = candidates
	r.autocomplete.selected = 0

	full, _, _ := r.buildDropdownLine(SteerPromptPrefix, "/", 1, 80, candidates, 0)
	lines := strings.Split(full, "\n")
	// 1 input line + up to maxSteerDropdownRows candidates.
	if len(lines) != maxSteerDropdownRows+1 {
		t.Fatalf("expected %d lines (%d candidates + 1 input), got %d",
			maxSteerDropdownRows+1, maxSteerDropdownRows, len(lines))
	}
}

func TestBuildDropdownLine_CursorMappingForWrappedInput(t *testing.T) {
	// Input line long enough to wrap → cursor should land on the wrapped row.
	r := &SteerInputReader{
		autocomplete: newInlineAutocomplete(),
	}
	r.autocomplete.candidates = []CompletionCandidate{{Text: "/help"}}
	r.autocomplete.selected = 0

	// Make the buffer long enough to wrap: 80 chars; cols=30.
	cols := 30
	longBuffer := strings.Repeat("x", 80)
	full, cursorRow, cursorCol := r.buildDropdownLine(SteerPromptPrefix, longBuffer, 80, cols, r.autocomplete.candidates, r.autocomplete.selected)
	lines := strings.Split(full, "\n")
	if len(lines) < 2 {
		t.Fatalf("setup: expected wrapped input line, got %d lines", len(lines))
	}
	// Last row should be the wrapped input line.
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, SteerPromptPrefix) {
		t.Fatalf("last line should start with steer prefix, got %q", last[:min(20, len(last))])
	}
	// CursorRow accounts for visual wrap rows within the input line.
	// It should be at least 1 (the dropdown candidate row) and the
	// cursor col should be within the terminal width.
	if cursorRow < 1 {
		t.Fatalf("cursor row should be ≥ 1 (at least the dropdown row + input), got %d", cursorRow)
	}
	// CursorCol should be clamped to cols (the wrap boundary).
	if cursorCol > cols {
		t.Fatalf("cursor col should be ≤ cols (%d), got %d", cols, cursorCol)
	}
}

// ---------------------------------------------------------------------------
// Autocomplete lifecycle: dropdown hides when no rich provider
// ---------------------------------------------------------------------------

func TestSteerInputReader_Dropdown_NoRichCompleterStaysHidden(t *testing.T) {
	// No richCompleter installed → dropdown stays hidden even if plain
	// completer is. Plain completer only powers Ctrl-] cycling.
	r := newTestReader(nil, nil)
	r.SetCompleter(func(line string, cursorPos int) []string {
		return []string{"/help"}
	})
	r.insertAtCursor([]byte("/h"))
	refreshTestDropdown(r)
	if r.autocomplete.visible {
		t.Fatal("without richCompleter, dropdown should not appear")
	}
}

func TestSteerInputReader_Dropdown_EditsRefreshCandidates(t *testing.T) {
	var callCount int
	rc := func(line string, cursorPos int) []CompletionCandidate {
		callCount++
		if !strings.HasPrefix(line, "/") {
			return nil
		}
		return []CompletionCandidate{
			{Text: line + "-matched"},
		}
	}
	r := newTestReaderWithRichCompleter(rc)
	r.insertAtCursor([]byte("/"))
	refreshTestDropdown(r)
	firstCalls := callCount

	// Adding more characters → refreshDropdownLocked sees a new line.
	r.insertAtCursor([]byte("h"))
	refreshTestDropdown(r)
	if callCount <= firstCalls {
		t.Fatalf("edit should trigger update; got %d calls, started at %d", callCount, firstCalls)
	}
	if len(r.autocomplete.candidates) == 0 {
		t.Fatal("after edit, candidates should be re-evaluated")
	}
}
