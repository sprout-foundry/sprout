//go:build js && wasm

// Tests for the AST WASM bridge functions in ast_funcs.go. Run via:
//
//	GOOS=js GOARCH=wasm go test \
//	  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" \
//	  ./cmd/wasm/
//
// The JS-facing functions (parseFileFunc, extractSymbolsFunc, supportedLanguagesFunc)
// depend on syscall/js and a live JS host, which is hard to fake. These tests
// pin the pure-Go pkg/ast logic that the WASM bridge wraps: parsing, symbol
// extraction, scope/depth tracking, and language detection.

package main

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/ast"
)

// ─── Supported Languages ────────────────────────────────────────

// TestSupportedLanguagesReturnsExpected verifies that the SupportedLanguages
// map contains the expected set of languages and that all values are true.
func TestSupportedLanguagesReturnsExpected(t *testing.T) {
	required := []string{"go", "typescript", "tsx", "javascript", "python"}

	for _, lang := range required {
		if !ast.SupportedLanguages[lang] {
			t.Errorf("SupportedLanguages[%q] = false, want true", lang)
		}
	}

	// No unexpected languages should be present.
	for lang := range ast.SupportedLanguages {
		found := false
		for _, want := range required {
			if lang == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("unexpected language in SupportedLanguages: %q", lang)
		}
	}
}

// TestSupportedLanguagesSort verifies that the sorted list produced by
// supportedLanguagesFunc would be in correct alphabetical order.
func TestSupportedLanguagesSort(t *testing.T) {
	names := make([]string, 0, len(ast.SupportedLanguages))
	for lang := range ast.SupportedLanguages {
		names = append(names, lang)
	}
	sort.Strings(names)

	// Verify the list is actually sorted.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("names not sorted: %q < %q at index %d", names[i], names[i-1], i)
		}
	}

	// Verify the sorted order matches expected alphabetical order.
	expected := []string{"go", "javascript", "python", "tsx", "typescript"}
	if len(names) != len(expected) {
		t.Fatalf("got %d languages, want %d", len(names), len(expected))
	}
	for i, want := range expected {
		if names[i] != want {
			t.Errorf("names[%d] = %q, want %q", i, names[i], want)
		}
	}
}

// ─── ParseFile: Go ──────────────────────────────────────────────

// TestParseFileGoCode verifies that Go source with multiple top-level
// functions is parsed and the symbols are extracted correctly.
func TestParseFileGoCode(t *testing.T) {
	code := "package main\n\nfunc hello() {}\nfunc world() {}\n"

	result, err := ast.ParseFile("test.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Go: %v", err)
	}
	defer result.Release()

	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}

	if len(result.Symbols) < 2 {
		t.Errorf("expected at least 2 symbols, got %d: %v", len(result.Symbols), symbolNames(result.Symbols))
		return
	}

	names := make(map[string]bool)
	for _, s := range result.Symbols {
		names[s.Name] = true
	}
	for _, want := range []string{"hello", "world"} {
		if !names[want] {
			t.Errorf("missing symbol %q", want)
		}
	}
}

// TestParseFileGoCodeWithStructs verifies that struct and interface types
// are classified correctly as "class" and "interface".
func TestParseFileGoCodeWithStructs(t *testing.T) {
	code := `package main

type Foo struct {
	Name string
}

type Bar interface {
	Say() string
}
`
	result, err := ast.ParseFile("types.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Go types: %v", err)
	}
	defer result.Release()

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	if k, ok := kinds["Foo"]; !ok {
		t.Error("missing symbol Foo")
	} else if k != "class" {
		t.Errorf("Foo kind = %q, want %q", k, "class")
	}

	if k, ok := kinds["Bar"]; !ok {
		t.Error("missing symbol Bar")
	} else if k != "interface" {
		t.Errorf("Bar kind = %q, want %q", k, "interface")
	}
}

// TestParseFileGoCodeWithMethods verifies that methods on receivers
// are detected as "method" kind.
func TestParseFileGoCodeWithMethods(t *testing.T) {
	code := `package main

type S struct{}

func (s *S) Do() {}
func (s S) ReadOnly() {}
`
	result, err := ast.ParseFile("methods.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Go methods: %v", err)
	}
	defer result.Release()

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	for _, name := range []string{"Do", "ReadOnly"} {
		if k, ok := kinds[name]; !ok {
			t.Errorf("missing symbol %q", name)
		} else if k != "method" {
			t.Errorf("%s kind = %q, want %q", name, k, "method")
		}
	}
}

// TestParseFileGoCodeLineNumbers verifies that line numbers are 1-based
// and accurate for the parsed symbols.
func TestParseFileGoCodeLineNumbers(t *testing.T) {
	// func greet() {} starts on line 3.
	code := "package main\n\nfunc greet() {}\n"

	result, err := ast.ParseFile("test.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile line numbers: %v", err)
	}
	defer result.Release()

	for _, s := range result.Symbols {
		if s.Name == "greet" {
			if s.StartLine != 3 {
				t.Errorf("greet StartLine = %d, want 3", s.StartLine)
			}
			if s.EndLine != 3 {
				t.Errorf("greet EndLine = %d, want 3", s.EndLine)
			}
			return
		}
	}
	t.Error("symbol greet not found")
}

// TestParseFileGoCodeEmptyContent verifies that parsing an empty Go file
// does not panic and returns empty symbols.
func TestParseFileGoCodeEmptyContent(t *testing.T) {
	result, err := ast.ParseFile("empty.go", []byte(""))
	if err != nil {
		t.Fatalf("ParseFile empty content: %v", err)
	}
	defer result.Release()

	if result.Language != "go" {
		t.Errorf("Language = %q, want %q", result.Language, "go")
	}
	if len(result.Symbols) != 0 {
		t.Errorf("expected 0 symbols for empty file, got %d", len(result.Symbols))
	}
}

// ─── ParseFile: TypeScript ──────────────────────────────────────

// TestParseFileTypeScriptCode verifies TypeScript parsing with function
// and const declarations.
func TestParseFileTypeScriptCode(t *testing.T) {
	code := "function greet(name: string): void {}\nconst x = 1;\n"

	result, err := ast.ParseFile("test.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TypeScript: %v", err)
	}
	defer result.Release()

	if result.Language != "typescript" {
		t.Errorf("Language = %q, want %q", result.Language, "typescript")
	}

	if len(result.Symbols) == 0 {
		t.Error("expected at least 1 symbol from TypeScript code")
	}
}

// TestParseFileTypeScriptCodeWithClass verifies TypeScript class declarations
// with methods and properties.
func TestParseFileTypeScriptCodeWithClass(t *testing.T) {
	code := `class Greeter {
	greeting: string;
	constructor(message: string) {
		this.greeting = message;
	}
	greet(): string {
		return this.greeting;
	}
}
`
	result, err := ast.ParseFile("greeter.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS class: %v", err)
	}
	defer result.Release()

	if result.Language != "typescript" {
		t.Errorf("Language = %q, want %q", result.Language, "typescript")
	}

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	if k, ok := kinds["Greeter"]; !ok {
		t.Error("missing symbol Greeter")
	} else if k != "class" {
		t.Errorf("Greeter kind = %q, want %q", k, "class")
	}
}

// TestParseFileTypeScriptCodeWithInterface verifies TypeScript interface detection.
func TestParseFileTypeScriptCodeWithInterface(t *testing.T) {
	code := `interface User {
	id: number;
	name: string;
}
`
	result, err := ast.ParseFile("user.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS interface: %v", err)
	}
	defer result.Release()

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	if k, ok := kinds["User"]; !ok {
		t.Error("missing symbol User")
	} else if k != "interface" {
		t.Errorf("User kind = %q, want %q", k, "interface")
	}
}

// TestParseFileTypeScriptCodeWithEnum verifies TypeScript enum detection.
func TestParseFileTypeScriptCodeWithEnum(t *testing.T) {
	code := `enum Color {
	Red,
	Green,
	Blue
}
`
	result, err := ast.ParseFile("color.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS enum: %v", err)
	}
	defer result.Release()

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	if k, ok := kinds["Color"]; !ok {
		t.Error("missing symbol Color")
	} else if k != "enum" {
		t.Errorf("Color kind = %q, want %q", k, "enum")
	}
}

// TestParseFileTypeScriptCodeWithTypeAlias verifies TypeScript type alias detection.
func TestParseFileTypeScriptCodeWithTypeAlias(t *testing.T) {
	code := `type ID = string;
type Point = { x: number; y: number };
`
	result, err := ast.ParseFile("types.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS type alias: %v", err)
	}
	defer result.Release()

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	for _, name := range []string{"ID", "Point"} {
		if k, ok := kinds[name]; !ok {
			t.Errorf("missing symbol %q", name)
		} else if k != "type" {
			t.Errorf("%s kind = %q, want %q", name, k, "type")
		}
	}
}

// ─── ParseFile: JavaScript ──────────────────────────────────────

// TestParseFileJavaScriptCode verifies that .js files are detected as
// "javascript" (not "typescript").
func TestParseFileJavaScriptCode(t *testing.T) {
	code := "function add(a, b) {\n\treturn a + b;\n}\n"

	result, err := ast.ParseFile("math.js", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile JavaScript: %v", err)
	}
	defer result.Release()

	if result.Language != "javascript" {
		t.Errorf("Language = %q, want %q", result.Language, "javascript")
	}

	if len(result.Symbols) == 0 {
		t.Error("expected at least 1 symbol from JavaScript code")
	}
}

// TestParseFileJavaScriptWithVariable verifies variable declarations in JS.
func TestParseFileJavaScriptWithVariable(t *testing.T) {
	code := "var config = { debug: true };\n"

	result, err := ast.ParseFile("config.js", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile JS var: %v", err)
	}
	defer result.Release()

	if result.Language != "javascript" {
		t.Errorf("Language = %q, want %q", result.Language, "javascript")
	}

	found := false
	for _, s := range result.Symbols {
		if s.Name == "config" && s.Kind == "variable" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected symbol config (variable), got: %v", symbolNames(result.Symbols))
	}
}

// ─── ParseFile: Python ──────────────────────────────────────────

// TestParseFilePythonCode verifies Python parsing with function and class.
func TestParseFilePythonCode(t *testing.T) {
	code := "def hello():\n    pass\n\nclass Foo:\n    pass\n"

	result, err := ast.ParseFile("test.py", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Python: %v", err)
	}
	defer result.Release()

	if result.Language != "python" {
		t.Errorf("Language = %q, want %q", result.Language, "python")
	}

	if len(result.Symbols) < 2 {
		t.Errorf("expected at least 2 symbols, got %d", len(result.Symbols))
		return
	}

	kinds := make(map[string]string)
	for _, s := range result.Symbols {
		kinds[s.Name] = s.Kind
	}

	if k, ok := kinds["hello"]; !ok {
		t.Error("missing symbol hello")
	} else if k != "function" {
		t.Errorf("hello kind = %q, want %q", k, "function")
	}

	if k, ok := kinds["Foo"]; !ok {
		t.Error("missing symbol Foo")
	} else if k != "class" {
		t.Errorf("Foo kind = %q, want %q", k, "class")
	}
}

// TestParseFilePythonCodeWithAsync verifies async function detection.
func TestParseFilePythonCodeWithAsync(t *testing.T) {
	code := "async def fetch():\n    pass\n"

	result, err := ast.ParseFile("async.py", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Python async: %v", err)
	}
	defer result.Release()

	found := false
	for _, s := range result.Symbols {
		if s.Name == "fetch" && s.Kind == "function" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected symbol fetch (function), got: %v", symbolNames(result.Symbols))
	}
}

// TestParseFilePythonCodeWithDecorator verifies decorated definitions.
func TestParseFilePythonCodeWithDecorator(t *testing.T) {
	code := "@app.route('/')\ndef index():\n    return 'OK'\n"

	result, err := ast.ParseFile("route.py", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Python decorator: %v", err)
	}
	defer result.Release()

	found := false
	for _, s := range result.Symbols {
		if s.Name == "index" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected symbol index, got: %v", symbolNames(result.Symbols))
	}
}

// ─── ParseFile: Error Cases ─────────────────────────────────────

// TestParseFileInvalidLanguage verifies that unsupported file types
// return an error rather than panicking.
func TestParseFileInvalidLanguage(t *testing.T) {
	_, err := ast.ParseFile("test.xyz", []byte("invalid content"))
	if err == nil {
		t.Fatal("expected error for unsupported file type, got nil")
	}
}

// TestParseFileEmptyPath verifies that passing an empty file path
// returns an error.
func TestParseFileEmptyPath(t *testing.T) {
	_, err := ast.ParseFile("", []byte("package main"))
	if err == nil {
		t.Fatal("expected error for empty file path, got nil")
	}
}

// ─── ExtractSymbols (Scoped) ────────────────────────────────────

// TestExtractSymbolsGoCode verifies that scoped symbol extraction works
// for Go code with structs, fields, and methods.
func TestExtractSymbolsGoCode(t *testing.T) {
	code := `package main

type Service struct {
	Name string
}

func (s *Service) DoThing() {
	x := 1
	_ = x
}
`
	result, err := ast.ParseFile("service.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile for scoped extraction: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)
	if len(scoped) == 0 {
		t.Fatal("expected at least one scoped symbol")
	}

	// Verify the struct "Service" at depth 0.
	if !hasSymbol(scoped, "Service", "class", "", 0) {
		t.Errorf("missing symbol Service(class, scope=\"\", depth=0), got: %v", scopedSummary(scoped))
	}

	// Verify the method "DoThing" scoped under Service at depth 1.
	if !hasSymbol(scoped, "DoThing", "method", "Service", 1) {
		t.Errorf("missing symbol DoThing(method, scope=Service, depth=1), got: %v", scopedSummary(scoped))
	}

	// Verify the field "Name" scoped under Service at depth 1.
	if !hasSymbol(scoped, "Name", "property", "Service", 1) {
		t.Errorf("missing symbol Name(property, scope=Service, depth=1), got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsGoInterface verifies interface method extraction.
func TestExtractSymbolsGoInterface(t *testing.T) {
	code := `package main

type Reader interface {
	Read(p []byte) (n int, err error)
	Close() error
}
`
	result, err := ast.ParseFile("reader.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Go interface: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// Interface at depth 0.
	if !hasSymbol(scoped, "Reader", "interface", "", 0) {
		t.Errorf("missing symbol Reader(interface, scope=\"\", depth=0), got: %v", scopedSummary(scoped))
	}

	// Methods at depth 1 under scope "Reader".
	if !hasSymbol(scoped, "Read", "method", "Reader", 1) {
		t.Errorf("missing symbol Read(method, scope=Reader, depth=1), got: %v", scopedSummary(scoped))
	}
	if !hasSymbol(scoped, "Close", "method", "Reader", 1) {
		t.Errorf("missing symbol Close(method, scope=Reader, depth=1), got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsGoConstAndVar verifies package-level const/variable extraction.
func TestExtractSymbolsGoConstAndVar(t *testing.T) {
	code := `package main

const MaxSize = 100
var GlobalCount int
`
	result, err := ast.ParseFile("vars.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Go vars: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	if !hasSymbol(scoped, "MaxSize", "constant", "", 0) {
		t.Errorf("missing symbol MaxSize(constant), got: %v", scopedSummary(scoped))
	}
	if !hasSymbol(scoped, "GlobalCount", "variable", "", 0) {
		t.Errorf("missing symbol GlobalCount(variable), got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsReturnsScopeAndDepth verifies that depth and scope
// values are non-trivial for nested code.
func TestExtractSymbolsReturnsScopeAndDepth(t *testing.T) {
	code := `package main

func outer() {
	inner := func() {
	}
}
`
	result, err := ast.ParseFile("test.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile for depth check: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// All symbols should have depth >= 0.
	for _, s := range scoped {
		if s.Depth < 0 {
			t.Errorf("symbol %q has negative depth %d", s.Name, s.Depth)
		}
	}

	// At least one top-level symbol (depth 0, empty scope).
	topLevel := false
	for _, s := range scoped {
		if s.Depth == 0 && s.Scope == "" {
			topLevel = true
			break
		}
	}
	if !topLevel {
		t.Error("expected at least one top-level symbol (depth=0, scope=\"\"), got: " + scopedSummary(scoped))
	}
}

// TestExtractSymbolsTypeScriptClass verifies scoped extraction for
// TypeScript classes with methods and properties.
func TestExtractSymbolsTypeScriptClass(t *testing.T) {
	code := `class Calculator {
	total: number;
	constructor() {
		this.total = 0;
	}
	add(n: number): number {
		return this.total += n;
	}
}
`
	result, err := ast.ParseFile("calc.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS class: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// Class at depth 0.
	if !hasSymbol(scoped, "Calculator", "class", "", 0) {
		t.Errorf("missing Calculator(class, depth=0), got: %v", scopedSummary(scoped))
	}

	// Members scoped under "Calculator" at depth 1.
	scopedMembers := 0
	for _, s := range scoped {
		if s.Scope == "Calculator" && s.Depth == 1 {
			scopedMembers++
		}
	}
	if scopedMembers == 0 {
		t.Error("expected at least one member scoped under Calculator at depth 1, got: " + scopedSummary(scoped))
	}
}

// TestExtractSymbolsTypeScriptInterface verifies interface member extraction.
func TestExtractSymbolsTypeScriptInterface(t *testing.T) {
	code := `interface Config {
	host: string;
	port: number;
	connect(): void;
}
`
	result, err := ast.ParseFile("config.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS interface: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	if !hasSymbol(scoped, "Config", "interface", "", 0) {
		t.Errorf("missing Config(interface), got: %v", scopedSummary(scoped))
	}

	// host and port should be properties under "Config".
	if !hasSymbol(scoped, "host", "property", "Config", 1) {
		t.Errorf("missing host(property, scope=Config), got: %v", scopedSummary(scoped))
	}
	if !hasSymbol(scoped, "port", "property", "Config", 1) {
		t.Errorf("missing port(property, scope=Config), got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsTypeScriptEnum verifies that enum declarations are
// extracted at the top level. (Nested enum member extraction depends on
// the tree-sitter grammar version producing enum_body/enum_assignment
// nodes, which varies across grammars, so we only assert the enum itself.)
func TestExtractSymbolsTypeScriptEnum(t *testing.T) {
	code := `enum Status {
	Pending,
	Active,
	Done
}
`
	result, err := ast.ParseFile("status.ts", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile TS enum: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// The enum itself should always be extracted at depth 0.
	if !hasSymbol(scoped, "Status", "enum", "", 0) {
		t.Errorf("missing Status(enum), got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsPythonClass verifies scoped extraction for Python
// classes with methods and class attributes.
func TestExtractSymbolsPythonClass(t *testing.T) {
	code := `class Greeter:
	name: str = "world"
	def __init__(self):
		pass
	def greet(self):
		pass
`
	result, err := ast.ParseFile("greet.py", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Python class: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// Class at depth 0.
	if !hasSymbol(scoped, "Greeter", "class", "", 0) {
		t.Errorf("missing Greeter(class), got: %v", scopedSummary(scoped))
	}

	// Methods scoped under "Greeter" at depth 1.
	methodCount := 0
	for _, s := range scoped {
		if s.Scope == "Greeter" && s.Kind == "method" && s.Depth == 1 {
			methodCount++
		}
	}
	if methodCount == 0 {
		t.Error("expected at least one method scoped under Greeter at depth 1, got: " + scopedSummary(scoped))
	}
}

// TestExtractSymbolsPythonDecoratedClass verifies decorator handling on classes.
func TestExtractSymbolsPythonDecoratedClass(t *testing.T) {
	code := `@dataclass
class Point:
	x: int = 0
	y: int = 0
`
	result, err := ast.ParseFile("point.py", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile Python decorated class: %v", err)
	}
	defer result.Release()

	scoped := ast.ExtractSymbols(result.Root, result.Bound, result.Language)

	// Decorated class should still be extracted.
	if !hasSymbol(scoped, "Point", "class", "", 0) {
		t.Errorf("missing Point(class) from decorated definition, got: %v", scopedSummary(scoped))
	}
}

// TestExtractSymbolsNilInput verifies that nil root/bound does not panic.
func TestExtractSymbolsNilInput(t *testing.T) {
	result := ast.ExtractSymbols(nil, nil, "go")
	if result != nil {
		t.Errorf("expected nil for nil inputs, got %v", result)
	}
}

// TestExtractSymbolsMaxDepth verifies that maxDepth controls nesting.
//
// NOTE: Go methods are direct children of root in the AST, so they are
// extracted at depth 1 even when maxDepth=1 (they are "top-level" AST
// nodes). The maxDepth gate only prevents extracting NESTED members
// INSIDE types (e.g. struct fields, interface methods). We use a Go
// struct test to verify this.
func TestExtractSymbolsMaxDepth(t *testing.T) {
	code := `package main

type Config struct {
	Host string
	Port int
}
`
	result, err := ast.ParseFile("config.go", []byte(code))
	if err != nil {
		t.Fatalf("ParseFile for maxDepth: %v", err)
	}
	defer result.Release()

	// maxDepth=1: only the type itself (depth 0), no struct fields.
	shallow := ast.ExtractSymbolsWithMaxDepth(result.Root, result.Bound, result.Language, 1)
	for _, s := range shallow {
		if s.Depth > 0 {
			t.Errorf("maxDepth=1 produced symbol %q at depth %d", s.Name, s.Depth)
		}
	}
	// Should have exactly 1 symbol: the "Config" type at depth 0.
	if len(shallow) != 1 {
		t.Errorf("maxDepth=1 produced %d symbols, want 1: %v", len(shallow), scopedSummary(shallow))
	}

	// maxDepth=2: type + struct fields.
	deep := ast.ExtractSymbolsWithMaxDepth(result.Root, result.Bound, result.Language, 2)
	if len(deep) < len(shallow) {
		t.Errorf("maxDepth=2 (%d symbols) < maxDepth=1 (%d symbols)", len(deep), len(shallow))
	}
	// Should include the type plus the two fields.
	if len(deep) < 2 {
		t.Errorf("maxDepth=2 produced %d symbols, want at least 2: %v", len(deep), scopedSummary(deep))
	}

	// maxDepth=0: no symbols.
	empty := ast.ExtractSymbolsWithMaxDepth(result.Root, result.Bound, result.Language, 0)
	if len(empty) != 0 {
		t.Errorf("maxDepth=0 should return no symbols, got %d", len(empty))
	}
}

// ─── Registry ───────────────────────────────────────────────────

// TestASTJSFuncsRegistryKeys verifies that astJSFuncs() returns a map
// with exactly the expected keys. Requires a JS runtime (WASM test env).
func TestASTJSFuncsRegistryKeys(t *testing.T) {
	reg := astJSFuncs()

	expectedKeys := []string{"parseFile", "extractSymbols", "supportedLanguages"}

	if len(reg) != len(expectedKeys) {
		t.Errorf("registry has %d keys, want %d", len(reg), len(expectedKeys))
	}

	for _, key := range expectedKeys {
		if _, ok := reg[key]; !ok {
			t.Errorf("registry missing key %q", key)
		}
	}
}

// ─── Helpers ────────────────────────────────────────────────────

// hasSymbol checks whether the scoped symbol list contains a symbol
// matching all given criteria.
func hasSymbol(syms []ast.ScopedSymbol, name, kind, scope string, depth int) bool {
	for _, s := range syms {
		if s.Name == name && s.Kind == kind && s.Scope == scope && s.Depth == depth {
			return true
		}
	}
	return false
}

// symbolNames extracts just the name field from a symbol slice.
func symbolNames(syms []ast.Symbol) []string {
	out := make([]string, 0, len(syms))
	for _, s := range syms {
		out = append(out, s.Name)
	}
	return out
}

// scopedSummary returns a compact summary of all scoped symbols for error messages.
func scopedSummary(syms []ast.ScopedSymbol) string {
	parts := make([]string, 0, len(syms))
	for _, s := range syms {
		parts = append(parts, fmt.Sprintf("%s(%s,%s,%d)", s.Name, s.Kind, s.Scope, s.Depth))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
