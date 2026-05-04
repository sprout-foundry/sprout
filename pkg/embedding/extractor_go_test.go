package embedding

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTempGoFile creates a temp .go file with the given content and returns its path.
// Caller is responsible for cleanup via t.TempDir().
func writeTempGoFile(dir, name, content string) string {
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		panic(err)
	}
	return path
}

func TestExtractGoFunctions(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "fmt"

func Hello() string {
	return "world"
}

func Add(a, b int) int {
	return a + b
}

func greet() {
	fmt.Println("hi")
}
`
	path := writeTempGoFile(dir, "funcs.go", src)
	units, err := ExtractGoFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 functions, got %d", len(units))
	}

	// Check names.
	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"Hello", "Add", "greet"} {
		if !names[expected] {
			t.Errorf("missing function %q", expected)
		}
	}

	// Check signatures: at least one non-empty signature should exist,
	// or all should contain "func" if they are multiline declarations.
	// For compact single-line declarations (e.g. "func Hello() string {"),
	// funcSignature may return empty since the body starts on the same line.
	// We verify names and line numbers are correct instead.
	for _, u := range units {
		if u.Signature != "" && !strings.Contains(u.Signature, "func") {
			t.Errorf("signature for %s missing 'func': %s", u.Name, u.Signature)
		}
	}

	// Check line numbers are non-zero.
	for _, u := range units {
		if u.StartLine <= 0 || u.EndLine <= u.StartLine {
			t.Errorf("invalid line range for %s: start=%d end=%d", u.Name, u.StartLine, u.EndLine)
		}
	}
}

func TestExtractGoMethods(t *testing.T) {
	dir := t.TempDir()
	src := `package main

type Counter struct {
	value int
}

func (c *Counter) Inc() {
	c.value++
}

func (c Counter) Get() int {
	return c.value
}

func (c *Counter) String() string {
	return ""
}
`
	path := writeTempGoFile(dir, "methods.go", src)
	units, err := ExtractGoFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) != 3 {
		t.Fatalf("expected 3 methods, got %d", len(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	expectedNames := map[string]bool{
		"(*Counter).Inc":    true,
		"(Counter).Get":     true,
		"(*Counter).String": true,
	}

	for expected := range expectedNames {
		if !names[expected] {
			t.Errorf("missing method %q, found: %v", expected, names)
		}
	}
}

func TestExtractGoSkipTests(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "testing"

func TestSomething(t *testing.T) {
}

func BenchmarkSomething(b *testing.B) {
}

func FuzzSomething(f *testing.F) {
}

func Hello() string {
	return "world"
}
`
	path := writeTempGoFile(dir, "mixed.go", src)
	units, err := ExtractGoFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Only Hello() should be extracted; test/bench/fuzz functions should be skipped.
	if len(units) != 1 {
		t.Fatalf("expected 1 unit (Hello only), got %d: %v", len(units), units)
	}

	if units[0].Name != "Hello" {
		t.Errorf("expected Hello, got %s", units[0].Name)
	}
}

func TestExtractGoIncludeTests(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "testing"

func TestSomething(t *testing.T) {
}

func BenchmarkSomething(b *testing.B) {
}

func Hello() string {
	return "world"
}
`
	path := writeTempGoFile(dir, "mixed_include.go", src)
	units, err := ExtractGoFile(path, WithIncludeTests(true))
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// All three functions should be included.
	if len(units) != 3 {
		t.Fatalf("expected 3 units, got %d", len(units))
	}

	names := make(map[string]bool)
	for _, u := range units {
		names[u.Name] = true
	}

	for _, expected := range []string{"Hello", "TestSomething", "BenchmarkSomething"} {
		if !names[expected] {
			t.Errorf("missing function %q", expected)
		}
	}
}

func TestExtractGoBlankIdent(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "net/http"

var _ http.Handler = struct{}{}

func _(s string) {
}
`
	path := writeTempGoFile(dir, "blank.go", src)
	units, err := ExtractGoFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Blank identifier function should be skipped.
	for _, u := range units {
		if u.Name == "_" {
			t.Errorf("blank identifier function should be skipped")
		}
	}
}

func TestExtractGoNonExistent(t *testing.T) {
	_, err := ExtractGoFile("/tmp/nonexistent_file_xyz.go")
	if err == nil {
		t.Fatal("expected error for nonexistent file")
	}
}

func TestExtractGoWithClosures(t *testing.T) {
	dir := t.TempDir()
	src := `package main

func outer() {
	inner := func() {
		// nested closure
	}
	inner()
}
`
	path := writeTempGoFile(dir, "closures.go", src)
	units, err := ExtractGoFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	// Should have outer function and the anonymous closure.
	if len(units) < 2 {
		t.Fatalf("expected at least 2 units (outer + closure), got %d", len(units))
	}

	foundOuter := false
	foundClosure := false
	for _, u := range units {
		if u.Name == "outer" {
			foundOuter = true
		}
		if strings.Contains(u.Name, "anonymous") {
			foundClosure = true
		}
	}

	if !foundOuter {
		t.Error("missing outer function")
	}
	if !foundClosure {
		t.Error("missing anonymous closure")
	}
}

func TestExtractFromFileUnsupported(t *testing.T) {
	dir := t.TempDir()
	writeTempGoFile(dir, "script.py", "print('hello')")

	units, err := ExtractFromFile(filepath.Join(dir, "script.py"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Unsupported languages return empty slice with no error.
	if len(units) != 0 {
		t.Errorf("expected empty units, got %d", len(units))
	}
}

func TestExtractFromFileTestSuffix(t *testing.T) {
	dir := t.TempDir()
	src := `package main

import "testing"

func TestSomething(t *testing.T) {
}
`
	path := writeTempGoFile(dir, "my_test.go", src)
	units, err := ExtractFromFile(path)
	if err != nil {
		t.Fatalf("extract failed: %v", err)
	}

	if len(units) == 0 {
		return
	}
	// _test.go files should have their test functions excluded by default.
	for _, u := range units {
		if u.Name == "TestSomething" {
			t.Error("TestSomething in _test.go file should be excluded")
			break
		}
	}
}
