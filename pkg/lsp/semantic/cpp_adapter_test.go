package semantic

import (
	"os/exec"
	"reflect"
	"regexp"
	"testing"
)

// --- Adapter Factory ---

func TestNewCppAdapter_ReturnsAdapter(t *testing.T) {
	adapter := NewCppAdapter()
	if adapter == nil {
		t.Fatal("expected non-nil adapter from NewCppAdapter()")
	}
	if _, ok := adapter.(Adapter); !ok {
		t.Error("expected adapter to implement Adapter interface")
	}
	if reflect.TypeOf(adapter).String() != "semantic.cppAdapter" {
		t.Errorf("expected type semantic.cppAdapter, got %s", reflect.TypeOf(adapter).String())
	}
}

// --- Diagnostics When Clang-Tidy Not Available ---

func TestCppAdapter_Diagnostics_NoClangTidy(t *testing.T) {
	// Only run when clang-tidy is NOT installed.
	_, err := exec.LookPath("clang-tidy")
	if err == nil {
		t.Skip("clang-tidy is installed; skipping NoClangTidy test")
	}

	adapter := NewCppAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/main.cpp",
		Content:  "int main() { return 0; }",
		Method:   "diagnostics",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to be true")
	}
	if len(result.Diagnostics) != 0 {
		t.Errorf("expected no diagnostics when clang-tidy is missing, got %d", len(result.Diagnostics))
	}
	if result.Error != "" {
		t.Errorf("expected no error, got %q", result.Error)
	}
}

// --- Hover Returns Error ---

func TestCppAdapter_Hover_ReturnsError(t *testing.T) {
	adapter := NewCppAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/main.cpp",
		Content:  "int main() {}",
		Method:   "hover",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for hover")
	}
	if result.Error != "hover requires clangd" {
		t.Errorf("expected error about clangd, got %q", result.Error)
	}
	if !result.Capabilities.Diagnostics {
		t.Error("expected Capabilities.Diagnostics to still be true")
	}
}

// --- Definition Returns Error ---

func TestCppAdapter_Definition_ReturnsError(t *testing.T) {
	adapter := NewCppAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/main.cpp",
		Content:  "int main() {}",
		Method:   "definition",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for definition")
	}
	if result.Error != "definition requires clangd" {
		t.Errorf("expected error about clangd, got %q", result.Error)
	}
}

// --- References Returns Error ---

func TestCppAdapter_References_ReturnsError(t *testing.T) {
	adapter := NewCppAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/main.cpp",
		Content:  "int main() {}",
		Method:   "references",
		Position: &Position{Line: 1, Column: 1},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Error == "" {
		t.Error("expected non-empty error for references")
	}
	if result.Error != "references requires clangd" {
		t.Errorf("expected error about clangd, got %q", result.Error)
	}
}

// --- Default / Unknown Method Returns Capabilities ---

func TestCppAdapter_Default_ReturnsCapabilities(t *testing.T) {
	adapter := NewCppAdapter()
	result, err := adapter.Run(ToolInput{
		FilePath: "/path/to/main.cpp",
		Content:  "int main() {}",
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

// --- cppLineColToOffset via shared LineColToOffset (1-based columns, convert to 0-based) ---

func TestCppLineColToOffset(t *testing.T) {
	tests := []struct {
		name    string
		content string
		line    int
		col     int // 1-based as clang-tidy reports
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
			// clang-tidy reports 1-based columns; LineColToOffset expects 0-based, so subtract 1
			got := LineColToOffset(tt.content, tt.line, tt.col-1)
			if got != tt.want {
				t.Errorf("LineColToOffset(%q, %d, %d-1) = %d, want %d", tt.content, tt.line, tt.col, got, tt.want)
			}
		})
	}
}

// --- clangTidyPattern ---

func TestClangTidyPattern(t *testing.T) {
	tests := []struct {
		name      string
		line      string
		wantMatch bool
		wantLine  int // 1-based
		wantCol   int // 1-based
		wantLevel string
		wantMsg   string
	}{
		{
			name:      "warning with check name",
			line:      "/tmp/main.cpp:10:5: warning: unused variable 'x' [misc-unused-parameters]",
			wantMatch: true,
			wantLine:  10,
			wantCol:   5,
			wantLevel: "warning",
			wantMsg:   "unused variable 'x'",
		},
		{
			name:      "error with check name",
			line:      "/tmp/main.cpp:3:1: error: use of undeclared identifier 'y' [clang-diagnostic-error]",
			wantMatch: true,
			wantLine:  3,
			wantCol:   1,
			wantLevel: "error",
			wantMsg:   "use of undeclared identifier 'y'",
		},
		{
			name:      "note without check name",
			line:      "/tmp/main.cpp:1:1: note: declaration of 'x' [readability-identifier-naming]",
			wantMatch: true,
			wantLine:  1,
			wantCol:   1,
			wantLevel: "note",
			wantMsg:   "declaration of 'x'",
		},
		{
			name:      "warning without check name",
			line:      "/some/path/file.cpp:42:8: warning: some message here",
			wantMatch: true,
			wantLine:  42,
			wantCol:   8,
			wantLevel: "warning",
			wantMsg:   "some message here",
		},
		{
			name:      "random text should not match",
			line:      "some random text",
			wantMatch: false,
		},
		{
			name:      "empty line should not match",
			line:      "",
			wantMatch: false,
		},
	}

	// Compile the pattern once outside the test loop — the production code uses a package-level var
	pattern := regexp.MustCompile(`^.+?:(\d+):(\d+):\s*(warning|error|note):\s*(.+?)(?:\s*\[([^\]]+)\])?$`)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := pattern.FindStringSubmatch(tt.line)
			if matches == nil && tt.wantMatch {
				t.Errorf("expected match for %q, got no match", tt.line)
				return
			}
			if matches != nil && !tt.wantMatch {
				t.Errorf("expected no match for %q, got match %v", tt.line, matches)
				return
			}
			if matches == nil && !tt.wantMatch {
				return // both false, OK
			}
			// matches[1] = line number, matches[2] = col number, matches[3] = level, matches[4] = message
			// Parse line and col manually (same as production)
			lineNum := 0
			for _, ch := range matches[1] {
				lineNum = lineNum*10 + int(ch-'0')
			}
			colNum := 0
			for _, ch := range matches[2] {
				colNum = colNum*10 + int(ch-'0')
			}
			level := matches[3]
			msg := matches[4]

			if lineNum != tt.wantLine {
				t.Errorf("parsed line = %d, want %d", lineNum, tt.wantLine)
			}
			if colNum != tt.wantCol {
				t.Errorf("parsed col = %d, want %d", colNum, tt.wantCol)
			}
			if level != tt.wantLevel {
				t.Errorf("parsed level = %q, want %q", level, tt.wantLevel)
			}
			if msg != tt.wantMsg {
				t.Errorf("parsed message = %q, want %q", msg, tt.wantMsg)
			}
		})
	}

	// Also verify the package-level var matches our compiled pattern
	t.Run("package-level pattern is usable", func(t *testing.T) {
		line := "/tmp/main.cpp:10:5: warning: unused variable 'x' [misc-unused-parameters]"
		matches := clangTidyPattern.FindStringSubmatch(line)
		if matches == nil {
			t.Error("expected package-level clangTidyPattern to match sample line")
		}
	})
}

// --- Registry Integration ---

func TestRegistryCppAdapter(t *testing.T) {
	registry := NewRegistry()
	// Register both "c" and "cpp" via RegisterAliases
	registry.RegisterAliases(NewCppAdapter, "c", "cpp")

	// Verify "c" alias
	adapterC, ok := registry.AdapterForLanguage("c")
	if !ok {
		t.Fatal("expected adapter for 'c' to be found")
	}
	if adapterC == nil {
		t.Fatal("expected non-nil adapter for 'c'")
	}

	// Verify "cpp" alias
	adapterCpp, ok := registry.AdapterForLanguage("cpp")
	if !ok {
		t.Fatal("expected adapter for 'cpp' to be found")
	}
	if adapterCpp == nil {
		t.Fatal("expected non-nil adapter for 'cpp'")
	}

	// Both should work for diagnostics
	for lang, adapter := range map[string]Adapter{"c": adapterC, "cpp": adapterCpp} {
		result, err := adapter.Run(ToolInput{
			FilePath: "/path/to/main.cpp",
			Content:  "int main() { return 0; }",
			Method:   "diagnostics",
		})
		if err != nil {
			t.Fatalf("unexpected error for %s: %v", lang, err)
		}
		if !result.Capabilities.Diagnostics {
			t.Errorf("expected Capabilities.Diagnostics to be true for %s adapter", lang)
		}
	}
}
