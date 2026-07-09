//go:build !js

package cmd

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// =============================================================================
// detectGitRepo
// =============================================================================

// TestGitRepoDetect_RepoFound verifies that a directory containing .git/
// is detected as a git repo and returns the correct .sprout path.
func TestGitRepoDetect_RepoFound(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	sproutDir, found := detectGitRepo(dir)
	if !found {
		t.Error("expected git repo to be detected when .git/ directory exists")
	}
	expected := filepath.Join(dir, ".sprout")
	if sproutDir != expected {
		t.Errorf("sproutDir = %q, want %q", sproutDir, expected)
	}
}

// TestGitRepoDetect_DeepSubdirectory verifies that running from a deep
// subdirectory still finds .git via ancestor walk, and returns .sprout
// at the repo root, not the subdirectory.
func TestGitRepoDetect_DeepSubdirectory(t *testing.T) {
	dir := t.TempDir()

	// Create the git repo at the root of the temp dir
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create a deep subdirectory structure
	deepDir := filepath.Join(dir, "src", "lib", "pkg", "internal")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}

	sproutDir, found := detectGitRepo(deepDir)
	if !found {
		t.Error("expected git repo to be detected from deep subdirectory via ancestor walk")
	}
	expected := filepath.Join(dir, ".sprout") // .sprout goes to repo root, NOT deepDir
	if sproutDir != expected {
		t.Errorf("sproutDir = %q, want %q", sproutDir, expected)
	}
}

// TestGitRepoDetect_MultipleLevels verifies that ancestor walk stops at
// the first .git/ found (closest ancestor).
func TestGitRepoDetect_MultipleLevels(t *testing.T) {
	dir := t.TempDir()

	// Create .git at root
	rootGit := filepath.Join(dir, ".git")
	if err := os.MkdirAll(rootGit, 0755); err != nil {
		t.Fatal(err)
	}

	// Also create .git in a subdirectory (should never be reached because
	// the root .git is found first)
	subDir := filepath.Join(dir, "sub")
	subGit := filepath.Join(subDir, ".git")
	if err := os.MkdirAll(subGit, 0755); err != nil {
		t.Fatal(err)
	}

	// Walk from sub/sub2 — should find the closest .git at dir/sub
	deepDir := filepath.Join(subDir, "sub2")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatal(err)
	}

	sproutDir, found := detectGitRepo(deepDir)
	if !found {
		t.Error("expected git repo to be detected")
	}
	// The closest .git from deepDir is at subDir
	expected := filepath.Join(subDir, ".sprout")
	if sproutDir != expected {
		t.Errorf("sproutDir = %q, want %q (closest ancestor .git)", sproutDir, expected)
	}
}

// TestGitRepoDetect_NoRepo verifies that a plain directory without .git
// returns found=false.
func TestGitRepoDetect_NoRepo(t *testing.T) {
	dir := t.TempDir()

	_, found := detectGitRepo(dir)
	if found {
		t.Error("expected no git repo to be detected in a plain directory")
	}
}

// TestGitRepoDetect_GitIsFile verifies that a .git file (as used by git
// submodules) does NOT trigger detection. Only .git directories count.
func TestGitRepoDetect_GitIsFile(t *testing.T) {
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: ../.git/modules/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	_, found := detectGitRepo(dir)
	if found {
		t.Error("expected .git file (submodule reference) NOT to trigger git repo detection")
	}
}

// TestGitRepoDetect_EmptyCwd verifies that an empty cwd (simulating
// os.Getwd() failure) gracefully returns found=false without panicking.
func TestGitRepoDetect_EmptyCwd(t *testing.T) {
	_, found := detectGitRepo("")
	if found {
		t.Error("expected empty cwd to return found=false")
	}
}

// TestGitRepoDetect_DotPath verifies that "." as cwd is handled and the
// loop terminates without infinite looping.
func TestGitRepoDetect_DotPath(t *testing.T) {
	_, found := detectGitRepo(".")
	if found {
		// If cwd is "." and there's a .git in the actual cwd, this
		// could legitimately return true. The key assertion is that
		// the function doesn't hang or panic.
		t.Log("detectGitRepo(\".\") found git repo in current directory")
	}
}

// TestGitRepoDetect_RootPath verifies that "/" as cwd terminates
// immediately without panicking.
func TestGitRepoDetect_RootPath(t *testing.T) {
	_, found := detectGitRepo("/")
	if found {
		t.Log("detectGitRepo(\"/\") found .git at filesystem root (unusual but valid)")
	}
	// The key assertion: no panic, no infinite loop.
}

// TestGitRepoDetect_AbsolutePathOnly verifies that detectGitRepo works
// correctly with absolute paths (the only kind it receives from os.Getwd).
func TestGitRepoDetect_AbsolutePathOnly(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// detectGitRepo is always called with the absolute path from os.Getwd().
	// Verify it works correctly with absolute paths.
	sproutDir, found := detectGitRepo(dir)
	if !found {
		t.Error("expected git repo to be detected with absolute path")
	}
	expected := filepath.Join(dir, ".sprout")
	if sproutDir != expected {
		t.Errorf("sproutDir = %q, want %q", sproutDir, expected)
	}
}

// =============================================================================
// autoDetectIsolatedConfig — mirrors PersistentPreRunE logic for testability
// =============================================================================

// autoDetectIsolatedConfig mirrors the auto-detection portion of
// PersistentPreRunE. It returns true if it detected and configured
// isolation (i.e. set isolatedConfig = true).
func autoDetectIsolatedConfig(cwd string) bool {
	if isolatedConfig {
		return false
	}
	isolatedDir, found := detectGitRepo(cwd)
	if !found {
		return false
	}
	if err := configuration.BootstrapIsolatedConfig(isolatedDir); err != nil {
		return false
	}
	isolatedConfig = true
	return true
}

// TestGitRepoAutoDetect_AlreadyIsolated verifies that when --isolated-config is
// already set, auto-detection is skipped entirely.
func TestGitRepoAutoDetect_AlreadyIsolated(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Simulate --isolated-config already being true
	origIsolated := isolatedConfig
	isolatedConfig = true
	defer func() { isolatedConfig = origIsolated }()

	detected := autoDetectIsolatedConfig(dir)
	if detected {
		t.Error("auto-detection should be skipped when isolatedConfig is already true")
	}
}

// TestGitRepoAutoDetect_GitRepoFound verifies the end-to-end flow: a git repo
// is found, .sprout/config.json is bootstrapped, and isolatedConfig
// becomes true.
func TestGitRepoAutoDetect_GitRepoFound(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Reset isolatedConfig for this test
	origIsolated := isolatedConfig
	isolatedConfig = false
	defer func() { isolatedConfig = origIsolated }()

	detected := autoDetectIsolatedConfig(dir)
	if !detected {
		t.Error("expected auto-detection to find git repo and configure isolation")
	}
	if !isolatedConfig {
		t.Error("expected isolatedConfig to be set to true after successful detection")
	}

	// Verify .sprout/config.json was created
	configPath := filepath.Join(dir, ".sprout", "config.json")
	if _, err := os.Stat(configPath); err != nil {
		t.Errorf("expected .sprout/config.json to exist after bootstrap: %v", err)
	}
}

// TestGitRepoAutoDetect_NoRepo verifies that auto-detection falls through
// silently when there is no git repo.
func TestGitRepoAutoDetect_NoRepo(t *testing.T) {
	dir := t.TempDir()

	origIsolated := isolatedConfig
	isolatedConfig = false
	defer func() { isolatedConfig = origIsolated }()

	detected := autoDetectIsolatedConfig(dir)
	if detected {
		t.Error("auto-detection should return false when no git repo exists")
	}
	if isolatedConfig {
		t.Error("isolatedConfig should remain false when no git repo found")
	}
}

// TestGitRepoAutoDetect_IdempotentBootstrap verifies that calling
// BootstrapIsolatedConfig twice on the same directory is safe
// and the config.json persists.
func TestGitRepoAutoDetect_IdempotentBootstrap(t *testing.T) {
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	sproutDir := filepath.Join(dir, ".sprout")

	// First bootstrap
	if err := configuration.BootstrapIsolatedConfig(sproutDir); err != nil {
		t.Fatalf("first BootstrapIsolatedConfig failed: %v", err)
	}

	configPath := filepath.Join(sproutDir, "config.json")
	firstStat, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config.json should exist after first bootstrap: %v", err)
	}

	// Second bootstrap — should be idempotent, no error
	if err := configuration.BootstrapIsolatedConfig(sproutDir); err != nil {
		t.Fatalf("second BootstrapIsolatedConfig should be idempotent, got error: %v", err)
	}

	secondStat, err := os.Stat(configPath)
	if err != nil {
		t.Fatalf("config.json should still exist after second bootstrap: %v", err)
	}

	// File should not have been modified (same mod time, same size)
	if !firstStat.ModTime().Equal(secondStat.ModTime()) {
		t.Error("config.json was modified by second bootstrap call — expected idempotent no-op")
	}
}

// TestGitRepoAutoDetect_GitIsFileNotDetected verifies the full flow treats a
// .git file the same as no repo.
func TestGitRepoAutoDetect_GitIsFileNotDetected(t *testing.T) {
	dir := t.TempDir()
	gitFile := filepath.Join(dir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: ../.git/modules/foo\n"), 0644); err != nil {
		t.Fatal(err)
	}

	origIsolated := isolatedConfig
	isolatedConfig = false
	defer func() { isolatedConfig = origIsolated }()

	detected := autoDetectIsolatedConfig(dir)
	if detected {
		t.Error("auto-detection should not trigger for .git file (submodule)")
	}
	if isolatedConfig {
		t.Error("isolatedConfig should remain false for .git file")
	}
}
