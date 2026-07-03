package console

import (
	"context"
	"strings"
	"testing"
)

func TestSelectList_NoItems(t *testing.T) {
	s := NewSelectList(SelectListOptions{Items: nil})
	_, ok, err := s.Run(context.Background())
	if ok {
		t.Fatalf("expected ok=false on empty list")
	}
	if err == nil {
		t.Fatalf("expected error on empty list")
	}
}

func TestSelectList_ApplyFilterEmptyKeepsAll(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha"},
			{Label: "bravo"},
			{Label: "charlie"},
		},
	})
	s.applyFilter("")
	if len(s.filtered) != 3 {
		t.Fatalf("filtered=%d want 3", len(s.filtered))
	}
}

func TestSelectList_ApplyFilterSubstring(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "claude-opus", Detail: "anthropic"},
			{Label: "claude-sonnet", Detail: "anthropic"},
			{Label: "gpt-4o", Detail: "openai"},
		},
		Searchable: true,
	})
	s.applyFilter("claude")
	if len(s.filtered) != 2 {
		t.Fatalf("filtered=%d want 2", len(s.filtered))
	}
	s.applyFilter("openai")
	if len(s.filtered) != 1 {
		t.Fatalf("filtered=%d want 1", len(s.filtered))
	}
	s.applyFilter("nothing-matches")
	if len(s.filtered) != 0 {
		t.Fatalf("filtered=%d want 0", len(s.filtered))
	}
}

func TestSelectList_MoveCursorBounds(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
			{Label: "c", Value: "c"},
		},
	})
	s.moveCursor(-5)
	if s.cursor != 0 {
		t.Fatalf("cursor=%d want 0 (lower bound)", s.cursor)
	}
	s.moveCursor(99)
	if s.cursor != 2 {
		t.Fatalf("cursor=%d want 2 (upper bound)", s.cursor)
	}
}

func TestSelectList_Confirm(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
			{Label: "charlie", Value: "C"},
		},
	})
	s.cursor = 1
	val, ok := s.confirm()
	if !ok || val != "B" {
		t.Fatalf("confirm val=%q ok=%v want B/true", val, ok)
	}
}

func TestSelectList_FilterBackspaceUTF8(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items:      []SelectItem{{Label: "test"}},
		Searchable: true,
	})
	s.filter = "café"
	s.filterBackspace()
	if s.filter != "caf" {
		t.Fatalf("after backspace filter=%q want 'caf'", s.filter)
	}
}

func TestRenderSelectRow_DetailRightAligned(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("CLICOLOR_FORCE", "1")
	row := renderSelectRow("claude-opus-4-7", "anthropic", true, 60)
	if !strings.Contains(row, "claude-opus-4-7") {
		t.Fatalf("row=%q missing label", row)
	}
	if !strings.Contains(row, "anthropic") {
		t.Fatalf("row=%q missing detail", row)
	}
	// Active row uses a filled-arrow prefix (heavier than `→`) so the
	// selection stands out from inactive rows even at a glance.  The escape
	// codes around it differ by color mode; assert on the visible glyph.
	if !strings.Contains(row, "❯") {
		t.Fatalf("active row=%q should contain the filled-arrow indicator", row)
	}
	// And the label itself should be bold-wrapped when color is enabled.
	if !strings.Contains(row, "\x1b[1m") {
		t.Fatalf("active row=%q should bold the label (\\x1b[1m escape)", row)
	}
}

func TestRenderSelectRow_InactiveRow(t *testing.T) {
	row := renderSelectRow("foo", "", false, 60)
	if !strings.HasPrefix(row, "  ") {
		t.Fatalf("inactive row should start with 2 spaces, got %q", row)
	}
}

func TestUTF8Width(t *testing.T) {
	cases := []struct {
		b    byte
		want int
	}{
		{0xC3, 2},
		{0xE2, 3},
		{0xF0, 4},
		{0x41, 1}, // not a lead byte but the default
	}
	for _, c := range cases {
		if got := utf8Width(c.b); got != c.want {
			t.Errorf("utf8Width(0x%02X)=%d want %d", c.b, got, c.want)
		}
	}
}

func TestSelectList_DismissOnAnyKey_PrintableChar(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
		},
		DismissOnAnyKey: true,
		Searchable:      false,
	})
	var buf [8]byte
	buf[0] = 'h' // 0x68, printable ASCII
	done, val, ok := s.processKey('h', 1, buf[:])
	if !done {
		t.Fatalf("expected done=true on printable key with DismissOnAnyKey")
	}
	if val != "" {
		t.Fatalf("expected empty value on dismiss, got %q", val)
	}
	if ok {
		t.Fatalf("expected ok=false on dismiss")
	}
	// The dismissed key should be captured so callers can forward it.
	if got := s.DismissKey(); got != "h" {
		t.Fatalf("DismissKey()=%q want \"h\"", got)
	}
}

func TestSelectList_DismissOnAnyKey_UTF8LeadByte(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
		},
		DismissOnAnyKey: true,
		Searchable:      false,
	})
	// 'á' is U+00E1, encoded as 0xC3 0xA1 in UTF-8.
	var buf [8]byte
	buf[0] = 0xC3
	buf[1] = 0xA1
	done, val, ok := s.processKey(0xC3, 2, buf[:])
	if !done {
		t.Fatalf("expected done=true on UTF-8 lead byte with DismissOnAnyKey")
	}
	if val != "" {
		t.Fatalf("expected empty value on dismiss, got %q", val)
	}
	if ok {
		t.Fatalf("expected ok=false on dismiss")
	}
	if got := s.DismissKey(); got != "á" {
		t.Fatalf("DismissKey()=%q want \"á\"", got)
	}
}

func TestSelectList_DismissOnAnyKey_DoesNotAffectSearchable(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "hello", Value: "H"},
			{Label: "world", Value: "W"},
		},
		DismissOnAnyKey: true,
		Searchable:      true, // Searchable takes precedence
	})
	var buf [8]byte
	buf[0] = 'h'
	done, _, _ := s.processKey('h', 1, buf[:])
	if done {
		t.Fatalf("expected done=false when Searchable=true (should filter, not dismiss)")
	}
	// Verify it filtered instead of dismissing
	if s.filter != "h" {
		t.Fatalf("expected filter='h', got %q", s.filter)
	}
	if len(s.filtered) != 1 || s.filtered[0] != 0 {
		t.Fatalf("expected 1 filtered item (hello), got %d", len(s.filtered))
	}
}

func TestSelectList_DismissOnAnyKey_EnterStillWorks(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
		},
		DismissOnAnyKey: true,
	})
	var buf [8]byte
	buf[0] = 0x0D // Enter
	done, val, ok := s.processKey(0x0D, 1, buf[:])
	if !done {
		t.Fatalf("expected done=true on Enter")
	}
	if !ok {
		t.Fatalf("expected ok=true on Enter confirm")
	}
	if val != "A" {
		t.Fatalf("expected value='A' (first item), got %q", val)
	}
}

func TestSelectList_DismissOnAnyKey_ArrowsStillWork(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
			{Label: "charlie", Value: "C"},
		},
		DismissOnAnyKey: true,
	})
	// Simulate Down arrow: ESC [ B — pass the full 3-byte CSI sequence
	// so handleEscape dispatches directly without reading from stdin.
	csiDown := []byte{0x1B, '[', 'B'}
	done, _, _ := s.processKey(0x1B, 3, csiDown)
	if done {
		t.Fatalf("expected done=false on arrow key")
	}
	if s.cursor != 1 {
		t.Fatalf("expected cursor=1 after Down, got %d", s.cursor)
	}

	// Now press Enter to confirm — should select the second item.
	var buf [8]byte
	buf[0] = 0x0D
	done, val, ok := s.processKey(0x0D, 1, buf[:])
	if !done || !ok {
		t.Fatalf("expected done=true, ok=true on Enter after arrow")
	}
	if val != "B" {
		t.Fatalf("expected value='B' (second item), got %q", val)
	}
}

func TestSelectList_DismissOff_PrintableNoOp(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{{Label: "x", Value: "X"}},
	})
	var buf [8]byte
	buf[0] = 'q'
	done, _, _ := s.processKey('q', 1, buf[:])
	if done {
		t.Fatal("expected done=false (printable char should be ignored without DismissOnAnyKey)")
	}
}

// DismissKey must stay empty when the picker exits via Enter (confirm)
// or Esc (cancel) — those aren't "start fresh by typing" gestures.
func TestSelectList_DismissKey_EmptyOnEnterAndCancel(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
			{Label: "bravo", Value: "B"},
		},
		DismissOnAnyKey: true,
	})
	// Enter → confirm, no dismiss key.
	var buf [8]byte
	buf[0] = 0x0D
	done, val, ok := s.processKey(0x0D, 1, buf[:])
	if !done || !ok || val != "A" {
		t.Fatalf("Enter should confirm A, got val=%q ok=%v done=%v", val, ok, done)
	}
	if got := s.DismissKey(); got != "" {
		t.Fatalf("DismissKey()=%q want \"\" after Enter", got)
	}

	// Ctrl+C → cancel, no dismiss key.
	buf[0] = 0x03
	done, _, ok = s.processKey(0x03, 1, buf[:])
	if !done || ok {
		t.Fatalf("Ctrl+C should cancel, got done=%v ok=%v", done, ok)
	}
	if got := s.DismissKey(); got != "" {
		t.Fatalf("DismissKey()=%q want \"\" after Ctrl+C", got)
	}
}

// Backspace/DEL must dismiss but NOT be recorded — forwarding a
// backspace into the REPL input buffer would delete a phantom char.
func TestSelectList_DismissKey_BackspaceNotRecorded(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "alpha", Value: "A"},
		},
		DismissOnAnyKey: true,
	})
	var buf [8]byte
	buf[0] = 0x7F // DEL
	done, _, _ := s.processKey(0x7F, 1, buf[:])
	if !done {
		t.Fatal("expected done=true on DEL with DismissOnAnyKey")
	}
	if got := s.DismissKey(); got != "" {
		t.Fatalf("DismissKey()=%q want \"\" (backspace should not be forwarded)", got)
	}
}

// --- SP-106 Phase 3: Mouse wheel scroll tests ---

func TestSelectList_DispatchMouseWheelUp(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
			{Label: "c", Value: "c"},
		},
	})
	s.cursor = 2 // start at bottom
	s.dispatchMouseWheel(MouseEventWheelUp)
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 after wheel up", s.cursor)
	}
}

func TestSelectList_DispatchMouseWheelDown(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
			{Label: "c", Value: "c"},
		},
	})
	s.cursor = 0 // start at top
	s.dispatchMouseWheel(MouseEventWheelDown)
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 after wheel down", s.cursor)
	}
}

func TestSelectList_DispatchMouseWheelUpClamped(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
		},
	})
	s.cursor = 0
	s.dispatchMouseWheel(MouseEventWheelUp)
	if s.cursor != 0 {
		t.Fatalf("cursor=%d want 0 (clamped at top)", s.cursor)
	}
}

func TestSelectList_DispatchMouseWheelDownClamped(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
		},
	})
	s.cursor = 1
	s.dispatchMouseWheel(MouseEventWheelDown)
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 (clamped at bottom)", s.cursor)
	}
}

func TestSelectList_DispatchMouseEvent_WheelUpPayload(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
			{Label: "c", Value: "c"},
		},
	})
	s.cursor = 2
	// SGR payload: button=64 (wheel up), col=10, row=5 → "64;10;5M"
	s.dispatchMouseEvent("64;10;5M")
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 after wheel-up payload", s.cursor)
	}
}

func TestSelectList_DispatchMouseEvent_WheelDownPayload(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
			{Label: "c", Value: "c"},
		},
	})
	s.cursor = 0
	// SGR payload: button=65 (wheel down), col=10, row=5 → "65;10;5M"
	s.dispatchMouseEvent("65;10;5M")
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 after wheel-down payload", s.cursor)
	}
}

func TestSelectList_DispatchMouseEvent_LeftRightNoOp(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
			{Label: "b", Value: "b"},
		},
	})
	s.cursor = 1
	// Wheel left (button=66) and wheel right (button=67) are no-ops.
	s.dispatchMouseEvent("66;10;5M")
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 (wheel left should be no-op)", s.cursor)
	}
	s.dispatchMouseEvent("67;10;5M")
	if s.cursor != 1 {
		t.Fatalf("cursor=%d want 1 (wheel right should be no-op)", s.cursor)
	}
}

func TestSelectList_DispatchMouseEvent_InvalidPayload(t *testing.T) {
	s := NewSelectList(SelectListOptions{
		Items: []SelectItem{
			{Label: "a", Value: "a"},
		},
	})
	s.cursor = 0
	// Malformed payloads should not panic.
	s.dispatchMouseEvent("garbage")
	s.dispatchMouseEvent("abc;10;5M")
	s.dispatchMouseEvent("")
	if s.cursor != 0 {
		t.Fatalf("cursor=%d want 0 (invalid payloads should be no-op)", s.cursor)
	}
}
