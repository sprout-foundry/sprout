package changes

import (
	"os"
	"path/filepath"
	"testing"
)

// change_tracking_pure_test.go — pure (agent-free) unit tests for the
// change-tracking cluster (SP-141 phase 2). Moved from pkg/agent so they
// can reach unexported ChangeTracker members; NewTestAgent+SetWorkspaceRoot
// became the local fakeAgent double.

func TestIsOutsideWorkspace_NilAgent(t *testing.T) {
	ct := &ChangeTracker{
		enabled: true,
		agent:   nil,
	}

	// Should not panic and should return false (don't redact)
	result := ct.isOutsideWorkspace("/tmp/file.txt")
	if result {
		t.Errorf("nil agent should not redact, got isOutsideWorkspace = true")
	}
}

func TestIsOutsideWorkspace_NestedPathInWorkspace(t *testing.T) {
	ws := t.TempDir()
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ws}, "test")

	// Deeply nested path inside workspace should not be redacted
	nestedPath := filepath.Join(ws, "a", "b", "c", "d", "file.go")
	result := ct.isOutsideWorkspace(nestedPath)
	if result {
		t.Errorf("nested path inside workspace should not be redacted")
	}
}

func TestIsOutsideWorkspace_SiblingDirectory(t *testing.T) {
	ws := t.TempDir()
	siblingDir := t.TempDir()
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ws}, "test")

	// A file in a sibling directory should be redacted
	filePath := filepath.Join(siblingDir, "file.go")
	result := ct.isOutsideWorkspace(filePath)
	if !result {
		t.Errorf("sibling directory should be redacted")
	}
}

func TestIsOutsideWorkspace_WorkspaceRootIsParentOfFile(t *testing.T) {
	ws := t.TempDir()
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ws}, "test")

	// File directly in workspace root
	filePath := filepath.Join(ws, "file.go")
	result := ct.isOutsideWorkspace(filePath)
	if result {
		t.Errorf("file in workspace root should not be redacted")
	}
}

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
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ws}, "test instruction")

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
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ws}, "test instruction")

	// CWD is NOT the workspace — normalization must still use ws.
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	otherDir := t.TempDir()
	os.Chdir(otherDir)

	resolved := ct.resolveAbsPath("nested/file.go")
	expected := filepath.Join(ws, "nested", "file.go")
	if resolved != expected {
		t.Errorf("expected resolution against workspace root %q, got %q", expected, resolved)
	}
}

// TestResolveAbsPath_FallsBackToCwd uses CWD when workspace root is empty.
func TestResolveAbsPath_FallsBackToCwd(t *testing.T) {
	ct := NewChangeTracker(&fakeAgent{workspaceRoot: ""}, "test instruction")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	tmp := t.TempDir()
	os.Chdir(tmp)

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
