package semantic

import (
	"os/exec"
	"reflect"
	"testing"
)

// --- Adapter Factory ---

func TestNewRustAdapter_ReturnsAdapter(t *testing.T) {
	adapter := NewRustAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter from NewRustAdapter()")
	}
	if _, ok := adapter.(Adapter); !ok {
		t.Error("expected adapter to implement Adapter interface")
	}
	if reflect.TypeOf(adapter).String() != "semantic.rustAdapter" {
		t.Errorf("expected type semantic.rustAdapter, got %s", reflect.TypeOf(adapter).String())
	}
}

// --- Diagnostics When Cargo Not Available ---

func TestRustAdapter_Diagnostics_NoCargo(t *testing.T) {
	// Only run when cargo is NOT installed.
	_, err := exec.LookPath("cargo")
	if err == nil {
		t.Skip("cargo is installed; skipping NoCargo test")
	}

	adapter := NewRustAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "diagnostics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to be true")
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected no diagnostics when cargo is missing, got %d", len(result.Diagnostics))
	}
	if result.Error != "" {
		t.Errorf("expected no error, got %q", result.Error)
	}
}

// --- Hover Returns Error ---

func TestRustAdapter_Hover_ReturnsError(t *testing.T) {
	adapter := NewRustAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "hover",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for hover")
	}
	if result.Error != "hover requires rust-analyzer" {
		t.Errorf("expected error about rust-analyzer, got %q", result.Error)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to still be true")
	}
}

// --- Definition Returns Error ---

func TestRustAdapter_Definition_ReturnsError(t *testing.T) {
	adapter := NewRustAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "definition",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for definition")
	}
	if result.Error != "definition requires rust-analyzer" {
		t.Errorf("expected error about rust-analyzer, got %q", result.Error)
	}
}

// --- References Returns Error ---

func TestRustAdapter_References_ReturnsError(t *testing.T) {
	adapter := NewRustAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "references",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for references")
	}
	if result.Error != "references requires rust-analyzer" {
		t.Errorf("expected error about rust-analyzer, got %q", result.Error)
	}
}

// --- Default / Unknown Method Returns Capabilities ---

func TestRustAdapter_Default_ReturnsCapabilities(t *testing.T) {
	adapter := NewRustAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "some_unknown_method",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	caps := fullCaps
	if !reflect.DeepEqual(result.Capabilities, caps) {
		t.Errorf("expected capabilities to match fullCaps, got %+v", result.Capabilities)
	}
	if result.Error != "" {
		t.Errorf("expected no error for unknown method, got %q", result.Error)
	}
}

// --- rustLineColToOffset via shared LineColToOffset (1-based columns, convert to 0-based) ---

func TestRustLineColToOffset(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		col     int // 1-based as cargo reports
		want    int
	}{
		{"line 1 col 1", "hello\nworld", 1, 1, 0},
		{"line 2 col 1", "hello\nworld", 2, 1, 6},
		{"line 1 col 5", "hello\nworld", 1, 5, 4},
		{"line 0 col 1 returns 0", "hello\nworld", 0, 1, 0},
		{"beyond content", "hello\nworld", 10, 1, 11},
		{"line 1 col 1 of single line", "hello", 1, 1, 0},
		{"line 1 col 3 of single line", "hello", 1, 3, 2},
		{"past end of line clamped", "hi", 1, 10, 2},
		{"empty content", "", 1, 1, 0},
		{"multi-line last line", "a\nb\nc", 3, 1, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Cargo reports 1-based columns; LineColToOffset expects 0-based, so subtract 1
			got := LineColToOffset(tt.content, tt.line, tt.col-1)
			if got != tt.want {
				t.Errorf("LineColToOffset(%q, %d, %d-1) = %d, want %d", tt.content, tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// --- Registry Integration ---

func TestRegistryRustAdapter(t *testing.T) {
	registry := NewRegistry()
	registry.Register("rust", NewRustAdapter)

	adapter, ok := registry.AdapterForLanguage("rust")
	if !ok {
		t.Fatal("expected adapter for 'rust' to be found")
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}

	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/src/main.rs",
		Content:  "fn main() {}",
		Method:   "diagnostics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to be true from registry adapter")
	}
}
