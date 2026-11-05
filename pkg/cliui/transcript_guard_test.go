package cliui

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestNoRawTranscriptWritesInCliui pins SP-171a: every line that reaches the
// conversation area must go through console.WriteTranscript (or
// console.SectionBreak / console.BlankLine), so it lands inside the status
// footer's scroll region. A raw fmt.Print* to stdout or stderr bypasses the
// region and can overwrite the pinned rows or scroll the whole screen.
//
// This is a source-level guard because the bug is invisible at runtime until
// the terminal is in a particular state; a unit test of the output cannot
// catch a writer that is simply not routed.
func TestNoRawTranscriptWritesInCliui(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}

	// Writes that are deliberately not transcript: the turn-end sink used by
	// tests, and the terminal subscriber's fallback when no reader is active.
	allowed := map[string]bool{}

	fset := token.NewFileSet()
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		if allowed[name] {
			continue
		}
		path := filepath.Join(".", name)
		f, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok || pkg.Name != "fmt" {
				return true
			}
			switch sel.Sel.Name {
			case "Print", "Printf", "Println", "Fprint", "Fprintf", "Fprintln":
			default:
				return true
			}
			// Fprint* to an explicit sink (a test writer, a bytes.Buffer) is
			// fine; only the os.Stdout / os.Stderr forms bypass the region.
			if strings.HasPrefix(sel.Sel.Name, "F") {
				if len(call.Args) == 0 {
					return true
				}
				target, ok := call.Args[0].(*ast.SelectorExpr)
				if !ok {
					return true
				}
				id, ok := target.X.(*ast.Ident)
				if !ok || id.Name != "os" {
					return true
				}
			}
			pos := fset.Position(call.Pos())
			if exemptFromTranscriptGuard(fset, f, call) {
				return true
			}
			t.Errorf("%s:%d: raw %s bypasses the transcript writer; use console.WriteTranscript, console.SectionBreak, or console.BlankLine",
				pos.Filename, pos.Line, sel.Sel.Name)
			return true
		})
	}
}

// exemptFromTranscriptGuard honors a `//nolint:transcript-guard` comment on
// the call's line. Reserved for writes that are not transcript text at all —
// the terminal bell, for one, which must not carry a cursor-park sequence.
func exemptFromTranscriptGuard(fset *token.FileSet, f *ast.File, call *ast.CallExpr) bool {
	line := fset.Position(call.Pos()).Line
	for _, cg := range f.Comments {
		for _, c := range cg.List {
			if !strings.Contains(c.Text, "nolint:transcript-guard") {
				continue
			}
			if fset.Position(c.Pos()).Line == line {
				return true
			}
		}
	}
	return false
}
