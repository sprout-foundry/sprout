package embedding

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// ---------------------------------------------------------------------------
// TS/JS — Symbol name parity between embedding extractor and AST
// ---------------------------------------------------------------------------

const tsParityFixture = `
// Standalone function
export function greet(name: string): string {
	return "hello " + name;
}

// Arrow function assigned to const
const add = (a: number, b: number): number => {
	return a + b;
};

// Class with methods
export class UserService {
	private db: string;
	readonly name: string;

	constructor(db: string) {
		this.db = db;
	}

	find(id: string): string | null {
		return this.db;
	}

	delete(id: string): void {
		this.db = "";
	}
}

// Interface
export interface User {
	id: string;
	name: string;
}

// Enum
export enum Role {
	Admin = "admin",
	User = "user",
}

// Type alias
export type Result = { ok: boolean };

// Variable (plain literal, should be skipped by embedding)
const MAX_RETRIES = 3;

// Variable assigned to object (should be extracted by embedding)
const config = { host: "localhost", port: 3000 };
`

func TestTSExtractSymbolParity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.ts")
	if err := os.WriteFile(path, []byte(tsParityFixture), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 1. Ground truth: ast.ExtractSymbols
	result, err := ast.ParseFile(path, []byte(tsParityFixture))
	if err != nil {
		t.Fatalf("ast.ParseFile: %v", err)
	}
	defer result.Release()

	scopedSyms := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// 2. Embedding extractor
	units, err := ExtractTSFile(path)
	if err != nil {
		t.Fatalf("ExtractTSFile: %v", err)
	}

	// Build expected name set from AST, applying the same filters the embedding
	// extractor uses: isExtractableKind + isPlainLiteralVar + test function filter.
	astNames := make(map[string]bool)
	for _, ss := range scopedSyms {
		if !isExtractableKind(ss.Kind) {
			continue
		}
		if !tCfgIncludeTests() && isTestFunction(ss.Name, ss.Scope) {
			continue
		}
		if ss.Kind == "variable" && isPlainLiteralVar(ss, result) {
			continue
		}
		astNames[formatScopedName(ss)] = true
	}

	// Build actual name set from embedding extractor
	embedNames := make(map[string]bool)
	for _, u := range units {
		embedNames[u.Name] = true
	}

	astList := sortedKeys(astNames)
	embedList := sortedKeys(embedNames)

	if !slices.Equal(astList, embedList) {
		missing := diffSets(astList, embedList)
		extra := diffSets(embedList, astList)
		var msg strings.Builder
		if len(missing) > 0 {
			msg.WriteString("\nIn AST but NOT in embedding extractor:\n")
			for _, n := range missing {
				msg.WriteString("  - " + n + "\n")
			}
		}
		if len(extra) > 0 {
			msg.WriteString("\nIn embedding but NOT in AST (documented difference):\n")
			for _, n := range extra {
				msg.WriteString("  - " + n + "\n")
			}
		}
		t.Error(msg.String())
	}

	t.Logf("AST (filtered) names: %v", astList)
	t.Logf("Embedding names:      %v", embedList)
}

// ---------------------------------------------------------------------------
// Python — Symbol name parity between embedding extractor and AST
// ---------------------------------------------------------------------------

const pyParityFixture = `
import os
from typing import List

def greet(name: str) -> str:
    return f"hello {name}"

class Calculator:
    total: int = 0

    def add(self, a: int, b: int) -> int:
        return a + b

    def multiply(self, a: int, b: int) -> int:
        return a * b

    @staticmethod
    def helper():
        pass

class NestedExample:
    class Inner:
        def inner_method(self):
            pass

    def outer_method(self):
        pass

@cache
def compute(n: int) -> int:
    return n * 2

async def fetch_data(url):
    pass
`

func TestPyExtractSymbolParity(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fixture.py")
	if err := os.WriteFile(path, []byte(pyParityFixture), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 1. Ground truth: ast.ExtractSymbols
	result, err := ast.ParseFile(path, []byte(pyParityFixture))
	if err != nil {
		t.Fatalf("ast.ParseFile: %v", err)
	}
	defer result.Release()

	scopedSyms := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// 2. Embedding extractor
	units, err := ExtractPyFile(path)
	if err != nil {
		t.Fatalf("ExtractPyFile: %v", err)
	}

	// Build expected name set from AST, applying the same filters the embedding
	// extractor uses: isExtractableKind + test function filter.
	astNames := make(map[string]bool)
	for _, ss := range scopedSyms {
		if !isExtractableKind(ss.Kind) {
			continue
		}
		if !tCfgIncludeTests() && isTestFunction(ss.Name, ss.Scope) {
			continue
		}
		astNames[formatScopedName(ss)] = true
	}

	// Build actual name set from embedding extractor
	embedNames := make(map[string]bool)
	for _, u := range units {
		embedNames[u.Name] = true
	}

	astList := sortedKeys(astNames)
	embedList := sortedKeys(embedNames)

	if !slices.Equal(astList, embedList) {
		missing := diffSets(astList, embedList)
		extra := diffSets(embedList, astList)
		var msg strings.Builder
		if len(missing) > 0 {
			msg.WriteString("\nIn AST but NOT in embedding extractor:\n")
			for _, n := range missing {
				msg.WriteString("  - " + n + "\n")
			}
		}
		if len(extra) > 0 {
			msg.WriteString("\nIn embedding but NOT in AST (documented difference):\n")
			for _, n := range extra {
				msg.WriteString("  - " + n + "\n")
			}
		}
		t.Error(msg.String())
	}

	t.Logf("AST (filtered) names: %v", astList)
	t.Logf("Embedding names:      %v", embedList)
}

// ---------------------------------------------------------------------------
// index/symbols.go — Symbol name parity
// ---------------------------------------------------------------------------

func TestIndexSymbolParity(t *testing.T) {
	dir := t.TempDir()

	t.Run("TypeScript", func(t *testing.T) {
		path := filepath.Join(dir, "fixture.ts")
		if err := os.WriteFile(path, []byte(tsParityFixture), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		result, err := ast.ParseFile(path, []byte(tsParityFixture))
		if err != nil {
			t.Fatalf("ast.ParseFile: %v", err)
		}
		defer result.Release()

		scopedSyms := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
		idxSymbols := extractSymbolsViaAST(path, []byte(tsParityFixture))

		// Both should agree on the top-level symbol names that are indexable.
		astNames := make(map[string]bool)
		for _, ss := range scopedSyms {
			if ss.Depth > 0 {
				continue
			}
			if !isIndexableKind(ss.Kind) {
				continue
			}
			astNames[ss.Name] = true
		}

		idxNames := make(map[string]bool)
		for _, s := range idxSymbols {
			idxNames[s.Name] = true
		}

		astList := sortedKeys(astNames)
		idxList := sortedKeys(idxNames)

		if !slices.Equal(astList, idxList) {
			missing := diffSets(astList, idxList)
			extra := diffSets(idxList, astList)
			var msg strings.Builder
			if len(missing) > 0 {
				msg.WriteString("\nIn AST but NOT in index:\n")
				for _, n := range missing {
					msg.WriteString("  - " + n + "\n")
				}
			}
			if len(extra) > 0 {
				msg.WriteString("\nIn index but NOT in AST:\n")
				for _, n := range extra {
					msg.WriteString("  - " + n + "\n")
				}
			}
			t.Error(msg.String())
		}

		t.Logf("AST top-level names: %v", astList)
		t.Logf("Index names:         %v", idxList)
	})

	t.Run("Python", func(t *testing.T) {
		path := filepath.Join(dir, "fixture.py")
		if err := os.WriteFile(path, []byte(pyParityFixture), 0644); err != nil {
			t.Fatalf("WriteFile: %v", err)
		}

		result, err := ast.ParseFile(path, []byte(pyParityFixture))
		if err != nil {
			t.Fatalf("ast.ParseFile: %v", err)
		}
		defer result.Release()

		scopedSyms := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
		idxSymbols := extractSymbolsViaAST(path, []byte(pyParityFixture))

		astNames := make(map[string]bool)
		for _, ss := range scopedSyms {
			if ss.Depth > 0 {
				continue
			}
			if !isIndexableKind(ss.Kind) {
				continue
			}
			astNames[ss.Name] = true
		}

		idxNames := make(map[string]bool)
		for _, s := range idxSymbols {
			idxNames[s.Name] = true
		}

		astList := sortedKeys(astNames)
		idxList := sortedKeys(idxNames)

		if !slices.Equal(astList, idxList) {
			missing := diffSets(astList, idxList)
			extra := diffSets(idxList, astList)
			var msg strings.Builder
			if len(missing) > 0 {
				msg.WriteString("\nIn AST but NOT in index:\n")
				for _, n := range missing {
					msg.WriteString("  - " + n + "\n")
				}
			}
			if len(extra) > 0 {
				msg.WriteString("\nIn index but NOT in AST:\n")
				for _, n := range extra {
					msg.WriteString("  - " + n + "\n")
				}
			}
			t.Error(msg.String())
		}

		t.Logf("AST top-level names: %v", astList)
		t.Logf("Index names:         %v", idxList)
	})
}

// ---------------------------------------------------------------------------
// Helpers — mirror pkg/index/symbols.go for testing without import
// ---------------------------------------------------------------------------

// indexSymbol mirrors pkg/index.Symbol for parity testing.
type indexSymbol struct {
	Name string
	Kind string
	Line int
}

// extractSymbolsViaAST mirrors pkg/index/symbols.go:extractSymbolsViaAST
// so we can call it from this test without importing the index package.
func extractSymbolsViaAST(path string, content []byte) []indexSymbol {
	result, err := ast.ParseFile(path, content)
	if err != nil {
		return nil
	}
	defer result.Release()

	scopedSymbols := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
	if scopedSymbols == nil {
		return nil
	}

	isGo := result.Language == "go"
	var out []indexSymbol
	for _, ss := range scopedSymbols {
		if ss.Depth > 1 {
			continue
		}
		if ss.Depth == 1 && !(ss.Kind == "method" && isGo) {
			continue
		}
		if !isIndexableKind(ss.Kind) {
			continue
		}
		kind := mapKind(ss.Kind)
		if kind == "" {
			continue
		}
		out = append(out, indexSymbol{
			Name: ss.Name,
			Kind: kind,
			Line: ss.StartLine,
		})
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// isIndexableKind returns true for AST kinds that are useful for the index
// (mirrors pkg/index/symbols.go).
func isIndexableKind(kind string) bool {
	switch kind {
	case "function", "method", "class", "interface", "type", "variable", "constant", "enum":
		return true
	default:
		return false
	}
}

// mapKind converts an AST kind string to the index kind string
// (mirrors pkg/index/symbols.go).
func mapKind(kind string) string {
	switch kind {
	case "function":
		return "func"
	case "method", "class", "interface", "type", "variable", "constant", "enum":
		return kind
	default:
		return ""
	}
}

// ---------------------------------------------------------------------------
// Common helpers
// ---------------------------------------------------------------------------

// tCfgIncludeTests mirrors the default ExtractConfig.IncludeTests value.
func tCfgIncludeTests() bool { return false }

// formatScopedName formats a ScopedSymbol the same way the embedding extractor
// does: "Scope.Name" for scoped, "Name" for top-level.
func formatScopedName(ss ast.ScopedSymbol) string {
	if ss.Scope != "" {
		return ss.Scope + "." + ss.Name
	}
	return ss.Name
}

// sortedKeys returns the keys of a map as a sorted slice.
func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// diffSets returns elements in a that are not in b.
func diffSets(a []string, b []string) []string {
	bset := make(map[string]bool, len(b))
	for _, x := range b {
		bset[x] = true
	}
	var diff []string
	for _, x := range a {
		if !bset[x] {
			diff = append(diff, x)
		}
	}
	return diff
}
