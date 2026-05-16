package ast

import (
	"testing"
)

// ---------------------------------------------------------------------------
// Go scoped symbols
// ---------------------------------------------------------------------------

func TestExtractGoSymbolsScoped(t *testing.T) {
	src := []byte(`package main

import "fmt"

type Server struct {
	Host string
	Port int
}

type Handler interface {
	Handle(req string) error
	Serve(w string)
}

func main() {
	fmt.Println("hello")
}

func (s *Server) Start() error {
	return nil
}

func (s Server) String() string {
	return s.Host
}

const MaxConn = 100

var GlobalCfg = "default"
`)

	result, err := ParseFile("server.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	// --- Top-level symbols (depth 0) ---
	assertSymbol(t, symbols, "Server", "class", "", 0)
	assertSymbol(t, symbols, "Handler", "interface", "", 0)
	assertSymbol(t, symbols, "main", "function", "", 0)
	assertSymbol(t, symbols, "MaxConn", "constant", "", 0)
	assertSymbol(t, symbols, "GlobalCfg", "variable", "", 0)

	// --- Method with pointer receiver (depth 1, scope "Server") ---
	assertSymbol(t, symbols, "Start", "method", "Server", 1)

	// --- Method with value receiver (depth 1, scope "Server") ---
	assertSymbol(t, symbols, "String", "method", "Server", 1)

	// --- Struct fields (depth 1, scope "Server") ---
	assertSymbol(t, symbols, "Host", "property", "Server", 1)
	assertSymbol(t, symbols, "Port", "property", "Server", 1)

	// --- Interface methods (depth 1, scope "Handler") ---
	assertSymbol(t, symbols, "Handle", "method", "Handler", 1)
	assertSymbol(t, symbols, "Serve", "method", "Handler", 1)
}

func TestExtractGoFields(t *testing.T) {
	src := []byte(`package main

type Config struct {
	Debug bool
	Name  string
}

type Simple struct {
	Value int
}
`)

	result, err := ParseFile("fields.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	// Config struct fields.
	assertSymbol(t, symbols, "Config", "class", "", 0)
	assertSymbol(t, symbols, "Debug", "property", "Config", 1)
	assertSymbol(t, symbols, "Name", "property", "Config", 1)

	// Simple struct (single field, no field_list wrapper).
	assertSymbol(t, symbols, "Simple", "class", "", 0)
	assertSymbol(t, symbols, "Value", "property", "Simple", 1)
}

// ---------------------------------------------------------------------------
// TypeScript scoped symbols
// ---------------------------------------------------------------------------

func TestExtractTSSymbolsScoped(t *testing.T) {
	src := []byte(`export class UserService {
	private db: Database;
	readonly name: string;

	constructor(db: Database) {
		this.db = db;
	}

	find(id: string): User | null {
		return this.db.getUser(id);
	}

	delete(id: string): void {
		this.db.removeUser(id);
	}
}

export interface User {
	id: string;
	name: string;
	email?: string;
}

export enum Role {
	Admin = "admin",
	User = "user",
	Guest = "guest",
}

export function createUser(name: string): User {
	return { id: "1", name, email: "" };
}

const MAX_RETRIES = 3;
`)

	result, err := ParseFile("user.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	// Top-level symbols.
	assertSymbol(t, symbols, "UserService", "class", "", 0)
	assertSymbol(t, symbols, "User", "interface", "", 0)
	assertSymbol(t, symbols, "Role", "enum", "", 0)
	assertSymbol(t, symbols, "createUser", "function", "", 0)
	assertSymbol(t, symbols, "MAX_RETRIES", "variable", "", 0)

	// Class members.
	assertSymbol(t, symbols, "db", "property", "UserService", 1)
	assertSymbol(t, symbols, "name", "property", "UserService", 1)
	assertSymbol(t, symbols, "constructor", "method", "UserService", 1)
	assertSymbol(t, symbols, "find", "method", "UserService", 1)
	assertSymbol(t, symbols, "delete", "method", "UserService", 1)

	// Interface members.
	assertSymbol(t, symbols, "id", "property", "User", 1)
	assertSymbol(t, symbols, "email", "property", "User", 1)

	// Enum members.
	assertSymbol(t, symbols, "Admin", "constant", "Role", 1)
	assertSymbol(t, symbols, "User", "constant", "Role", 1)
	assertSymbol(t, symbols, "Guest", "constant", "Role", 1)
}

// ---------------------------------------------------------------------------
// JavaScript scoped symbols
// ---------------------------------------------------------------------------

func TestExtractJSSymbolsScoped(t *testing.T) {
	src := []byte(`class Calculator {
	add(a, b) {
		return a + b;
	}

	multiply(a, b) {
		return a * b;
	}
}

function greet(name) {
	return "Hello " + name;
}

const VERSION = "1.0";
`)

	result, err := ParseFile("calc.js", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	assertSymbol(t, symbols, "Calculator", "class", "", 0)
	assertSymbol(t, symbols, "add", "method", "Calculator", 1)
	assertSymbol(t, symbols, "multiply", "method", "Calculator", 1)
	assertSymbol(t, symbols, "greet", "function", "", 0)
	assertSymbol(t, symbols, "VERSION", "variable", "", 0)
}

// ---------------------------------------------------------------------------
// Python scoped symbols
// ---------------------------------------------------------------------------

func TestExtractPythonSymbolsScoped(t *testing.T) {
	src := []byte(`import os

class Calculator:
    total: int = 0

    def add(self, a: int, b: int) -> int:
        return a + b

    def multiply(self, a: int, b: int) -> int:
        return a * b

    @staticmethod
    def divide(a: float, b: float) -> float:
        return a / b

def greet(name: str) -> str:
    return f"hello {name}"

async def fetch(url):
    pass
`)

	result, err := ParseFile("calc.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	// Top-level symbols.
	assertSymbol(t, symbols, "Calculator", "class", "", 0)
	assertSymbol(t, symbols, "greet", "function", "", 0)
	assertSymbol(t, symbols, "fetch", "function", "", 0)

	// Class members.
	assertSymbol(t, symbols, "total", "property", "Calculator", 1)
	assertSymbol(t, symbols, "add", "method", "Calculator", 1)
	assertSymbol(t, symbols, "multiply", "method", "Calculator", 1)
	assertSymbol(t, symbols, "divide", "method", "Calculator", 1)
}

func TestExtractPythonDecoratedClass(t *testing.T) {
	src := []byte(`@dataclass
class Point:
    x: int
    y: int

    def distance(self) -> float:
        return (self.x**2 + self.y**2)**0.5
`)

	result, err := ParseFile("point.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	// Decorated class at top level.
	assertSymbol(t, symbols, "Point", "class", "", 0)

	// Class members.
	assertSymbol(t, symbols, "x", "property", "Point", 1)
	assertSymbol(t, symbols, "y", "property", "Point", 1)
	assertSymbol(t, symbols, "distance", "method", "Point", 1)

	// Verify the decorated class starts at the decorator line (line 1).
	for _, s := range symbols {
		if s.Name == "Point" && s.Scope == "" {
			if s.StartLine != 1 {
				t.Errorf("decorated class Point: StartLine = %d, want 1 (decorator line)", s.StartLine)
			}
			break
		}
	}
}

func TestExtractPythonDecoratedFunction(t *testing.T) {
	src := []byte(`@cache
def compute(n):
    return n * 2

@dataclass
class Config:
    host: str
    port: int = 80
`)

	result, err := ParseFile("deco.py", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	assertSymbol(t, symbols, "compute", "function", "", 0)
	assertSymbol(t, symbols, "Config", "class", "", 0)
	assertSymbol(t, symbols, "host", "property", "Config", 1)
	assertSymbol(t, symbols, "port", "property", "Config", 1)
}

// ---------------------------------------------------------------------------
// TSX
// ---------------------------------------------------------------------------

func TestExtractTSXSymbolsScoped(t *testing.T) {
	src := []byte(`export class App {
	state: number;

	update(): void {
		this.state++;
	}
}
`)

	result, err := ParseFile("App.tsx", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	assertSymbol(t, symbols, "App", "class", "", 0)
	assertSymbol(t, symbols, "state", "property", "App", 1)
	assertSymbol(t, symbols, "update", "method", "App", 1)
}

// ---------------------------------------------------------------------------
// TypeScript non-exported symbols
// ---------------------------------------------------------------------------

func TestExtractTSSymbolsNonExported(t *testing.T) {
	src := []byte(`function helper() {
	return 42;
}

class LocalClass {
	x: number;
}

const localConst = "hello";
`)

	result, err := ParseFile("local.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	assertSymbol(t, symbols, "helper", "function", "", 0)
	assertSymbol(t, symbols, "LocalClass", "class", "", 0)
	assertSymbol(t, symbols, "x", "property", "LocalClass", 1)
	assertSymbol(t, symbols, "localConst", "variable", "", 0)
}

// ---------------------------------------------------------------------------
// MaxDepth limiting
// ---------------------------------------------------------------------------

func TestExtractSymbolsMaxDepth(t *testing.T) {
	src := []byte(`package main

type Foo struct {
	Bar string
}

func (f *Foo) Do() {}
`)

	result, err := ParseFile("foo.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// maxDepth=1: only top-level, no nested members.
	topOnly := ExtractSymbolsWithMaxDepth(result.Root, result.Bound, "go", 1)
	var topNames []string
	for _, s := range topOnly {
		topNames = append(topNames, s.Name)
	}
	// Should contain only "Foo" and "Do" (method at top-level in Go AST).
	// The method appears as a top-level node in the Go AST.
	assertContains(t, topNames, "Foo")
	assertContains(t, topNames, "Do")

	// Verify no depth-1 struct fields when maxDepth=1.
	for _, s := range topOnly {
		if s.Depth == 1 && s.Kind == "property" {
			t.Errorf("maxDepth=1 should not include struct fields, got: %s (depth %d)", s.Name, s.Depth)
		}
	}

	// maxDepth=2: include nested members.
	full := ExtractSymbolsWithMaxDepth(result.Root, result.Bound, "go", 2)
	foundBar := false
	for _, s := range full {
		if s.Name == "Bar" && s.Kind == "property" && s.Scope == "Foo" && s.Depth == 1 {
			foundBar = true
			break
		}
	}
	if !foundBar {
		t.Error("maxDepth=2 should include struct field Bar")
	}
}

func TestExtractSymbolsMaxDepthZero(t *testing.T) {
	src := []byte(`package main
func hello() {}
`)
	result, err := ParseFile("hello.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// maxDepth=0: nothing should be extracted.
	symbols := ExtractSymbolsWithMaxDepth(result.Root, result.Bound, "go", 0)
	if len(symbols) != 0 {
		t.Errorf("maxDepth=0 should produce no symbols, got %d: %+v", len(symbols), symbols)
	}
}

// ---------------------------------------------------------------------------
// Empty / nil handling
// ---------------------------------------------------------------------------

func TestExtractSymbolsEmptyFile(t *testing.T) {
	src := []byte(`package main
`)
	result, err := ParseFile("empty.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)
	if len(symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d: %+v", len(symbols), symbols)
	}
}

func TestExtractSymbolsNilRoot(t *testing.T) {
	symbols := ExtractSymbols(nil, nil, "go")
	if symbols != nil {
		t.Errorf("expected nil for nil root, got %+v", symbols)
	}
}

// ---------------------------------------------------------------------------
// DefaultMaxDepth constant
// ---------------------------------------------------------------------------

func TestDefaultMaxDepth(t *testing.T) {
	if DefaultMaxDepth != 2 {
		t.Errorf("DefaultMaxDepth = %d, want 2", DefaultMaxDepth)
	}
}

// ---------------------------------------------------------------------------
// ScopedSymbol struct embedding
// ---------------------------------------------------------------------------

func TestScopedSymbolEmbedding(t *testing.T) {
	ss := ScopedSymbol{
		Symbol: Symbol{
			Name:      "MyFunc",
			Kind:      "function",
			StartLine: 1,
			EndLine:   5,
		},
		Scope: "MyClass",
		Depth: 1,
	}
	if ss.Name != "MyFunc" {
		t.Errorf("embedded Name = %q, want %q", ss.Name, "MyFunc")
	}
	if ss.Kind != "function" {
		t.Errorf("embedded Kind = %q, want %q", ss.Kind, "function")
	}
	if ss.Scope != "MyClass" {
		t.Errorf("Scope = %q, want %q", ss.Scope, "MyClass")
	}
	if ss.Depth != 1 {
		t.Errorf("Depth = %d, want 1", ss.Depth)
	}
}

// ---------------------------------------------------------------------------
// Depth and scope validation for all extracted symbols
// ---------------------------------------------------------------------------

func TestExtractSymbolsDepthInvariant(t *testing.T) {
	// Use a mixed TS source to verify depth/scope invariants.
	src := []byte(`export class Outer {
	field: string;

	method(): void {}
}

export interface Config {
	host: string;
	port: number;
}

export enum Status {
	Active,
	Inactive,
}

export function init() {}
`)

	result, err := ParseFile("app.ts", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		// Depth 0 symbols must have empty scope.
		if s.Depth == 0 && s.Scope != "" {
			t.Errorf("depth-0 symbol %q has non-empty Scope = %q", s.Name, s.Scope)
		}
		// Depth 1 symbols must have non-empty scope.
		if s.Depth == 1 && s.Scope == "" {
			t.Errorf("depth-1 symbol %q has empty Scope", s.Name)
		}
		// Depth should never exceed maxDepth-1 (DefaultMaxDepth=2).
		if s.Depth >= DefaultMaxDepth {
			t.Errorf("symbol %q has Depth = %d >= DefaultMaxDepth %d", s.Name, s.Depth, DefaultMaxDepth)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: negative maxDepth
// ---------------------------------------------------------------------------

func TestExtractSymbolsMaxDepthNegative(t *testing.T) {
	src := []byte(`package main
func hello() {}
`)
	result, err := ParseFile("hello.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// maxDepth=-1: should return nil.
	symbols := ExtractSymbolsWithMaxDepth(result.Root, result.Bound, "go", -1)
	if symbols != nil {
		t.Errorf("maxDepth=-1 should produce nil, got %d symbols: %+v", len(symbols), symbols)
	}
}

// ---------------------------------------------------------------------------
// Test: generic fallback for unsupported language
// ---------------------------------------------------------------------------

func TestExtractGenericSymbolsFallback(t *testing.T) {
	src := []byte(`package main

func hello() {
	println("world")
}

type Greeting struct {
	Msg string
}
`)

	result, err := ParseFile("greeting.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	// Pass an unsupported language to trigger the generic fallback.
	symbols := ExtractSymbolsWithMaxDepth(result.Root, result.Bound, "ruby", 2)
	if len(symbols) == 0 {
		t.Errorf("generic fallback should return at least some symbols, got 0")
	}

	// The generic fallback uses childText(node, bt, "name") which works for
	// function_declaration (has "name" field). Verify we got the function.
	foundHello := false
	for _, s := range symbols {
		if s.Name == "hello" {
			foundHello = true
			break
		}
	}
	if !foundHello {
		t.Errorf("generic fallback should find function 'hello', got: %+v", symbols)
	}
}

// ---------------------------------------------------------------------------
// Test: Go depth invariant (methods have non-empty scope)
// ---------------------------------------------------------------------------

func TestExtractGoSymbolsDepthInvariant(t *testing.T) {
	src := []byte(`package main

type Server struct {
	Host string
	Port int
}

type Handler interface {
	Handle(req string) error
}

func main() {}

func (s *Server) Start() error {
	return nil
}

func (s Server) String() string {
	return s.Host
}

const MaxConn = 100

var GlobalCfg = "default"
`)

	result, err := ParseFile("invariant.go", src)
	if err != nil {
		t.Fatalf("ParseFile: %v", err)
	}
	defer result.Release()

	symbols := ExtractSymbols(result.Root, result.Bound, result.Language)

	for _, s := range symbols {
		// Depth 0 symbols must have empty scope.
		if s.Depth == 0 && s.Scope != "" {
			t.Errorf("Go depth-0 symbol %q has non-empty Scope = %q", s.Name, s.Scope)
		}
		// Depth 1 symbols must have non-empty scope.
		if s.Depth == 1 && s.Scope == "" {
			t.Errorf("Go depth-1 symbol %q has empty Scope (kind=%q)", s.Name, s.Kind)
		}
	}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// assertSymbol checks that a symbol with the given name, kind, scope, and depth
// exists in the slice.
func assertSymbol(t *testing.T, symbols []ScopedSymbol, name, kind, scope string, depth int) {
	t.Helper()
	for _, s := range symbols {
		if s.Name == name && s.Kind == kind && s.Scope == scope && s.Depth == depth {
			return // found
		}
	}
	t.Errorf("missing symbol: Name=%q, Kind=%q, Scope=%q, Depth=%d (got: %+v)",
		name, kind, scope, depth, symbols)
}

// assertContains checks that a string slice contains a value.
func assertContains(t *testing.T, items []string, want string) {
	t.Helper()
	for _, item := range items {
		if item == want {
			return
		}
	}
	t.Errorf("slice %v does not contain %q", items, want)
}
