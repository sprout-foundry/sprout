package agent

import (
	"testing"
)

func TestTodoStatusSymbolV2(t *testing.T) {
	tests := []struct {
		name     string
		status   string
		expected string
	}{
		{"pending", "pending", "[ ]"},
		{"in_progress", "in_progress", "[~]"},
		{"completed", "completed", "[x]"},
		{"cancelled", "cancelled", "[-]"},
		{"unknown status", "unknown", "[?]"},
		{"empty status", "", "[?]"},
		{"random string", "foo_bar", "[?]"},
		{"status with spaces", " pending ", "[?]"}, // exact match required
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := todoStatusSymbol(tt.status)
			if result != tt.expected {
				t.Errorf("todoStatusSymbol(%q) = %q; want %q", tt.status, result, tt.expected)
			}
		})
	}
}
