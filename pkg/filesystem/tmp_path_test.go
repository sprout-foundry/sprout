package filesystem

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestIsInTmpPathResolvedTempDir guards the macOS fetch_url read-back bug:
// os.TempDir() is /var/folders/.../T, but SafeResolvePath* hands
// isInTmpPath the symlink-EvalSymlinks'd form (/private/var/folders/.../T),
// which the unresolved prefix check used to reject with
// "file access outside working directory".
func TestIsInTmpPathResolvedTempDir(t *testing.T) {
	tempDir := os.TempDir()
	resolvedTemp, err := filepath.EvalSymlinks(tempDir)
	if err != nil {
		t.Skipf("cannot resolve temp dir symlinks: %v", err)
	}

	filePath := filepath.Join(resolvedTemp, "sprout", "fetch", "fetch_deadbeef.txt")

	if !isInTmpPath(tempDir) {
		t.Errorf("isInTmpPath(%q) = false, want true", tempDir)
	}
	if !isInTmpPath(resolvedTemp) {
		t.Errorf("isInTmpPath(%q) = false, want true (resolved temp dir)", resolvedTemp)
	}
	if !isInTmpPath(filePath) {
		t.Errorf("isInTmpPath(%q) = false, want true (file under resolved temp dir)", filePath)
	}

	// Sanity: an unrelated path must still be rejected.
	if isInTmpPath(filepath.Join(os.Getenv("HOME"), "not-temp.txt")) {
		t.Errorf("isInTmpPath(%q) = true, want false", filepath.Join(os.Getenv("HOME"), "not-temp.txt"))
	}
}

// TestSafeResolvePathFetchTempFile reproduces the read_file call an agent
// makes on a fetch_url temp file (the "File path:" line points at
// $TMPDIR/sprout/fetch/fetch_*.txt). The path is passed as given, so the
// resolver must classify it as tmp without falling through to the
// working-directory check.
func TestSafeResolvePathFetchTempFile(t *testing.T) {
	dir := filepath.Join(os.TempDir(), "sprout", "fetch")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatalf("create temp dir: %v", err)
	}
	defer os.RemoveAll(dir)

	fetchFile := filepath.Join(dir, "fetch_60cd6d0c2aa9bff4.txt")
	if err := os.WriteFile(fetchFile, []byte("fetched content"), 0600); err != nil {
		t.Fatalf("write fetch file: %v", err)
	}

	resolved, err := SafeResolvePathWithBypass(context.Background(), fetchFile)
	if err != nil {
		t.Fatalf("SafeResolvePathWithBypass(%q) error = %v, want no error (tmp path must be allowed)", fetchFile, err)
	}
	if resolved == "" {
		t.Error("SafeResolvePathWithBypass returned empty resolved path")
	}
}

// TestIsUnderTmpPathExported checks the exported classifier (used by the
// agent's Gate 1 path-tier logic) agrees with the resolved temp dir.
func TestIsUnderTmpPathExported(t *testing.T) {
	resolvedTemp, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		t.Skipf("cannot resolve temp dir symlinks: %v", err)
	}
	if !IsUnderTmpPath(filepath.Join(resolvedTemp, "sub", "file.txt")) {
		t.Errorf("IsUnderTmpPath(%q) = false, want true", filepath.Join(resolvedTemp, "sub", "file.txt"))
	}
}
