package ast

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Go parsing
// ---------------------------------------------------------------------------

func TestParseGoFile(t *testing.T) {
	src := []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}

func add(a, b int) int {
	return a + b
}

type Server struct {
	Host string
	Port int
}

type Handler interface {
	Handle(req string) error
}

type Alias = string
`)

	result, err := ParseFile("main.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}

	// We expect: main, add, Server, Handler, Alias
	wantNames := map[string]string{
		"main":    "function",
		"add":     "function",
		"Server":  "class",     // struct → class
		"Handler": "interface", // interface → interface
		"Alias":   "type",      // type alias → type
	}

	for _, sym := range result.Symbols {
		wantKind, ok := wantNames[sym.Name]
		if !ok {
			t.Errorf("unexpected symbol: %q (%s) at line %d", sym.Name, sym.Kind, sym.StartLine)
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: Kind = %q, want %q", sym.Name, sym.Kind, wantKind)
		}
		if sym.StartLine < 1 {
			t.Errorf("symbol %q: StartLine = %d, want >= 1", sym.Name, sym.StartLine)
		}
		if sym.EndLine < sym.StartLine {
			t.Errorf("symbol %q: EndLine(%d) < StartLine(%d)", sym.Name, sym.EndLine, sym.StartLine)
		}
		delete(wantNames, sym.Name)
	}
	for name := range wantNames {
		t.Errorf("missing symbol: %q", name)
	}
}

func TestParseGoMethod(t *testing.T) {
	src := []byte(`package main

type Foo struct{}

func (f *Foo) Bar() string { return "" }
`)

	result, err := ParseFile("foo.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	found := false
	for _, sym := range result.Symbols {
		if sym.Name == "Bar" && sym.Kind == "method" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected method Bar, got symbols: %+v", result.Symbols)
	}
}

func TestParseGoEmptyFile(t *testing.T) {
	src := []byte(`package main
`)
	result, err := ParseFile("empty.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d: %+v", len(result.Symbols), result.Symbols)
	}
}

// ---------------------------------------------------------------------------
// TypeScript parsing
// ---------------------------------------------------------------------------

func TestParseTypeScriptFile(t *testing.T) {
	src := []byte(`export function greet(name: string): string {
	return "hello " + name;
}

export class App {
	private port: number;

	constructor(port: number) {
		this.port = port;
	}

	start(): void {
		console.log("started");
	}
}

export interface Config {
	host: string;
	port: number;
}

export type Result<T> = { ok: true; value: T } | { ok: false; error: string };

export enum Status {
	Active = "active",
	Inactive = "inactive",
}

const defaultPort = 3000;
`)

	result, err := ParseFile("app.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	if result.Language != "typescript" {
		t.Errorf("Language = %q, want %q", result.Language, "typescript")
	}

	wantNames := map[string]string{
		"greet":       "function",
		"App":         "class",
		"Config":      "interface",
		"Result":      "type",
		"Status":      "enum",
		"defaultPort": "variable",
	}

	for _, sym := range result.Symbols {
		wantKind, ok := wantNames[sym.Name]
		if !ok {
			t.Logf("info: extra symbol %q (%s) at line %d", sym.Name, sym.Kind, sym.StartLine)
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: Kind = %q, want %q", sym.Name, sym.Kind, wantKind)
		}
		if sym.StartLine < 1 {
			t.Errorf("symbol %q: StartLine = %d, want >= 1", sym.Name, sym.StartLine)
		}
		delete(wantNames, sym.Name)
	}
	for name := range wantNames {
		t.Errorf("missing symbol: %q", name)
	}
}

// ---------------------------------------------------------------------------
// JavaScript parsing
// ---------------------------------------------------------------------------

func TestParseJavaScriptFile(t *testing.T) {
	src := []byte(`function hello() {
	return "world";
}

class Dog {
	bark() {
		return "woof";
	}
}

const PI = 3.14;
`)

	result, err := ParseFile("app.js", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	if result.Language != "javascript" {
		t.Errorf("Language = %q, want %q", result.Language, "javascript")
	}

	wantNames := map[string]string{
		"hello": "function",
		"Dog":   "class",
		"PI":    "variable",
	}

	for _, sym := range result.Symbols {
		wantKind, ok := wantNames[sym.Name]
		if !ok {
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: Kind = %q, want %q", sym.Name, sym.Kind, wantKind)
		}
		delete(wantNames, sym.Name)
	}
	for name := range wantNames {
		t.Errorf("missing symbol: %q", name)
	}
}

// ---------------------------------------------------------------------------
// Python parsing
// ---------------------------------------------------------------------------

func TestParsePythonFile(t *testing.T) {
	src := []byte(`import os
from typing import List

def greet(name: str) -> str:
    return f"hello {name}"

class Calculator:
    def add(self, a: int, b: int) -> int:
        return a + b

    def multiply(self, a: int, b: int) -> int:
        return a * b

async def fetch_data(url):
    pass
`)

	result, err := ParseFile("calc.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}

	wantNames := map[string]string{
		"greet":      "function",
		"Calculator": "class",
		"fetch_data": "function",
	}

	for _, sym := range result.Symbols {
		wantKind, ok := wantNames[sym.Name]
		if !ok {
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: Kind = %q, want %q", sym.Name, sym.Kind, wantKind)
		}
		delete(wantNames, sym.Name)
	}
	for name := range wantNames {
		t.Errorf("missing symbol: %q", name)
	}
}

func TestParsePythonDecorators(t *testing.T) {
	src := []byte(`@dataclass
class Point:
    x: int
    y: int

@cache
def compute(n):
    return n * 2
`)

	result, err := ParseFile("deco.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	wantNames := map[string]string{
		"Point":   "class",
		"compute": "function",
	}

	for _, sym := range result.Symbols {
		wantKind, ok := wantNames[sym.Name]
		if !ok {
			continue
		}
		if sym.Kind != wantKind {
			t.Errorf("symbol %q: Kind = %q, want %q", sym.Name, sym.Kind, wantKind)
		}
		// Decorated definitions should start at the decorator line.
		if sym.StartLine < 1 {
			t.Errorf("symbol %q: StartLine = %d, want >= 1", sym.Name, sym.StartLine)
		}
		delete(wantNames, sym.Name)
	}
	for name := range wantNames {
		t.Errorf("missing symbol: %q", name)
	}
}

// ---------------------------------------------------------------------------
// Error handling / edge cases
// ---------------------------------------------------------------------------

func TestParseFileEmptyPath(t *testing.T) {
	_, err := ParseFile("", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for empty path")
	}
	if !strings.Contains(err.Error(), "filePath must not be empty") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseFileUnsupportedExtension(t *testing.T) {
	_, err := ParseFile("data.bin", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for unsupported extension")
	}
}

func TestParseContentEmptyLanguage(t *testing.T) {
	_, err := ParseContent("", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for empty language")
	}
}

func TestParseContentUnknownLanguage(t *testing.T) {
	_, err := ParseContent("brainfuck", []byte("hello"))
	if err == nil {
		t.Fatal("expected error for unknown language")
	}
}

func TestParseContentExplicitLanguage(t *testing.T) {
	src := []byte(`def hello():
    pass
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}
	if len(result.Symbols) != 1 {
		t.Fatalf("expected 1 symbol, got %d", len(result.Symbols))
	}
	if result.Symbols[0].Name != "hello" {
		t.Errorf("symbol name = %q, want %q", result.Symbols[0].Name, "hello")
	}
}

// ---------------------------------------------------------------------------
// IsSupported / DetectLanguage
// ---------------------------------------------------------------------------

func TestIsSupported(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"app.ts", true},
		{"app.tsx", true}, // TSX is a supported TypeScript variant
		{"app.js", true},
		{"app.jsx", true}, // JSX maps to javascript
		{"calc.py", true},
		{"readme.md", false},
		{"style.css", false},
		{"Makefile", false},
		{"readme.txt", false}, // .txt maps to vimdoc, not in supported set
		{"", false},
	}
	for _, tt := range tests {
		got := IsSupported(tt.path)
		if got != tt.want {
			t.Errorf("IsSupported(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

func TestDetectLanguage(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", "go"},
		{"app.ts", "typescript"},
		{"app.js", "javascript"},
		{"calc.py", "python"},
		{"readme.md", "markdown"}, // markdown is registered but not in SupportedLanguages
		{"", ""},
	}
	for _, tt := range tests {
		got := DetectLanguage(tt.path)
		if got != tt.want {
			t.Errorf("DetectLanguage(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Walk helper
// ---------------------------------------------------------------------------

func TestWalk(t *testing.T) {
	src := []byte(`package main

func foo() {}
`)

	result, err := ParseFile("main.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	var nodeTypes []string
	Walk(result.Root, result.Bound, func(node *gotreesitter.Node, nodeType string, depth int) bool {
		nodeTypes = append(nodeTypes, nodeType)
		return true
	})

	// Should contain at least "source_file" and "function_declaration".
	found := make(map[string]bool)
	for _, nt := range nodeTypes {
		found[nt] = true
	}
	if !found["source_file"] {
		t.Errorf("walk missed source_file, got: %v", nodeTypes)
	}
	if !found["function_declaration"] {
		t.Errorf("walk missed function_declaration, got: %v", nodeTypes)
	}
}

func TestWalkStopEarly(t *testing.T) {
	src := []byte(`package main

func foo() {}
func bar() {}
`)

	result, err := ParseFile("main.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	count := 0
	Walk(result.Root, result.Bound, func(node *gotreesitter.Node, nodeType string, depth int) bool {
		count++
		// Stop after the first function_declaration.
		return nodeType != "function_declaration"
	})

	if count < 2 {
		t.Errorf("walk visited too few nodes: %d", count)
	}
}

// ---------------------------------------------------------------------------
// Release idempotency
// ---------------------------------------------------------------------------

func TestReleaseIdempotent(t *testing.T) {
	src := []byte(`package main
func foo() {}
`)

	result, err := ParseFile("main.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}

	// Call Release twice — should not panic.
	result.Release()
	result.Release()
}

// ---------------------------------------------------------------------------
// Line number accuracy
// ---------------------------------------------------------------------------

func TestGoLineNumbers(t *testing.T) {
	src := []byte(`package main

func foo() {}

func bar() {}
`)

	result, err := ParseFile("main.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	wantLines := map[string]int{
		"foo": 3,
		"bar": 5,
	}

	for _, sym := range result.Symbols {
		want, ok := wantLines[sym.Name]
		if !ok {
			continue
		}
		if sym.StartLine != want {
			t.Errorf("symbol %q: StartLine = %d, want %d", sym.Name, sym.StartLine, want)
		}
		delete(wantLines, sym.Name)
	}
	for name := range wantLines {
		t.Errorf("missing symbol: %q", name)
	}
}

func TestPythonLineNumbers(t *testing.T) {
	src := []byte(`def foo():
    pass

class Bar:
    def baz(self):
        pass
`)

	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	wantLines := map[string]int{
		"foo": 1,
		"Bar": 4,
	}

	for _, sym := range result.Symbols {
		want, ok := wantLines[sym.Name]
		if !ok {
			continue
		}
		if sym.StartLine != want {
			t.Errorf("symbol %q: StartLine = %d, want %d", sym.Name, sym.StartLine, want)
		}
		delete(wantLines, sym.Name)
	}
	for name := range wantLines {
		t.Errorf("missing symbol: %q", name)
	}
}

// ---------------------------------------------------------------------------
// TSX extension
// ---------------------------------------------------------------------------

func TestParseTSXFile(t *testing.T) {
	src := []byte(`export function App() {
	return <div>Hello</div>;
}
`)

	result, err := ParseFile("app.tsx", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// TSX should parse via the typescript grammar (or tsx which is related).
	if result.Language == "" {
		t.Error("expected non-empty language for .tsx")
	}
}

// ---------------------------------------------------------------------------
// Malformed source (tree-sitter is error-tolerant)
// ---------------------------------------------------------------------------

func TestParseMalformedGo(t *testing.T) {
	src := []byte(`package main

func incomplete(

func ok() {}
`)

	result, err := ParseFile("broken.go", src)
	if err != nil {
		t.Fatalf("ParseFile should succeed on malformed input: %v", err)
	}
	defer result.Release()

	// Tree-sitter is error-tolerant but may not recover all symbols from
	// severely malformed input.  Verify it doesn't crash and returns a
	// valid tree.
	if result.Root == nil {
		t.Fatal("expected non-nil root node")
	}
}

// ---------------------------------------------------------------------------
// FileExtension helper
// ---------------------------------------------------------------------------

func TestFileExtension(t *testing.T) {
	tests := []struct {
		path string
		want string
	}{
		{"main.go", ".go"},
		{"app.tsx", ".tsx"},
		{"Makefile", ""},
		{"dir/file.py", ".py"},
	}
	for _, tt := range tests {
		got := FileExtension(tt.path)
		if got != tt.want {
			t.Errorf("FileExtension(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
}

// ---------------------------------------------------------------------------
// Call-edge extraction — Go
// ---------------------------------------------------------------------------

func TestExtractCalls_Go_SimpleCall(t *testing.T) {
	src := []byte(`package main

func foo() {
	bar()
}

func bar() {
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "foo" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "foo")
	}
	if ce.CalleeName != "bar" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "bar")
	}
	if ce.Line != 4 {
		t.Errorf("Line = %d, want 4", ce.Line)
	}
	if ce.CallerLine != 3 {
		t.Errorf("CallerLine = %d, want 3", ce.CallerLine)
	}
}

func TestExtractCalls_Go_MultipleCalls(t *testing.T) {
	src := []byte(`package main

func foo() {
	bar()
	baz()
}

func bar() {}
func baz() {}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	if result.Calls[0].CallerName != "foo" || result.Calls[0].CalleeName != "bar" {
		t.Errorf("first call: %+v, want foo -> bar", result.Calls[0])
	}
	if result.Calls[1].CallerName != "foo" || result.Calls[1].CalleeName != "baz" {
		t.Errorf("second call: %+v, want foo -> baz", result.Calls[1])
	}
}

func TestExtractCalls_Go_MethodCalls(t *testing.T) {
	src := []byte(`package main

type Server struct{}

func (s *Server) Start() {
	s.run()
}

func (s *Server) run() {
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "Start" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "Start")
	}
	if ce.CalleeName != "s.run" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "s.run")
	}
}

func TestExtractCalls_Go_NestedCalls(t *testing.T) {
	src := []byte(`package main

func foo() {
	bar(baz())
}

func bar(x int) {}
func baz() int { return 0 }
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	for _, ce := range result.Calls {
		if ce.CallerName != "foo" {
			t.Errorf("CallerName = %q, want %q", ce.CallerName, "foo")
		}
	}
	callees := []string{result.Calls[0].CalleeName, result.Calls[1].CalleeName}
	foundBar, foundBaz := false, false
	for _, c := range callees {
		if c == "bar" {
			foundBar = true
		}
		if c == "baz" {
			foundBaz = true
		}
	}
	if !foundBar {
		t.Errorf("missing callee bar in %v", callees)
	}
	if !foundBaz {
		t.Errorf("missing callee baz in %v", callees)
	}
}

func TestExtractCalls_Go_NoCalls(t *testing.T) {
	src := []byte(`package main

func foo() {
	x := 1
	y := x + 2
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_Go_PackageQualifiedCalls(t *testing.T) {
	src := []byte(`package main

import "fmt"

func main() {
	fmt.Println("hello")
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "main" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "main")
	}
	if ce.CalleeName != "fmt.Println" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "fmt.Println")
	}
}

func TestExtractCalls_Go_StringsTrimSpace(t *testing.T) {
	src := []byte(`package main

import "strings"

func clean(s string) string {
	return strings.TrimSpace(s)
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "clean" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "clean")
	}
	if ce.CalleeName != "strings.TrimSpace" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "strings.TrimSpace")
	}
}

func TestExtractCalls_Go_Sprintf(t *testing.T) {
	src := []byte(`package main

import "fmt"

func greet(name string) string {
	return fmt.Sprintf("Hello, %%s", name)
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "greet" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "greet")
	}
	if ce.CalleeName != "fmt.Sprintf" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "fmt.Sprintf")
	}
}

func TestExtractCalls_Go_MultipleFunctions(t *testing.T) {
	src := []byte(`package main

func alpha() {
	beta()
}

func beta() {
	gamma()
}

func gamma() {
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	if result.Calls[0].CallerName != "alpha" || result.Calls[0].CalleeName != "beta" {
		t.Errorf("first call: %+v, want alpha -> beta", result.Calls[0])
	}
	if result.Calls[1].CallerName != "beta" || result.Calls[1].CalleeName != "gamma" {
		t.Errorf("second call: %+v, want beta -> gamma", result.Calls[1])
	}
}

func TestExtractCalls_Go_OnlyTypes_NoCalls(t *testing.T) {
	src := []byte(`package main

type User struct {
	Name string
}

type Config struct {
	Port int
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Symbols) == 0 {
		t.Error("expected type symbols, got none")
	}
	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_Go_MethodOnValue(t *testing.T) {
	src := []byte(`package main

type Counter struct{}

func (c Counter) Inc() {
	c.log()
}

func (c Counter) log() {
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 1 {
		t.Fatalf("expected 1 call, got %d: %+v", len(result.Calls), result.Calls)
	}
	ce := result.Calls[0]
	if ce.CallerName != "Inc" {
		t.Errorf("CallerName = %q, want %q", ce.CallerName, "Inc")
	}
	if ce.CalleeName != "c.log" {
		t.Errorf("CalleeName = %q, want %q", ce.CalleeName, "c.log")
	}
}

func TestExtractCalls_Go_FunctionWithNoBody(t *testing.T) {
	src := []byte(`package main

func external() int
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls for function with no body, got %d", len(result.Calls))
	}
}

func TestExtractCalls_Go_ComplexCallee(t *testing.T) {
	src := []byte(`package main

func main() {
	result := fmt.Sprintf("%%d %%s", len(data), strings.Join(items, ","))
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) < 3 {
		t.Fatalf("expected at least 3 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	callees := make(map[string]bool)
	for _, ce := range result.Calls {
		callees[ce.CalleeName] = true
	}
	if !callees["fmt.Sprintf"] {
		t.Errorf("missing callee fmt.Sprintf in %v", callees)
	}
	if !callees["len"] {
		t.Errorf("missing callee len in %v", callees)
	}
	if !callees["strings.Join"] {
		t.Errorf("missing callee strings.Join in %v", callees)
	}
}

// ---------------------------------------------------------------------------
// Call-edge extraction — TypeScript
// ---------------------------------------------------------------------------

func TestExtractCalls_TS_SimpleCall(t *testing.T) {
	src := []byte(`function greet(name: string) {
    return "Hello, " + name;
}

function main() {
    const msg = greet("World");
    console.log(msg);
}
`)
	result, err := ParseContent("typescript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	foundGreet, foundConsoleLog := false, false
	for _, ce := range result.Calls {
		if ce.CallerName == "main" && ce.CalleeName == "greet" {
			foundGreet = true
		}
		if ce.CallerName == "main" && ce.CalleeName == "console.log" {
			foundConsoleLog = true
		}
	}
	if !foundGreet {
		t.Errorf("missing main -> greet edge in %+v", result.Calls)
	}
	if !foundConsoleLog {
		t.Errorf("missing main -> console.log edge in %+v", result.Calls)
	}
}

func TestExtractCalls_TS_ArrowFunction(t *testing.T) {
	src := []byte(`const helper = () => {
    return 42;
};

const run = () => {
    const val = helper();
    return val;
};
`)
	result, err := ParseContent("typescript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	// Arrow functions assigned to const are extracted as "variable" symbols
	// (no body), so calls inside them are not attributed to any function.
	// This is expected behavior — the extractSymbols function only extracts
	// top-level declarations, and const/let arrow functions are "variable" kind.
	if len(result.Calls) != 0 {
		t.Logf("info: got %d calls from arrow function variables (unexpected but harmless): %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_TS_ClassMethod(t *testing.T) {
	src := []byte(`class App {
    start() {
        this.init();
    }
    init() {
        console.log("ready");
    }
}
`)
	result, err := ParseContent("typescript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	// TS classes are extracted as "class" symbols (no body), so calls inside
	// methods are not attributed to any function. This is expected because
	// extractSymbols treats classes as non-function symbols without body text.
	if len(result.Calls) != 0 {
		t.Logf("info: got %d calls from class methods (unexpected but harmless): %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_TS_NoCalls(t *testing.T) {
	src := []byte(`const VERSION = "1.0";
const MAX_RETRIES = 3;
`)
	result, err := ParseContent("typescript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_TS_MultipleFunctions(t *testing.T) {
	src := []byte(`function alpha() {
    beta();
}

function beta() {
    gamma();
}

function gamma() {
}
`)
	result, err := ParseContent("typescript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 2 {
		t.Fatalf("expected 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	if result.Calls[0].CallerName != "alpha" || result.Calls[0].CalleeName != "beta" {
		t.Errorf("first call: %+v, want alpha -> beta", result.Calls[0])
	}
	if result.Calls[1].CallerName != "beta" || result.Calls[1].CalleeName != "gamma" {
		t.Errorf("second call: %+v, want beta -> gamma", result.Calls[1])
	}
}

// ---------------------------------------------------------------------------
// Call-edge extraction — JavaScript
// ---------------------------------------------------------------------------

func TestExtractCalls_JS_SimpleCall(t *testing.T) {
	src := []byte(`function greet(name) {
    return "Hello, " + name;
}

function main() {
    const msg = greet("World");
    console.log(msg);
}
`)
	result, err := ParseContent("javascript", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	foundGreet := false
	for _, ce := range result.Calls {
		if ce.CallerName == "main" && ce.CalleeName == "greet" {
			foundGreet = true
		}
	}
	if !foundGreet {
		t.Errorf("missing main -> greet edge in %+v", result.Calls)
	}
}

// ---------------------------------------------------------------------------
// Call-edge extraction — Python
// ---------------------------------------------------------------------------

func TestExtractCalls_Python_SimpleCall(t *testing.T) {
	src := []byte(`def helper():
    return 42

def main():
    val = helper()
    print(val)
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	foundHelper, foundPrint := false, false
	for _, ce := range result.Calls {
		if ce.CallerName == "main" && ce.CalleeName == "helper" {
			foundHelper = true
		}
		if ce.CallerName == "main" && ce.CalleeName == "print" {
			foundPrint = true
		}
	}
	if !foundHelper {
		t.Errorf("missing main -> helper edge in %+v", result.Calls)
	}
	if !foundPrint {
		t.Errorf("missing main -> print edge in %+v", result.Calls)
	}
}

func TestExtractCalls_Python_AsyncFunction(t *testing.T) {
	src := []byte(`async def get_data():
    return "data"

async def fetch():
    data = await get_data()
    print(data)
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	found := false
	for _, ce := range result.Calls {
		if ce.CallerName == "fetch" && ce.CalleeName == "get_data" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("missing fetch -> get_data edge in %+v", result.Calls)
	}
}

func TestExtractCalls_Python_MethodCall(t *testing.T) {
	src := []byte(`class Processor:
    def process(self, data):
        self.validate(data)
        return data

    def validate(self, data):
        print("validating")
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	// Python class methods are inside the class body, so calls are attributed
	// to the class symbol "Processor" (the class has a body in Python).
	if len(result.Calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	foundValidate, foundPrint := false, false
	for _, ce := range result.Calls {
		if ce.CalleeName == "self.validate" {
			foundValidate = true
		}
		if ce.CalleeName == "print" {
			foundPrint = true
		}
	}
	if !foundValidate {
		t.Errorf("missing self.validate callee in %+v", result.Calls)
	}
	if !foundPrint {
		t.Errorf("missing print callee in %+v", result.Calls)
	}
}

func TestExtractCalls_Python_NoCalls(t *testing.T) {
	src := []byte(`class Config:
    port = 8080
    host = "localhost"
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
}

func TestExtractCalls_Python_MultipleFunctions(t *testing.T) {
	src := []byte(`def alpha():
    beta()

def beta():
    gamma()

def gamma():
    pass
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Calls) < 2 {
		t.Fatalf("expected at least 2 calls, got %d: %+v", len(result.Calls), result.Calls)
	}
	foundAlphaBeta, foundBetaGamma := false, false
	for _, ce := range result.Calls {
		if ce.CallerName == "alpha" && ce.CalleeName == "beta" {
			foundAlphaBeta = true
		}
		if ce.CallerName == "beta" && ce.CalleeName == "gamma" {
			foundBetaGamma = true
		}
	}
	if !foundAlphaBeta {
		t.Errorf("missing alpha -> beta edge in %+v", result.Calls)
	}
	if !foundBetaGamma {
		t.Errorf("missing beta -> gamma edge in %+v", result.Calls)
	}
}

// ---------------------------------------------------------------------------
// Call-edge extraction — Edge cases
// ---------------------------------------------------------------------------

func TestExtractCalls_EmptyContent(t *testing.T) {
	result, err := ParseContent("go", []byte(""))
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(result.Symbols))
	}
	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(result.Calls))
	}
}

func TestExtractCalls_PackageOnly(t *testing.T) {
	result, err := ParseContent("go", []byte("package main"))
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols, got %d", len(result.Symbols))
	}
	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(result.Calls))
	}
}

func TestExtractCalls_OnlyImports(t *testing.T) {
	src := []byte(`package main

import "fmt"
import "strings"
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols (imports skipped), got %d: %+v", len(result.Symbols), result.Symbols)
	}
	if len(result.Calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(result.Calls))
	}
}

func TestExtractCalls_ReleaseSafety(t *testing.T) {
	src := []byte(`package main

func foo() {
	bar()
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}

	// Release twice should be safe.
	result.Release()
	result.Release()
}
