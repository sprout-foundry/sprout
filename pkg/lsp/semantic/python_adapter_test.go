package semantic

import (
	"os/exec"
	"reflect"
	"testing"
)

// --- Adapter Factory ---

func TestNewPythonAdapter_ReturnsAdapter(t *testing.T) {
	adapter := NewPythonAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter from NewPythonAdapter()")
	}
	if _, ok := adapter.(Adapter); !ok {
		t.Error("expected adapter to implement Adapter interface")
	}
	// The concrete type is pythonAdapter (struct)
	if reflect.TypeOf(adapter).String() != "semantic.pythonAdapter" {
		t.Errorf("expected type semantic.pythonAdapter, got %s", reflect.TypeOf(adapter).String())
	}
}

// --- Diagnostics When Ruff Not Available ---

func TestPythonAdapter_Diagnostics_NoRuff(t *testing.T) {
	// Only run this test when ruff is NOT installed, which is the expected
	// environment for CI / most dev machines.
	_, err := exec.LookPath("ruff")
	if err == nil {
		t.Skip("ruff is installed; skipping NoRuff test")
	}

	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\ny = 2\n",
		Method:   "diagnostics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to be true")
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected no diagnostics when ruff is missing, got %d", len(result.Diagnostics))
	}
	if result.Error != "" {
		t.Errorf("expected no error, got %q", result.Error)
	}
}

// --- Hover Returns Error ---

func TestPythonAdapter_Hover_ReturnsError(t *testing.T) {
	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\n",
		Method:   "hover",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for hover")
	}
	if result.Error != "hover requires LSP server (e.g., pylsp)" {
		t.Errorf("expected error containing 'requires LSP server', got %q", result.Error)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to still be true")
	}
}

// --- Definition Returns Error ---

func TestPythonAdapter_Definition_ReturnsError(t *testing.T) {
	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "import os\n",
		Method:   "definition",
		Position: &Position{Line: 1, Column: 8},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for definition")
	}
	if result.Error != "definition requires LSP server (e.g., pylsp)" {
		t.Errorf("expected error about LSP server, got %q", result.Error)
	}
}

// --- References Returns Error ---

func TestPythonAdapter_References_ReturnsError(t *testing.T) {
	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\nprint(x)\n",
		Method:   "references",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for references")
	}
	if result.Error != "references requires LSP server (e.g., pylsp)" {
		t.Errorf("expected error about LSP server, got %q", result.Error)
	}
}

// --- Rename Returns Error ---

func TestPythonAdapter_Rename_ReturnsError(t *testing.T) {
	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\n",
		Method:   "rename",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for rename")
	}
	if result.Error != "rename requires LSP server (e.g., pylsp)" {
		t.Errorf("expected error about LSP server, got %q", result.Error)
	}
}

// --- Default / Unknown Method Returns Capabilities ---

func TestPythonAdapter_Default_ReturnsCapabilities(t *testing.T) {
	adapter := NewPythonAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\n",
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

// --- LineColToOffset for Python (0-based columns) ---

func TestPythonLineColToOffset(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		col     int // 0-based for ruff
		want    int
	}{
		{"line 1 col 0", "hello\nworld", 1, 0, 0},
		{"line 2 col 0", "hello\nworld", 2, 0, 6},
		{"line 1 col 4", "hello\nworld", 1, 4, 4},
		{"line 0 col 0 returns 0", "hello\nworld", 0, 0, 0},
		{"beyond content", "hello\nworld", 10, 0, 11},
		{"line 1 col 0 of single line", "hello", 1, 0, 0},
		{"line 1 col 2 of single line", "hello", 1, 2, 2},
		{"past end of line clamped", "hi", 1, 10, 2},
		{"empty content", "", 1, 0, 0},
		{"multi-line last line", "a\nb\nc", 3, 0, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := LineColToOffset(tt.content, tt.line, tt.col)
			if got != tt.want {
				t.Errorf("LineColToOffset(%q, %d, %d) = %d, want %d", tt.content, tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// --- ruffSeverity ---

func TestRuffSeverity(t *testing.T) {
	tests := []struct {
		code string
		want string
	}{
		{"E501", "error"},
		{"F401", "error"},
		{"W292", "warning"},
		{"E", "error"},
		{"F", "error"},
		{"W", "warning"},
		{"C901", "info"},
		{"I001", "info"},
		{"", "info"},
	}
	for _, tt := range tests {
		t.Run(tt.code, func(t *testing.T) {
			got := ruffSeverity(tt.code)
			if got != tt.want {
				t.Errorf("ruffSeverity(%q) = %q, want %q", tt.code, got, tt.want)
			}
		})
	}
}

// --- Registry Integration ---

func TestRegistryPythonAdapter(t *testing.T) {
	registry := NewRegistry()
	registry.Register("python", NewPythonAdapter)

	adapter, ok := registry.AdapterForLanguage("python")
	if !ok {
		t.Fatal("expected adapter for 'python' to be found")
	}
	if adapter == nil {
		t.Fatal("expected non-nil adapter")
	}

	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/example.py",
		Content:  "x = 1\n",
		Method:   "diagnostics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to be true from registry adapter")
	}
}
