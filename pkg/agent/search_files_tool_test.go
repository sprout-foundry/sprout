//go:build !windows
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
		"search_pattern": "hello", // should match both lines (case-insensitive)
		"directory":      root,
		"case_sensitive": false,
		"max_results":    10,
	}

	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	_, out, err := ExecuteTool(ctx, "search_files", args, agent, "")
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

	// The search_files handler treats patterns wrapped in / as regex.
	// Use /^World$/ to enable regex mode for this case-sensitive match.
	args := map[string]interface{}{
		"search_pattern": "/^World$/",
		"directory":      root,
		"case_sensitive": true,
		"max_results":    10,
	}
	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	_, out, err := ExecuteTool(ctx, "search_files", args, agent, "")
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
		"search_pattern": "needle",
		"directory":      root,
		"file_glob":      "*.go",
	}
	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	_, out, err := ExecuteTool(ctx, "search_files", args, agent, "")
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}
	if !strings.Contains(out, "keep/file") || strings.Contains(out, "skip/file.txt") {
		t.Fatalf("glob filter not applied correctly, got: %s", out)
	}
}

func TestSearchFiles_ExcludeDotSprout(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, ".sprout/hidden.txt", "secret needle\n")
	writeTestFile(t, root, "visible.txt", "needle\n")

	args := map[string]interface{}{
		"search_pattern": "needle",
		"directory":      root,
	}
	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	_, out, err := ExecuteTool(ctx, "search_files", args, agent, "")
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}
	if strings.Contains(out, ".sprout/hidden.txt") {
		t.Fatalf(".sprout directory should be excluded by default, got: %s", out)
	}
	if !strings.Contains(out, "visible.txt") {
		t.Fatalf("expected visible.txt to appear, got: %s", out)
	}
}

func TestSearchFiles_DefaultMaxResultsAndLineTruncation(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 60; i++ {
		writeTestFile(t, root, filepath.Join("dir", fmt.Sprintf("file-%d.txt", i)), strings.Repeat("A", 600)+" needle match")
	}

	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	// The default max_results=50 means only 50 results will be returned for 60 files.
	_, out, err := ExecuteTool(ctx, "search_files", map[string]interface{}{
		"search_pattern": "needle",
		"directory":      root,
	}, agent, "")
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}

	// Should cap results at max_results (50). The header shows the count of
	// results collected, which stops at the max_results default of 50.
	if !strings.Contains(out, "Found 50 result(s)") {
		t.Fatalf("expected exactly 50 results (default max_results cap), got: %s", out)
	}
}

func TestSearchFiles_MaxBytesLimit(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, root, "one.txt", "needle one\n")
	writeTestFile(t, root, "two.txt", "needle two\n")
	writeTestFile(t, root, "three.txt", "needle three\n")

	ctx := context.Background()
	agent := &Agent{client: NewScriptedClient()}
	_, out, err := ExecuteTool(ctx, "search_files", map[string]interface{}{
		"search_pattern": "needle",
		"directory":      root,
		"max_bytes":      60,
	}, agent, "")
	if err != nil {
		t.Fatalf("search_files error: %v", err)
	}

	// With max_bytes=60, only the first result (~60 bytes) fits.
	// The handler returns results it collected without a truncation warning,
	// but should include at least one match and not all three.
	if !strings.Contains(out, "one.txt") {
		t.Fatalf("expected at least one.txt match, got: %s", out)
	}
	// The second and third results exceed max_bytes, so they should not appear
	if strings.Contains(out, "three.txt") {
		t.Fatalf("max_bytes limit exceeded — should not see three.txt: %s", out)
	}
}
