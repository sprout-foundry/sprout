package ast

import (
	"strings"
	"testing"

	gotreesitter "github.com/odvcencio/gotreesitter"
)

// ---------------------------------------------------------------------------
// Go function body extraction
// ---------------------------------------------------------------------------

func TestGoFunctionBody(t *testing.T) {
	src := []byte(`package main

func add(a, b int) int {
	return a + b
}

type Foo struct {
	X int
}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// Check via parser.go symbols (parse returns Symbol with Body).
	for _, sym := range result.Symbols {
		switch sym.Name {
		case "add":
			if sym.Body == "" {
				t.Error("add function should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "return a + b") {
				t.Errorf("add Body = %q, want to contain 'return a + b'", sym.Body)
			}
		case "Foo":
			if sym.Body != "" {
				t.Errorf("Foo struct should have empty Body, got %q", sym.Body)
			}
		}
	}
}

func TestGoMethodBody(t *testing.T) {
	src := []byte(`package main

type Server struct{}

func (s *Server) Start() error {
	s.active = true
	return nil
}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	found := false
	for _, sym := range result.Symbols {
		if sym.Name == "Start" && sym.Kind == "method" {
			found = true
			if sym.Body == "" {
				t.Error("Start method should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "s.active") {
				t.Errorf("Start Body = %q, want to contain 's.active'", sym.Body)
			}
		}
	}
	if !found {
		t.Fatal("expected method Start")
	}
}

func TestGoScopedSymbolBody(t *testing.T) {
	src := []byte(`package main

func greet(name string) string {
	return "hello " + name
}

type Greeter struct {
	lang string
}

func (g *Greeter) Hello() string {
	return g.lang + " hello"
}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Name {
		case "greet":
			if s.Body == "" {
				t.Error("greet function should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "hello") {
				t.Errorf("greet Body = %q, want to contain 'hello'", s.Body)
			}
		case "Hello":
			if s.Scope != "Greeter" {
				t.Errorf("Hello Scope = %q, want 'Greeter'", s.Scope)
			}
			if s.Body == "" {
				t.Error("Hello method should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "g.lang") {
				t.Errorf("Hello Body = %q, want to contain 'g.lang'", s.Body)
			}
		case "Greeter":
			// Go structs don't get body extraction.
			if s.Body != "" {
				t.Errorf("Greeter struct should have empty Body, got %q", s.Body)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TypeScript function body extraction
// ---------------------------------------------------------------------------

func TestTSFunctionBody(t *testing.T) {
	src := []byte(`export function greet(name: string): string {
	return "hello " + name;
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "greet" {
			if sym.Body == "" {
				t.Error("greet function should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "return") {
				t.Errorf("greet Body = %q, want to contain 'return'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected function greet in symbols")
}

func TestTSMethodBody(t *testing.T) {
	src := []byte(`class Calculator {
	add(a: number, b: number): number {
		return a + b;
	}

	multiply(a: number, b: number): number {
		return a * b;
	}
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Name {
		case "add":
			if s.Scope != "Calculator" {
				t.Errorf("add Scope = %q, want 'Calculator'", s.Scope)
			}
			if s.Body == "" {
				t.Error("add method should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "return a + b") {
				t.Errorf("add Body = %q, want to contain 'return a + b'", s.Body)
			}
		case "multiply":
			if s.Body == "" {
				t.Error("multiply method should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "a * b") {
				t.Errorf("multiply Body = %q, want to contain 'a * b'", s.Body)
			}
		}
	}
}

func TestTSConstructorBody(t *testing.T) {
	src := []byte(`class App {
	private port: number;

	constructor(port: number) {
		this.port = port;
	}

	start(): void {
		console.log("started");
	}
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Name == "constructor" && s.Kind == "method" {
			if s.Body == "" {
				t.Error("constructor should have non-empty Body")
			}
			if !strings.Contains(s.Body, "this.port") {
				t.Errorf("constructor Body = %q, want to contain 'this.port'", s.Body)
			}
			return
		}
	}
	t.Fatal("expected constructor in scoped symbols")
}

func TestTSNoBodyForNonFunctions(t *testing.T) {
	src := []byte(`class App {
	state: number;

	update(): void {
		this.state++;
	}
}

interface Config {
	host: string;
}

enum Status {
	Active,
	Inactive,
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Kind {
		case "class", "interface", "enum", "property", "constant":
			if s.Body != "" {
				t.Errorf("%s %q (kind=%s) should have empty Body, got %q", s.Kind, s.Name, s.Kind, s.Body)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// JavaScript body extraction
// ---------------------------------------------------------------------------

func TestJSFunctionBody(t *testing.T) {
	src := []byte(`function greet(name) {
	return "Hello " + name;
}
`)
	result, err := ParseFile("test.js", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "greet" {
			if sym.Body == "" {
				t.Error("greet function should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "Hello") {
				t.Errorf("greet Body = %q, want to contain 'Hello'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected function greet in symbols")
}

func TestJSMethodBody(t *testing.T) {
	src := []byte(`class Calculator {
	add(a, b) {
		return a + b;
	}
}
`)
	result, err := ParseFile("test.js", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Name == "add" && s.Kind == "method" {
			if s.Body == "" {
				t.Error("add method should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "a + b") {
				t.Errorf("add Body = %q, want to contain 'a + b'", s.Body)
			}
			return
		}
	}
	t.Fatal("expected method add in scoped symbols")
}

// ---------------------------------------------------------------------------
// Python body extraction
// ---------------------------------------------------------------------------

func TestPythonFunctionBody(t *testing.T) {
	src := []byte(`def greet(name):
    return f"hello {name}"
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "greet" {
			if sym.Body == "" {
				t.Error("greet function should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "hello") {
				t.Errorf("greet Body = %q, want to contain 'hello'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected function greet in symbols")
}

func TestPythonClassBody(t *testing.T) {
	src := []byte(`class Calculator:
    def add(self, a, b):
        return a + b

    def multiply(self, a, b):
        return a * b
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// Check via parser.go symbols — Python classes get body extraction.
	for _, sym := range result.Symbols {
		if sym.Name == "Calculator" && sym.Kind == "class" {
			if sym.Body == "" {
				t.Error("Calculator class should have non-empty Body in Python")
			}
			if !strings.Contains(sym.Body, "add") {
				t.Errorf("Calculator Body = %q, want to contain 'add'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected class Calculator in symbols")
}

func TestPythonScopedSymbolBody(t *testing.T) {
	src := []byte(`class Calculator:
    total: int = 0

    def add(self, a, b):
        return a + b

    def multiply(self, a, b):
        return a * b
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Name {
		case "Calculator":
			// Python class should have body in scoped symbols too.
			if s.Body == "" {
				t.Error("Calculator class should have non-empty Body in scoped symbols")
			}
		case "add":
			if s.Scope != "Calculator" {
				t.Errorf("add Scope = %q, want 'Calculator'", s.Scope)
			}
			if s.Body == "" {
				t.Error("add method should have non-empty Body in scoped symbols")
			}
			if !strings.Contains(s.Body, "a + b") {
				t.Errorf("add Body = %q, want to contain 'a + b'", s.Body)
			}
		case "multiply":
			if s.Body == "" {
				t.Error("multiply method should have non-empty Body in scoped symbols")
			}
		case "total":
			// Property should have no body.
			if s.Body != "" {
				t.Errorf("total property should have empty Body, got %q", s.Body)
			}
		}
	}
}

func TestPythonAsyncFunctionBody(t *testing.T) {
	src := []byte(`async def fetch(url):
    response = await get(url)
    return response.text
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "fetch" {
			if sym.Body == "" {
				t.Error("fetch async function should have non-empty Body")
			}
			if !strings.Contains(sym.Body, "await") {
				t.Errorf("fetch Body = %q, want to contain 'await'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected async function fetch in symbols")
}

func TestPythonDecoratedFunctionBody(t *testing.T) {
	src := []byte(`@cache
def compute(n):
    return n * 2
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Name == "compute" && s.Kind == "function" {
			if s.Body == "" {
				t.Error("compute decorated function should have non-empty Body")
			}
			if !strings.Contains(s.Body, "n * 2") {
				t.Errorf("compute Body = %q, want to contain 'n * 2'", s.Body)
			}
			return
		}
	}
	t.Fatal("expected decorated function compute in scoped symbols")
}

func TestPythonDecoratedClassBody(t *testing.T) {
	src := []byte(`@dataclass
class Point:
    x: int
    y: int

    def distance(self):
        return (self.x**2 + self.y**2)**0.5
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Name == "Point" && s.Kind == "class" && s.Scope == "" {
			if s.Body == "" {
				t.Error("Point decorated class should have non-empty Body")
			}
			return
		}
	}
	t.Fatal("expected decorated class Point in scoped symbols")
}

func TestPythonDecoratedMethodBody(t *testing.T) {
	src := []byte(`class Calc:
    @staticmethod
    def divide(a, b):
        return a / b

    @property
    def name(self):
        return "calc"
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Name {
		case "divide":
			if s.Body == "" {
				t.Error("divide decorated method should have non-empty Body")
			}
			if !strings.Contains(s.Body, "a / b") {
				t.Errorf("divide Body = %q, want to contain 'a / b'", s.Body)
			}
		case "name":
			if s.Body == "" {
				t.Error("name decorated method should have non-empty Body")
			}
			if !strings.Contains(s.Body, "calc") {
				t.Errorf("name Body = %q, want to contain 'calc'", s.Body)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// TSX body extraction
// ---------------------------------------------------------------------------

func TestTSXFunctionBody(t *testing.T) {
	src := []byte(`export function App() {
	return <div>Hello</div>;
}
`)
	result, err := ParseFile("test.tsx", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "App" {
			if sym.Body == "" {
				t.Error("App function should have non-empty Body")
			}
			return
		}
	}
	t.Fatal("expected function App in symbols")
}

// ---------------------------------------------------------------------------
// Body extraction via ParseContent (no file path)
// ---------------------------------------------------------------------------

func TestParseContentBodyGo(t *testing.T) {
	src := []byte(`package main

func hello() string {
	return "world"
}
`)
	result, err := ParseContent("go", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "hello" {
			if sym.Body == "" {
				t.Error("hello should have non-empty Body via ParseContent")
			}
			if !strings.Contains(sym.Body, "world") {
				t.Errorf("hello Body = %q, want to contain 'world'", sym.Body)
			}
			return
		}
	}
	t.Fatal("expected function hello via ParseContent")
}

func TestParseContentBodyPython(t *testing.T) {
	src := []byte(`def hello():
    return "world"
`)
	result, err := ParseContent("python", src)
	if err != nil {
		t.Fatalf("ParseContent: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "hello" {
			if sym.Body == "" {
				t.Error("hello should have non-empty Body via ParseContent for Python")
			}
			return
		}
	}
	t.Fatal("expected function hello via ParseContent")
}

// ---------------------------------------------------------------------------
// Edge cases
// ---------------------------------------------------------------------------

func TestEmptyBodyForNonFunctionTypes(t *testing.T) {
	src := []byte(`package main

type Alias = string

const MaxConn = 100

var GlobalCfg = "default"
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Body != "" {
			t.Errorf("%s %q should have empty Body (non-function type), got %q", s.Kind, s.Name, s.Body)
		}
	}
}

func TestEmptyFunctionBody(t *testing.T) {
	src := []byte(`package main

func empty() {}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "empty" {
			// Empty body should still produce some body text (e.g. "{}").
			if sym.Body == "" {
				t.Error("empty function should still have body text (braces)")
			}
			return
		}
	}
	t.Fatal("expected function empty in symbols")
}

func TestPythonEmptyClass(t *testing.T) {
	src := []byte(`class Empty:
    pass
`)
	result, err := ParseFile("test.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	for _, sym := range result.Symbols {
		if sym.Name == "Empty" {
			// Python class should have body even if it's just "pass".
			if sym.Body == "" {
				t.Error("Empty class should have non-empty Body in Python")
			}
			return
		}
	}
	t.Fatal("expected class Empty in symbols")
}

func TestTSInterfaceNoBody(t *testing.T) {
	src := []byte(`interface Config {
	host: string;
	port: number;
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Kind == "interface" || s.Kind == "property" {
			if s.Body != "" {
				t.Errorf("%s %q should have empty Body, got %q", s.Kind, s.Name, s.Body)
			}
		}
	}
}

func TestTSEnumNoBody(t *testing.T) {
	src := []byte(`enum Role {
	Admin = "admin",
	User = "user",
}
`)
	result, err := ParseFile("test.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		if s.Kind == "enum" || s.Kind == "constant" {
			if s.Body != "" {
				t.Errorf("%s %q should have empty Body, got %q", s.Kind, s.Name, s.Body)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// Registry extensibility
// ---------------------------------------------------------------------------

type testExtractor struct{}

func (e *testExtractor) ExtractBody(node *gotreesitter.Node, bt *gotreesitter.BoundTree) string {
	return "custom-body"
}

func TestRegistryExtensibility(t *testing.T) {
	// Register a custom extractor for a test language.
	RegisterBodyExtractor("ruby", &testExtractor{})

	// Unregister it after the test (restore to generic fallback).
	defer func() {
		RegisterBodyExtractor("ruby", &genericBodyExtractor{})
	}()

	// Parse a Go file but pass "ruby" as the language to trigger the custom extractor.
	src := []byte(`package main

func hello() {}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, "ruby")

	// The custom extractor should be called for the function.
	for _, s := range symbols {
		if s.Name == "hello" && s.Kind == "function" {
			if s.Body != "custom-body" {
				t.Errorf("expected custom body 'custom-body', got %q", s.Body)
			}
			return
		}
	}
	t.Fatal("expected function hello with custom body")
}

// ---------------------------------------------------------------------------
// ScopedSymbolWithBody helper
// ---------------------------------------------------------------------------

func TestScopedSymbolWithBodyHelper(t *testing.T) {
	// Create a simple function and verify the helper works.
	src := []byte(`package main

func add(a, b int) int {
	return a + b
}
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// Verify the helper produces the right Body.
	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)
	for _, s := range symbols {
		if s.Name == "add" {
			if s.Body == "" {
				t.Error("scopedSymbolWithBody should populate Body for Go functions")
			}
			if !strings.Contains(s.Body, "return a + b") {
				t.Errorf("Body = %q, want to contain 'return a + b'", s.Body)
			}
			if s.Kind != "function" {
				t.Errorf("Kind = %q, want 'function'", s.Kind)
			}
			return
		}
	}
	t.Fatal("expected function add in scoped symbols")
}

// ---------------------------------------------------------------------------
// Verify non-function symbols still use scopedSymbol (no body)
// ---------------------------------------------------------------------------

func TestScopedSymbolNoBodyForNonFunctions(t *testing.T) {
	src := []byte(`package main

type MyStruct struct {
	Field string
}

const MaxConn = 100

var GlobalCfg = "default"
`)
	result, err := ParseFile("test.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		switch s.Kind {
		case "class", "type", "interface", "variable", "constant", "property":
			if s.Body != "" {
				t.Errorf("non-function symbol %s %q should have empty Body, got %q", s.Kind, s.Name, s.Body)
			}
		}
	}
}
