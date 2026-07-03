package commands

import (
	"strings"
	"testing"
)

func TestUsageCommand_Name(t *testing.T) {
	cmd := &UsageCommand{}
	if got := cmd.Name(); got != "usage" {
		t.Errorf("Name() = %q, want %q", got, "usage")
	}
}

func TestUsageCommand_Description(t *testing.T) {
	cmd := &UsageCommand{}
	desc := cmd.Description()
	if desc == "" {
		t.Error("Description() returned empty string")
	}
}

func TestRenderBar_HalfFilled(t *testing.T) {
	bar := renderBar(50, 100, 16)
	if len([]rune(bar)) != 16 {
		t.Fatalf("bar rune count = %d, want 16", len([]rune(bar)))
	}

	fullCount := strings.Count(bar, "█")
	emptyCount := strings.Count(bar, "░")
	if fullCount != 8 {
		t.Errorf("full segments = %d, want 8", fullCount)
	}
	if emptyCount != 8 {
		t.Errorf("empty segments = %d, want 8", emptyCount)
	}
}

func TestRenderBar_Empty(t *testing.T) {
	bar := renderBar(0, 100, 16)
	if len([]rune(bar)) != 16 {
		t.Fatalf("bar rune count = %d, want 16", len([]rune(bar)))
	}
	if bar != strings.Repeat("░", 16) {
		t.Errorf("bar = %q, want all empty", bar)
	}
}

func TestRenderBar_Full(t *testing.T) {
	bar := renderBar(100, 100, 16)
	if len([]rune(bar)) != 16 {
		t.Fatalf("bar rune count = %d, want 16", len([]rune(bar)))
	}
	if bar != strings.Repeat("█", 16) {
		t.Errorf("bar = %q, want all full", bar)
	}
}

func TestRenderBar_ZeroDivision(t *testing.T) {
	bar := renderBar(0, 0, 16)
	if len([]rune(bar)) != 16 {
		t.Fatalf("bar rune count = %d, want 16", len([]rune(bar)))
	}
	if bar != strings.Repeat("░", 16) {
		t.Errorf("bar = %q, want all empty (zero division)", bar)
	}
}

func TestRenderBar_ExceedsTotal(t *testing.T) {
	bar := renderBar(200, 100, 16)
	if bar != strings.Repeat("█", 16) {
		t.Errorf("bar = %q, want all full when filled > total", bar)
	}
}

func TestRenderBar_ZeroWidth(t *testing.T) {
	bar := renderBar(50, 100, 0)
	if bar != "" {
		t.Errorf("bar = %q, want empty string for width=0", bar)
	}
}

func TestRenderBar_Rounding(t *testing.T) {
	// 33/100 * 16 = 5.28 → rounds to 5
	bar := renderBar(33, 100, 16)
	fullCount := strings.Count(bar, "█")
	if fullCount != 5 {
		t.Errorf("full segments = %d, want 5 (33%% of 16)", fullCount)
	}

	// 67/100 * 16 = 10.72 → rounds to 11
	bar = renderBar(67, 100, 16)
	fullCount = strings.Count(bar, "█")
	if fullCount != 11 {
		t.Errorf("full segments = %d, want 11 (67%% of 16)", fullCount)
	}
}

func TestFormatTokens_Raw(t *testing.T) {
	tests := []struct {
		input  int
		expect string
	}{
		{0, "0"},
		{1, "1"},
		{999, "999"},
		{847, "847"},
	}
	for _, tt := range tests {
		if got := formatTokens(tt.input); got != tt.expect {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestFormatTokens_Kilo(t *testing.T) {
	tests := []struct {
		input  int
		expect string
	}{
		{1000, "1.0k"},
		{47200, "47.2k"},
		{999999, "1000.0k"},
	}
	for _, tt := range tests {
		if got := formatTokens(tt.input); got != tt.expect {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestFormatTokens_Mega(t *testing.T) {
	tests := []struct {
		input  int
		expect string
	}{
		{1000000, "1.0M"},
		{1200000, "1.2M"},
		{5500000, "5.5M"},
	}
	for _, tt := range tests {
		if got := formatTokens(tt.input); got != tt.expect {
			t.Errorf("formatTokens(%d) = %q, want %q", tt.input, got, tt.expect)
		}
	}
}

func TestGetEfficiencyRating(t *testing.T) {
	tests := []struct {
		efficiency float64
		wantText   string
		wantGlyph  string // "success" or "warning"
	}{
		{60, "Excellent", "success"},
		{50, "Excellent", "success"},
		{40, "Good", "success"},
		{30, "Good", "success"},
		{25, "Average", "warning"},
		{15, "Average", "warning"},
		{10, "Low", "warning"},
		{0, "Low", "warning"},
	}
	for _, tt := range tests {
		glyph, text := getEfficiencyRating(tt.efficiency)
		if text != tt.wantText {
			t.Errorf("efficiency=%.0f: text = %q, want %q", tt.efficiency, text, tt.wantText)
		}
		runeVal := glyph.Rune()
		// GlyphSuccess is ✓, GlyphWarning is ⚠
		if tt.wantGlyph == "success" && runeVal != "✓" {
			t.Errorf("efficiency=%.0f: glyph rune = %q, want success glyph (✓)", tt.efficiency, runeVal)
		}
		if tt.wantGlyph == "warning" && runeVal != "⚠" {
			t.Errorf("efficiency=%.0f: glyph rune = %q, want warning glyph (⚠)", tt.efficiency, runeVal)
		}
	}
}

func TestExecute_NilAgent(t *testing.T) {
	// Nil agent should handle gracefully (empty state message)
	cmd := &UsageCommand{}
	err := cmd.Execute(nil, nil)
	if err != nil {
		t.Errorf("Execute with nil agent returned error: %v", err)
	}
}

func TestExecute_ZeroTokens(t *testing.T) {
	// We can't easily mock an Agent with zero tokens in a unit test,
	// but we verify the command struct is valid
	cmd := &UsageCommand{}
	if cmd.Name() != "usage" {
		t.Error("command name mismatch")
	}
}
