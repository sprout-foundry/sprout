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

	for _, want := range []string{"## repo_map:", "### main.go", "- func NewUser",
		"- type User struct", "- type Handler interface", "- var DefaultTimeout", "- const MaxRetries"} {
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

	for _, want := range []string{"- func StartServer", "- type Config struct",
		"- class App", "- interface User", "- type Role", "- function fetchData", "- const VERSION",
		"- def calculate_total", "- class DataProcessor", "- def fetch_data"} {
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
	if !strings.Contains(result, "### main.go") {
		t.Error("missing main.go heading")
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
	if !strings.Contains(result, "- func MainFunc") {
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
	if strings.Count(result, "- func Hello") != 1 {
		t.Errorf("expected 1 'func Hello', got %d", strings.Count(result, "- func Hello"))
	}
	if strings.Count(result, "- func World") != 1 {
		t.Errorf("expected 1 'func World', got %d", strings.Count(result, "- func World"))
	}
}
