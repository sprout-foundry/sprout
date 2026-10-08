package filediscovery

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGetIgnoreRules_CachesPerRoot(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("dist/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	first := GetIgnoreRules(root)
	if first == nil {
		t.Fatal("expected rules, got nil")
	}
	if !first.MatchesPath("dist/x.js") {
		t.Error("dist/x.js should be ignored")
	}

	// Second call must return the SAME compiled instance (cache hit), even
	// though the files were not touched.
	second := GetIgnoreRules(root)
	if second != first {
		t.Error("expected cached rules instance for unchanged ignore files")
	}
}

func TestGetIgnoreRules_InvalidatesOnGitignoreChange(t *testing.T) {
	root := t.TempDir()
	gitignore := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(gitignore, []byte("dist/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	first := GetIgnoreRules(root)
	if first == nil {
		t.Fatal("expected rules, got nil")
	}

	// Rewrite with a different rule set. mtime granularity can be coarse
	// (1s on some filesystems), so guarantee a different stamp by bumping
	// the time explicitly and only advancing the size-identical case when
	// the mtime actually moved.
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(gitignore, future, future); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(gitignore, []byte("build/\n"), 0644); err != nil {
		t.Fatal(err)
	}

	second := GetIgnoreRules(root)
	if second == nil {
		t.Fatal("expected rules after change, got nil")
	}
	if second == first {
		t.Error("expected recompiled rules after .gitignore changed")
	}
	if first.MatchesPath("build/x.js") {
		t.Error("old rules should not match the new pattern")
	}
	if !second.MatchesPath("build/x.js") {
		t.Error("new rules should ignore build/x.js")
	}
}

func TestGetIgnoreRules_NoIgnoreFilesReturnsNil(t *testing.T) {
	root := t.TempDir()
	if got := GetIgnoreRules(root); got != nil {
		t.Errorf("expected nil rules for root without ignore files, got %v", got)
	}
	// Creating one later must produce rules (cache stores the nil too).
	if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0644); err != nil {
		t.Fatal(err)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(filepath.Join(root, ".gitignore"), future, future); err != nil {
		t.Fatal(err)
	}
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected rules after .gitignore appears, got nil")
	}
	if !rules.MatchesPath("debug.log") {
		t.Error("debug.log should be ignored")
	}
}

func TestGetIgnoreRules_ReadsSproutIgnore(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".sprout"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ".sprout", ".ignore"), []byte("secrets/\n"), 0644); err != nil {
		t.Fatal(err)
	}
	rules := GetIgnoreRules(root)
	if rules == nil {
		t.Fatal("expected rules from .sprout/.ignore, got nil")
	}
	if !rules.MatchesPath("secrets/key.txt") {
		t.Error("secrets/key.txt should be ignored via .sprout/.ignore")
	}
}
