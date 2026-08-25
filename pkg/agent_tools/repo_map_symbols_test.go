package tools

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// =============================================================================
// symbolDisplayPrefix
// =============================================================================

func TestSymbolDisplayPrefix_PythonFunction(t *testing.T) {
	// Python functions map to "def" for backward compatibility.
	assert.Equal(t, "def", symbolDisplayPrefix("function", ".py"))
}

func TestSymbolDisplayPrefix_PythonClass(t *testing.T) {
	// Python classes pass through the kind as-is.
	assert.Equal(t, "class", symbolDisplayPrefix("class", ".py"))
}

func TestSymbolDisplayPrefix_PythonUnknownKind(t *testing.T) {
	// Unknown Python kinds pass through as-is.
	assert.Equal(t, "unknown", symbolDisplayPrefix("unknown", ".py"))
}

func TestSymbolDisplayPrefix_TSVariable(t *testing.T) {
	// TS/JS variables map to "const" for backward compatibility.
	assert.Equal(t, "const", symbolDisplayPrefix("variable", ".ts"))
	assert.Equal(t, "const", symbolDisplayPrefix("variable", ".tsx"))
	assert.Equal(t, "const", symbolDisplayPrefix("variable", ".js"))
	assert.Equal(t, "const", symbolDisplayPrefix("variable", ".jsx"))
}

func TestSymbolDisplayPrefix_TSFunction(t *testing.T) {
	// TS/JS functions pass through the kind as-is.
	assert.Equal(t, "function", symbolDisplayPrefix("function", ".ts"))
	assert.Equal(t, "function", symbolDisplayPrefix("function", ".js"))
}

func TestSymbolDisplayPrefix_TSClass(t *testing.T) {
	assert.Equal(t, "class", symbolDisplayPrefix("class", ".ts"))
}

func TestSymbolDisplayPrefix_TSInterface(t *testing.T) {
	assert.Equal(t, "interface", symbolDisplayPrefix("interface", ".ts"))
}

func TestSymbolDisplayPrefix_TSUnknownKind(t *testing.T) {
	// Unknown TS kinds pass through as-is.
	assert.Equal(t, "unknown", symbolDisplayPrefix("unknown", ".ts"))
}

func TestSymbolDisplayPrefix_UnknownExtension(t *testing.T) {
	// Unknown extensions pass through the kind as-is regardless of kind.
	assert.Equal(t, "function", symbolDisplayPrefix("function", ".rs"))
	assert.Equal(t, "variable", symbolDisplayPrefix("variable", ".java"))
	assert.Equal(t, "def", symbolDisplayPrefix("def", ".c"))
}

// =============================================================================
// shouldSkipGoFunc
// =============================================================================

func TestShouldSkipGoFunc_TestFunction(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", "package p\nfunc TestSomething(t *testing.T) {}", 0)
	assert.NoError(t, err)
	assert.True(t, shouldSkipGoFunc(node.Decls[0].(*ast.FuncDecl)))
}

func TestShouldSkipGoFunc_BenchmarkFunction(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", "package p\nfunc BenchmarkX(b *testing.B) {}", 0)
	assert.NoError(t, err)
	assert.True(t, shouldSkipGoFunc(node.Decls[0].(*ast.FuncDecl)))
}

func TestShouldSkipGoFunc_FuzzFunction(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", "package p\nfunc FuzzX(f *testing.F) {}", 0)
	assert.NoError(t, err)
	assert.True(t, shouldSkipGoFunc(node.Decls[0].(*ast.FuncDecl)))
}

func TestShouldSkipGoFunc_BlankIdentifier(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", "package p\nimport _ \"fmt\"\nfunc _() {}", 0)
	assert.NoError(t, err)
	// Decl 0: import, Decl 1: func _() {}
	assert.True(t, shouldSkipGoFunc(node.Decls[1].(*ast.FuncDecl)))
}

func TestShouldSkipGoFunc_RegularFunction(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "test.go", "package p\nfunc PublicAPI() {}", 0)
	assert.NoError(t, err)
	assert.False(t, shouldSkipGoFunc(node.Decls[0].(*ast.FuncDecl)))
}

func TestShouldSkipGoFunc_NilName(t *testing.T) {
	// Edge case: FuncDecl with nil Name (should not panic).
	decl := &ast.FuncDecl{}
	assert.True(t, shouldSkipGoFunc(decl))
}

// =============================================================================
// goFuncName
// =============================================================================

func TestGoFuncName_SimpleFunction(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "x.go", "package p\nfunc Hello() {}", 0)
	assert.NoError(t, err)
	assert.Equal(t, "func Hello", goFuncName(node.Decls[0].(*ast.FuncDecl)))
}

func TestGoFuncName_PointerReceiver(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "x.go", "package p\nfunc (s *Server) Start() {}", 0)
	assert.NoError(t, err)
	assert.Equal(t, "func (*Server).Start", goFuncName(node.Decls[0].(*ast.FuncDecl)))
}

func TestGoFuncName_ValueReceiver(t *testing.T) {
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "x.go", "package p\nfunc (s Server) String() string { return \"\" }", 0)
	assert.NoError(t, err)
	assert.Equal(t, "func (Server).String", goFuncName(node.Decls[0].(*ast.FuncDecl)))
}

func TestGoFuncName_QuotedPackageName(t *testing.T) {
	// When a receiver type references an imported package.
	fset := token.NewFileSet()
	node, err := parser.ParseFile(fset, "x.go",
		"package p\nimport \"net/http\"\nfunc (r *http.Request) Method() string { return \"\" }", 0)
	assert.NoError(t, err)
	// Find the function decl (skip the import GenDecl).
	var fn *ast.FuncDecl
	for _, d := range node.Decls {
		if fd, ok := d.(*ast.FuncDecl); ok {
			fn = fd
			break
		}
	}
	assert.NotNil(t, fn)
	assert.Equal(t, "func (*http.Request).Method", goFuncName(fn))
}

func TestGoFuncName_NilReceiver(t *testing.T) {
	// FuncDecl with nil Recv - should not panic and return simple name.
	decl := &ast.FuncDecl{
		Name: &ast.Ident{Name: "Orphan"},
	}
	assert.Equal(t, "func Orphan", goFuncName(decl))
}

func TestGoFuncName_EmptyReceiverList(t *testing.T) {
	// FuncDecl with non-nil Recv but empty List - treat as regular function.
	decl := &ast.FuncDecl{
		Name: &ast.Ident{Name: "Odd"},
		Recv: &ast.FieldList{},
	}
	assert.Equal(t, "func Odd", goFuncName(decl))
}

// =============================================================================
// goRecvType
// =============================================================================

func TestGoRecvType_Ident(t *testing.T) {
	id := &ast.Ident{Name: "MyStruct"}
	assert.Equal(t, "MyStruct", goRecvType(id))
}

func TestGoRecvType_SelectorExpr(t *testing.T) {
	se := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "net"},
		Sel: &ast.Ident{Name: "http"},
	}
	assert.Equal(t, "net.http", goRecvType(se))
}

func TestGoRecvType_NestedSelectorExpr(t *testing.T) {
	inner := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "a"},
		Sel: &ast.Ident{Name: "b"},
	}
	outer := &ast.SelectorExpr{
		X:   inner,
		Sel: &ast.Ident{Name: "c"},
	}
	assert.Equal(t, "a.b.c", goRecvType(outer))
}

func TestGoRecvType_UnknownExpression(t *testing.T) {
	// An expression of an unexpected type (e.g. parenthesized, binary) returns "?".
	parens := &ast.ParenExpr{X: &ast.Ident{Name: "x"}}
	assert.Equal(t, "?", goRecvType(parens))
}

// =============================================================================
// extractGoSymbolsAST
// =============================================================================

func TestExtractGoSymbolsAST_Basic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := []byte(`package main

type User struct { Name string }
func NewUser(name string) *User { return nil }
func (u *User) GetName() string { return u.Name }
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	assert.Len(t, syms, 3)
	assert.Equal(t, "type User", syms[0].Name)
	assert.Equal(t, 3, syms[0].Line)
	assert.Equal(t, "func NewUser", syms[1].Name)
	assert.Equal(t, 4, syms[1].Line)
	assert.Equal(t, "func (*User).GetName", syms[2].Name)
	assert.Equal(t, 5, syms[2].Line)
}

func TestExtractGoSymbolsAST_ExcludesTestFunctions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.go")
	content := []byte(`package main

func PublicAPI() {}
func TestSomething(t *testing.T) {}
func BenchmarkX(b *testing.B) {}
func FuzzX(f *testing.F) {}
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	assert.Len(t, syms, 1)
	assert.Equal(t, "func PublicAPI", syms[0].Name)
}

func TestExtractGoSymbolsAST_ExcludesTestFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app_test.go")
	content := []byte(`package main

func TestRealTest(t *testing.T) {}
func Helper() {}
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	// _test.go files return nil, no error.
	assert.Nil(t, syms)
}

func TestExtractGoSymbolsAST_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	content := []byte(`package main

func incomplete(
`)
	_, err := extractGoSymbolsAST(path, content)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

func TestExtractGoSymbolsAST_EmptyPackage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.go")
	content := []byte(`package main
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	assert.Empty(t, syms)
}

func TestExtractGoSymbolsAST_TypesOnly(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "types.go")
	content := []byte(`package main

type A struct{}
type B interface{}
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	assert.Len(t, syms, 2)
	assert.Equal(t, "type A", syms[0].Name)
	assert.Equal(t, "iface B", syms[1].Name)
}

func TestExtractGoSymbolsAST_ValueAndPointerReceivers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handler.go")
	content := []byte(`package main

type Handler struct{}
func (h *Handler) PtrMethod() {}
func (h Handler) ValMethod() {}
func Standalone() {}
`)
	syms, err := extractGoSymbolsAST(path, content)
	assert.NoError(t, err)
	assert.Len(t, syms, 4)
	assert.Equal(t, "type Handler", syms[0].Name)
	assert.Equal(t, "func (*Handler).PtrMethod", syms[1].Name)
	assert.Equal(t, "func (Handler).ValMethod", syms[2].Name)
	assert.Equal(t, "func Standalone", syms[3].Name)
}

// =============================================================================
// extractSymbolsForFile
// =============================================================================

func TestExtractSymbolsForFile_Go(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.go")
	content := []byte(`package main

func StartServer() error { return nil }
type Config struct { Port int }
`)
	syms, err := extractSymbolsForFile(path, ".go", content)
	assert.NoError(t, err)
	assert.Len(t, syms, 2)
	assert.Equal(t, "func StartServer", syms[0].Name)
	assert.Equal(t, "type Config", syms[1].Name)
}

func TestExtractSymbolsForFile_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.rs")
	content := []byte(`fn main() {}`)
	_, err := extractSymbolsForFile(path, ".rs", content)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}

func TestExtractSymbolsForFile_TS(t *testing.T) {
	// TypeScript files go through tree-sitter via pkg/ast.
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ts")
	content := []byte(`export class App {
  start() {}
}
export const VERSION = "1.0";
`)
	syms, err := extractSymbolsForFile(path, ".ts", content)
	assert.NoError(t, err)
	// tree-sitter should find at least the class.
	assert.True(t, len(syms) > 0, "expected at least one symbol from valid TS")
	found := false
	for _, s := range syms {
		if strings.HasPrefix(s.Name, "class App") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'class App' in symbols, got: %v", syms)
}

func TestExtractSymbolsForFile_Python(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utils.py")
	content := []byte(`def calculate_total(items): return sum(items)
class DataProcessor: pass
`)
	syms, err := extractSymbolsForFile(path, ".py", content)
	assert.NoError(t, err)
	assert.True(t, len(syms) > 0, "expected at least one symbol from valid Python")
	found := false
	for _, s := range syms {
		if strings.HasPrefix(s.Name, "def calculate_total") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected 'def calculate_total' in symbols, got: %v", syms)
}
