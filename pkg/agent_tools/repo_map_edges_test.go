package tools

import (
	"go/ast"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =============================================================================
// exprToString
// =============================================================================

func TestExprToString_Ident(t *testing.T) {
	expr := &ast.Ident{Name: "foo"}
	assert.Equal(t, "foo", exprToString(expr))
}

func TestExprToString_SelectorExpr(t *testing.T) {
	expr := &ast.SelectorExpr{
		X:   &ast.Ident{Name: "fmt"},
		Sel: &ast.Ident{Name: "Println"},
	}
	assert.Equal(t, "fmt.Println", exprToString(expr))
}

func TestExprToString_NestedSelectorExpr(t *testing.T) {
	expr := &ast.SelectorExpr{
		X: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "a"},
			Sel: &ast.Ident{Name: "b"},
		},
		Sel: &ast.Ident{Name: "c"},
	}
	assert.Equal(t, "a.b.c", exprToString(expr))
}

func TestExprToString_StarExpr(t *testing.T) {
	expr := &ast.StarExpr{X: &ast.Ident{Name: "T"}}
	assert.Equal(t, "*T", exprToString(expr))
}

func TestExprToString_ParenExpr(t *testing.T) {
	expr := &ast.ParenExpr{X: &ast.Ident{Name: "x"}}
	assert.Equal(t, "(x)", exprToString(expr))
}

func TestExprToString_BasicLit(t *testing.T) {
	expr := &ast.BasicLit{Value: `"hello"`}
	assert.Equal(t, `"hello"`, exprToString(expr))
}

func TestExprToString_FuncLit(t *testing.T) {
	expr := &ast.FuncLit{}
	assert.Equal(t, "func(...)", exprToString(expr))
}

func TestExprToString_ArrayType(t *testing.T) {
	expr := &ast.ArrayType{Elt: &ast.Ident{Name: "byte"}}
	assert.Equal(t, "byte", exprToString(expr))
}

func TestExprToString_UnknownType(t *testing.T) {
	// A struct literal is not handled by exprToString.
	expr := &ast.StructType{
		Fields: &ast.FieldList{},
	}
	result := exprToString(expr)
	assert.True(t, strings.HasPrefix(result, "?"), "expected unknown type prefix, got: %s", result)
}

func TestExprToString_StarExprSelector(t *testing.T) {
	// *http.Request as a callee (unlikely but tests the combination).
	expr := &ast.StarExpr{
		X: &ast.SelectorExpr{
			X:   &ast.Ident{Name: "http"},
			Sel: &ast.Ident{Name: "Request"},
		},
	}
	assert.Equal(t, "*http.Request", exprToString(expr))
}

// =============================================================================
// ExtractCallsAndSymbols — Go
// =============================================================================

func TestExtractCallsAndSymbols_Go(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := []byte(`package main

import "fmt"

type User struct { Name string }

func NewUser(name string) *User {
	return &User{Name: name}
}

func (u *User) Greet() string {
	return "Hello, " + u.Name
}

func run() {
	u := NewUser("Alice")
	fmt.Println(u.Greet())
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Symbols: type User, func NewUser, func (*User).Greet, func run
	assert.Len(t, result.Symbols, 4)

	// Edges: run calls NewUser and fmt.Println
	// (u.Greet() is also a call from run)
	require.GreaterOrEqual(t, len(result.Edges), 2)

	// Check that run is the source of all edges.
	for _, e := range result.Edges {
		assert.Equal(t, "func run", e.SourceQualifiedName)
		assert.Equal(t, "calls", e.EdgeType)
	}

	// Verify specific callees exist.
	callees := make(map[string]bool)
	for _, e := range result.Edges {
		callees[e.TargetQualifiedName] = true
	}
	assert.True(t, callees["NewUser"], "expected run -> NewUser edge, got callees: %v", callees)
	assert.True(t, callees["fmt.Println"], "expected run -> fmt.Println edge, got callees: %v", callees)
}

func TestExtractCallsAndSymbols_Go_NoCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "types.go")
	content := []byte(`package main

type Config struct {
	Port int
	Host string
}

type Handler interface {
	Handle() error
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.NotEmpty(t, result.Symbols)
	assert.Empty(t, result.Edges)
}

func TestExtractCallsAndSymbols_Go_TestFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app_test.go")
	content := []byte(`package main

func TestSomething(t *testing.T) {}
func HelperForTest() {}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// _test.go files return empty results.
	assert.Empty(t, result.Symbols)
	assert.Empty(t, result.Edges)
}

func TestExtractCallsAndSymbols_Go_MultipleFunctions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chain.go")
	content := []byte(`package main

func alpha() {
	beta()
}

func beta() {
	gamma()
}

func gamma() {
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Edges, 2)
	assert.Equal(t, "func alpha", result.Edges[0].SourceQualifiedName)
	assert.Equal(t, "beta", result.Edges[0].TargetQualifiedName)
	assert.Equal(t, "func beta", result.Edges[1].SourceQualifiedName)
	assert.Equal(t, "gamma", result.Edges[1].TargetQualifiedName)
}

func TestExtractCallsAndSymbols_Go_PackageQualified(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := []byte(`package main

import "strings"

func clean(s string) string {
	return strings.TrimSpace(s)
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Edges, 1)
	assert.Equal(t, "func clean", result.Edges[0].SourceQualifiedName)
	assert.Equal(t, "strings.TrimSpace", result.Edges[0].TargetQualifiedName)
}

func TestExtractCallsAndSymbols_Go_MethodCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "server.go")
	content := []byte(`package main

type Server struct{}

func (s *Server) Start() {
	s.run()
}

func (s *Server) run() {
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Edges, 1)
	assert.Equal(t, "func (*Server).Start", result.Edges[0].SourceQualifiedName)
	assert.Equal(t, "s.run", result.Edges[0].TargetQualifiedName)
}

func TestExtractCallsAndSymbols_Go_NestedCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested.go")
	content := []byte(`package main

func foo() {
	bar(baz())
}

func bar(x int) {}
func baz() int { return 0 }
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Edges, 2)
	for _, e := range result.Edges {
		assert.Equal(t, "func foo", e.SourceQualifiedName)
	}
	callees := []string{result.Edges[0].TargetQualifiedName, result.Edges[1].TargetQualifiedName}
	assert.Contains(t, callees, "bar")
	assert.Contains(t, callees, "baz")
}

func TestExtractCallsAndSymbols_Go_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.go")
	content := []byte(`package main
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Symbols)
	assert.Empty(t, result.Edges)
}

func TestExtractCallsAndSymbols_Go_ParseError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.go")
	content := []byte(`package main

func incomplete(
`)
	_, err := ExtractCallsAndSymbols(path, content)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "parse")
}

// =============================================================================
// ExtractCallsAndSymbols — TypeScript
// =============================================================================

func TestExtractCallsAndSymbols_TS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ts")
	content := []byte(`function greet(name: string) {
    return "Hello, " + name;
}

function main() {
    const msg = greet("World");
    console.log(msg);
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.GreaterOrEqual(t, len(result.Symbols), 2)
	assert.GreaterOrEqual(t, len(result.Edges), 1)

	// Check that at least one edge goes from main to greet.
	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "greet" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> greet edge, got edges: %v", result.Edges)
}

func TestExtractCallsAndSymbols_TS_Class(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.ts")
	content := []byte(`class App {
    start() {
        this.init();
    }
    init() {
        console.log("ready");
    }
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// TS classes are extracted as "class" symbols (no body), so calls inside
	// methods are not attributed to any function. This is expected because
	// extractSymbols treats classes as non-function symbols without body text.
	assert.Empty(t, result.Edges)
}

func TestExtractCallsAndSymbols_TS_NoCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "consts.ts")
	content := []byte(`const VERSION = "1.0";
const MAX_RETRIES = 3;
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Edges)
}

// =============================================================================
// ExtractCallsAndSymbols — JavaScript
// =============================================================================

func TestExtractCallsAndSymbols_JS(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.js")
	content := []byte(`function greet(name) {
    return "Hello, " + name;
}

function main() {
    const msg = greet("World");
    console.log(msg);
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.GreaterOrEqual(t, len(result.Symbols), 2)
	assert.GreaterOrEqual(t, len(result.Edges), 1)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "greet" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected main -> greet edge, got: %v", result.Edges)
}

// =============================================================================
// ExtractCallsAndSymbols — Python
// =============================================================================

func TestExtractCallsAndSymbols_Python(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "utils.py")
	content := []byte(`def helper():
    return 42

def main():
    val = helper()
    print(val)
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.GreaterOrEqual(t, len(result.Symbols), 2)
	assert.GreaterOrEqual(t, len(result.Edges), 1)

	foundHelper, foundPrint := false, false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "helper" {
			foundHelper = true
		}
		if e.SourceQualifiedName == "main" && e.TargetQualifiedName == "print" {
			foundPrint = true
		}
	}
	assert.True(t, foundHelper, "expected main -> helper edge, got: %v", result.Edges)
	assert.True(t, foundPrint, "expected main -> print edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_Python_Async(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "asyncio.py")
	content := []byte(`async def get_data():
    return "data"

async def fetch():
    data = await get_data()
    print(data)
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	found := false
	for _, e := range result.Edges {
		if e.SourceQualifiedName == "fetch" && e.TargetQualifiedName == "get_data" {
			found = true
			break
		}
	}
	assert.True(t, found, "expected fetch -> get_data edge, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_Python_ClassMethods(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "processor.py")
	content := []byte(`class Processor:
    def process(self, data):
        self.validate(data)
        return data

    def validate(self, data):
        print("validating")
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Python class methods are inside the class body, so calls are attributed
	// to the class symbol "Processor" (the class has a body in Python).
	foundValidate, foundPrint := false, false
	for _, e := range result.Edges {
		if e.TargetQualifiedName == "self.validate" {
			foundValidate = true
		}
		if e.TargetQualifiedName == "print" {
			foundPrint = true
		}
	}
	assert.True(t, foundValidate, "expected self.validate callee, got: %v", result.Edges)
	assert.True(t, foundPrint, "expected print callee, got: %v", result.Edges)
}

func TestExtractCallsAndSymbols_Python_NoCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.py")
	content := []byte(`class Config:
    port = 8080
    host = "localhost"
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Empty(t, result.Edges)
}

// =============================================================================
// ExtractCallsAndSymbols — Edge cases
// =============================================================================

func TestExtractCallsAndSymbols_UnsupportedExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.rs")
	content := []byte(`fn main() {}`)
	_, err := ExtractCallsAndSymbols(path, content)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported file extension")
}

func TestExtractCallsAndSymbols_Go_FunctionNoBody(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "iface.go")
	content := []byte(`package main

func external() int
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// External declarations have no body, so no calls.
	assert.Empty(t, result.Edges)
}

func TestExtractCallsAndSymbols_Go_ComplexCallee(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "main.go")
	content := []byte(`package main

func main() {
	result := fmt.Sprintf("%d %s", len(data), strings.Join(items, ","))
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.GreaterOrEqual(t, len(result.Edges), 3)
	callees := make(map[string]bool)
	for _, e := range result.Edges {
		callees[e.TargetQualifiedName] = true
	}
	assert.True(t, callees["fmt.Sprintf"], "expected fmt.Sprintf callee, got: %v", callees)
	assert.True(t, callees["len"], "expected len callee, got: %v", callees)
	assert.True(t, callees["strings.Join"], "expected strings.Join callee, got: %v", callees)
}

func TestExtractCallsAndSymbols_Go_ValueReceiver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "counter.go")
	content := []byte(`package main

type Counter struct{}

func (c Counter) Inc() {
	c.log()
}

func (c Counter) log() {
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Len(t, result.Edges, 1)
	assert.Equal(t, "func (Counter).Inc", result.Edges[0].SourceQualifiedName)
	assert.Equal(t, "c.log", result.Edges[0].TargetQualifiedName)
}

func TestExtractCallsAndSymbols_Go_ExcludesTestFunctions(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "app.go")
	content := []byte(`package main

func PublicAPI() {
	internal()
}

func TestSomething(t *testing.T) {
	internal()
}

func internal() {
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// Only PublicAPI should appear as a symbol (TestSomething is excluded).
	foundTestSym := false
	for _, s := range result.Symbols {
		if strings.Contains(s.Name, "TestSomething") {
			foundTestSym = true
		}
	}
	assert.False(t, foundTestSym, "TestSomething should be excluded from symbols")

	// Only one edge: PublicAPI -> internal
	require.Len(t, result.Edges, 1)
	assert.Equal(t, "func PublicAPI", result.Edges[0].SourceQualifiedName)
	assert.Equal(t, "internal", result.Edges[0].TargetQualifiedName)
}

func TestExtractCallsAndSymbols_Go_QuotedPackageReceiver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "handler.go")
	content := []byte(`package main

import "net/http"

func (r *http.Request) Method() string {
	return r.URL.Path
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	// The function should be named with the receiver prefix.
	found := false
	for _, s := range result.Symbols {
		if strings.Contains(s.Name, "http.Request") && strings.Contains(s.Name, "Method") {
			found = true
			break
		}
	}
	assert.True(t, found, "expected func (*http.Request).Method in symbols, got: %v", result.Symbols)
}

// =============================================================================
// SymbolWithEdges — struct verification
// =============================================================================

func TestSymbolWithEdges_StructFields(t *testing.T) {
	// Verify the struct has the expected fields.
	result := &SymbolWithEdges{
		Symbols: []symbolEntry{{Name: "test", Line: 1}},
		Edges:   nil,
	}
	assert.Len(t, result.Symbols, 1)
	assert.Equal(t, "test", result.Symbols[0].Name)
	assert.Equal(t, 1, result.Symbols[0].Line)
	assert.Nil(t, result.Edges)
}

func TestExtractCallsAndSymbols_Go_FunctionNoCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "simple.go")
	content := []byte(`package main

func foo() {
	x := 1
	y := x + 2
}
`)
	result, err := ExtractCallsAndSymbols(path, content)
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Len(t, result.Symbols, 1)
	assert.Empty(t, result.Edges)
}
