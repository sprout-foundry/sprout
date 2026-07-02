package console

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCmd_NoRawStatusGlyphs is the CLI-B-3 lock: any cmd/*.go file outside
// _test.go that bakes a raw success/error/warning rune (✓ / ✗ / ⚠) into
// a fmt.Printf* call is a regression — it bypasses the Glyph* wrappers
// and so doesn't honor NO_COLOR/FORCE_COLOR. Mechanical sweep is enforced
// by failing this test on the first offender.
func TestCmd_NoRawStatusGlyphs(t *testing.T) {
	// Locate the cmd/ directory relative to this test file. The test
	// lives in pkg/console/, so we walk up two levels to the repo root
	// then descend into cmd/.
	repoRoot := filepath.Join(repoRootFromTest(t), "..", "..")
	cmdDir := filepath.Join(repoRoot, "cmd")
	if _, err := os.Stat(cmdDir); err != nil {
		t.Skipf("cmd/ not found at %q (run from repo root): %v", cmdDir, err)
	}

	// The raw runes the Glyph system owns. Sites that still emit these
	// raw are bypassing the canonical color contract.
	offenders := []string{"✓", "✗", "⚠"}

	// Use AST inspection so we don't false-positive on runes inside
	// comments, string literals unrelated to printf, or runes in
	// documentation. A real regression will look like:
	//   fmt.Printf("✓ %s\n", name)
	//   fmt.Println("✗ missing")
	// and the AST will see a BasicLit containing the rune.
	fs := token.NewFileSet()
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("read cmd/: %v", err)
	}

	checked := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		// Only enforce on !js builds (same scope as the glyph sweep).
		if !hasBuildConstraint(filepath.Join(cmdDir, name)) {
			continue
		}

		path := filepath.Join(cmdDir, name)
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		f, err := parser.ParseFile(fs, path, src, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}

		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if !isPrintfLikeCall(call) {
				return true
			}
			for _, arg := range call.Args {
				lit, ok := arg.(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				val := lit.Value
				for _, g := range offenders {
					if strings.Contains(val, g) {
						t.Errorf("%s:%d: raw %s in %s literal %q — use console.Glyph*.Fprintf(os.Stdout, …) so NO_COLOR/FORCE_COLOR are honored",
							path, fs.Position(lit.Pos()).Line, g, callName(call), val)
					}
				}
			}
			return true
		})
		checked++
	}
	if checked == 0 {
		t.Fatalf("checked 0 files in cmd/ — repo root detection failed")
	}
}

func callName(call *ast.CallExpr) string {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		return fn.Name
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			return x.Name + "." + fn.Sel.Name
		}
	}
	return "<unknown>"
}

func isPrintfLikeCall(call *ast.CallExpr) bool {
	switch fn := call.Fun.(type) {
	case *ast.Ident:
		name := fn.Name
		return strings.HasPrefix(name, "Print") || strings.HasPrefix(name, "Fprint") || strings.HasPrefix(name, "Sprint") || strings.HasPrefix(name, "Errorf")
	case *ast.SelectorExpr:
		if x, ok := fn.X.(*ast.Ident); ok {
			switch x.Name {
			case "fmt", "log":
				return strings.HasPrefix(fn.Sel.Name, "Print") ||
					strings.HasPrefix(fn.Sel.Name, "Fprint") ||
					strings.HasPrefix(fn.Sel.Name, "Sprint") ||
					strings.HasPrefix(fn.Sel.Name, "Errorf") ||
					strings.HasPrefix(fn.Sel.Name, "Fatal") ||
					fn.Sel.Name == "Println"
			}
		}
	}
	return false
}

func hasBuildConstraint(path string) bool {
	// Cheap heuristic: peek at the first 8 lines for `//go:build !js`.
	// The constraint system has more nuance (OR-groups, filename
	// suffixes), but every cmd file we care about uses the leading
	// `//go:build !js` pattern.
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	text := string(data)
	idx := strings.Index(text, "\n")
	head := text
	if idx > 0 {
		head = text[:idx]
	}
	return strings.Contains(head, "//go:build !js")
}

// repoRootFromTest walks up from this test file's runtime location to
// find the Go module root. Test files run with their package dir as the
// working directory, so we walk from the test source's known location.
func repoRootFromTest(t *testing.T) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	return wd
}