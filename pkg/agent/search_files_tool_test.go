package agent

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTestFile creates a file with given content, ensuring parent dirs
func writeTestFile(t *testing.T, root, rel string, content string) string {
	t.Helper()
	p := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatalf("mkdirs failed: %v", err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write file failed: %v", err)
	}
	return p
}

func TestSearchFiles_SubstringCaseInsensitive(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "a.txt", "Hello World\nSecond line")
	writeTestFile(t, root, "sub/b.txt", "nothing here\nHELLO again")

	args := map[string]interface{}{
		"pattern":        "hello", // should match both lines (case-insensitive)
		"directory":      root,
		"case_sensitive": false,
		"max_results":    10,
	}

	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", args, agent)
	if err != nil {
		t.Fatalf("search_files returned error: %v", err)
	}

	// Expect both files to appear
	if !strings.Contains(out, "a.txt:") {
		t.Fatalf("expected match in a.txt, got: %s", out)
	}
	if !strings.Contains(out, "sub/b.txt:") {
		t.Fatalf("expected match in sub/b.txt, got: %s", out)
	}
}

func TestSearchFiles_RegexCaseSensitive(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "c.md", "alpha\nWorld\nworld")

	// Case sensitive regex should only match "World" (capital W)
	args := map[string]interface{}{
		"pattern":        "^World$",
		"directory":      root,
		"case_sensitive": true,
		"max_results":    10,
	}
	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", args, agent)
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}
	if !strings.Contains(out, "c.md:") || !strings.Contains(out, ":2:World") {
		t.Fatalf("expected 'World' on line 2, got: %s", out)
	}
	if strings.Contains(out, "world") && strings.Contains(out, ":3:") {
		t.Fatalf("did not expect lowercase 'world' to match in case-sensitive mode: %s", out)
	}
}

func TestSearchFiles_GlobFilterAndMaxResults(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "keep/file1.go", "needle\n")
	writeTestFile(t, root, "keep/file2.go", "needle here too\n")
	writeTestFile(t, root, "skip/file.txt", "needle but should be excluded by glob\n")

	args := map[string]interface{}{
		"pattern":      "needle",
		"directory":    root,
		"file_pattern": "*.go",
		"max_results":  1, // ensure truncation
	}
	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", args, agent)
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}
	if !strings.Contains(out, "keep/file") || strings.Contains(out, "skip/file.txt") {
		t.Fatalf("glob filter not applied correctly, got: %s", out)
	}
	// Since max_results=1, ensure only one line appears
	if cnt := strings.Count(strings.TrimSpace(out), "\n") + 1; cnt > 1 {
		t.Fatalf("expected at most 1 result, got %d: %s", cnt, out)
	}
}

func TestSearchFiles_ExcludeDotLedit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".ledit/hidden.txt", "secret needle\n")
	writeTestFile(t, root, "visible.txt", "needle\n")

	args := map[string]interface{}{
		"pattern":   "needle",
		"directory": root,
	}
	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", args, agent)
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}
	if strings.Contains(out, ".ledit/hidden.txt") {
		t.Fatalf(".ledit directory should be excluded by default, got: %s", out)
	}
	if !strings.Contains(out, "visible.txt") {
		t.Fatalf("expected visible.txt to appear, got: %s", out)
	}
}

func TestSearchFiles_DefaultMaxResultsAndLineTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < defaultSearchMaxResults+10; i++ {
		writeTestFile(t, root, filepath.Join("dir", fmt.Sprintf("file-%d.txt", i)), strings.Repeat("A", 600)+" needle match")
	}

	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", map[string]interface{}{
		"pattern":   "needle",
		"directory": root,
	}, agent)
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(out), "\n")
	if len(lines) > defaultSearchMaxResults {
		t.Fatalf("expected at most %d results, got %d", defaultSearchMaxResults, len(lines))
	}

	if !strings.Contains(out, "...") {
		t.Fatalf("expected long lines to be truncated with ellipsis, got: %s", out)
	}
}

func TestSearchFiles_MaxBytesLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "one.txt", "needle one\n")
	writeTestFile(t, root, "two.txt", "needle two\n")
	writeTestFile(t, root, "three.txt", "needle three\n")

	reg := GetToolRegistry()
	ctx := context.Background()
	agent := &Agent{client: newStubClient("openrouter", "anthropic/claude-3")}
	out, err := reg.ExecuteTool(ctx, "search_files", map[string]interface{}{
		"pattern":   "needle",
		"directory": root,
		"max_bytes": 40,
	}, agent)
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}

	lineCount := strings.Count(strings.TrimSpace(out), "\n") + 1
	if lineCount > 2 { // should stop early due to byte cap
		t.Fatalf("expected byte cap to limit results, got %d lines: %s", lineCount, out)
	}
}
