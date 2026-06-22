package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ---------------------------------------------------------------------------
// ChangeTracker redaction tests
// ---------------------------------------------------------------------------

func TestTrackFileWrite_InWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Create a file inside the workspace
	filePath := filepath.Join(ws, "test.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	newContent := "package main\n\nfunc main() {}\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode == RedactedContentMarker {
		t.Errorf("in-workspace file should NOT be redacted, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode == RedactedContentMarker {
		t.Errorf("in-workspace file should NOT be redacted, got NewCode = %q", change.NewCode)
	}
	if change.OriginalCode != "package main\n" {
		t.Errorf("OriginalCode = %q, want %q", change.OriginalCode, "package main\n")
	}
	if change.NewCode != newContent {
		t.Errorf("NewCode = %q, want %q", change.NewCode, newContent)
	}
}

func TestTrackFileWrite_OutOfWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Create a file outside the workspace
	externalDir := t.TempDir()
	filePath := filepath.Join(externalDir, "secrets.txt")
	originalContent := "AWS_SECRET_KEY=abc123"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("failed to create external file: %v", err)
	}

	newContent := "AWS_SECRET_KEY=xyz789"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != RedactedContentMarker {
		t.Errorf("out-of-workspace file should be redacted, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode != RedactedContentMarker {
		t.Errorf("out-of-workspace file should be redacted, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileWrite_NewFileInWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Write a new file that doesn't exist yet (inside workspace)
	filePath := filepath.Join(ws, "newfile.go")
	newContent := "package main\n"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != "" {
		t.Errorf("new file should have empty OriginalCode, got %q", change.OriginalCode)
	}
	if change.NewCode != newContent {
		t.Errorf("NewCode = %q, want %q", change.NewCode, newContent)
	}
	if change.Operation != "create" {
		t.Errorf("Operation = %q, want %q", change.Operation, "create")
	}
}

func TestTrackFileWrite_NewFileOutOfWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Write a new file outside workspace
	externalDir := t.TempDir()
	filePath := filepath.Join(externalDir, "new_secrets.txt")
	newContent := "secret=value"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != RedactedContentMarker {
		t.Errorf("out-of-workspace new file should be redacted, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode != RedactedContentMarker {
		t.Errorf("out-of-workspace new file should be redacted, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileWrite_EmptyWorkspaceRoot(t *testing.T) {
	// When workspaceRoot is empty, files should NOT be redacted
	agent := NewTestAgent()
	agent.workspaceRoot = ""

	ct := NewChangeTracker(agent, "test instruction")

	// Create a file in /tmp (outside any workspace)
	externalDir := t.TempDir()
	filePath := filepath.Join(externalDir, "file.txt")
	originalContent := "sensitive data"
	if err := os.WriteFile(filePath, []byte(originalContent), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	newContent := "updated data"
	if err := ct.TrackFileWrite(filePath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode == RedactedContentMarker {
		t.Errorf("empty workspace should NOT redact, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode == RedactedContentMarker {
		t.Errorf("empty workspace should NOT redact, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileWrite_RelativePathInWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Change to workspace dir so relative path resolves inside workspace
	origWd, _ := os.Getwd()
	os.Chdir(ws)
	defer os.Chdir(origWd)

	// Create a file with relative path inside workspace
	relPath := "subdir/file.go"
	subDir := filepath.Join(ws, "subdir")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create subdir: %v", err)
	}
	filePath := filepath.Join(subDir, "file.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	newContent := "package main\n\nfunc main() {}\n"
	if err := ct.TrackFileWrite(relPath, newContent); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode == RedactedContentMarker {
		t.Errorf("relative path resolving inside workspace should NOT be redacted, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode == RedactedContentMarker {
		t.Errorf("relative path resolving inside workspace should NOT be redacted, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileWrite_Disabled(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")
	ct.Disable()

	filePath := filepath.Join(ws, "test.go")
	if err := os.WriteFile(filePath, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("failed to create file: %v", err)
	}

	if err := ct.TrackFileWrite(filePath, "new content"); err != nil {
		t.Fatalf("TrackFileWrite on disabled tracker: %v", err)
	}

	if ct.GetChangeCount() != 0 {
		t.Errorf("disabled tracker should have 0 changes, got %d", ct.GetChangeCount())
	}
}

// ---------------------------------------------------------------------------
// TrackFileEdit tests
// ---------------------------------------------------------------------------

func TestTrackFileEdit_InWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	originalContent := "func old() {}\n"
	newContent := "func new() {}\n"

	if err := ct.TrackFileEdit(filepath.Join(ws, "file.go"), originalContent, newContent); err != nil {
		t.Fatalf("TrackFileEdit: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != originalContent {
		t.Errorf("OriginalCode = %q, want %q", change.OriginalCode, originalContent)
	}
	if change.NewCode != newContent {
		t.Errorf("NewCode = %q, want %q", change.NewCode, newContent)
	}
	if change.Operation != "edit" {
		t.Errorf("Operation = %q, want %q", change.Operation, "edit")
	}
}

func TestTrackFileEdit_OutOfWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	originalContent := "SECRET=abc123"
	newContent := "SECRET=xyz789"

	if err := ct.TrackFileEdit("/etc/shadow", originalContent, newContent); err != nil {
		t.Fatalf("TrackFileEdit: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode != RedactedContentMarker {
		t.Errorf("out-of-workspace edit should be redacted, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode != RedactedContentMarker {
		t.Errorf("out-of-workspace edit should be redacted, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileEdit_EmptyWorkspaceRoot(t *testing.T) {
	agent := NewTestAgent()
	agent.workspaceRoot = ""

	ct := NewChangeTracker(agent, "test instruction")

	originalContent := "data"
	newContent := "updated"

	if err := ct.TrackFileEdit("/tmp/file.txt", originalContent, newContent); err != nil {
		t.Fatalf("TrackFileEdit: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	change := changes[0]
	if change.OriginalCode == RedactedContentMarker {
		t.Errorf("empty workspace should NOT redact, got OriginalCode = %q", change.OriginalCode)
	}
	if change.NewCode == RedactedContentMarker {
		t.Errorf("empty workspace should NOT redact, got NewCode = %q", change.NewCode)
	}
}

func TestTrackFileEdit_Disabled(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")
	ct.Disable()

	if err := ct.TrackFileEdit(filepath.Join(ws, "file.go"), "old", "new"); err != nil {
		t.Fatalf("TrackFileEdit on disabled tracker: %v", err)
	}

	if ct.GetChangeCount() != 0 {
		t.Errorf("disabled tracker should have 0 changes, got %d", ct.GetChangeCount())
	}
}

// ---------------------------------------------------------------------------
// isOutsideWorkspace edge cases
// ---------------------------------------------------------------------------

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
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")

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
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")

	// A file in a sibling directory should be redacted
	filePath := filepath.Join(siblingDir, "file.go")
	result := ct.isOutsideWorkspace(filePath)
	if !result {
		t.Errorf("sibling directory should be redacted")
	}
}

func TestIsOutsideWorkspace_WorkspaceRootIsParentOfFile(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")

	// File directly in workspace root
	filePath := filepath.Join(ws, "file.go")
	result := ct.isOutsideWorkspace(filePath)
	if result {
		t.Errorf("file in workspace root should not be redacted")
	}
}

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

// ---------------------------------------------------------------------------
// GetTrackedFiles / GetChangeCount / Clear / Reset
// ---------------------------------------------------------------------------

func TestGetTrackedFiles(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")

	file1 := filepath.Join(ws, "a.go")
	file2 := filepath.Join(ws, "b.go")

	ct.TrackFileWrite(file1, "content1")
	ct.TrackFileWrite(file2, "content2")

	files := ct.GetTrackedFiles()
	if len(files) != 2 {
		t.Fatalf("expected 2 files, got %d", len(files))
	}
	if files[0] != file1 || files[1] != file2 {
		t.Errorf("unexpected files: %v", files)
	}
}

func TestClear(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")
	ct.TrackFileWrite(filepath.Join(ws, "a.go"), "content")

	if ct.GetChangeCount() != 1 {
		t.Fatalf("expected 1 change before clear")
	}

	ct.Clear()

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 changes after clear, got %d", ct.GetChangeCount())
	}
}

func TestReset(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "old instruction")
	oldID := ct.GetRevisionID()
	ct.TrackFileWrite(filepath.Join(ws, "a.go"), "content")

	ct.Reset("new instruction")

	if ct.GetChangeCount() != 0 {
		t.Errorf("expected 0 changes after reset, got %d", ct.GetChangeCount())
	}
	if ct.GetRevisionID() == oldID {
		t.Errorf("revision ID should change after reset")
	}
}

// ---------------------------------------------------------------------------
// Enable / Disable / IsEnabled
// ---------------------------------------------------------------------------

func TestEnableDisable(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test")

	if !ct.IsEnabled() {
		t.Fatalf("new tracker should be enabled by default")
	}

	ct.Disable()
	if ct.IsEnabled() {
		t.Errorf("tracker should be disabled")
	}

	ct.Enable()
	if !ct.IsEnabled() {
		t.Errorf("tracker should be enabled again")
	}
}

// ---------------------------------------------------------------------------
// H3: Path normalization at track time
// ---------------------------------------------------------------------------

// TestTrackFileWrite_RelativePathNormalizedToAbsolute (H3) verifies that
// a relative path passed to TrackFileWrite is stored as an absolute path.
// Without this, a later CWD change would make recovery resolve the
// relative path to the wrong location.
func TestTrackFileWrite_RelativePathNormalizedToAbsolute(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	// Track a relative path (as the LLM typically provides).
	relPath := "pkg/agent/foo.go"
	if err := ct.TrackFileWrite(relPath, "package main\n"); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	stored := changes[0].FilePath
	if !filepath.IsAbs(stored) {
		t.Errorf("stored FilePath should be absolute, got %q", stored)
	}

	// The absolute path should resolve under the workspace root.
	expected, err := filepath.Abs(filepath.Join(ws, "pkg/agent/foo.go"))
	if err != nil {
		t.Fatalf("filepath.Abs: %v", err)
	}
	if stored != expected {
		t.Errorf("stored FilePath = %q, want %q", stored, expected)
	}
}

// TestTrackFileEdit_RelativePathNormalizedToAbsolute (H3) verifies the
// same normalization applies to TrackFileEdit.
func TestTrackFileEdit_RelativePathNormalizedToAbsolute(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

	relPath := "src/main.go"
	if err := ct.TrackFileEdit(relPath, "old", "new"); err != nil {
		t.Fatalf("TrackFileEdit: %v", err)
	}

	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}

	stored := changes[0].FilePath
	if !filepath.IsAbs(stored) {
		t.Errorf("stored FilePath should be absolute, got %q", stored)
	}
}

// TestResolveAbsPath_AlreadyAbsolute returns cleaned absolute paths
// unchanged.
func TestResolveAbsPath_AlreadyAbsolute(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

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
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")

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
	agent := NewTestAgent()
	agent.workspaceRoot = ""

	ct := NewChangeTracker(agent, "test instruction")

	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	tmp := t.TempDir()
	os.Chdir(tmp)

	resolved := ct.resolveAbsPath("file.go")
	expected := filepath.Join(tmp, "file.go")
	if resolved != expected {
		t.Errorf("expected CWD-based resolution %q, got %q", expected, resolved)
	}
}

// TestRecovery_ResolvesCorrectlyAfterChdir (H3 integration) verifies
// the end-to-end fix: track a relative path, change CWD, then verify
// recovery resolves to the ORIGINAL location (not the new CWD).
func TestRecovery_ResolvesCorrectlyAfterChdir(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws

	ct := NewChangeTracker(agent, "test instruction")
	agent.changeTracker = ct

	// Track a relative path while CWD == workspace.
	origWd, _ := os.Getwd()
	defer os.Chdir(origWd)
	os.Chdir(ws)

	// Create the file so it exists for tracking.
	relPath := "target.go"
	filePath := filepath.Join(ws, relPath)
	if err := os.WriteFile(filePath, []byte("original content\n"), 0644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	if err := ct.TrackFileWrite(relPath, "modified content\n"); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// Simulate a `cd` to a different directory.
	otherDir := t.TempDir()
	os.Chdir(otherDir)

	// The stored path must be absolute so recovery resolves to the
	// ORIGINAL location regardless of CWD.
	changes := ct.GetChanges()
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	stored := changes[0].FilePath
	if !filepath.IsAbs(stored) {
		t.Fatalf("stored path must be absolute for CWD-independent recovery, got %q", stored)
	}

	// Recovery via handleRecoverFile must resolve to the original
	// workspace path, not the new CWD.
	result, err := handleRecoverFile(nil, agent, map[string]interface{}{
		"path":  stored,
		"scope": "latest",
	})
	if err != nil {
		t.Fatalf("handleRecoverFile: %v", err)
	}

	// The result should report a successful restore.
	if !strings.Contains(result, `"recovered": true`) {
		t.Errorf("expected recovery to succeed, got: %s", result)
	}
	// The path in the result should be the ORIGINAL workspace location.
	if !strings.Contains(result, `"path": "`+filePath) {
		t.Errorf("expected recovery path to be %q, got: %s", filePath, result)
	}

	// Verify the file on disk was restored at the original location.
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("read file at original location: %v", err)
	}
	if string(content) != "original content\n" {
		t.Errorf("file at original location should be restored, got %q", string(content))
	}
}
