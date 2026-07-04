package agent

import (
	"context"
	"strings"
	"testing"

	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/codegraph"
)

// ============================================================================
// formatSymbolList tests
// ============================================================================

func TestFormatSymbolList_Empty(t *testing.T) {
	result := formatSymbolList(nil)
	if result != "" {
		t.Errorf("formatSymbolList(nil) = %q, want empty string", result)
	}

	result = formatSymbolList([]codegraph.Symbol{})
	if result != "" {
		t.Errorf("formatSymbolList([]) = %q, want empty string", result)
	}
}

func TestFormatSymbolList_Single(t *testing.T) {
	symbols := []codegraph.Symbol{
		{Kind: "func", QualifiedName: "pkg/foo.Bar", FilePath: "pkg/foo.go", Line: 42},
	}

	result := formatSymbolList(symbols)
	expected := "  - func pkg/foo.Bar (pkg/foo.go:42)\n"
	if result != expected {
		t.Errorf("formatSymbolList() = %q, want %q", result, expected)
	}
}

func TestFormatSymbolList_Multiple(t *testing.T) {
	symbols := []codegraph.Symbol{
		{Kind: "func", QualifiedName: "pkg/a.Foo", FilePath: "pkg/a.go", Line: 10},
		{Kind: "method", QualifiedName: "pkg/b.(*Widget).DoIt", FilePath: "pkg/b.go", Line: 55},
		{Kind: "func", QualifiedName: "main.main", FilePath: "main.go", Line: 1},
	}

	result := formatSymbolList(symbols)
	if result == "" {
		t.Fatal("formatSymbolList returned empty string for 3 symbols")
	}

	// Verify each symbol appears in the output.
	lines := []string{
		"  - func pkg/a.Foo (pkg/a.go:10)",
		"  - method pkg/b.(*Widget).DoIt (pkg/b.go:55)",
		"  - func main.main (main.go:1)",
	}
	for _, want := range lines {
		if !strings.Contains(result, want) {
			t.Errorf("formatSymbolList output missing %q\nGot:\n%s", want, result)
		}
	}
}

// ============================================================================
// Tool registration tests
// ============================================================================

func TestCodegraphTools_Registration(t *testing.T) {
	handlerReg := tools.GetNewToolRegistry()

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
				t.Skipf("tool %q not registered in handler registry (legacy-only)", tc.name)
			}
			def := h.Definition()

			if tc.name != "" && def.Description == "" {
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

func TestGetCallers_Parameters(t *testing.T) {
	h, found := tools.GetNewToolRegistry().Lookup("get_callers")
	if !found {
		t.Skip("get_callers not registered in handler registry (legacy-only)")
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
	if !p.Required && !isInRequired(p.Name, def.Required) {
		t.Error("qualified_name should be required")
	}
}

func TestGetCallees_Parameters(t *testing.T) {
	h, found := tools.GetNewToolRegistry().Lookup("get_callees")
	if !found {
		t.Skip("get_callees not registered in handler registry (legacy-only)")
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
	if !p.Required && !isInRequired(p.Name, def.Required) {
		t.Error("qualified_name should be required")
	}
}

func TestFindDeadCode_Parameters(t *testing.T) {
	h, found := tools.GetNewToolRegistry().Lookup("find_dead_code")
	if !found {
		t.Skip("find_dead_code not registered in handler registry (legacy-only)")
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

// isInRequired checks if name is in the Required list from a handler definition.
func isInRequired(name string, required []string) bool {
	for _, r := range required {
		if r == name {
			return true
		}
	}
	return false
}

// ============================================================================
// Handler "not indexed" flow tests
//
// openCodegraphStore() returns (nil, nil) when there's no git root or the
// .sprout/codegraph.db file doesn't exist. In that case the handlers should
// return a friendly "not indexed" message instead of an error.
//
// We test this by calling openCodegraphStore() first to determine whether
// the DB exists. If it doesn't, we exercise the "not indexed" path.
// If it does (e.g. the developer has indexed the repo), we still verify
// the handlers run without panic.
// ============================================================================

func TestGetCallers_NotIndexed(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store != nil {
		defer store.Close()
		t.Skip("codegraph DB exists — skipping 'not indexed' path; DB is already indexed")
	}

	ctx := context.Background()
	agent := NewTestAgent()
	result, err := handleGetCallers(ctx, agent, map[string]interface{}{
		"qualified_name": "pkg/foo.Bar",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result for not-indexed case")
	}
	if !strings.Contains(result, "not been indexed") {
		t.Errorf("expected 'not been indexed' message, got: %s", result)
	}
}

func TestGetCallees_NotIndexed(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store != nil {
		defer store.Close()
		t.Skip("codegraph DB exists — skipping 'not indexed' path; DB is already indexed")
	}

	ctx := context.Background()
	agent := NewTestAgent()
	result, err := handleGetCallees(ctx, agent, map[string]interface{}{
		"qualified_name": "pkg/foo.Bar",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result for not-indexed case")
	}
	if !strings.Contains(result, "not been indexed") {
		t.Errorf("expected 'not been indexed' message, got: %s", result)
	}
}

func TestFindDeadCode_NotIndexed(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store != nil {
		defer store.Close()
		t.Skip("codegraph DB exists — skipping 'not indexed' path; DB is already indexed")
	}

	ctx := context.Background()
	agent := NewTestAgent()
	result, err := handleFindDeadCode(ctx, agent, map[string]interface{}{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == "" {
		t.Fatal("expected non-empty result for not-indexed case")
	}
	if !strings.Contains(result, "not been indexed") {
		t.Errorf("expected 'not been indexed' message, got: %s", result)
	}
}

// ============================================================================
// Handler output format tests (when DB is available)
//
// These tests verify the output format when the codegraph DB exists.
// They are skipped gracefully if the DB hasn't been indexed yet.
// ============================================================================

func TestGetCallers_OutputFormat(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store == nil {
		t.Skip("codegraph DB does not exist — skipping output format test")
	}
	defer store.Close()

	ctx := context.Background()
	agent := NewTestAgent()

	// Query a well-known symbol that likely exists in the repo.
	result, err := handleGetCallers(ctx, agent, map[string]interface{}{
		"qualified_name": "main.main",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// The result should either list callers or say "no callers found".
	if !strings.Contains(result, "Callers of") && !strings.Contains(result, "No callers found") {
		t.Errorf("unexpected output format: %s", result)
	}
}

func TestGetCallees_OutputFormat(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store == nil {
		t.Skip("codegraph DB does not exist — skipping output format test")
	}
	defer store.Close()

	ctx := context.Background()
	agent := NewTestAgent()

	result, err := handleGetCallees(ctx, agent, map[string]interface{}{
		"qualified_name": "main.main",
	})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Callees of") && !strings.Contains(result, "No callees found") {
		t.Errorf("unexpected output format: %s", result)
	}
}

func TestFindDeadCode_OutputFormat(t *testing.T) {
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store == nil {
		t.Skip("codegraph DB does not exist — skipping output format test")
	}
	defer store.Close()

	ctx := context.Background()
	agent := NewTestAgent()

	result, err := handleFindDeadCode(ctx, agent, map[string]interface{}{})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(result, "Dead code found") && !strings.Contains(result, "No dead code found") {
		t.Errorf("unexpected output format: %s", result)
	}
}

func TestFindDeadCode_DirectoryParameterIgnored(t *testing.T) {
	// The handler captures the directory parameter but currently doesn't
	// use it for filtering. Verify the handler doesn't error when a
	// directory is passed.
	store, err := openCodegraphStore()
	if err != nil {
		t.Skipf("openCodegraphStore returned error: %v", err)
	}
	if store == nil {
		// Even without DB, the handler should not error on the directory param.
		ctx := context.Background()
		agent := NewTestAgent()
		result, err := handleFindDeadCode(ctx, agent, map[string]interface{}{
			"directory": "some/path",
		})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(result, "not been indexed") {
			t.Errorf("expected 'not been indexed' message, got: %s", result)
		}
		return
	}
	defer store.Close()

	ctx := context.Background()
	agent := NewTestAgent()
	_, err = handleFindDeadCode(ctx, agent, map[string]interface{}{
		"directory": "some/path",
	})
	if err != nil {
		t.Fatalf("unexpected error with directory parameter: %v", err)
	}
}
