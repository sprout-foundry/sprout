package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func createTestFiles(t *testing.T, dir string, files map[string]string) {
	t.Helper()
	for name, content := range files {
		path := filepath.Join(dir, name)
		requireErr(t, os.MkdirAll(filepath.Dir(path), 0o755), "create dir for %s", name)
		requireErr(t, os.WriteFile(path, []byte(content), 0o644), "write %s", name)
	}
}

func requireErr(t *testing.T, err error, format string, args ...interface{}) {
	t.Helper()
	if err != nil {
		t.Fatalf(format+": %v", append([]interface{}{err}, args...)...)
	}
}

// TestGenerateRepoMapBasic verifies symbol extraction from a Go file using AST.
func TestGenerateRepoMapBasic(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go": `package main

type User struct { Name string }
type Handler interface { Handle() }
func NewUser(name string) *User { return nil }
var DefaultTimeout = 30
const MaxRetries = 3
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	// AST extracts: types and functions (not var/const).
	for _, want := range []string{
		"## repo_map:",
		"### main.go",
		"- func NewUser:5",
		"- type User:3",
		"- type Handler:4",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output:\n%s", want, result)
		}
	}

	// AST does NOT extract var/const declarations.
	for _, dontWant := range []string{"DefaultTimeout", "MaxRetries"} {
		if strings.Contains(result, dontWant) {
			t.Errorf("unexpected %q in output (AST doesn't extract var/const):\n%s", dontWant, result)
		}
	}
}

// TestGenerateRepoMapMultipleLanguages verifies symbol extraction across Go, TypeScript, and Python.
func TestGenerateRepoMapMultipleLanguages(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"server.go": `package main

func StartServer() error { return nil }
type Config struct { Port int }
`,
		"src/app.ts": `export class App { start() {} }
export interface User { name: string; }
export type Role = "admin" | "user";
export async function fetchData() { return {}; }
export const VERSION = "1.0";
`,
		"src/utils.py": `def calculate_total(items): return sum(items)
class DataProcessor: pass
async def fetch_data(url): pass
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	// Go: AST extraction (types without "struct"/"interface" suffix)
	for _, want := range []string{
		"- func StartServer:3",
		"- type Config:4",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output:\n%s", want, result)
		}
	}

	// TypeScript: tree-sitter extraction (with regex fallback)
	for _, want := range []string{
		"- class App:1",
		"- interface User:2",
		"- type Role:3",
		"- function fetchData:4",
		"- const VERSION:5",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output:\n%s", want, result)
		}
	}

	// Python: tree-sitter extraction (with regex fallback)
	for _, want := range []string{
		"- def calculate_total:1",
		"- class DataProcessor:2",
		"- def fetch_data:3",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output:\n%s", want, result)
		}
	}
}

// TestGenerateRepoMapEmptyDirectory verifies that an empty directory returns a header.
func TestGenerateRepoMapEmptyDirectory(t *testing.T) {
	dir := t.TempDir()
	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if !strings.Contains(result, "## repo_map:") {
		t.Error("missing repo_map header")
	}
	if !strings.Contains(result, "No source files") {
		t.Error("expected 'No source files' message")
	}
}

// TestGenerateRepoMapBinaryFileSkipped verifies binary files are not processed.
func TestGenerateRepoMapBinaryFileSkipped(t *testing.T) {
	dir := t.TempDir()
	requireErr(t, os.WriteFile(filepath.Join(dir, "data.go"), []byte{0x00, 0x01, 0xFF}, 0o644), "write binary")
	createTestFiles(t, dir, map[string]string{"main.go": `package main

func Hello() {}
`})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if !strings.Contains(result, "- func Hello:3") {
		t.Errorf("missing func Hello with line number:\n%s", result)
	}
	if strings.Contains(result, "### data.go") {
		t.Error("binary file should not appear")
	}
}

// TestGenerateRepoMapTokenLimit verifies output is truncated when token budget is exceeded.
func TestGenerateRepoMapTokenLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 200; i++ {
		content := fmt.Sprintf(`package main

func ProcessDataItem%d(ctx context.Context, input []byte) error { return nil }
func ValidateRequest%d(req *http.Request) bool { return true }
`, i, i)
		path := filepath.Join(dir, fmt.Sprintf("module/file_%03d.go", i))
		requireErr(t, os.MkdirAll(filepath.Dir(path), 0o755), "create dir")
		requireErr(t, os.WriteFile(path, []byte(content), 0o644), "write file")
	}

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if !strings.Contains(result, "truncated") {
		t.Errorf("expected truncation notice. Got %d chars", len(result))
	}
}

// TestGenerateRepoMapIgnoredDirectories verifies that ignored dirs are skipped.
func TestGenerateRepoMapIgnoredDirectories(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"node_modules", ".git", "vendor", "dist", "build"} {
		subdir := filepath.Join(dir, d)
		requireErr(t, os.MkdirAll(subdir, 0o755), "create dir %s", d)
		requireErr(t, os.WriteFile(filepath.Join(subdir, "hidden.go"),
			[]byte(`package hidden

func HiddenFunc() {}
`), 0o644), "write hidden file")
	}
	createTestFiles(t, dir, map[string]string{"main.go": `package main

func MainFunc() {}
`})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if !strings.Contains(result, "- func MainFunc:3") {
		t.Errorf("missing func MainFunc:\n%s", result)
	}
	if strings.Contains(result, "func HiddenFunc") {
		t.Error("hidden files should not appear")
	}
}

// TestGenerateRepoMapFileLimit verifies that only 200 files are included.
func TestGenerateRepoMapFileLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 250; i++ {
		content := fmt.Sprintf("package main\n\nfunc Func%d() {}\n", i)
		requireErr(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%03d.go", i)), []byte(content), 0o644), "write")
	}
	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if strings.Count(result, "### file_") > repoMapMaxFiles {
		t.Errorf("expected at most %d file sections", repoMapMaxFiles)
	}
}

// TestGenerateRepoMapNonSourceFiles verifies non-source files are skipped.
func TestGenerateRepoMapNonSourceFiles(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go":     "package main\n\nfunc Hello() {}",
		"README.md":   "# My Project",
		"config.json": `{"key": "value"}`,
		"style.css":   `body { margin: 0; }`,
	})
	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if !strings.Contains(result, "### main.go") {
		t.Error("missing main.go")
	}
	if strings.Contains(result, "### README.md") || strings.Contains(result, "### config.json") {
		t.Error("non-source files should not appear")
	}
}

// TestGenerateRepoMapContextCancellation verifies context cancellation is respected.
func TestGenerateRepoMapContextCancellation(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 500; i++ {
		content := fmt.Sprintf("package main\n\nfunc Func%d() {}\n", i)
		requireErr(t, os.WriteFile(filepath.Join(dir, fmt.Sprintf("file_%03d.go", i)), []byte(content), 0o644), "write")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := GenerateRepoMap(ctx, dir)
	if err != nil && !errors.Is(err, context.Canceled) {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestGenerateRepoMapDeduplication verifies duplicate symbols are not repeated.
// The test file has duplicate function names which is invalid Go — AST parsing
// will fail and fall back to regex. Regex + dedup should still produce unique entries.
func TestGenerateRepoMapDeduplication(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"dups.go": `package main
func Hello() {}
func Hello() {}
func World() {}
func World() {}
`,
	})
	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")
	if strings.Count(result, "- func Hello:2") != 1 {
		t.Errorf("expected 1 'func Hello:2', got %d:\n%s", strings.Count(result, "- func Hello:2"), result)
	}
	if strings.Count(result, "- func World:4") != 1 {
		t.Errorf("expected 1 'func World:4', got %d:\n%s", strings.Count(result, "- func World:4"), result)
	}
}

// TestGenerateRepoMapLineNumbers verifies that symbols include correct 1-based line numbers.
func TestGenerateRepoMapLineNumbers(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"example.go": `package example

// Some comment
type Config struct {
	Port int
}

func NewConfig() *Config { return nil }
func (c *Config) Start() {}
`,
		"app.ts": `// Header comment
export class App {
  start() {}
}

export interface User { name: string; }
export async function initApp() { return {}; }
`,
		"lib.py": `# Library module
import os

class Parser:
    def parse(self): pass

def load_data(path):
    return open(path).read()

async def fetch(url): pass
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	// Go: AST extraction — Config on line 4, NewConfig on line 8, (*Config).Start on line 9
	if !strings.Contains(result, "- type Config:4") {
		t.Errorf("missing 'type Config:4', got:\n%s", result)
	}
	if !strings.Contains(result, "- func NewConfig:8") {
		t.Errorf("missing 'func NewConfig:8', got:\n%s", result)
	}
	if !strings.Contains(result, "- func (*Config).Start:9") {
		t.Errorf("missing 'func (*Config).Start:9', got:\n%s", result)
	}

	// TS: tree-sitter extraction (with regex fallback) — App on line 2, User on line 6, initApp on line 7
	if !strings.Contains(result, "- class App:2") {
		t.Errorf("missing 'class App:2', got:\n%s", result)
	}
	if !strings.Contains(result, "- interface User:6") {
		t.Errorf("missing 'interface User:6', got:\n%s", result)
	}
	if !strings.Contains(result, "- function initApp:7") {
		t.Errorf("missing 'function initApp:7', got:\n%s", result)
	}

	// Python: tree-sitter extraction (with regex fallback) — Parser on line 4, load_data on line 7, fetch on line 10
	if !strings.Contains(result, "- class Parser:4") {
		t.Errorf("missing 'class Parser:4', got:\n%s", result)
	}
	if !strings.Contains(result, "- def load_data:7") {
		t.Errorf("missing 'def load_data:7', got:\n%s", result)
	}
	if !strings.Contains(result, "- def fetch:10") {
		t.Errorf("missing 'def fetch:10', got:\n%s", result)
	}
}

// TestGenerateRepoMapGoMethodReceivers verifies that Go method receivers are
// correctly included in the symbol name via AST extraction.
func TestGenerateRepoMapGoMethodReceivers(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"handler.go": `package main

type Handler struct {
	Name string
}

func (h *Handler) ServeHTTP(w interface{}, r interface{}) {}
func (h Handler) String() string { return h.Name }
func NewHandler(name string) *Handler { return nil }
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	if !strings.Contains(result, "- func (*Handler).ServeHTTP:7") {
		t.Errorf("missing 'func (*Handler).ServeHTTP:7', got:\n%s", result)
	}
	if !strings.Contains(result, "- func (Handler).String:8") {
		t.Errorf("missing 'func (Handler).String:8', got:\n%s", result)
	}
	if !strings.Contains(result, "- func NewHandler:9") {
		t.Errorf("missing 'func NewHandler:9', got:\n%s", result)
	}
}

// TestGenerateRepoMapGoTestExclusion verifies that test functions and _test.go
// files are excluded from the repo map.
func TestGenerateRepoMapGoTestExclusion(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"app.go": `package main

func PublicAPI() {}
func TestSomething(t interface{}) {}
func BenchmarkSomething(b interface{}) {}
`,
		"app_test.go": `package main

func TestRealTest(t interface{}) {}
func HelperForTest() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	// PublicAPI should appear.
	if !strings.Contains(result, "- func PublicAPI:3") {
		t.Errorf("missing 'func PublicAPI:3', got:\n%s", result)
	}

	// Test/Benchmark functions should be excluded from app.go.
	if strings.Contains(result, "TestSomething") || strings.Contains(result, "BenchmarkSomething") {
		t.Errorf("test/benchmark functions should be excluded:\n%s", result)
	}

	// _test.go file should be excluded entirely.
	if strings.Contains(result, "### app_test.go") || strings.Contains(result, "TestRealTest") || strings.Contains(result, "HelperForTest") {
		t.Errorf("_test.go file should be excluded:\n%s", result)
	}
}

// TestGenerateRepoMapGoFullFileRead verifies that large Go files are
// read completely for AST parsing within the size limit.
func TestGenerateRepoMapGoFullFileRead(t *testing.T) {
	dir := t.TempDir()

	// Build a large Go file (~1MB) with a function near the end.
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	// Pad with comments to create a ~1MB file (well within the 2MB limit).
	for i := 0; i < 17000; i++ {
		sb.WriteString(fmt.Sprintf("// padding comment line %d to make this file large enough\n", i))
	}
	sb.WriteString("func DeepFunction() string { return \"found\" }\n")
	content := sb.String()

	if len(content) < 500*1024 {
		t.Skipf("test content only %d bytes, need > 500KB", len(content))
	}
	if len(content) > repoMapMaxFullFileSize {
		t.Skipf("test content %d bytes exceeds repoMapMaxFullFileSize %d", len(content), repoMapMaxFullFileSize)
	}

	requireErr(t, os.WriteFile(filepath.Join(dir, "large.go"), []byte(content), 0o644), "write large file")

	result, err := GenerateRepoMap(context.Background(), dir)
	requireErr(t, err, "generate repo map")

	if !strings.Contains(result, "- func DeepFunction:") {
		t.Errorf("missing 'func DeepFunction' from large file (AST needs full file):\n%s", result)
	}
}
