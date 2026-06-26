package index

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestBuildSymbols_LineNumbers(t *testing.T) {
	dir := t.TempDir()

	src := `package main

const MagicNumber = 42

var Config = "test"

func Hello() {}

type Person struct{}

type Greeter interface {
	Greet() string
}

func (p Person) Greet() string {
	return "hi"
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}
	if idx == nil || len(idx.Files) == 0 {
		t.Fatal("expected symbols for main.go")
	}

	fs := idx.Files[0]
	if filepath.Base(fs.File) != "main.go" {
		t.Fatalf("expected main.go, got %s", fs.File)
	}

	// Build a map for easy lookup
	symbolMap := make(map[string]Symbol)
	for _, s := range fs.Symbols {
		symbolMap[s.Name] = s
	}

	// Verify line numbers
	tests := []struct {
		name     string
		expected int // 1-based line number
	}{
		{"MagicNumber", 3},
		{"Config", 5},
		{"Hello", 7},
		{"Person", 9},
		{"Greeter", 11},
		{"Greet", 15},
	}

	for _, tt := range tests {
		s, ok := symbolMap[tt.name]
		if !ok {
			t.Errorf("expected symbol %q, not found", tt.name)
			continue
		}
		if s.Line != tt.expected {
			t.Errorf("symbol %q: expected line %d, got %d", tt.name, tt.expected, s.Line)
		}
	}
}

func TestBuildSymbols_GoInterfacesConstantsVariables(t *testing.T) {
	dir := t.TempDir()

	src := `package main

const (
	Pi         = 3.14
	AppName    = "sprout"
)

var GlobalVar = 42

type MyInterface interface {
	Method1()
}

func (s *Service) Method1() {}

type MyType struct{}

const SingleConst = 100
var SingleVar = "test"
`
	if err := os.WriteFile(filepath.Join(dir, "types.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var found struct {
		constant   bool
		variable   bool
		interface_ bool
		func_      bool
		type_      bool
	}
	for _, fs := range idx.Files {
		for _, s := range fs.Symbols {
			switch s.Name {
			case "Pi", "AppName", "SingleConst":
				found.constant = true
			case "GlobalVar", "SingleVar":
				found.variable = true
			case "MyInterface":
				found.interface_ = true
			case "Method1":
				found.func_ = true
			case "MyType":
				found.type_ = true
			}
		}
	}

	if !found.constant {
		t.Error("expected to find constants (Pi, AppName, SingleConst)")
	}
	if !found.variable {
		t.Error("expected to find variables (GlobalVar, SingleVar)")
	}
	if !found.interface_ {
		t.Error("expected to find interface MyInterface")
	}
	if !found.func_ {
		t.Error("expected to find method Method1")
	}
	if !found.type_ {
		t.Error("expected to find type MyType")
	}
}

func TestBuildSymbols_Python(t *testing.T) {
	dir := t.TempDir()

	src := `"""Module docstring."""

MY_CONSTANT = 42
another_var = "hello"

def my_function():
    pass

class MyClass:
    pass
`
	if err := os.WriteFile(filepath.Join(dir, "module.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var foundFunc, foundClass, foundConst bool
	for _, fs := range idx.Files {
		for _, s := range fs.Symbols {
			switch s.Name {
			case "my_function":
				foundFunc = true
			case "MyClass":
				foundClass = true
			case "MY_CONSTANT":
				foundConst = true
			}
		}
	}

	if !foundFunc {
		t.Error("expected to find func my_function")
	}
	if !foundClass {
		t.Error("expected to find class MyClass")
	}
	if !foundConst {
		t.Error("expected to find constant MY_CONSTANT")
	}
}

func TestBuildSymbols_TSJS(t *testing.T) {
	dir := t.TempDir()

	src := `const MY_EXPORT = 42;
let counter = 0;

interface UserProps {
    name: string;
}

type Mode = 'dev' | 'prod';

class ApiClient {
    fetch() {}
}

function processData() {}

export const helper = () => {};
`
	if err := os.WriteFile(filepath.Join(dir, "main.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var found struct {
		constant   bool
		variable   bool
		interface_ bool
		type_      bool
		class      bool
		func_      bool
	}
	for _, fs := range idx.Files {
		for _, s := range fs.Symbols {
			switch s.Name {
			case "MY_EXPORT":
				found.constant = true
			case "counter":
				found.variable = true
			case "UserProps":
				found.interface_ = true
			case "Mode":
				found.type_ = true
			case "ApiClient":
				found.class = true
			case "processData", "helper":
				found.func_ = true
			}
		}
	}

	if !found.constant {
		t.Error("expected to find const MY_EXPORT")
	}
	if !found.variable {
		t.Error("expected to find let counter")
	}
	if !found.interface_ {
		t.Error("expected to find interface UserProps")
	}
	if !found.type_ {
		t.Error("expected to find type Mode")
	}
	if !found.class {
		t.Error("expected to find class ApiClient")
	}
	if !found.func_ {
		t.Error("expected to find functions")
	}
}

func TestBuildSymbols_TsxJsx(t *testing.T) {
	dir := t.TempDir()

	// Test .tsx file
	tsxSrc := `interface Props {
    title: string;
}

export function Component() {
    return <div>Hello</div>;
}
`
	if err := os.WriteFile(filepath.Join(dir, "component.tsx"), []byte(tsxSrc), 0644); err != nil {
		t.Fatal(err)
	}

	// Test .jsx file
	jsxSrc := `function LegacyComponent() {
    return <span>Old</span>;
}

export default LegacyComponent;
`
	if err := os.WriteFile(filepath.Join(dir, "legacy.jsx"), []byte(jsxSrc), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var files []string
	for _, fs := range idx.Files {
		files = append(files, filepath.Base(fs.File))
	}
	if len(files) != 2 {
		t.Fatalf("expected 2 files (tsx and jsx), got %d: %v", len(files), files)
	}
}

func TestBuildSymbols_TsxJsxLineNumbers(t *testing.T) {
	dir := t.TempDir()

	src := `interface Props {
    title: string;
}

export function Component() {
    return <div>Hello</div>;
}

const VERSION = "1.0";
`
	if err := os.WriteFile(filepath.Join(dir, "comp.tsx"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var componentLine, versionLine int
	for _, fs := range idx.Files {
		for _, s := range fs.Symbols {
			switch s.Name {
			case "Component":
				componentLine = s.Line
			case "VERSION":
				versionLine = s.Line
			}
		}
	}

	if componentLine != 5 {
		t.Errorf("Component expected line 5, got %d", componentLine)
	}
	if versionLine != 9 {
		t.Errorf("VERSION expected line 9, got %d", versionLine)
	}
}

func TestLoadSymbols_Cache(t *testing.T) {
	dir := t.TempDir()

	// Create a source file
	src := `package main
func Hello() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// Build symbols to create cache
	BuildSymbols(dir)

	// Load from cache
	cached, err := LoadSymbols(dir)
	if err != nil {
		t.Fatalf("LoadSymbols: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached index")
	}
	if len(cached.Files) != 1 {
		t.Errorf("expected 1 file, got %d", len(cached.Files))
	}
}

func TestLoadSymbols_NoCache(t *testing.T) {
	dir := t.TempDir()

	// Load from non-existent cache should return nil, nil
	cached, err := LoadSymbols(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cached != nil {
		t.Error("expected nil index for non-existent cache")
	}
}

func TestLoadSymbols_ValidatesFiles(t *testing.T) {
	dir := t.TempDir()

	// Create a source file
	src := `package main
func Hello() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	// Build symbols
	BuildSymbols(dir)

	// Delete the file to simulate it being removed
	if err := os.Remove(filepath.Join(dir, "main.go")); err != nil {
		t.Fatal(err)
	}

	// Load should filter out the missing file
	cached, err := LoadSymbols(dir)
	if err != nil {
		t.Fatalf("LoadSymbols: %v", err)
	}
	if cached == nil {
		t.Fatal("expected cached index")
	}
	if len(cached.Files) != 0 {
		t.Errorf("expected 0 files after deletion, got %d", len(cached.Files))
	}
}

func TestBuildSymbols_SortedResults(t *testing.T) {
	dir := t.TempDir()

	// Create files in non-alphabetical order
	files := map[string]string{
		"zebra.go": `package main
func Zebra() {}
`,
		"apple.go": `package main
func Apple() {}
`,
		"beta.go": `package main
func Beta() {}
`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	// Files should be sorted
	var fileNames []string
	for _, fs := range idx.Files {
		fileNames = append(fileNames, filepath.Base(fs.File))
	}
	if !sort.IsSorted(stringsSlice(fileNames)) {
		t.Errorf("files not sorted: %v", fileNames)
	}
}

func TestBuildSymbols_SymbolsSortedByLine(t *testing.T) {
	dir := t.TempDir()

	src := `package main

func Third() {}
func First() {}
func Second() {}
`
	if err := os.WriteFile(filepath.Join(dir, "sorted.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	if len(idx.Files) != 1 {
		t.Fatalf("expected 1 file, got %d", len(idx.Files))
	}

	symbols := idx.Files[0].Symbols
	for i := 1; i < len(symbols); i++ {
		if symbols[i].Line <= symbols[i-1].Line {
			t.Errorf("symbols not sorted by line: %v", symbols)
			break
		}
	}
}

func TestSearchSymbolFiles(t *testing.T) {
	dir := t.TempDir()

	src := `package main

func Hello() {}
func Helper() {}

const Magic = 42

type Person struct{}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	// Search for "hello" - should match Hello function
	hits := SearchSymbolFiles(idx, []string{"hello"})
	if len(hits) != 1 {
		t.Fatalf("expected 1 file match, got %d", len(hits))
	}
	if len(hits[0].Symbols) != 1 {
		t.Fatalf("expected 1 symbol in Hello match, got %d", len(hits[0].Symbols))
	}
	if hits[0].Symbols[0].Name != "Hello" {
		t.Errorf("expected Hello, got %s", hits[0].Symbols[0].Name)
	}

	// Search for "per" - should match Person type and also variable 'per' if present
	hits = SearchSymbolFiles(idx, []string{"per"})
	if len(hits) != 1 {
		t.Fatalf("expected 1 file match for 'per', got %d", len(hits))
	}
	if len(hits[0].Symbols) < 1 {
		t.Fatalf("expected at least 1 symbol in match, got %d", len(hits[0].Symbols))
	}

	// Search for "xxx" - should match nothing
	hits = SearchSymbolFiles(idx, []string{"xxx"})
	if len(hits) != 0 {
		t.Fatalf("expected 0 matches for 'xxx', got %d", len(hits))
	}

	// Search for multiple tokens
	hits = SearchSymbolFiles(idx, []string{"hel", "mag"})
	if len(hits) != 1 {
		t.Fatalf("expected 1 file for multi-token, got %d", len(hits))
	}
	// Should have Hello and Magic (and possibly Person, Helper)
	symbols := hits[0].Symbols
	if len(symbols) < 2 {
		t.Errorf("expected at least 2 symbols for multi-token, got %d", len(symbols))
	}
}

func TestSearchSymbolFiles_ReturnsFilteredSymbols(t *testing.T) {
	dir := t.TempDir()

	src := `package main

func HelloWorld() {}
func Helper() {}

const Magic = 42
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	// Search for "hel" - should match functions containing "hel"
	hits := SearchSymbolFiles(idx, []string{"hel"})

	if len(hits) != 1 {
		t.Fatalf("expected 1 file match, got %d", len(hits))
	}

	symbolNames := make([]string, 0)
	for _, s := range hits[0].Symbols {
		symbolNames = append(symbolNames, s.Name)
	}

	// Should have HelloWorld and Helper (both contain "hel")
	if len(symbolNames) != 2 {
		t.Errorf("expected 2 symbols containing 'hel', got %v", symbolNames)
	}

	// Magic should NOT be included (no "hel")
	for _, name := range symbolNames {
		if name == "Magic" {
			t.Error("Magic should not be in results for 'hel' search")
		}
	}
}

func TestSearchSymbolFiles_MinTokenLength(t *testing.T) {
	dir := t.TempDir()

	src := `package main
func Hello() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	// Tokens < 3 chars should be ignored
	hits := SearchSymbolFiles(idx, []string{"he"}) // "he" is only 2 chars
	if len(hits) != 0 {
		t.Errorf("expected 0 matches for 2-char token, got %d", len(hits))
	}

	hits = SearchSymbolFiles(idx, []string{"hel"}) // "hel" is 3 chars
	if len(hits) != 1 {
		t.Errorf("expected 1 match for 3-char token, got %d", len(hits))
	}
}

func TestBuildSymbols_Rust(t *testing.T) {
	dir := t.TempDir()

	src := `struct User {
    name: String,
}

enum Color {
    Red,
    Green,
    Blue,
}

trait Greetable {
    fn greet(&self);
}

impl User {
    fn new(name: String) -> Self {
        User { name }
    }
}

impl Greetable for User {
    fn greet(&self) {
        println!("Hello, {}!", self.name);
    }
}

const APP_NAME: &str = "sprout";

fn main() {
    println!("Hello");
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	var found struct {
		struct_  bool
		enum     bool
		trait    bool
		impl     bool
		constant bool
		func_    bool
	}
	for _, fs := range idx.Files {
		for _, s := range fs.Symbols {
			switch s.Name {
			case "User":
				found.struct_ = true
				found.impl = true
			case "Color":
				found.enum = true
			case "Greetable":
				found.trait = true
			case "APP_NAME":
				found.constant = true
			case "main":
				found.func_ = true
			}
		}
	}

	if !found.struct_ {
		t.Error("expected to find struct User")
	}
	if !found.enum {
		t.Error("expected to find enum Color")
	}
	if !found.trait {
		t.Error("expected to find trait Greetable")
	}
	if !found.impl {
		t.Error("expected to find impl blocks")
	}
	if !found.constant {
		t.Error("expected to find const APP_NAME")
	}
	if !found.func_ {
		t.Error("expected to find fn main")
	}
}

func TestBuildAndSearchSymbols_GoFile(t *testing.T) {
	orig, _ := os.Getwd()
	dir := t.TempDir()
	defer os.Chdir(orig)
	_ = os.Chdir(dir)

	src := "package m\n\nfunc Hello() {}\n\ntype Person struct{}\n"
	if err := os.WriteFile(filepath.Join(dir, "m.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}
	if idx == nil || len(idx.Files) == 0 {
		t.Fatalf("expected symbols for m.go")
	}
	// Ensure persisted file exists
	if _, err := os.Stat(filepath.Join(dir, ".sprout", "symbols.json")); err != nil {
		t.Fatalf("symbols.json missing: %v", err)
	}

	// Test SearchSymbolFiles (renamed from SearchSymbols)
	hits := SearchSymbolFiles(idx, []string{"hello", "person"})
	if len(hits) == 0 {
		t.Fatalf("expected search hits for tokens")
	}
}

func TestBuildSymbols_JsonOutput(t *testing.T) {
	dir := t.TempDir()

	src := `package main
func Hello() {}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	idx, err := BuildSymbols(dir)
	if err != nil {
		t.Fatalf("BuildSymbols: %v", err)
	}

	// Verify JSON serialization includes line field
	data, err := json.Marshal(idx)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	// Check that line is present in output
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}

	files, ok := raw["files"].([]interface{})
	if !ok || len(files) == 0 {
		t.Fatal("expected files in output")
	}

	firstFile := files[0].(map[string]interface{})
	symbols, ok := firstFile["symbols"].([]interface{})
	if !ok || len(symbols) == 0 {
		t.Fatal("expected symbols in output")
	}

	firstSymbol := symbols[0].(map[string]interface{})
	if _, ok := firstSymbol["line"]; !ok {
		t.Error("expected 'line' field in symbol JSON output")
	}
}

// Helper for sorting strings
type stringsSlice []string

func (s stringsSlice) Len() int           { return len(s) }
func (s stringsSlice) Less(i, j int) bool { return s[i] < s[j] }
func (s stringsSlice) Swap(i, j int)      { s[i], s[j] = s[j], s[i] }
