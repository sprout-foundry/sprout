package tools

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	codegraph "github.com/sprout-foundry/sprout/pkg/codegraph"
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	// AST extracts: types and functions (not var/const).
	for _, want := range []string{
		"## repo_map:",
		"### main.go",
		"- func NewUser:5",
		"- type User:3",
		"- iface Handler:4",
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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
	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	// With the new depth-aware prioritization, the per-directory file cap kicks
	// in before the char budget does. The summary header itself signals that
	// the output is partial: total source files > dirs covered, plus at least
	// one module/file_NNN.go appeared.
	if !strings.Contains(result, "### module/file_") {
		t.Errorf("expected at least one module/file_NNN.go entry in output, got %d chars:\n%s", len(result), result)
	}
	// Output should not return ALL 200 raw files because the per-directory
	// cap (and char budget) bound the visible set.
	fileCount := strings.Count(result, "\n### ")
	if fileCount >= 200 {
		t.Errorf("expected per-directory cap to bound visible files; got %d sections", fileCount)
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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
	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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
	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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
	_, err := GenerateRepoMap(ctx, dir, 3, "")
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
	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
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

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	if !strings.Contains(result, "- func DeepFunction:") {
		t.Errorf("missing 'func DeepFunction' from large file (AST needs full file):\n%s", result)
	}
}

// ============================================================================
// formatRepoMapFromNodes Tests (SP-107-5)
// ============================================================================

// TestFormatRepoMapFromNodes verifies the formatting of store-backed nodes
// into the repo map output format.
func TestFormatRepoMapFromNodes(t *testing.T) {
	nodes := []codegraph.Symbol{
		{DisplayName: "run", FilePath: "pkg/app/app.go", Line: 10, Kind: "func"},
		{DisplayName: "fetchData", FilePath: "pkg/app/app.go", Line: 20, Kind: "func"},
		{DisplayName: "Config", FilePath: "pkg/app/app.go", Line: 5, Kind: "type"},
		{DisplayName: "handler", FilePath: "pkg/api/handler.go", Line: 3, Kind: "func"},
	}

	result := formatRepoMapFromNodes("/home/user/myproject", nodes)

	for _, want := range []string{
		"## repo_map: myproject",
		"### pkg/app/app.go",
		"### pkg/api/handler.go",
		"- func run:10",
		"- func fetchData:20",
		"- type Config:5",
		"- func handler:3",
	} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output:\n%s", want, result)
		}
	}
}

// TestFormatRepoMapFromNodes_Empty verifies that nil and empty node slices
// return an empty string (signals caller to fall through).
func TestFormatRepoMapFromNodes_Empty(t *testing.T) {
	result := formatRepoMapFromNodes("/tmp", nil)
	if result != "" {
		t.Errorf("expected empty string for nil nodes, got: %q", result)
	}

	result = formatRepoMapFromNodes("/tmp", []codegraph.Symbol{})
	if result != "" {
		t.Errorf("expected empty string for empty nodes, got: %q", result)
	}
}

// TestFormatRepoMapFromNodes_Truncation verifies that output is truncated
// when the character budget is exceeded.
func TestFormatRepoMapFromNodes_Truncation(t *testing.T) {
	var nodes []codegraph.Symbol
	for i := 0; i < 400; i++ {
		nodes = append(nodes, codegraph.Symbol{
			DisplayName: fmt.Sprintf("longFunctionName_%d", i),
			FilePath:    fmt.Sprintf("pkg/path/to/file_%d.go", i),
			Line:        i + 1,
			Kind:        "func",
		})
	}

	result := formatRepoMapFromNodes("/project", nodes)

	if !strings.Contains(result, "## repo_map: project") {
		t.Error("missing repo_map header")
	}
	if strings.Contains(result, "No source files with symbols found") {
		t.Error("should have found files, not 'No source files' message")
	}
	// With 100 files, the output should be truncated.
	if !strings.Contains(result, "truncated") {
		t.Error("expected truncation notice with 100 files")
	}
}

// TestFormatRepoMapFromNodes_DeterministicOrder verifies that file paths
// appear in sorted order.
func TestFormatRepoMapFromNodes_DeterministicOrder(t *testing.T) {
	nodes := []codegraph.Symbol{
		{DisplayName: "zFunc", FilePath: "zzz.go", Line: 1, Kind: "func"},
		{DisplayName: "aFunc", FilePath: "aaa.go", Line: 1, Kind: "func"},
		{DisplayName: "mFunc", FilePath: "mmm.go", Line: 1, Kind: "func"},
	}

	result := formatRepoMapFromNodes("/project", nodes)

	// aaa.go should appear before mmm.go which should appear before zzz.go.
	aaaIdx := strings.Index(result, "### aaa.go")
	mmmIdx := strings.Index(result, "### mmm.go")
	zzzIdx := strings.Index(result, "### zzz.go")

	if aaaIdx >= mmmIdx {
		t.Errorf("aaa.go should appear before mmm.go")
	}
	if mmmIdx >= zzzIdx {
		t.Errorf("mmm.go should appear before zzz.go")
	}
}

// ============================================================================
// GenerateRepoMap Store Fallback Tests (SP-107-5)
// ============================================================================

// TestGenerateRepoMap_FallbackWhenStoreUnavailable verifies that when no
// .sprout/codegraph.db exists, GenerateRepoMap falls through to filesystem
// walk and still produces correct output.
func TestGenerateRepoMap_FallbackWhenStoreUnavailable(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a Go source file in the temp dir.
	srcDir := filepath.Join(tmpDir, "pkg", "hello")
	requireErr(t, os.MkdirAll(srcDir, 0755), "create dir")
	requireErr(t, os.WriteFile(filepath.Join(srcDir, "hello.go"), []byte(`package hello

func Greet() string {
	return "hello"
}

type Greeter struct{}
`), 0644), "write hello.go")

	result, err := GenerateRepoMap(context.Background(), tmpDir, 3, "")
	requireErr(t, err, "generate repo map")

	if !strings.Contains(result, "pkg/hello/hello.go") {
		t.Errorf("missing file path in output:\n%s", result)
	}
	if !strings.Contains(result, "func Greet") {
		t.Errorf("missing 'func Greet' in output:\n%s", result)
	}
	if !strings.Contains(result, "type Greeter") {
		t.Errorf("missing 'type Greeter' in output:\n%s", result)
	}
}

// ============================================================================
// Depth Parameter Tests
// ============================================================================

// TestGenerateRepoMapDepth1 verifies that depth=1 produces a directory tree
// with file counts and no symbol listings.
func TestGenerateRepoMapDepth1(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go": `package main

func MainFunc() {}
`,
		"src/server.go": `package main

func StartServer() {}
`,
		"src/handlers/api.go": `package handlers

func HandleAPI() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 1, "")
	requireErr(t, err, "generate repo map depth=1")

	// Should contain the directory tree header.
	if !strings.Contains(result, "### Directory Tree") {
		t.Errorf("expected 'Directory Tree' section in depth=1 output:\n%s", result)
	}

	// Should NOT contain symbol listings.
	if strings.Contains(result, "### src/server.go") {
		t.Errorf("depth=1 should not include file sections with symbols:\n%s", result)
	}
	if strings.Contains(result, "- func MainFunc:") {
		t.Errorf("depth=1 should not include symbols:\n%s", result)
	}

	// Should include file counts per directory.
	if !strings.Contains(result, "src/ (") {
		t.Errorf("expected directory with file count in depth=1 output:\n%s", result)
	}
}

// TestGenerateRepoMapDepth2 verifies that depth=2 includes symbols for
// root-level and top-level files only, with a max of 15 symbols per file.
func TestGenerateRepoMapDepth2(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go": `package main

func RootFunc() {}
`,
		"src/server.go": `package main

func TopLevelFunc() {}
`,
		"src/deep/nested.go": `package deep

func DeepFunc() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 2, "")
	requireErr(t, err, "generate repo map depth=2")

	// Root-level files should have symbols.
	if !strings.Contains(result, "- func RootFunc:") {
		t.Errorf("depth=2 should include root-level symbols:\n%s", result)
	}

	// Top-level files (depth <= 1) should have symbols.
	if !strings.Contains(result, "- func TopLevelFunc:") {
		t.Errorf("depth=2 should include top-level file symbols:\n%s", result)
	}

	// Deeper files (depth > 1) should NOT have symbols.
	if strings.Contains(result, "### src/deep/nested.go") {
		t.Errorf("depth=2 should not include symbols from depth > 1:\n%s", result)
	}
	if strings.Contains(result, "- func DeepFunc:") {
		t.Errorf("depth=2 should not include deep symbols:\n%s", result)
	}
}

// TestGenerateRepoMapDepth2SymbolCap verifies that depth=2 caps at 15 symbols.
func TestGenerateRepoMapDepth2SymbolCap(t *testing.T) {
	dir := t.TempDir()
	// Create a Go file with 20 functions.
	var sb strings.Builder
	sb.WriteString("package main\n\n")
	for i := 0; i < 20; i++ {
		fmt.Fprintf(&sb, "func Func%d() {}\n", i)
	}
	createTestFiles(t, dir, map[string]string{
		"main.go": sb.String(),
	})

	result, err := GenerateRepoMap(context.Background(), dir, 2, "")
	requireErr(t, err, "generate repo map depth=2")

	// Count the number of symbol lines (should be at most 15).
	symbolCount := strings.Count(result, "- func Func")
	if symbolCount > 15 {
		t.Errorf("depth=2 should cap at 15 symbols, got %d:\n%s", symbolCount, result)
	}
}

// TestGenerateRepoMapDepth3 verifies that depth=3 (default) uses full extraction.
func TestGenerateRepoMapDepth3(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"src/deep/nested.go": `package deep

func DeepFunc() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map depth=3")

	// depth=3 should include symbols from deep files.
	if !strings.Contains(result, "- func DeepFunc:") {
		t.Errorf("depth=3 should include deep symbols:\n%s", result)
	}
}

// TestGenerateRepoMapDepthDefault verifies that depth=0 defaults to depth=3.
func TestGenerateRepoMapDepthDefault(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go": `package main

func MainFunc() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 0, "")
	requireErr(t, err, "generate repo map depth=0")

	if !strings.Contains(result, "- func MainFunc:") {
		t.Errorf("depth=0 should default to depth=3 with full symbols:\n%s", result)
	}
}

// ============================================================================
// Query Parameter Tests
// ============================================================================

// TestGenerateRepoMapQueryPathFilter verifies that the query parameter filters
// files by path (case-insensitive).
func TestGenerateRepoMapQueryPathFilter(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"server.go": `package main

func ServerFunc() {}
`,
		"client.go": `package main

func ClientFunc() {}
`,
	})

	// Query for "server" should only return server.go.
	result, err := GenerateRepoMap(context.Background(), dir, 3, "server")
	requireErr(t, err, "generate repo map with query")

	if !strings.Contains(result, "### server.go") {
		t.Errorf("query 'server' should include server.go:\n%s", result)
	}
	if strings.Contains(result, "### client.go") {
		t.Errorf("query 'server' should exclude client.go:\n%s", result)
	}
}

// TestGenerateRepoMapQuerySymbolFilter verifies that the query parameter also
// filters at the symbol level.
func TestGenerateRepoMapQuerySymbolFilter(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"main.go": `package main

func ProcessData() {}
func ValidateInput() {}
`,
	})

	// Query for "process" should match the ProcessData function but not ValidateInput.
	result, err := GenerateRepoMap(context.Background(), dir, 3, "process")
	requireErr(t, err, "generate repo map with symbol query")

	if !strings.Contains(result, "- func ProcessData:") {
		t.Errorf("query 'process' should include ProcessData:\n%s", result)
	}
	if strings.Contains(result, "ValidateInput") {
		t.Errorf("query 'process' should exclude ValidateInput:\n%s", result)
	}
}

// TestGenerateRepoMapQueryCaseInsensitive verifies that the query is case-insensitive.
func TestGenerateRepoMapQueryCaseInsensitive(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"MyModule.go": `package main

func MyFunction() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 3, "mymodule")
	requireErr(t, err, "generate repo map with lowercase query")

	if !strings.Contains(result, "### MyModule.go") {
		t.Errorf("case-insensitive query should match MyModule.go:\n%s", result)
	}
}

// ============================================================================
// Concept Grouping and Entry Points Tests
// ============================================================================

// TestGenerateRepoMapConceptSummary verifies that the Structure section appears
// in output for repos with organized directories.
func TestGenerateRepoMapConceptSummary(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"src/components/Button.tsx": `export function Button() {}`,
		"src/components/Input.tsx":  `export function Input() {}`,
		"src/api/server.ts":         `export function serve() {}`,
		"src/utils/helpers.ts":      `export function help() {}`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	// Should contain a Structure line.
	if !strings.Contains(result, "- Structure:") {
		t.Errorf("expected 'Structure' line in output:\n%s", result)
	}
}

// TestGenerateRepoMapEntryPoints verifies that entry-point files are detected.
func TestGenerateRepoMapEntryPoints(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"index.ts": `export function main() {}`,
		"src/server.go": `package main

func Start() {}
`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	// Should contain an Entry points line mentioning index.ts.
	if !strings.Contains(result, "- Entry points:") {
		t.Errorf("expected 'Entry points' line in output:\n%s", result)
	}
	if !strings.Contains(result, "index.ts") {
		t.Errorf("expected index.ts in entry points:\n%s", result)
	}
}

// TestGenerateRepoMapTestFileConcept verifies that test directories are
// categorized as Tests in the concept summary.
func TestGenerateRepoMapTestFileConcept(t *testing.T) {
	dir := t.TempDir()
	createTestFiles(t, dir, map[string]string{
		"src/App.tsx":       `export function App() {}`,
		"e2e/test_auth.ts":  `export function testAuth() {}`,
		"e2e/test_utils.ts": `export function testUtils() {}`,
	})

	result, err := GenerateRepoMap(context.Background(), dir, 3, "")
	requireErr(t, err, "generate repo map")

	if !strings.Contains(result, "Tests") {
		t.Errorf("expected 'Tests' concept for e2e/ directory:\n%s", result)
	}
}

// ============================================================================
// Helper Function Tests
// ============================================================================

// TestIsTestFile verifies the isTestFile helper function.
func TestIsTestFile(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main_test.go", true},
		{"pkg/foo/foo_test.go", true},
		{"button.spec.ts", true},
		{"button.test.tsx", true},
		{"test_helper.py", true},
		{"helper_test.py", true},
		{"e2e/auth.ts", true},
		{"__tests__/unit.ts", true},
		{"spec/models.ts", true},
		{"main.go", false},
		{"server.ts", false},
		{"utils.py", false},
		{"src/components/Button.tsx", false},
	}
	for _, tt := range tests {
		got := isTestFile(tt.path)
		if got != tt.want {
			t.Errorf("isTestFile(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestIsEntryPoint verifies the isEntryPoint helper function.
func TestIsEntryPoint(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"main.go", true},
		{"index.ts", true},
		{"App.tsx", true},
		{"app.ts", true},
		{"package.json", true},
		{"go.mod", true},
		{"tsconfig.json", true},
		{"metro.config.js", true},
		{"Cargo.toml", true},
		{"Dockerfile", true},
		{"Makefile", true},
		// Not at root/top level.
		{"src/deep/main.go", false},
		// Not an entry point.
		{"server.go", false},
		{"helper.ts", false},
		{"random.json", false},
	}
	for _, tt := range tests {
		got := isEntryPoint(tt.path)
		if got != tt.want {
			t.Errorf("isEntryPoint(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestGetConceptForDir verifies the getConceptForDir helper function.
func TestGetConceptForDir(t *testing.T) {
	tests := []struct {
		dir  string
		want string
	}{
		{"components", "UI"},
		{"ui", "UI"},
		{"views", "UI"},
		{"screens", "UI"},
		{"pages", "UI"},
		{"services", "Services"},
		{"api", "Services"},
		{"controllers", "Services"},
		{"handlers", "Services"},
		{"utils", "Utilities"},
		{"helpers", "Utilities"},
		{"lib", "Utilities"},
		{"config", "Config"},
		{"scripts", "Config"},
		{"src", "Core"},
		{"pkg", "Core"},
		{"cmd", "Core"},
		{"internal", "Core"},
		{"models", "Core"},
		{"e2e", "Tests"},
		{"__tests__", "Tests"},
		{"tests", "Tests"},
		{"random_dir", "Other"},
	}
	for _, tt := range tests {
		got := getConceptForDir(tt.dir)
		if got != tt.want {
			t.Errorf("getConceptForDir(%q) = %v, want %v", tt.dir, got, tt.want)
		}
	}
}

// TestFilterByQuery verifies the filterByQuery helper function.
func TestFilterByQuery(t *testing.T) {
	files := []fileEntry{
		{relPath: "src/server.go"},
		{relPath: "src/client.go"},
		{relPath: "pkg/api/handler.go"},
		{relPath: "main.go"},
	}

	result := filterByQuery(files, "server")
	if len(result) != 1 {
		t.Fatalf("expected 1 result, got %d", len(result))
	}
	if result[0].relPath != "src/server.go" {
		t.Errorf("expected src/server.go, got %s", result[0].relPath)
	}

	// Case-insensitive.
	result = filterByQuery(files, "SERVER")
	if len(result) != 1 {
		t.Fatalf("expected 1 case-insensitive result, got %d", len(result))
	}

	// Match in path.
	result = filterByQuery(files, "api")
	if len(result) != 1 {
		t.Fatalf("expected 1 result for 'api', got %d", len(result))
	}
	if result[0].relPath != "pkg/api/handler.go" {
		t.Errorf("expected pkg/api/handler.go, got %s", result[0].relPath)
	}

	// No match.
	result = filterByQuery(files, "nonexistent")
	if len(result) != 0 {
		t.Errorf("expected 0 results for nonexistent query, got %d", len(result))
	}
}
