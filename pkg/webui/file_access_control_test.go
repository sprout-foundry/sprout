//go:build !js

package webui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIsWithinWorkspace_SamePath(t *testing.T) {
	root := "/home/user/project"

	if !isWithinWorkspace(root, root) {
		t.Error("expected same path to be within workspace")
	}
}

func TestIsWithinWorkspace_ChildPath(t *testing.T) {
	root := "/home/user/project"

	if !isWithinWorkspace("/home/user/project/src/main.go", root) {
		t.Error("expected child path to be within workspace")
	}
	if !isWithinWorkspace("/home/user/project/a/b/c/deep.txt", root) {
		t.Error("expected deep child path to be within workspace")
	}
}

func TestIsWithinWorkspace_OutsideWorkspace(t *testing.T) {
	root := "/home/user/project"

	if isWithinWorkspace("/home/user/other", root) {
		t.Error("expected sibling directory to be outside workspace")
	}
	if isWithinWorkspace("/tmp/evil", root) {
		t.Error("expected /tmp path to be outside workspace")
	}
	if isWithinWorkspace("/etc/passwd", root) {
		t.Error("expected /etc/passwd to be outside workspace")
	}
}

func TestIsWithinWorkspace_PathTraversal(t *testing.T) {
	root := "/home/user/project"

	// Path with ".." prefix
	if isWithinWorkspace("/home/user/project/../other", root) {
		t.Error("expected path with '..' to be outside workspace")
	}
	if isWithinWorkspace("/home/user/project/src/../../other", root) {
		t.Error("expected path with '..' traversal to be outside workspace")
	}
}

func TestIsWithinWorkspace_RealPaths(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceRoot := filepath.Join(tmpDir, "workspace")
	os.MkdirAll(workspaceRoot, 0755)

	// Inside workspace
	inside := filepath.Join(workspaceRoot, "src", "file.go")
	os.MkdirAll(filepath.Dir(inside), 0755)
	if !isWithinWorkspace(inside, workspaceRoot) {
		t.Error("expected real child path to be within workspace")
	}

	// Same as workspace root
	if !isWithinWorkspace(workspaceRoot, workspaceRoot) {
		t.Error("expected workspace root to be within itself")
	}

	// Outside workspace
	outside := filepath.Join(tmpDir, "other")
	os.MkdirAll(outside, 0755)
	if isWithinWorkspace(outside, workspaceRoot) {
		t.Error("expected sibling directory to be outside workspace")
	}
}

// evalSymlinks resolves symlinks on a path (best-effort; returns original on error).
// Used in tests to match canonicalizePath behavior on macOS (/var → /private/var).
func evalSymlinks(t *testing.T, path string) string {
	t.Helper()
	if evaled, err := filepath.EvalSymlinks(path); err == nil {
		return evaled
	}
	return path
}

// evalParent resolves symlinks on the nearest existing ancestor of path.
// Use for write-path tests where the file and parent dirs may not exist yet.
func evalParent(t *testing.T, path string) string {
	t.Helper()
	dir := filepath.Dir(path)
	for {
		if evaled, err := filepath.EvalSymlinks(dir); err == nil {
			rest := strings.TrimPrefix(path, dir+string(filepath.Separator))
			return filepath.Join(evaled, rest)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return path // reached root
		}
		dir = parent
	}
}

func TestCanonicalizePath_RelativePath(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file in the temp dir so EvalSymlinks succeeds
	child := filepath.Join(tmpDir, "child")
	os.MkdirAll(child, 0755)
	testFile := filepath.Join(child, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	result, err := canonicalizePath("child/file.txt", tmpDir, false)
	if err != nil {
		t.Fatalf("canonicalizePath relative: %v", err)
	}
	if result != evalSymlinks(t, testFile) {
		t.Errorf("canonicalizePath(relative) = %q, want %q", result, evalSymlinks(t, testFile))
	}
}

func TestCanonicalizePath_AbsolutePath(t *testing.T) {
	tmpDir := t.TempDir()

	testFile := filepath.Join(tmpDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	result, err := canonicalizePath(testFile, "/some/other/root", false)
	if err != nil {
		t.Fatalf("canonicalizePath absolute: %v", err)
	}
	if result != evalSymlinks(t, testFile) {
		t.Errorf("canonicalizePath(absolute) = %q, want %q", result, evalSymlinks(t, testFile))
	}
}

func TestCanonicalizePath_EmptyPath(t *testing.T) {
	_, err := canonicalizePath("", "/some/root", false)
	if err == nil {
		t.Fatal("expected error for empty path")
	}
}

func TestCanonicalizePath_WhitespaceOnlyPath(t *testing.T) {
	_, err := canonicalizePath("   ", "/some/root", false)
	if err == nil {
		t.Fatal("expected error for whitespace-only path")
	}
}

func TestCanonicalizePath_WriteNonExistingFile(t *testing.T) {
	tmpDir := t.TempDir()

	// File doesn't exist but parent does
	newFile := filepath.Join(tmpDir, "new.txt")
	result, err := canonicalizePath("new.txt", tmpDir, true)
	if err != nil {
		t.Fatalf("canonicalizePath for write (new file): %v", err)
	}
	if result != evalParent(t, newFile) {
		t.Errorf("canonicalizePath(write, new) = %q, want %q", result, evalParent(t, newFile))
	}
}

func TestCanonicalizePath_WriteNestedNonExistingDir(t *testing.T) {
	tmpDir := t.TempDir()

	// Neither file nor parent dir exist
	newFile := filepath.Join(tmpDir, "a", "b", "c", "new.txt")
	result, err := canonicalizePath("a/b/c/new.txt", tmpDir, true)
	if err != nil {
		t.Fatalf("canonicalizePath for write (nested new): %v", err)
	}
	if result != evalParent(t, newFile) {
		t.Errorf("canonicalizePath(write, nested) = %q, want %q", result, evalParent(t, newFile))
	}
}

func TestCanonicalizePath_Symlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create real directory and a symlink to it
	realDir := filepath.Join(tmpDir, "real")
	os.MkdirAll(realDir, 0755)
	testFile := filepath.Join(realDir, "file.txt")
	os.WriteFile(testFile, []byte("test"), 0644)

	linkDir := filepath.Join(tmpDir, "link")
	err := os.Symlink(realDir, linkDir)
	if err != nil {
		// Symlinks may not be supported on Windows; skip
		t.Skip("symlinks not supported")
	}

	// Resolving through symlink should resolve to real path
	result, err := canonicalizePath("link/file.txt", tmpDir, false)
	if err != nil {
		t.Fatalf("canonicalizePath with symlink: %v", err)
	}
	if result != evalSymlinks(t, testFile) {
		t.Errorf("canonicalizePath(symlink) = %q, want %q", result, evalSymlinks(t, testFile))
	}
}
