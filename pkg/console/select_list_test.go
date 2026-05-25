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
	row := renderSelectRow("claude-opus-4-7", "anthropic", true, 60)
	if !strings.Contains(row, "claude-opus-4-7") {
		t.Fatalf("row=%q missing label", row)
	}
	if !strings.Contains(row, "anthropic") {
		t.Fatalf("row=%q missing detail", row)
	}
	// Active row should start with the action glyph prefix.
	if !strings.HasPrefix(row, GlyphAction.Prefix()) {
		t.Fatalf("active row=%q should start with GlyphAction prefix", row)
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
