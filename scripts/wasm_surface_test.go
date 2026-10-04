package scripts_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// Every function the WASM shell exposes to JavaScript is collected from the
// *JSFuncs() maps into one object passed to js.ValueOf at startup. A bare Go
// func there makes js.ValueOf panic and the browser runtime never loads, so
// each entry must be wrapped in js.FuncOf. cmd/wasm only builds for js/wasm,
// so this checks the source instead of running it.
func TestWASMSurfaceFuncsAreWrapped(t *testing.T) {
	dir := filepath.Join(repoRoot(t), "cmd", "wasm")
	fset := token.NewFileSet()
	files, err := filepath.Glob(filepath.Join(dir, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	checked := 0
	for _, path := range files {
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		for _, decl := range f.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Body == nil || len(fn.Name.Name) < len("JSFuncs") || fn.Name.Name[len(fn.Name.Name)-len("JSFuncs"):] != "JSFuncs" {
				continue
			}
			ast.Inspect(fn.Body, func(n ast.Node) bool {
				lit, ok := n.(*ast.CompositeLit)
				if !ok {
					return true
				}
				if _, isMap := lit.Type.(*ast.MapType); !isMap {
					return true
				}
				for _, elt := range lit.Elts {
					kv, ok := elt.(*ast.KeyValueExpr)
					if !ok {
						continue
					}
					checked++
					if !isJSFuncOf(kv.Value) {
						t.Errorf("%s: %s entry %s is not wrapped in js.FuncOf", fset.Position(kv.Pos()), fn.Name.Name, exprString(kv.Key))
					}
				}
				return true
			})
		}
	}
	if checked == 0 {
		t.Fatal("found no *JSFuncs map entries in cmd/wasm; the check is not looking at the right code")
	}
}

func isJSFuncOf(e ast.Expr) bool {
	call, ok := e.(*ast.CallExpr)
	if !ok {
		return false
	}
	sel, ok := call.Fun.(*ast.SelectorExpr)
	if !ok {
		return false
	}
	pkg, ok := sel.X.(*ast.Ident)
	return ok && pkg.Name == "js" && sel.Sel.Name == "FuncOf"
}

func exprString(e ast.Expr) string {
	if lit, ok := e.(*ast.BasicLit); ok {
		return lit.Value
	}
	return "?"
}
