package console

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// dropdownMockCompleter returns candidates for any slash-prefixed line,
// matching the broad shape of buildRichSlashCommandCompleter.
func dropdownMockCompleter(line string, _ int) []CompletionCandidate {
	if !strings.HasPrefix(line, "/") {
		return nil
	}
	return []CompletionCandidate{
		{Text: "/help", Description: "Show help"},
		{Text: "/heart", Description: ""},
	}
}

// testInputReaderWithFooter builds an InputReader wired to a footer
// backed by a buffer. The footer is forced isTTY+active so the
// pinned-block render path executes. terminalSize() returns (0,0) on
// fd=-1, so drawLocked/applyScrollRegionLocked bail before writing —
// but the footer state fields (steerLine, steerCursorRow/Col, etc.)
// are still recorded, which is what the assertions check.
func testInputReaderWithFooter() (*InputReader, *StatusFooter, *bytes.Buffer) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.richCompleter = dropdownMockCompleter
	var buf bytes.Buffer
	footer := &StatusFooter{w: &buf, isTTY: true, active: true, steerCursor: -1, fd: -1}
	ir.SetStatusFooter(footer)
	return ir, footer, &buf
}

func footerState(f *StatusFooter) (active, wrapped bool, line string, row, col int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.steerActive, f.steerWrappedActive, f.steerLine, f.steerCursorRow, f.steerCursorCol
}

func TestInputReader_DropdownPinsToFooter(t *testing.T) {
	ir, footer, _ := testInputReaderWithFooter()

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
		ir.InsertChar("e")
	})

	if !ir.autocomplete.visible {
		t.Fatal("dropdown should be visible after typing /he")
	}
	if !ir.pinnedDropdownActive {
		t.Error("pinnedDropdownActive should be true while the dropdown is visible")
	}
	active, wrapped, line, row, col := footerState(footer)
	if !active {
		t.Error("footer should be steer-active while the dropdown is pinned")
	}
	if !wrapped {
		t.Error("footer should be in wrapped mode")
	}
	if !strings.Contains(line, "/help") || !strings.Contains(line, "/heart") {
		t.Errorf("footer block should contain candidate rows, got %q", line)
	}
	if !strings.Contains(line, "> /he") {
		t.Errorf("footer block should contain the prompt line, got %q", line)
	}
	// Cursor sits on the input line (2 candidates + 1 input row).
	if row != 2 {
		t.Errorf("steerCursorRow = %d, want 2 (input line)", row)
	}
	if col != len("> /he") {
		t.Errorf("steerCursorCol = %d, want %d", col, len("> /he"))
	}
}

func TestInputReader_DropdownHideClearsFooter(t *testing.T) {
	ir, footer, _ := testInputReaderWithFooter()

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
		ir.InsertChar("e")
	})
	if !ir.pinnedDropdownActive {
		t.Fatal("setup: dropdown should be pinned")
	}

	// Backspace to clear the buffer → dropdown hides.
	captureStdout(t, func() {
		ir.Backspace()
		ir.Backspace()
		ir.Backspace()
	})

	if ir.autocomplete.visible {
		t.Error("dropdown should be hidden after clearing the line")
	}
	if ir.pinnedDropdownActive {
		t.Error("pinnedDropdownActive should be false after hide")
	}
	active, _, line, _, _ := footerState(footer)
	if active {
		t.Error("footer steer state should be cleared after hide")
	}
	if line != "" {
		t.Errorf("footer steerLine should be empty after hide, got %q", line)
	}
	// Reserved rows return to the 2-row baseline (rule + content).
	if got := footer.reservedRows(); got != 2 {
		t.Errorf("reservedRows after hide = %d, want 2", got)
	}
}

func TestInputReader_DropdownNoFooterDoesNotPin(t *testing.T) {
	ir := NewInputReader("> ")
	ir.terminalWidth = 80
	ir.richCompleter = dropdownMockCompleter

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
		ir.InsertChar("e")
	})

	if !ir.autocomplete.visible {
		t.Error("dropdown state should still update without a footer")
	}
	if ir.pinnedDropdownActive {
		t.Error("pinnedDropdownActive should stay false without a footer")
	}
}

func TestInputReader_DropdownShowHideReservedRows(t *testing.T) {
	ir, footer, _ := testInputReaderWithFooter()

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
	})
	// 2 candidates + 1 prompt row + rule + content.
	if got := footer.reservedRows(); got != 5 {
		t.Errorf("reservedRows while pinned = %d, want 5 (2 candidates + 1 prompt + rule + content)", got)
	}

	captureStdout(t, func() {
		ir.Backspace()
		ir.Backspace()
	})
	if got := footer.reservedRows(); got != 2 {
		t.Errorf("reservedRows after hide = %d, want 2", got)
	}
}

func TestInputReader_DropdownShowHideTransitionNoStaleRows(t *testing.T) {
	ir, footer, _ := testInputReaderWithFooter()

	// Show → hide → show again; the footer state must fully cycle so no
	// stale pinned rows persist from the first block.
	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
	})
	if !ir.pinnedDropdownActive {
		t.Fatal("first show: dropdown should be pinned")
	}

	captureStdout(t, func() {
		ir.Backspace()
		ir.Backspace()
	})
	if ir.pinnedDropdownActive {
		t.Fatal("hide: dropdown should unpin")
	}
	active, _, line, _, _ := footerState(footer)
	if active || line != "" {
		t.Fatalf("hide: footer should be cleared, got active=%v line=%q", active, line)
	}

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
		ir.InsertChar("e")
	})
	if !ir.pinnedDropdownActive {
		t.Fatal("second show: dropdown should re-pin")
	}
	active, _, line, row, _ := footerState(footer)
	if !active || line == "" {
		t.Fatalf("second show: footer should be re-pinned, got active=%v line=%q", active, line)
	}
	if row != 2 {
		t.Errorf("second show: steerCursorRow = %d, want 2 (input line)", row)
	}
}

func TestInputReader_DropdownEnterAcceptClearsFooter(t *testing.T) {
	ir, footer, _ := testInputReaderWithFooter()

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
		ir.InsertChar("e")
	})
	if !ir.pinnedDropdownActive {
		t.Fatal("setup: dropdown should be pinned")
	}

	// Simulate the Enter handler's accept + hide + suppressed refresh.
	captureStdout(t, func() {
		text := ir.autocomplete.accept()
		if text != "" {
			ir.line = text
			ir.cursorPos = len(ir.line)
		}
		ir.autocomplete.hide()
		ir.suppressAutocompleteNextRefresh = true
		ir.Refresh()
	})

	if ir.autocomplete.visible {
		t.Error("dropdown should be hidden after Enter accept")
	}
	if ir.pinnedDropdownActive {
		t.Error("pinnedDropdownActive should be false after Enter accept")
	}
	active, _, line, _, _ := footerState(footer)
	if active || line != "" {
		t.Errorf("footer should be cleared after Enter accept, got active=%v line=%q", active, line)
	}
	if ir.line != "/help" {
		t.Errorf("accepted text should be /help, got %q", ir.line)
	}
}

func TestInputReader_DropdownWrapAwareCursor(t *testing.T) {
	// A long prompt line that wraps should place the cursor on the
	// wrapped row at the right column.
	ir, footer, _ := testInputReaderWithFooter()
	ir.terminalWidth = 30

	captureStdout(t, func() {
		ir.InsertChar("/")
		ir.InsertChar("h")
	})
	// Widen the buffer beyond the 30-col wrap.
	long := strings.Repeat("x", 60)
	captureStdout(t, func() {
		ir.line = "/h" + long
		ir.cursorPos = len(ir.line)
		ir.Refresh()
	})

	_, _, line, row, col := footerState(footer)
	lines := strings.Split(line, "\n")
	// 2 candidate rows + a single (unwrapped) input row in the block
	// string; the footer wraps the input row at draw time.
	if len(lines) != 3 {
		t.Fatalf("expected 2 candidates + 1 input line, got %d lines: %q", len(lines), line)
	}
	if !strings.HasPrefix(lines[2], "> /h") {
		t.Errorf("last block row should be the prompt line, got %q", lines[2])
	}
	// The cursor sits on the LAST wrapped row of the input line:
	// 2 candidates + (rows of "> /h"+60x at cols=30 → 3 rows) - 1.
	if row != 4 {
		t.Errorf("cursor row = %d, want 4 (last wrapped row of the input line)", row)
	}
	if col != 4 {
		t.Errorf("cursor col = %d, want 4 (end of the wrapped line)", col)
	}
}

func TestBuildDropdownBlock_ScrollWindowKeepsSelectionVisible(t *testing.T) {
	candidates := make([]CompletionCandidate, 12)
	for i := range candidates {
		candidates[i] = CompletionCandidate{Text: fmt.Sprintf("/cmd%02d", i)}
	}

	// Selected within the first window → offset 0, marker visible.
	_, _, full, _, _ := func() (bool, bool, string, int, int) {
		r := &SteerInputReader{autocomplete: newInlineAutocomplete()}
		f, row, col := r.buildDropdownLine("> ", "/c", 2, 80, candidates, 3)
		_ = row
		_ = col
		return true, true, f, row, col
	}()
	lines := strings.Split(full, "\n")
	if len(lines) != maxDropdownRows+1 {
		t.Fatalf("expected %d rows, got %d", maxDropdownRows+1, len(lines))
	}
	if !strings.Contains(lines[3], "▶") {
		t.Errorf("selected=3 should show marker in window row 3, got %q", lines[3])
	}

	// Selected past the window → window slides so the selection is the
	// last visible candidate row.
	r := &SteerInputReader{autocomplete: newInlineAutocomplete()}
	full2, _, _ := r.buildDropdownLine("> ", "/c", 2, 80, candidates, 9)
	lines2 := strings.Split(full2, "\n")
	if len(lines2) != maxDropdownRows+1 {
		t.Fatalf("expected %d rows, got %d", maxDropdownRows+1, len(lines2))
	}
	// Window covers candidates 5..9; the selected one (9) is last.
	if !strings.Contains(lines2[maxDropdownRows-1], "/cmd09") || !strings.Contains(lines2[maxDropdownRows-1], "▶") {
		t.Errorf("selected=9 should be the last visible row with marker, got %q", lines2[maxDropdownRows-1])
	}
	if strings.Contains(full2, "/cmd00") {
		t.Errorf("scrolled-out candidate /cmd00 should not render, got %q", full2)
	}

	// Selection at the very end → window clamped to the tail.
	full3, _, _ := r.buildDropdownLine("> ", "/c", 2, 80, candidates, 11)
	lines3 := strings.Split(full3, "\n")
	if !strings.Contains(lines3[maxDropdownRows-1], "/cmd11") || !strings.Contains(lines3[maxDropdownRows-1], "▶") {
		t.Errorf("selected=11 should be the last visible row with marker, got %q", lines3[maxDropdownRows-1])
	}
}
