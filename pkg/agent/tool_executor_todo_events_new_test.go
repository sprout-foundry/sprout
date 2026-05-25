package agent

import (
	"strings"
	"testing"
)

func TestTodoStatusSymbolV2(t *testing.T) {
	// Glyph-prefixed in SP-057 Phase 2; assert against the visible
	// rune rather than the exact ANSI-decorated string.
	tests := []struct {
		name     string
		status   string
		wantRune string
	}{
		{"pending", "pending", "·"},
		{"in_progress", "in_progress", "→"},
		{"completed", "completed", "✓"},
		{"cancelled", "cancelled", "⏹"},
		{"unknown status", "unknown", "ⓘ"},
		{"empty status", "", "ⓘ"},
		{"random string", "foo_bar", "ⓘ"},
		{"status with spaces", " pending ", "ⓘ"}, // exact match required
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := todoStatusSymbol(tt.status)
			if !strings.Contains(result, tt.wantRune) {
				t.Errorf("todoStatusSymbol(%q) = %q; want contains %q", tt.status, result, tt.wantRune)
			}
		})
	}
}
