package utils

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureRuntimeStateExcluded(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".git", "info"), 0o755); err != nil {
		t.Fatal(err)
	}
	excl := filepath.Join(dir, ".git", "info", "exclude")
	if err := os.WriteFile(excl, []byte("# git's own comment\n*.tmp"), 0o644); err != nil {
		t.Fatal(err)
	}
	EnsureRuntimeStateExcluded(dir)
	EnsureRuntimeStateExcluded(dir) // idempotent
	b, err := os.ReadFile(excl)
	if err != nil {
		t.Fatal(err)
	}
	got := string(b)
	if !strings.HasPrefix(got, "# git's own comment\n*.tmp\n") {
		t.Errorf("existing lines must stay first and intact, got %q", got)
	}
	for _, p := range RuntimeStatePatterns {
		if n := strings.Count(got, p+"\n"); n != 1 {
			t.Errorf("%s appears %d times, want once:\n%s", p, n, got)
		}
	}
}

func TestEnsureRuntimeStateExcludedOutsideARepo(t *testing.T) {
	dir := t.TempDir()
	EnsureRuntimeStateExcluded(dir)
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Errorf("no .git must be created outside a repository: %v", err)
	}
}
