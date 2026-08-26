//go:build !js

package txn

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// ETH-2 transactional escalation: core package tests against real temp
// directories and real git. The path-safety table below is the security
// surface of the push plane — every row is a way a manifest could reach
// outside the workdir, and every one must land in "skipped", never on disk.

// requireGit skips the test when git is unavailable.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skipf("git not available: %v", err)
	}
}

// txnTestGit runs git in dir, failing the test on error.
func txnTestGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s in %s: %v\n%s", strings.Join(args, " "), dir, err, out)
	}
	return string(out)
}

// newTxnTestRepo creates a repo with one committed file on branch main.
func newTxnTestRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()
	txnTestGit(t, dir, "init", "-b", "main")
	txnTestGit(t, dir, "config", "user.email", "test@example.com")
	txnTestGit(t, dir, "config", "user.name", "Test User")
	txnWriteFile(t, dir, "README.md", "hello\n")
	txnTestGit(t, dir, "add", ".")
	txnTestGit(t, dir, "commit", "-m", "initial commit")
	return dir
}

func txnWriteFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func txnReadFile(t *testing.T, dir, rel string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func b64(s string) string {
	return encodeBase64([]byte(s))
}

// manifestOf builds a minimal manifest with the given file entries.
func manifestOf(files ...DeltaFile) DeltaManifest {
	m := newManifest()
	m.Base = DeltaBase{Client: TxnClientWASM}
	m.Files = files
	return m
}

func TestValidateRelPath_Table(t *testing.T) {
	cases := []struct {
		path   string
		reason string
	}{
		{"src/main.go", ""},
		{"a/b/c.txt", ""},
		{".hidden", ""},
		{"a/.hidden", ""},
		{"pkg/txn/txn.go", ""},
		{"", SkipReasonEmptyPath},
		{"   ", SkipReasonEmptyPath},
		{"/etc/passwd", SkipReasonAbsolutePath},
		{"/", SkipReasonAbsolutePath},
		{"C:/Windows/system32", SkipReasonAbsolutePath},
		{"c:\\windows\\system32", SkipReasonAbsolutePath},
		{"\\\\server\\share\\f", SkipReasonAbsolutePath},
		{"../escape", SkipReasonPathTraversal},
		{"a/../../escape", SkipReasonPathTraversal},
		{"..", SkipReasonPathTraversal},
		{"a/../b", SkipReasonPathTraversal},
		{".git/config", SkipReasonGitPath},
		{".git", SkipReasonGitPath},
		{"sub/.git/HEAD", SkipReasonGitPath},
		{"a/.git", SkipReasonGitPath},
		{"a\x00b", SkipReasonNulInPath},
		{"a//b", SkipReasonInvalidPath},
		{"a/", SkipReasonInvalidPath},
		{"./a", SkipReasonInvalidPath},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			if got := validateRelPath(tc.path); got != tc.reason {
				t.Errorf("validateRelPath(%q) = %q, want %q", tc.path, got, tc.reason)
			}
		})
	}
}

func TestApplyDelta_WritesFilesAndCreatesParents(t *testing.T) {
	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "src/main.go", ContentBase64: b64("package main\n")},
		DeltaFile{Path: "a/b/c.txt", ContentBase64: b64("deep")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 2 || result.Deleted != 0 {
		t.Fatalf("result = %+v, want applied=2 deleted=0", result)
	}
	if result.Status != StatusOK {
		t.Fatalf("status = %q, want ok", result.Status)
	}
	if got := txnReadFile(t, dir, "src/main.go"); got != "package main\n" {
		t.Fatalf("src/main.go = %q", got)
	}
	if got := txnReadFile(t, dir, "a/b/c.txt"); got != "deep" {
		t.Fatalf("a/b/c.txt = %q", got)
	}
	info, err := os.Stat(filepath.Join(dir, "src"))
	if err != nil || !info.IsDir() {
		t.Fatalf("parent dir not created: %v", err)
	}
	if info.Mode().Perm() != 0o755 {
		t.Fatalf("parent dir mode = %o, want 755", info.Mode().Perm())
	}
}

func TestApplyDelta_DefaultModeAndExplicitMode(t *testing.T) {
	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "default.txt", ContentBase64: b64("x")},
		DeltaFile{Path: "run.sh", ContentBase64: b64("#!/bin/sh\n"), Mode: "0755"},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Status != StatusOK {
		t.Fatalf("status = %q: %+v", result.Status, result.Skipped)
	}
	if perm := modeOf(t, dir, "default.txt"); perm != 0o644 {
		t.Fatalf("default.txt mode = %o, want 644", perm)
	}
	if perm := modeOf(t, dir, "run.sh"); perm != 0o755 {
		t.Fatalf("run.sh mode = %o, want 755", perm)
	}
}

func TestApplyDelta_RewritesExistingFileAndConvergesMode(t *testing.T) {
	dir := t.TempDir()
	txnWriteFile(t, dir, "existing.txt", "old")
	if err := os.Chmod(filepath.Join(dir, "existing.txt"), 0o600); err != nil {
		t.Fatal(err)
	}

	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "existing.txt", ContentBase64: b64("new")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
	if got := txnReadFile(t, dir, "existing.txt"); got != "new" {
		t.Fatalf("content = %q, want new", got)
	}
	// WriteFile applies the mode only on creation; the explicit chmod is
	// what makes push/pull converge on a pre-existing file.
	if perm := modeOf(t, dir, "existing.txt"); perm != 0o644 {
		t.Fatalf("mode = %o, want 644 after rewrite", perm)
	}
}

func TestApplyDelta_DeletesAfterWrites(t *testing.T) {
	dir := t.TempDir()
	txnWriteFile(t, dir, "old.go", "x")
	txnWriteFile(t, dir, "keep.txt", "y")

	manifest := manifestOf(DeltaFile{Path: "new.txt", ContentBase64: b64("n")})
	manifest.Deletes = []string{"old.go"}
	result, err := ApplyDelta(context.Background(), dir, manifest)
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 || result.Deleted != 1 {
		t.Fatalf("result = %+v, want applied=1 deleted=1", result)
	}
	if _, err := os.Stat(filepath.Join(dir, "old.go")); !os.IsNotExist(err) {
		t.Fatal("old.go still exists")
	}
	if got := txnReadFile(t, dir, "keep.txt"); got != "y" {
		t.Fatalf("keep.txt = %q, want y", got)
	}
}

func TestApplyDelta_DeleteMissingIsNoOpNotSkip(t *testing.T) {
	dir := t.TempDir()
	manifest := manifestOf()
	manifest.Deletes = []string{"never-existed.txt"}
	result, err := ApplyDelta(context.Background(), dir, manifest)
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Deleted != 0 {
		t.Fatalf("deleted = %d, want 0", result.Deleted)
	}
	if result.Status != StatusOK || len(result.Skipped) != 0 {
		t.Fatalf("missing delete target must be a no-op, got %+v", result)
	}
}

func TestApplyDelta_UnsafePathsAreSkippedNotErrors(t *testing.T) {
	dir := t.TempDir()
	unsafe := []string{
		"/etc/passwd",
		"../outside.txt",
		"a/../../outside.txt",
		".git/config",
		".git",
		"sub/.git/x",
		"a\x00b",
		"",
	}
	files := make([]DeltaFile, 0, len(unsafe))
	for _, p := range unsafe {
		files = append(files, DeltaFile{Path: p, ContentBase64: b64("evil")})
	}
	files = append(files, DeltaFile{Path: "ok.txt", ContentBase64: b64("fine")})

	manifest := manifestOf(files...)
	manifest.Deletes = []string{"../outside.txt", ".git/HEAD"}

	result, err := ApplyDelta(context.Background(), dir, manifest)
	if err != nil {
		t.Fatalf("unsafe paths must not fail the request: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1 (only ok.txt)", result.Applied)
	}
	if result.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", result.Status)
	}
	// files + deletes that were refused
	if len(result.Skipped) != len(unsafe)+2 {
		t.Fatalf("skipped = %d entries, want %d: %+v", len(result.Skipped), len(unsafe)+2, result.Skipped)
	}
	skippedPaths := map[string]bool{}
	for _, s := range result.Skipped {
		skippedPaths[s.Path] = true
		if s.Reason == "" {
			t.Errorf("skipped entry %q has no reason", s.Path)
		}
	}
	for _, p := range unsafe {
		if !skippedPaths[p] {
			t.Errorf("path %q not reported as skipped", p)
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "ok.txt")); err != nil {
		t.Fatalf("ok.txt not applied: %v", err)
	}
	// Nothing may exist outside the workdir.
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), "outside.txt")); err == nil {
		t.Fatal("../outside.txt escaped the workdir")
	}
}

func TestApplyDelta_SymlinkEscapeRejected(t *testing.T) {
	outside := t.TempDir()
	target := filepath.Join(outside, "secret.txt")
	if err := os.WriteFile(target, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	// A directory symlink pointing outside the workdir.
	if err := os.Symlink(outside, filepath.Join(dir, "linkdir")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	// A file symlink pointing outside the workdir.
	if err := os.Symlink(target, filepath.Join(dir, "linkfile")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}

	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "linkdir/pwned.txt", ContentBase64: b64("evil")},
		DeltaFile{Path: "linkfile", ContentBase64: b64("evil")},
		DeltaFile{Path: "real.txt", ContentBase64: b64("ok")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1; skipped=%+v", result.Applied, result.Skipped)
	}
	if result.Status != StatusPartial {
		t.Fatalf("status = %q, want partial", result.Status)
	}
	if _, err := os.Stat(filepath.Join(outside, "pwned.txt")); err == nil {
		t.Fatal("write escaped the workdir through a directory symlink")
	}
	if got, err := os.ReadFile(target); err != nil || string(got) != "secret" {
		t.Fatalf("file symlink target was overwritten: %q %v", got, err)
	}
}

func TestApplyDelta_SymlinkInsideWorkdirIsFollowed(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(dir, "alias")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "alias/inside.txt", ContentBase64: b64("ok")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 || result.Status != StatusOK {
		t.Fatalf("result = %+v, want a clean apply", result)
	}
	if got := txnReadFile(t, dir, "real/inside.txt"); got != "ok" {
		t.Fatalf("real/inside.txt = %q, want ok", got)
	}
}

func TestApplyDelta_Base64FailureSkipped(t *testing.T) {
	dir := t.TempDir()
	result, err := ApplyDelta(context.Background(), dir, manifestOf(
		DeltaFile{Path: "bad.txt", ContentBase64: "!!!not base64!!!"},
		DeltaFile{Path: "good.txt", ContentBase64: b64("fine")},
	))
	if err != nil {
		t.Fatalf("ApplyDelta: %v", err)
	}
	if result.Applied != 1 {
		t.Fatalf("applied = %d, want 1", result.Applied)
	}
	if len(result.Skipped) != 1 || result.Skipped[0].Reason != SkipReasonInvalidBase64 {
		t.Fatalf("skipped = %+v, want one invalid_base64", result.Skipped)
	}
	if _, err := os.Stat(filepath.Join(dir, "bad.txt")); !os.IsNotExist(err) {
		t.Fatal("bad.txt must not be created")
	}
}

func modeOf(t *testing.T, dir, rel string) os.FileMode {
	t.Helper()
	info, err := os.Stat(filepath.Join(dir, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
