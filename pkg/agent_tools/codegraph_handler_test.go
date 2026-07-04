package tools

import (
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/codegraph"
)

// ============================================================================
// formatSymbolListHandler tests
// ============================================================================

func TestFormatSymbolListHandler_Empty(t *testing.T) {
	result := formatSymbolListHandler("TestTitle", nil)
	expected := "TestTitle:\n"
	if result != expected {
		t.Errorf("formatSymbolListHandler(nil) = %q, want %q", result, expected)
	}

	result = formatSymbolListHandler("TestTitle", []codegraph.Symbol{})
	if result != expected {
		t.Errorf("formatSymbolListHandler([]) = %q, want %q", result, expected)
	}
}

func TestFormatSymbolListHandler_Single(t *testing.T) {
	symbols := []codegraph.Symbol{
		{Kind: "func", QualifiedName: "pkg/foo.Bar", FilePath: "pkg/foo.go", Line: 42},
	}

	result := formatSymbolListHandler("TestTitle", symbols)
	expected := "TestTitle:\n  - func pkg/foo.Bar (pkg/foo.go:42)\n"
	if result != expected {
		t.Errorf("formatSymbolListHandler() = %q, want %q", result, expected)
	}
}

func TestFormatSymbolListHandler_Multiple(t *testing.T) {
	symbols := []codegraph.Symbol{
		{Kind: "func", QualifiedName: "pkg/a.Foo", FilePath: "pkg/a.go", Line: 10},
		{Kind: "method", QualifiedName: "pkg/b.(*Widget).DoIt", FilePath: "pkg/b.go", Line: 55},
		{Kind: "func", QualifiedName: "main.main", FilePath: "main.go", Line: 1},
	}

	result := formatSymbolListHandler("Symbols", symbols)
	if result == "" {
		t.Fatal("formatSymbolListHandler returned empty string for 3 symbols")
	}

	// Verify the title prefix is present.
	if !strings.HasPrefix(result, "Symbols:\n") {
		t.Errorf("expected title prefix \"Symbols:\\n\", got: %s", result)
	}

	// Verify each symbol appears in the output.
	lines := []string{
		"  - func pkg/a.Foo (pkg/a.go:10)",
		"  - method pkg/b.(*Widget).DoIt (pkg/b.go:55)",
		"  - func main.main (main.go:1)",
	}
	for _, want := range lines {
		if !strings.Contains(result, want) {
			t.Errorf("formatSymbolListHandler output missing %q\nGot:\n%s", want, result)
		}
	}
}

// ============================================================================
// Tool registration tests
// ============================================================================

func TestCodegraphTools_Registration(t *testing.T) {
	handlerReg := GetNewToolRegistry()

	type toolCheck struct {
		name       string
		paramCount int
		required   int
		reqParam   string
	}

	tools := []toolCheck{
		{"get_callers", 1, 1, "qualified_name"},
		{"get_callees", 1, 1, "qualified_name"},
		{"find_dead_code", 1, 0, ""},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			h, found := handlerReg.Lookup(tc.name)
			if !found {
				t.Fatalf("tool %q is not registered in handler registry", tc.name)
			}
			def := h.Definition()

			if def.Name != tc.name {
				t.Errorf("tool name = %q, want %q", def.Name, tc.name)
			}
			if def.Description == "" {
				t.Errorf("tool %q should have a non-empty description", tc.name)
			}

			if len(def.Parameters) != tc.paramCount {
				t.Errorf("tool %q: expected %d parameters, got %d",
					tc.name, tc.paramCount, len(def.Parameters))
			}

			// Verify required parameter.
			if tc.required > 0 {
				requiredSet := make(map[string]struct{}, len(def.Required))
				for _, rn := range def.Required {
					requiredSet[rn] = struct{}{}
				}
				if _, ok := requiredSet[tc.reqParam]; !ok {
					t.Errorf("tool %q: required parameter %q not found in Required list",
						tc.name, tc.reqParam)
				}
			}
		})
	}
}

// ============================================================================
// Parameter definition tests
// ============================================================================

func TestGetCallers_Parameters(t *testing.T) {
	h, found := GetNewToolRegistry().Lookup("get_callers")
	if !found {
		t.Fatal("get_callers not registered in handler registry")
	}
	def := h.Definition()

	if len(def.Parameters) != 1 {
		t.Fatalf("get_callers: expected 1 parameter, got %d", len(def.Parameters))
	}
	p := def.Parameters[0]
	if p.Name != "qualified_name" {
		t.Errorf("param name = %q, want %q", p.Name, "qualified_name")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if !p.Required {
		t.Error("qualified_name should be required")
	}
	if !isInRequired(p.Name, def.Required) {
		t.Error("qualified_name should be in Required list")
	}
}

func TestGetCallees_Parameters(t *testing.T) {
	h, found := GetNewToolRegistry().Lookup("get_callees")
	if !found {
		t.Fatal("get_callees not registered in handler registry")
	}
	def := h.Definition()

	if len(def.Parameters) != 1 {
		t.Fatalf("get_callees: expected 1 parameter, got %d", len(def.Parameters))
	}
	p := def.Parameters[0]
	if p.Name != "qualified_name" {
		t.Errorf("param name = %q, want %q", p.Name, "qualified_name")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if !p.Required {
		t.Error("qualified_name should be required")
	}
	if !isInRequired(p.Name, def.Required) {
		t.Error("qualified_name should be in Required list")
	}
}

func TestFindDeadCode_Parameters(t *testing.T) {
	h, found := GetNewToolRegistry().Lookup("find_dead_code")
	if !found {
		t.Fatal("find_dead_code not registered in handler registry")
	}
	def := h.Definition()

	if len(def.Parameters) != 1 {
		t.Fatalf("find_dead_code: expected 1 parameter, got %d", len(def.Parameters))
	}
	p := def.Parameters[0]
	if p.Name != "directory" {
		t.Errorf("param name = %q, want %q", p.Name, "directory")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if p.Required {
		t.Error("directory should NOT be required")
	}
	if isInRequired(p.Name, def.Required) {
		t.Error("directory should NOT be in Required list")
	}
}

// ============================================================================
// requireArgs helper tests
// ============================================================================

func TestRequireArgs_AllPresent(t *testing.T) {
	err := requireArgs("test_tool", map[string]any{
		"foo": "bar",
		"baz": "qux",
	}, "foo", "baz")
	if err != nil {
		t.Errorf("expected nil error, got: %v", err)
	}
}

func TestRequireArgs_Missing(t *testing.T) {
	err := requireArgs("test_tool", map[string]any{
		"foo": "bar",
	}, "foo", "missing_key")
	if err == nil {
		t.Fatal("expected error for missing key")
	}
	if !strings.Contains(err.Error(), "missing required parameter") {
		t.Errorf("expected 'missing required parameter' error, got: %v", err)
	}
}

func TestRequireArgs_EmptyString(t *testing.T) {
	err := requireArgs("test_tool", map[string]any{
		"foo": "",
	}, "foo")
	if err == nil {
		t.Fatal("expected error for empty string")
	}
	if !strings.Contains(err.Error(), "must not be empty") {
		t.Errorf("expected 'must not be empty' error, got: %v", err)
	}
}

func TestRequireArgs_NonStringValue(t *testing.T) {
	// Non-string values (e.g., int) should not trigger the empty-string check.
	err := requireArgs("test_tool", map[string]any{
		"count": 42,
	}, "count")
	if err != nil {
		t.Errorf("expected nil error for non-string value, got: %v", err)
	}
}

// ============================================================================
// Helpers
// ============================================================================

// isInRequired checks if name is in the Required list from a handler definition.
func isInRequired(name string, required []string) bool {
	for _, r := range required {
		if r == name {
			return true
		}
	}
	return false
}
