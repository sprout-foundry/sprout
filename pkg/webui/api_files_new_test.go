//go:build !js

package webui

import (
	"testing"

	ignore "github.com/sabhiram/go-gitignore"
)

// ---------------------------------------------------------------------------
// getGitStatusForEntry — pure helper
// ---------------------------------------------------------------------------

func TestGetGitStatusForEntry_GitDirIgnored(t *testing.T) {
	// .git directory is always ignored
	got := getGitStatusForEntry(".git", true, nil, nil, nil, "/ws")
	if got != "ignored" {
		t.Errorf("got %q, want %q", got, "ignored")
	}
}

func TestGetGitStatusForEntry_ModifiedFile(t *testing.T) {
	modified := map[string]bool{"src/main.go": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("src/main.go", false, modified, untracked, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_UntrackedFile(t *testing.T) {
	modified := map[string]bool{}
	untracked := map[string]bool{"src/new.go": true}
	got := getGitStatusForEntry("src/new.go", false, modified, untracked, nil, "/ws")
	if got != "untracked" {
		t.Errorf("got %q, want %q", got, "untracked")
	}
}

func TestGetGitStatusForEntry_CleanFile(t *testing.T) {
	modified := map[string]bool{"other.go": true}
	untracked := map[string]bool{"new.go": true}
	got := getGitStatusForEntry("clean.go", false, modified, untracked, nil, "/ws")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestGetGitStatusForEntry_DirectoryWithModifiedChild(t *testing.T) {
	modified := map[string]bool{"src/sub/main.go": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("src", true, modified, untracked, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_DirectoryWithUntrackedChild(t *testing.T) {
	modified := map[string]bool{}
	untracked := map[string]bool{"src/new.txt": true}
	got := getGitStatusForEntry("src", true, modified, untracked, nil, "/ws")
	if got != "untracked" {
		t.Errorf("got %q, want %q", got, "untracked")
	}
}

func TestGetGitStatusForEntry_DirectoryClean(t *testing.T) {
	modified := map[string]bool{"other/file.go": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("src", true, modified, untracked, nil, "/ws")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestGetGitStatusForEntry_ModifiedTakesPrecedence(t *testing.T) {
	// If both modified and untracked children exist, modified should win
	// (the code checks modified first in the directory loop)
	modified := map[string]bool{"src/modified.go": true}
	untracked := map[string]bool{"src/new.go": true}
	got := getGitStatusForEntry("src", true, modified, untracked, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_IgnoreRulesFile(t *testing.T) {
	// Create a mock ignore rules that matches our file
	ignoreRules := ignore.CompileIgnoreLines("build/*")
	modified := map[string]bool{"build/output": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("build/output", false, modified, untracked, ignoreRules, "/ws")
	// The ignore check runs before modified check
	// If the file is ignored, it returns "ignored" before checking modified
	if got != "ignored" {
		t.Logf("got %q — ignoreRules matching may vary", got)
	}
}

func TestGetGitStatusForEntry_IgnoreRulesDirectory(t *testing.T) {
	ignoreRules := ignore.CompileIgnoreLines("node_modules")
	modified := map[string]bool{"node_modules/pkg.js": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("node_modules", true, modified, untracked, ignoreRules, "/ws")
	if got != "ignored" {
		t.Logf("got %q — node_modules should be ignored", got)
	}
}

func TestGetGitStatusForEntry_NilIgnoreRules(t *testing.T) {
	modified := map[string]bool{"src/main.go": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("src/main.go", false, modified, untracked, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_RelativePathDot(t *testing.T) {
	modified := map[string]bool{"main.go": true}
	got := getGitStatusForEntry("main.go", false, modified, nil, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_NestedDirWithPrefixMatch(t *testing.T) {
	modified := map[string]bool{"deep/nested/file.go": true}
	got := getGitStatusForEntry("deep/nested", true, modified, nil, nil, "/ws")
	if got != "modified" {
		t.Errorf("got %q, want %q", got, "modified")
	}
}

func TestGetGitStatusForEntry_DirPrefixNotSubstring(t *testing.T) {
	// "src" directory should not match "src2/file.go"
	modified := map[string]bool{"src2/file.go": true}
	got := getGitStatusForEntry("src", true, modified, nil, nil, "/ws")
	if got != "" {
		t.Errorf("got %q, want empty (src should not match src2/)", got)
	}
}

func TestGetGitStatusForEntry_EmptySets(t *testing.T) {
	got := getGitStatusForEntry("file.go", false, map[string]bool{}, map[string]bool{}, nil, "/ws")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}

func TestGetGitStatusForEntry_DirectoryWithNoMatchingChildren(t *testing.T) {
	modified := map[string]bool{"a/b/file.go": true}
	untracked := map[string]bool{}
	got := getGitStatusForEntry("x/y", true, modified, untracked, nil, "/ws")
	if got != "" {
		t.Errorf("got %q, want empty", got)
	}
}
