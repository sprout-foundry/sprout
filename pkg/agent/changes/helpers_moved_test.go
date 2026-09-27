package changes

import (
	"os"
	"path/filepath"
	"testing"
)

// Moved from pkg/agent/change_tracking_test.go (SP-141 phase 2): these
// groups test the tracker's own unexported helpers, so they live in the
// package that owns them.

// ---------------------------------------------------------------------------
// determineWriteOperation tests
// ---------------------------------------------------------------------------

func TestDetermineWriteOperation_Create(t *testing.T) {
	op := determineWriteOperation("", "new content")
	if op != "create" {
		t.Errorf("empty original should be 'create', got %q", op)
	}
}

func TestDetermineWriteOperation_Write(t *testing.T) {
	op := determineWriteOperation("old content", "new content")
	if op != "write" {
		t.Errorf("different content should be 'write', got %q", op)
	}
}

func TestDetermineWriteOperation_Overwrite(t *testing.T) {
	op := determineWriteOperation("same content", "same content")
	if op != "overwrite" {
		t.Errorf("identical content should be 'overwrite', got %q", op)
	}
}

// TestResolveAbsPath_AlreadyAbsolute returns cleaned absolute paths
// unchanged.
func TestResolveAbsPath_AlreadyAbsolute(t *testing.T) {
	ws := t.TempDir()
	ct := NewChangeTracker(nil, "")

	abs := filepath.Join(ws, "a", "b", "c.go")
	resolved := ct.resolveAbsPath(abs)
	if resolved != filepath.Clean(abs) {
		t.Errorf("absolute path should be cleaned but unchanged, got %q want %q", resolved, filepath.Clean(abs))
	}
}

// TestResolveAbsPath_UsesWorkspaceRoot resolves relative paths against
// the workspace root, not the process CWD.
func TestResolveAbsPath_UsesWorkspaceRoot(t *testing.T) {
	ws := t.TempDir()
	ct := NewChangeTracker(stubAgentView{workspaceRoot: ws}, "test instruction")

	// CWD is NOT the workspace — normalization must still use ws.
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	otherDir := t.TempDir()
	if err := os.Chdir(otherDir); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved := ct.resolveAbsPath("nested/file.go")
	expected := filepath.Join(ws, "nested", "file.go")
	if resolved != expected {
		t.Errorf("expected resolution against workspace root %q, got %q", expected, resolved)
	}
}

// TestResolveAbsPath_FallsBackToCwd uses CWD when workspace root is empty.
func TestResolveAbsPath_FallsBackToCwd(t *testing.T) {
	ct := NewChangeTracker(nil, "")

	origWd, _ := os.Getwd()
	defer func() { _ = os.Chdir(origWd) }()
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	resolved := ct.resolveAbsPath("file.go")
	// On macOS, t.TempDir() returns /var/folders/... (symlink to /private/var/...)
	// but os.Getwd() (used by resolveAbsPath) returns the resolved /private/var/...
	// form. Resolve the expected path to match.
	expected, err := filepath.EvalSymlinks(filepath.Join(tmp, "file.go"))
	if err != nil {
		// File doesn't exist; resolve just the directory.
		resolvedDir, _ := filepath.EvalSymlinks(tmp)
		expected = filepath.Join(resolvedDir, "file.go")
	}
	if resolved != expected {
		t.Errorf("expected CWD-based resolution %q, got %q", expected, resolved)
	}
}
