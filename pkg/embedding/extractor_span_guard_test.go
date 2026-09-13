package embedding

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// Regression tests for AST-span slicing guards. Tree-sitter spans can be
// inverted or past-end on malformed input; slicing source bytes with such a
// span panics with "slice bounds out of range" and takes down the whole
// agent turn (the panic surfaces through the query-loop recover).

func TestIsPlainLiteralVarInvalidSpans(t *testing.T) {
	result := &ast.ASTResult{Source: []byte("const x = 1;")}

	cases := []struct {
		name string
		sym  ast.ScopedSymbol
	}{
		{"inverted span", ast.ScopedSymbol{Symbol: ast.Symbol{Name: "x", Kind: "variable", StartByte: 10, EndByte: 4}}},
		{"end past source", ast.ScopedSymbol{Symbol: ast.Symbol{Name: "x", Kind: "variable", StartByte: 0, EndByte: 9999}}},
		{"start past source", ast.ScopedSymbol{Symbol: ast.Symbol{Name: "x", Kind: "variable", StartByte: 9999, EndByte: 10000}}},
		{"negative start", ast.ScopedSymbol{Symbol: ast.Symbol{Name: "x", Kind: "variable", StartByte: -1, EndByte: 5}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// Must not panic; an invalid span is classified as a plain
			// literal so the symbol is skipped.
			if !isPlainLiteralVar(tc.sym, result) {
				t.Fatalf("isPlainLiteralVar(%+v) = false, want true", tc.sym)
			}
		})
	}
}

func TestExtractPyFileNoTrailingNewline(t *testing.T) {
	// A file with no trailing newline previously let lineEndByte overcount
	// the final line's end offset by one (its phantom newline), which could
	// push bodyEndByte past len(src) and panic on the body slice.
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.py")
	src := "def greet(name):\n    return \"hello \" + name"
	if err := os.WriteFile(path, []byte(src), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("ExtractPyFile: %v", err)
	}
	if len(units) == 0 {
		t.Fatal("ExtractPyFile returned no units for a valid module")
	}
	found := false
	for _, u := range units {
		if u.Name == "greet" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected unit %q, got %+v", "greet", units)
	}
}
