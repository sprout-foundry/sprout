package agent

import (
	"context"
	"strings"
	"testing"

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
	registry := newDefaultToolRegistry()

	tools := []struct {
		name        string
		paramCount  int
		required    int
		reqParam    string
		hasHandler  bool
		hasDesc     bool
	}{
		{"get_callers", 1, 1, "qualified_name", true, true},
		{"get_callees", 1, 1, "qualified_name", true, true},
		{"find_dead_code", 1, 0, "", true, true},
	}

	for _, tc := range tools {
		t.Run(tc.name, func(t *testing.T) {
			config, found := registry.GetToolConfig(tc.name)
			if !found {
				t.Fatalf("tool %q not registered", tc.name)
			}

			if !tc.hasDesc && config.Description == "" {
				t.Errorf("tool %q has empty description", tc.name)
			}
			if tc.hasDesc && config.Description == "" {
				t.Errorf("tool %q should have a non-empty description", tc.name)
			}

			if tc.hasHandler && config.Handler == nil {
				t.Errorf("tool %q has nil handler", tc.name)
			}

			if len(config.Parameters) != tc.paramCount {
				t.Errorf("tool %q: expected %d parameters, got %d",
					tc.name, tc.paramCount, len(config.Parameters))
			}

			// Verify required parameter.
			if tc.required > 0 {
				foundReq := false
				for _, p := range config.Parameters {
					if p.Name == tc.reqParam && p.Required {
						foundReq = true
						break
					}
				}
				if !foundReq {
					t.Errorf("tool %q: required parameter %q not found or not marked required",
						tc.name, tc.reqParam)
				}
			}
		})
	}
}

func TestGetCallers_Parameters(t *testing.T) {
	registry := newDefaultToolRegistry()
	config, found := registry.GetToolConfig("get_callers")
	if !found {
		t.Fatal("get_callers not registered")
	}

	// Verify the parameter type and alternatives.
	if len(config.Parameters) != 1 {
		t.Fatalf("get_callers: expected 1 parameter, got %d", len(config.Parameters))
	}
	p := config.Parameters[0]
	if p.Name != "qualified_name" {
		t.Errorf("param name = %q, want %q", p.Name, "qualified_name")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if !p.Required {
		t.Error("qualified_name should be required")
	}
	// Check alternatives.
	expectedAlts := []string{"name", "symbol"}
	if len(p.Alternatives) != len(expectedAlts) {
		t.Errorf("alternatives: got %v, want %v", p.Alternatives, expectedAlts)
	}
}

func TestGetCallees_Parameters(t *testing.T) {
	registry := newDefaultToolRegistry()
	config, found := registry.GetToolConfig("get_callees")
	if !found {
		t.Fatal("get_callees not registered")
	}

	if len(config.Parameters) != 1 {
		t.Fatalf("get_callees: expected 1 parameter, got %d", len(config.Parameters))
	}
	p := config.Parameters[0]
	if p.Name != "qualified_name" {
		t.Errorf("param name = %q, want %q", p.Name, "qualified_name")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if !p.Required {
		t.Error("qualified_name should be required")
	}
}

func TestFindDeadCode_Parameters(t *testing.T) {
	registry := newDefaultToolRegistry()
	config, found := registry.GetToolConfig("find_dead_code")
	if !found {
		t.Fatal("find_dead_code not registered")
	}

	if len(config.Parameters) != 1 {
		t.Fatalf("find_dead_code: expected 1 parameter, got %d", len(config.Parameters))
	}
	p := config.Parameters[0]
	if p.Name != "directory" {
		t.Errorf("param name = %q, want %q", p.Name, "directory")
	}
	if p.Type != "string" {
		t.Errorf("param type = %q, want %q", p.Type, "string")
	}
	if p.Required {
		t.Error("directory should NOT be required")
	}
	expectedAlts := []string{"dir"}
	if len(p.Alternatives) != len(expectedAlts) {
		t.Errorf("alternatives: got %v, want %v", p.Alternatives, expectedAlts)
	}
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
