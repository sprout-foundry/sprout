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

// TestGenerateRepoMapBasic verifies symbol extraction from a Go file.
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

	for _, want := range []string{"## repo_map:", "### main.go", "- func NewUser:4",
		"- type User struct:2", "- type Handler interface:3", "- var DefaultTimeout:5", "- const MaxRetries:6"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output", want)
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

	for _, want := range []string{"- func StartServer:2", "- type Config struct:3",
		"- class App:1", "- interface User:2", "- type Role:3", "- function fetchData:4", "- const VERSION:5",
		"- def calculate_total:1", "- class DataProcessor:2", "- def fetch_data:3"} {
		if !strings.Contains(result, want) {
			t.Errorf("missing %q in output", want)
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
	if !strings.Contains(result, "- func Hello:2") {
		t.Error("missing main func with line number")
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
	if !strings.Contains(result, "- func MainFunc:2") {
		t.Error("missing main func")
	}
	if strings.Contains(result, "func HiddenFunc") {
		t.Error("hidden files should not appear")
	}
}

// TestGenerateRepoMapFileLimit verifies that only 200 files are included.
func TestGenerateRepoMapFileLimit(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 250; i++ {
		content := fmt.Sprintf("package main\nfunc Func%d() {}\n", i)
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
		"main.go":     "package main\nfunc Hello() {}",
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
		content := fmt.Sprintf("package main\nfunc Func%d() {}\n", i)
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
		t.Errorf("expected 1 'func Hello:2', got %d", strings.Count(result, "- func Hello:2"))
	}
	if strings.Count(result, "- func World:4") != 1 {
		t.Errorf("expected 1 'func World:4', got %d", strings.Count(result, "- func World:4"))
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

	// Go: Config struct on line 4, NewConfig on line 8, Start on line 9
	if !strings.Contains(result, "- type Config struct:4") {
		t.Errorf("missing 'type Config struct:4', got:\n%s", result)
	}
	if !strings.Contains(result, "- func NewConfig:8") {
		t.Errorf("missing 'func NewConfig:8', got:\n%s", result)
	}
	if !strings.Contains(result, "- func Start:9") {
		t.Errorf("missing 'func Start:9', got:\n%s", result)
	}

	// TS: App on line 2, User on line 6, initApp on line 7
	if !strings.Contains(result, "- class App:2") {
		t.Errorf("missing 'class App:2', got:\n%s", result)
	}
	if !strings.Contains(result, "- interface User:6") {
		t.Errorf("missing 'interface User:6', got:\n%s", result)
	}
	if !strings.Contains(result, "- function initApp:7") {
		t.Errorf("missing 'function initApp:7', got:\n%s", result)
	}

	// Python: Parser on line 4, load_data on line 7, fetch on line 10
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
