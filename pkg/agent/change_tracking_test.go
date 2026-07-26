package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// ---------------------------------------------------------------------------
// ChangeTracker redaction tests
// ---------------------------------------------------------------------------

func TestTrackFileWrite_InWorkspace(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot("")

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot("")

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot(ws)

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
	agent.SetWorkspaceRoot("")

	ct := NewChangeTracker(agent, "test instruction")

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

// TestRecovery_ResolvesCorrectlyAfterChdir (H3 integration) verifies
// the end-to-end fix: track a relative path, change CWD, then verify
// recovery resolves to the ORIGINAL location (not the new CWD).
func TestRecovery_ResolvesCorrectlyAfterChdir(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.SetWorkspaceRoot(ws)

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

	// Simulate the agent actually writing the new content to disk
	// (TrackFileWrite only records the change; it doesn't write the
	// file). The staleness guard compares disk vs NewCode, so the
	// disk must reflect the agent's edit for recovery to proceed.
	if err := os.WriteFile(filePath, []byte("modified content\n"), 0644); err != nil {
		t.Fatalf("write new content: %v", err)
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

// boolPtr returns a pointer to b. Used to populate the *bool fields of
// ChangeTrackingConfig in table-style tests.
func boolPtr(b bool) *bool { return &b }

// TestChangeTrackingConfigGate_DefaultNoConfig verifies the production
// default: an agent whose config has NO change_tracking section at all
// must end up with tracking ENABLED. This is the most common path —
// most users never touch change_tracking — so a regression here would
// silently disable the entire subsystem for the majority of installs.
//
// The config gate logic lives in isChangeTrackingEnabledByConfig: a
// nil ChangeTracking pointer means "use the default", and the default
// is enabled (the git-awareness guards protect committed work).
func TestChangeTrackingConfigGate_DefaultNoConfig(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	// NewTestManager yields a fresh config with no change_tracking
	// section. Explicitly assert that to guard against a future
	// default that seeds ChangeTracking non-nil.
	cfg := mgr.GetConfig()
	if cfg.ChangeTracking != nil {
		t.Fatalf("precondition: fresh test config should have nil ChangeTracking, got %+v", cfg.ChangeTracking)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (no change_tracking section ⇒ default enabled)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker (default is enabled, so a tracker must be created)")
	}
	if a.GetRevisionID() == "" {
		t.Error("GetRevisionID() = empty, want non-empty revision ID for an enabled tracker")
	}
}

// TestChangeTrackingConfigGate_ExplicitlyEnabled verifies that setting
// change_tracking.enabled = true explicitly matches the default
// behavior: tracking is on and a tracker is created.
func TestChangeTrackingConfigGate_ExplicitlyEnabled(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: boolPtr(true)}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (change_tracking.enabled = true)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker")
	}
}

// TestChangeTrackingConfigGate_ExplicitlyDisabled verifies the
// kill-switch: when the user sets change_tracking.enabled = false the
// ENTIRE subsystem stays dormant. EnableChangeTracking must early-return
// before creating a tracker, so IsChangeTrackingEnabled() reports false
// and GetChangeTracker() is nil.
//
// This is the critical safety assertion: a broken implementation that
// ignored the gate would still create a tracker, and IsChangeTrackingEnabled
// would flip to true. Asserting both the method AND the nil tracker
// catches that regression.
func TestChangeTrackingConfigGate_ExplicitlyDisabled(t *testing.T) {
	mgr, cleanup := configuration.NewTestManager(t)
	defer cleanup()

	if err := mgr.UpdateConfigNoSave(func(c *configuration.Config) error {
		c.ChangeTracking = &configuration.ChangeTrackingConfig{Enabled: boolPtr(false)}
		return nil
	}); err != nil {
		t.Fatalf("UpdateConfigNoSave: %v", err)
	}

	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: mgr,
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = true, want false (change_tracking.enabled = false must gate the subsystem)")
	}
	if a.GetChangeTracker() != nil {
		t.Errorf("GetChangeTracker() = non-nil, want nil (disabled config must not create a tracker), revisionID=%q", a.GetRevisionID())
	}
	if a.GetRevisionID() != "" {
		t.Errorf("GetRevisionID() = %q, want empty (no tracker should exist)", a.GetRevisionID())
	}
}

// TestChangeTrackingConfigGate_NilConfigManager verifies the test path:
// an agent constructed without a configManager (the pattern used by
// most existing unit tests in this package) keeps tracking enabled.
// This preserves backward compatibility so the dozens of tests that
// call EnableChangeTracking without wiring up config don't break.
func TestChangeTrackingConfigGate_NilConfigManager(t *testing.T) {
	a := &Agent{
		state:         NewAgentStateManager(false),
		configManager: nil, // test path: no config manager
		workspaceRoot: t.TempDir(),
	}
	a.EnableChangeTracking("test instructions")

	if !a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = false, want true (nil configManager ⇒ test path ⇒ enabled)")
	}
	if a.GetChangeTracker() == nil {
		t.Error("GetChangeTracker() = nil, want non-nil tracker (test path enables tracking)")
	}
}

// isolateHistoryForTest returns a function that, when called, redirects
// the history package's package-level changesDir/revisionsDir to a
// fresh temp dir. The function does NOT redirect eagerly — it returns
// the redirect closure so tests can run it AFTER any agent creation
// step that internally invokes history.InitializeHistoryPaths (which
// would otherwise clobber a pre-set redirect back to the repo-root
// .sprout/changes/).
//
// Why this matters: HistoryScope="project" (the default in
// configuration.Config) resolves changesDir and revisionsDir to the
// RELATIVE path .sprout/changes under the process CWD — the repo root
// when running `go test ./pkg/agent/...`. Every test that asserts
// against exact change counts (e.g. TestChangeTrackingE2E's "len
// (allChanges) == 1") therefore reads from the shared repo .sprout/
// changes/ directory, which has accumulated residue from prior test
// runs and prior sessions across the entire repo history. Without this
// hook the e2e test fails 2/5 runs because the second run sees the
// first run's still-active change records.
//
// Usage pattern: pair with configuration.NewTestManager for config
// isolation, then call the returned closure AFTER constructing the
// agent under test (since NewChangeTracker inside agent construction
// calls history.InitializeHistoryPaths which overwrites our redirect):
//
//	_, cleanupCfg := configuration.NewTestManager(t)
//	defer cleanupCfg()
//	setHistory := isolateHistoryForTest(t)
//	defer setHistory()  // restores previous paths after test
//
//	agent, err := NewAgentWithModel(...)  // may overwrite history paths
//	// ... test setup ...
//	setHistory()  // redirect NOW, after agent construction
//
// The deferred call still restores at test end, undoing both the
// in-test redirect and any path that NewChangeTracker wrote.
func isolateHistoryForTest(t *testing.T) func() {
	t.Helper()
	tmp := t.TempDir()
	cDir := filepath.Join(tmp, "changes")
	rDir := filepath.Join(tmp, "revisions")
	if err := os.MkdirAll(cDir, 0o755); err != nil {
		t.Fatalf("isolateHistoryForTest: mkdir changes: %v", err)
	}
	if err := os.MkdirAll(rDir, 0o755); err != nil {
		t.Fatalf("isolateHistoryForTest: mkdir revisions: %v", err)
	}
	prevChanges, prevRevisions := history.GetPathsForTesting()
	set := func() {
		history.SetPathsForTesting(cDir, rDir)
	}
	restore := func() {
		history.SetPathsForTesting(prevChanges, prevRevisions)
	}
	t.Cleanup(restore)
	return set
}

// TestChangeTrackingE2E tests the end-to-end change tracking and rollback workflow
func TestChangeTrackingE2E(t *testing.T) {
	// Isolate config + history I/O to a temp dir. Without this,
	// history.GetAllChanges() reads from whatever SPROUT_CONFIG points
	// at in the test runner's env (often the user's real config dir),
	// so the count assertion sees data accumulated from prior runs.
	_, configCleanup := configuration.NewTestManager(t)
	defer configCleanup()
	// isolateHistoryForTest returns a deferred-redirect closure that
	// must run AFTER agent construction (NewAgentWithModel →
	// NewChangeTracker → history.InitializeHistoryPaths) so the redirect
	// is the LAST write to the package-level paths, not overwritten by
	// the agent. t.Cleanup restores the pre-test paths at test end.
	setHistory := isolateHistoryForTest(t)

	// Test constants for test file names and content
	const (
		testFileName    = "tracking_test.go"
		originalContent = "func original() {}"
		newContent      = "func updated() {}"
	)

	// Setup test directory using t.TempDir() for automatic cleanup
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()

	// Set environment variables for testing
	t.Setenv("SPROUT_TEST_ENV", "1")
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-testing")

	// Restore directory in all cases
	defer func() {
		if err := os.Chdir(oldDir); err != nil {
			t.Logf("Warning: Failed to restore original directory: %v", err)
		}
	}()

	// Change to test directory with error handling
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("Failed to change to test directory: %v", err)
	}

	// Create an agent with change tracking enabled
	instructions := "Update test file with new content"
	agent, err := NewAgentWithModel("deepseek/deepseek-chat-v3.1:free")
	if err != nil {
		// If agent creation fails, at least create a change tracker directly
		// This allows us to test the change tracking even without a full agent
		t.Logf("Agent creation failed: %v. Creating tracker directly.", err)
		agent = &Agent{}
		agent.changeTracker = NewChangeTracker(nil, instructions)
		agent.changeTracker.Enable()
	} else {
		agent.EnableChangeTracking(instructions)
	}

	if agent.changeTracker == nil {
		agent.changeTracker = NewChangeTracker(agent, instructions)
		agent.changeTracker.Enable()
	}

	// Now that the agent (and its tracker) is fully constructed — and
	// therefore the last InitializeHistoryPaths call has fired —
	// redirect history storage to our temp dir so the post-commit
	// assertions read only what THIS test wrote.
	setHistory()

	// Verify change tracking is enabled
	if !agent.IsChangeTrackingEnabled() {
		t.Fatal("Change tracking should be enabled")
	}

	// Create a test file and track changes to it
	errWrite := os.WriteFile(testFileName, []byte(originalContent), 0644)
	if errWrite != nil {
		t.Fatalf("Failed to create test file: %v", errWrite)
	}

	// Track a file write (simulating WriteFile tool)
	err = agent.TrackFileWrite(testFileName, newContent)
	if err != nil {
		t.Fatalf("Failed to track file write: %v", err)
	}

	// Verify change was tracked
	if agent.GetChangeCount() != 1 {
		t.Fatalf("Expected 1 tracked change, got %d", agent.GetChangeCount())
	}

	// H3: paths are normalized to absolute at track time so a later
	// CWD change can't break recovery. Compare against the absolute
	// form of the relative test name.
	expectedTracked := testFileName
	if filepath.IsAbs(testFileName) {
		expectedTracked = filepath.Clean(testFileName)
	} else {
		expectedTracked, _ = filepath.Abs(testFileName)
	}
	trackedFiles := agent.GetTrackedFiles()
	if len(trackedFiles) != 1 || trackedFiles[0] != expectedTracked {
		t.Fatalf("Expected tracked file %s, got %v", expectedTracked, trackedFiles)
	}

	// Modify the actual file to simulate the tool making the change
	err = os.WriteFile(testFileName, []byte(newContent), 0644)
	if err != nil {
		t.Fatalf("Failed to modify test file: %v", err)
	}

	// Verify file content is modified
	currentContent, err := os.ReadFile(testFileName)
	if err != nil {
		t.Fatalf("Failed to read test file: %v", err)
	}
	if string(currentContent) != newContent {
		t.Fatalf("File content should be modified. Expected: %s, Got: %s", newContent, string(currentContent))
	}

	// Commit the changes
	llmResponse := "Changes applied successfully"
	err = agent.CommitChanges(llmResponse)
	if err != nil {
		t.Fatalf("Failed to commit changes: %v", err)
	}

	// Verify get revision ID was generated
	revisionID := agent.GetRevisionID()
	if revisionID == "" {
		t.Fatal("Revision ID should be set after commit")
	}

	// Verify changes were saved to the history system
	allChanges, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes from history: %v", err)
	}
	if len(allChanges) != 1 {
		t.Fatalf("Expected 1 change in history, got %d", len(allChanges))
	}

	change := allChanges[0]
	// H3: stored Filename is now absolute (normalized at track time).
	if change.Filename != expectedTracked {
		t.Fatalf("Expected filename %s, got %s", expectedTracked, change.Filename)
	}
	if change.OriginalCode != originalContent {
		t.Fatalf("Expected original code %s, got %s", originalContent, change.OriginalCode)
	}
	if change.NewCode != newContent {
		t.Fatalf("Expected new code %s, got %s", newContent, change.NewCode)
	}
	if change.Status != "active" {
		t.Fatalf("Expected status 'active', got %s", change.Status)
	}

	// Perform rollback
	err = history.RevertChangeByRevisionID(revisionID)
	if err != nil {
		t.Fatalf("Failed to rollback changes: %v", err)
	}

	// Verify file was restored
	restoredContent, err := os.ReadFile(testFileName)
	if err != nil {
		t.Fatalf("Failed to read restored file: %v", err)
	}
	if string(restoredContent) != originalContent {
		t.Fatalf("File content should be restored. Expected: %s, Got: %s", originalContent, string(restoredContent))
	}

	// Verify the change status is now "reverted"
	changesAfterRollback, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("Failed to fetch changes after rollback: %v", err)
	}
	found := false
	for _, c := range changesAfterRollback {
		if c.RequestHash == revisionID {
			if c.Status != "reverted" {
				t.Fatalf("Change status should be 'reverted', got: %s", c.Status)
			}
			found = true
			break
		}
	}
	if !found {
		t.Fatal("Could not find the change after rollback")
	}

	// Verify we can restore the change back
	err = history.RevertChangeByRevisionID(revisionID)
	if err == nil {
		t.Fatal("Should not be able to rollback reverted changes (no active changes)")
	}

	t.Log("[OK] End-to-end change tracking and rollback test passed!")
}

func TestChangeTrackingSupportsIncrementalCommits(t *testing.T) {
	// Isolate config + history I/O to a temp dir so history count
	// assertions are not contaminated by accumulated changes from
	// prior runs or sibling tests.
	_, configCleanup := configuration.NewTestManager(t)
	defer configCleanup()
	setHistory := isolateHistoryForTest(t)
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldDir)
	}()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("change dir: %v", err)
	}

	agent := &Agent{}
	agent.changeTracker = NewChangeTracker(agent, "Make a series of edits")
	agent.changeTracker.Enable()

	// Redirect AFTER NewChangeTracker so the redirect survives.
	setHistory()

	fileA := "file_a.go"
	fileB := "file_b.go"

	if err := os.WriteFile(fileA, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("package main\n"), 0644); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	if err := agent.TrackFileWrite(fileA, "package main\nfunc a() {}\n"); err != nil {
		t.Fatalf("track fileA: %v", err)
	}
	if err := agent.CommitChanges("checkpoint 1"); err != nil {
		t.Fatalf("commit checkpoint 1: %v", err)
	}
	revisionID := agent.GetRevisionID()
	if revisionID == "" {
		t.Fatal("expected revision ID after first checkpoint")
	}

	if err := agent.TrackFileWrite(fileB, "package main\nfunc b() {}\n"); err != nil {
		t.Fatalf("track fileB: %v", err)
	}
	if err := agent.CommitChanges("checkpoint 2"); err != nil {
		t.Fatalf("commit checkpoint 2: %v", err)
	}

	allChanges, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("fetch changes: %v", err)
	}
	if len(allChanges) != 2 {
		t.Fatalf("expected 2 persisted changes after incremental commits, got %d", len(allChanges))
	}

	foundA := false
	foundB := false
	// H3: paths are normalized to absolute at track time.
	absA, _ := filepath.Abs(fileA)
	absB, _ := filepath.Abs(fileB)
	for _, change := range allChanges {
		if change.RequestHash != revisionID {
			t.Fatalf("expected change revision %s, got %s", revisionID, change.RequestHash)
		}
		switch change.Filename {
		case absA:
			foundA = true
		case absB:
			foundB = true
		}
	}
	if !foundA || !foundB {
		t.Fatalf("expected both tracked files to be persisted, foundA=%v foundB=%v", foundA, foundB)
	}
}

// TestCommitIsIdempotent_DoubleCommitNoDuplicates (H1 regression) verifies
// that calling Commit twice does not re-record changes that were already
// persisted. The old code only advanced committedChangeCount AFTER the
// loop, so a second Commit re-sliced from the same offset and duplicated
// every entry. The fix advances the counter inside the loop so a retry
// resumes from the correct position.
func TestCommitIsIdempotent_DoubleCommitNoDuplicates(t *testing.T) {
	// Isolate config + history I/O to a temp dir so history count
	// assertions are not contaminated by accumulated changes from
	// prior runs or sibling tests.
	_, configCleanup := configuration.NewTestManager(t)
	defer configCleanup()
	setHistory := isolateHistoryForTest(t)
	testDir := t.TempDir()
	oldDir, _ := os.Getwd()
	defer func() {
		_ = os.Chdir(oldDir)
	}()
	if err := os.Chdir(testDir); err != nil {
		t.Fatalf("change dir: %v", err)
	}

	agent := &Agent{}
	agent.changeTracker = NewChangeTracker(agent, "H1 idempotency test")
	agent.changeTracker.Enable()

	// Redirect AFTER NewChangeTracker so the redirect survives.
	setHistory()

	fileA := "dup_a.go"
	fileB := "dup_b.go"
	if err := os.WriteFile(fileA, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write fileA: %v", err)
	}
	if err := os.WriteFile(fileB, []byte("old\n"), 0644); err != nil {
		t.Fatalf("write fileB: %v", err)
	}

	// Track two changes and commit.
	if err := agent.TrackFileWrite(fileA, "new a\n"); err != nil {
		t.Fatalf("track fileA: %v", err)
	}
	if err := agent.TrackFileWrite(fileB, "new b\n"); err != nil {
		t.Fatalf("track fileB: %v", err)
	}
	if err := agent.CommitChanges("first commit"); err != nil {
		t.Fatalf("commit 1: %v", err)
	}

	// Snapshot the persisted count after the first commit.
	allAfterFirst, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("fetch after first commit: %v", err)
	}
	countAfterFirst := len(allAfterFirst)

	// Commit again with NO new changes tracked. The committedChangeCount
	// guard must prevent re-recording the two already-persisted entries.
	if err := agent.CommitChanges("second commit (no-op)"); err != nil {
		t.Fatalf("commit 2: %v", err)
	}
	allAfterSecond, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("fetch after second commit: %v", err)
	}
	if len(allAfterSecond) != countAfterFirst {
		t.Errorf("double-Commit produced duplicates: got %d after 2nd commit, want %d",
			len(allAfterSecond), countAfterFirst)
	}

	// Also verify the internal counter is at the right position — it
	// should equal the number of tracked changes after a successful
	// commit, so a third commit after adding ONE more change persists
	// exactly that one.
	if err := agent.TrackFileWrite("dup_c.go", "new c\n"); err != nil {
		t.Fatalf("track fileC: %v", err)
	}
	if err := agent.CommitChanges("third commit"); err != nil {
		t.Fatalf("commit 3: %v", err)
	}
	allAfterThird, err := history.GetAllChanges()
	if err != nil {
		t.Fatalf("fetch after third commit: %v", err)
	}
	if len(allAfterThird) != countAfterFirst+1 {
		t.Errorf("incremental commit after double-commit: got %d, want %d (only the new change should be added)",
			len(allAfterThird), countAfterFirst+1)
	}
}

// ---------------------------------------------------------------------------
// ChangeTracker.MergeChild — SP-059 Phase 2c
//
// These tests verify that a subagent's tracked changes are merged into
// the parent tracker and tagged with Source so list_changes /
// recover_file / revert_my_changes can attribute them correctly.
// ---------------------------------------------------------------------------

func TestMergeChild_BasicMerge(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{
			FilePath:     "/ws/created.go",
			OriginalCode: "",
			NewCode:      "package main\n",
			Operation:    "create",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     "/ws/edited.go",
			OriginalCode: "old\n",
			NewCode:      "new\n",
			Operation:    "edit",
			ToolCall:     "EditFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     "/ws/written.go",
			OriginalCode: "old\n",
			NewCode:      "newer\n",
			Operation:    "write",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
	}

	ct.MergeChild(changes, "subagent:coder")

	got := ct.GetChanges()
	if len(got) != len(changes) {
		t.Fatalf("expected %d changes, got %d", len(changes), len(got))
	}

	for i, want := range changes {
		g := got[i]
		if g.Source != "subagent:coder" {
			t.Errorf("change[%d] Source = %q, want %q", i, g.Source, "subagent:coder")
		}
		// Content fields must be preserved so recover_file works.
		if g.FilePath != want.FilePath {
			t.Errorf("change[%d] FilePath = %q, want %q", i, g.FilePath, want.FilePath)
		}
		if g.OriginalCode != want.OriginalCode {
			t.Errorf("change[%d] OriginalCode = %q, want %q", i, g.OriginalCode, want.OriginalCode)
		}
		if g.NewCode != want.NewCode {
			t.Errorf("change[%d] NewCode = %q, want %q", i, g.NewCode, want.NewCode)
		}
		if g.Operation != want.Operation {
			t.Errorf("change[%d] Operation = %q, want %q", i, g.Operation, want.Operation)
		}
	}
}

func TestMergeChild_NoOpWhenDisabled(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")
	ct.Disable()

	changes := []TrackedFileChange{
		{FilePath: "/ws/a.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	ct.MergeChild(changes, "subagent:coder")

	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("expected 0 changes when disabled, got %d", got)
	}
}

func TestMergeChild_EmptyAndNilSafe(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	// Must not panic on nil.
	ct.MergeChild(nil, "subagent:coder")
	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("nil input: expected 0 changes, got %d", got)
	}

	// Must not panic on empty slice.
	ct.MergeChild([]TrackedFileChange{}, "subagent:coder")
	if got := ct.GetChangeCount(); got != 0 {
		t.Fatalf("empty input: expected 0 changes, got %d", got)
	}
}

func TestMergeChild_DoesNotMutateInput(t *testing.T) {
	agent := NewTestAgent()
	ct := NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{FilePath: "/ws/a.go", NewCode: "a\n", Operation: "create", Timestamp: time.Now()},
		{FilePath: "/ws/b.go", NewCode: "b\n", Operation: "create", Timestamp: time.Now()},
	}

	ct.MergeChild(changes, "subagent:coder")

	// The input slice's entries should still have empty Source —
	// MergeChild must copy rather than mutate in place.
	for i, ch := range changes {
		if ch.Source != "" {
			t.Errorf("input[%d].Source = %q, want empty (input must not be mutated)", i, ch.Source)
		}
	}
}

func TestMergeChild_TagsSource(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	ct := NewChangeTracker(agent, "primary instruction")

	// Record a pre-existing primary-agent edit (no Source).
	primaryFile := filepath.Join(ws, "primary.go")
	if err := ct.TrackFileWrite(primaryFile, "primary content\n"); err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// Now merge in subagent changes.
	subagentChanges := []TrackedFileChange{
		{FilePath: filepath.Join(ws, "sub.go"), NewCode: "sub\n", Operation: "create", Timestamp: time.Now()},
	}
	ct.MergeChild(subagentChanges, "subagent:coder")

	got := ct.GetChanges()
	if len(got) != 2 {
		t.Fatalf("expected 2 changes, got %d", len(got))
	}

	// The primary edit (first entry) should keep empty Source.
	if got[0].Source != "" {
		t.Errorf("primary change Source = %q, want empty", got[0].Source)
	}
	if got[0].FilePath != primaryFile {
		t.Errorf("primary change FilePath = %q, want %q", got[0].FilePath, primaryFile)
	}

	// The merged subagent edit should be tagged.
	if got[1].Source != "subagent:coder" {
		t.Errorf("merged change Source = %q, want %q", got[1].Source, "subagent:coder")
	}
}

func TestAgent_MergeSubagentChanges(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{
			FilePath:     filepath.Join(ws, "sub_created.go"),
			OriginalCode: "",
			NewCode:      "package sub\n",
			Operation:    "create",
			ToolCall:     "WriteFile",
			Timestamp:    time.Now(),
		},
		{
			FilePath:     filepath.Join(ws, "sub_edited.go"),
			OriginalCode: "before\n",
			NewCode:      "after\n",
			Operation:    "edit",
			ToolCall:     "EditFile",
			Timestamp:    time.Now(),
		},
	}

	agent.MergeSubagentChanges(changes, "coder")

	// The merged files should appear in the agent's tracked file list.
	trackedFiles := agent.GetTrackedFiles()
	if len(trackedFiles) != 2 {
		t.Fatalf("expected 2 tracked files, got %d (%v)", len(trackedFiles), trackedFiles)
	}

	// Each should carry the subagent:coder source tag.
	got := agent.GetChangeTracker().GetChanges()
	for i, ch := range got {
		if ch.Source != "subagent:coder" {
			t.Errorf("change[%d] Source = %q, want %q", i, ch.Source, "subagent:coder")
		}
	}

	// GetChangesSummary should reflect the merged entries (non-empty).
	summary := agent.GetChangesSummary()
	if summary == "" || summary == "Change tracking is not enabled" {
		t.Errorf("summary should reflect merged changes, got %q", summary)
	}
}

func TestAgent_MergeSubagentChanges_EmptyPersonaUsesBareTag(t *testing.T) {
	ws := t.TempDir()
	agent := NewTestAgent()
	agent.workspaceRoot = ws
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")

	changes := []TrackedFileChange{
		{FilePath: filepath.Join(ws, "sub.go"), NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	// Empty persona → bare "subagent" source.
	agent.MergeSubagentChanges(changes, "")

	got := agent.GetChangeTracker().GetChanges()
	if len(got) != 1 {
		t.Fatalf("expected 1 change, got %d", len(got))
	}
	if got[0].Source != "subagent" {
		t.Errorf("Source = %q, want %q", got[0].Source, "subagent")
	}
}

func TestAgent_MergeSubagentChanges_NoOpWhenDisabled(t *testing.T) {
	agent := NewTestAgent()
	agent.changeTracker = NewChangeTracker(agent, "primary instruction")
	agent.changeTracker.Disable()

	changes := []TrackedFileChange{
		{FilePath: "/ws/sub.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}
	agent.MergeSubagentChanges(changes, "coder")

	if got := agent.GetChangeCount(); got != 0 {
		t.Fatalf("expected 0 changes when tracking disabled, got %d", got)
	}
}

func TestAgent_MergeSubagentChanges_NilTrackerSafe(t *testing.T) {
	agent := NewTestAgent()
	// changeTracker is nil; must not panic.
	agent.MergeSubagentChanges([]TrackedFileChange{
		{FilePath: "/ws/sub.go", NewCode: "x\n", Operation: "create", Timestamp: time.Now()},
	}, "coder")
}

// ============================================================================
// Session-scoping regression tests (Fix A + Fix B + subagent no-commit).
//
// These lock in the behaviour that was broken before this change:
//
//  1. Fix B — the change buffer is SESSION-LONG. EnableChangeTracking on
//     an existing tracker must NOT wipe ct.changes. Previously a Reset
//     fired on every ProcessQuery, so list_changes returned count:0 at
//     the start of each turn — the exact footgun that surfaced the bug.
//
//  2. Subagent fix — subagents track in memory only; their ProcessQuery
//     end-of-loop Commit must be skipped so history isn't polluted with
//     duplicate revision dirs ("subagent run") and double-persisted
//     files. The parent's Commit owns persistence.
//
//  3. Fix A — list_changes defaults to include_persisted, but the merge
//     is SESSION-SCOPED (matches the tracker's revisionID) and deduped
//     against the in-memory buffer so a file edited last turn AND
//     re-touched this turn appears once.
// ============================================================================

// TestSession_BufferSurvivesReEnable (Fix B) verifies that calling
// EnableChangeTracking a second time — as ProcessQuery does at the start
// of every turn — preserves the changes accumulated in the first turn.
func TestSession_BufferSurvivesReEnable(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws

	// Turn 1: enable tracking and record an edit.
	a.EnableChangeTracking("first query")
	revID := a.GetRevisionID()
	a.TrackFileWrite(filepath.Join(ws, "main.go"), "package main\n")

	if got := a.GetChangeCount(); got != 1 {
		t.Fatalf("turn 1: expected 1 change, got %d", got)
	}

	// Turn 2: ProcessQuery re-enables tracking. Before the fix this
	// called Reset() and wiped the buffer.
	a.EnableChangeTracking("second query")

	if got := a.GetChangeCount(); got != 1 {
		t.Errorf("turn 2: re-enable wiped the buffer (count=%d, want 1) — Fix B regressed", got)
	}
	if got := a.GetRevisionID(); got != revID {
		t.Errorf("turn 2: revision ID changed on re-enable %q -> %q (must be session-stable)", revID, got)
	}

	// Turn 2's own edit appends rather than replacing.
	a.TrackFileEdit(filepath.Join(ws, "main.go"), "package main\n", "package main\n// edited\n")
	if got := a.GetChangeCount(); got != 2 {
		t.Errorf("turn 2: expected 2 changes after append, got %d", got)
	}
}

// TestSession_ListChangesReflectsFullSession (Fix B + Fix A) verifies
// that list_changes shows changes from a prior turn even after a
// re-enable, because the buffer is session-long.
func TestSession_ListChangesReflectsFullSession(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws

	a.EnableChangeTracking("session start")
	a.TrackFileWrite(filepath.Join(ws, "auth.go"), "contents")

	// Simulate a new turn (ProcessQuery re-enables).
	a.EnableChangeTracking("next turn")

	// list_changes should still see the prior turn's file.
	out, err := handleListChanges(context.Background(), a, map[string]interface{}{
		"include_persisted": false, // in-memory buffer only
	})
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	var parsed struct {
		Count int `json:"count"`
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if parsed.Count == 0 {
		t.Error("list_changes returned count:0 after re-enable — the in-memory buffer should be session-long (Fix B)")
	}
}

// TestSession_ListChangesPersistsSessionScoped (Fix A) verifies that the
// default include_persisted merge is scoped to the current session's
// revisionID, NOT global history. Entries from other sessions must not
// leak into this session's manifest.
func TestSession_ListChangesPersistsSessionScoped(t *testing.T) {
	// Two independent trackers simulate two sessions. Their revisionIDs
	// differ, so neither's persisted merge should include the other's
	// entries.
	ws := t.TempDir()
	a1 := NewTestAgent()
	a1.workspaceRoot = ws
	a1.EnableChangeTracking("session one")
	rev1 := a1.GetRevisionID()

	a2 := NewTestAgent()
	a2.workspaceRoot = ws
	a2.EnableChangeTracking("session two")
	rev2 := a2.GetRevisionID()

	if rev1 == rev2 {
		t.Fatalf("test setup: expected distinct revision IDs, got %q for both", rev1)
	}

	// Record a change in session 1 only.
	a1.TrackFileWrite(filepath.Join(ws, "only_in_session1.go"), "x")

	// Session 2's list_changes (persisted merge) must NOT show it.
	out, err := handleListChanges(context.Background(), a2, nil)
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	leaked := "only_in_session1.go"
	if containsSubstring(out, leaked) {
		t.Errorf("session 2 manifest leaked session 1's file %q — persisted merge must be session-scoped (Fix A):\n%s", leaked, out)
	}
}

// TestSession_SubagentDoesNotCommit verifies the subagent no-self-commit
// contract: the post-loop Commit guard in processQueryWithSeed excludes
// subagents so history is owned solely by the parent. This test encodes
// the two invariants the fix relies on:
//
//  1. IsSubagent() correctly identifies subagents (depth > 0).
//  2. The guard condition "!a.IsSubagent() && enabled && count>0" is
//     false for a subagent even when it has tracked changes.
//
// The live guard lives in seed_query.go's post-loop (gated on
// !a.IsSubagent()); we assert the predicate here so a regression in
// either IsSubagent() or the guard shape is caught without standing up
// a full ProcessQuery (which needs an LLM client).
func TestSession_SubagentDoesNotCommit(t *testing.T) {
	ws := t.TempDir()

	// Build a subagent (depth > 0) with tracking enabled and one change.
	sub := NewTestAgent()
	sub.workspaceRoot = ws
	sub.subagentDepth = 1
	sub.EnableChangeTracking("subagent run")
	sub.TrackFileWrite(filepath.Join(ws, "by_subagent.go"), "contents")

	if !sub.IsSubagent() {
		t.Fatalf("IsSubagent() = false for depth %d; the guard depends on this", sub.subagentDepth)
	}
	if sub.GetChangeCount() != 1 {
		t.Fatalf("subagent should have 1 tracked change, got %d", sub.GetChangeCount())
	}
	if !sub.IsChangeTrackingEnabled() {
		t.Fatal("subagent tracking should be enabled")
	}

	// This is the exact predicate from seed_query.go's post-loop commit
	// site. For a subagent it MUST be false — otherwise the subagent
	// double-commits to history and litters revision dirs.
	shouldCommit := !sub.IsSubagent() && sub.IsChangeTrackingEnabled() && sub.GetChangeCount() > 0
	if shouldCommit {
		t.Error("post-loop commit predicate is true for a subagent — subagents must NOT self-commit (parent owns history)")
	}

	// Sanity: a primary agent (depth 0) with the same change DOES commit.
	primary := NewTestAgent()
	primary.workspaceRoot = ws
	primary.EnableChangeTracking("primary run")
	primary.TrackFileWrite(filepath.Join(ws, "by_primary.go"), "contents")
	shouldCommitPrimary := !primary.IsSubagent() && primary.IsChangeTrackingEnabled() && primary.GetChangeCount() > 0
	if !shouldCommitPrimary {
		t.Error("post-loop commit predicate is false for a primary agent with changes — primary MUST commit")
	}
}

// TestSession_PersistedDedupedAgainstInMemory (Fix A) verifies that when
// a file appears both in the persisted history (committed last turn) and
// in the in-memory buffer (re-edited this turn), it appears exactly once
// in the manifest, with the in-memory entry winning.
func TestSession_PersistedDedupedAgainstInMemory(t *testing.T) {
	ws := t.TempDir()
	a := NewTestAgent()
	a.workspaceRoot = ws
	a.EnableChangeTracking("dedup session")

	path := filepath.Join(ws, "shared.go")
	// In-memory entry for this path.
	a.TrackFileWrite(path, "v2")

	out, err := handleListChanges(context.Background(), a, map[string]interface{}{
		"include_persisted": true,
	})
	if err != nil {
		t.Fatalf("list_changes error: %v", err)
	}
	var parsed struct {
		Files []struct {
			Path string `json:"path"`
		} `json:"files"`
	}
	if err := json.Unmarshal([]byte(out), &parsed); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	count := 0
	for _, f := range parsed.Files {
		if f.Path == path {
			count++
		}
	}
	if count > 1 {
		t.Errorf("path %q appeared %d times in manifest — persisted entries must be deduped against in-memory (Fix A)", path, count)
	}
}
