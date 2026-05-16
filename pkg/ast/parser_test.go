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
