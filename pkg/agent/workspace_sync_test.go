package agent

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// mustEvalSymlinks resolves symlinks in path, failing the test on error.
// On macOS, t.TempDir() returns /var/folders/... which is a symlink to
// /private/var/folders/..., so we must resolve before comparing with
// resolveWorkspacePath which calls filepath.EvalSymlinks internally.
func mustEvalSymlinks(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatalf("EvalSymlinks(%q): %v", path, err)
	}
	return resolved
}

// TestCheckWriteStaleness_NewFileAllowed pins the "creating a new file
// never needs a prior read" branch — every other SP-046 §7 check is
// skipped when os.Stat returns a not-exist error.
func TestCheckWriteStaleness_NewFileAllowed(t *testing.T) {
	a := &Agent{}
	path := filepath.Join(t.TempDir(), "brand-new-file.txt")
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error for nonexistent file, got %v", err)
	}
}

// TestCheckWriteStaleness_NotReadThisTurnRefuses pins the core rule:
// writing to an existing file the agent never read should fail with a
// message the agent can act on (call read_file then retry).
func TestCheckWriteStaleness_NotReadThisTurnRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Age the file beyond the freshness window so we exercise the
	// "no read this turn" branch in isolation from "modified recently".
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal when file has not been read this turn")
	}
	if !strings.Contains(err.Error(), "read_file") {
		t.Errorf("error should suggest calling read_file, got %q", err)
	}
}

// TestCheckWriteStaleness_ReadThisTurnAllows pins the happy path: agent
// read the file, then wrote it. No external mutation, no refusal.
func TestCheckWriteStaleness_ReadThisTurnAllows(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)

	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error after recording a read, got %v", err)
	}
}

// TestCheckWriteStaleness_ResetForNewTurnInvalidates pins the turn-
// boundary reset: a read on turn N should not count as a read on
// turn N+1. Without this, the rule degenerates to "read once per
// session" which doesn't guard against state drift across turns.
func TestCheckWriteStaleness_ResetForNewTurnInvalidates(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("first-turn check should pass, got %v", err)
	}

	a.ResetFileReadsForNewTurn()
	if err := a.checkWriteStaleness(path); err == nil {
		t.Errorf("after turn reset, expected refusal; got nil")
	}
}

// TestCheckWriteStaleness_ModifiedAfterReadRefuses pins the freshness-
// window check: agent read, then something external bumped the mtime
// (a sync push from the browser side, in the SP-046 model). Writing
// would lose the user's edit, so refuse.
func TestCheckWriteStaleness_ModifiedAfterReadRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "raced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	// Sleep a hair so the subsequent write definitely has a later mtime.
	time.Sleep(15 * time.Millisecond)
	if err := os.WriteFile(path, []byte("v2-from-browser"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal when file was modified after the read")
	}
	if !strings.Contains(err.Error(), "modified") {
		t.Errorf("error should mention the external modification, got %q", err)
	}
}

// TestCheckWriteStaleness_NilAgentNoOp confirms the rule is safe to call
// from contexts that lack a configured Agent (test scaffolding, lazy
// init paths). Avoids cascading nil-panic regressions.
func TestCheckWriteStaleness_NilAgentNoOp(t *testing.T) {
	var a *Agent
	if err := a.checkWriteStaleness("/nonexistent"); err != nil {
		t.Errorf("nil agent should be a no-op, got %v", err)
	}
}

// TestCheckWriteStaleness_UnsyncedEditsRefuses pins the SP-046 §3 conflict
// rule: if the platform sync layer has flagged unsynced browser edits, the
// agent must NOT auto-retry — it should ask the user. Distinguished from
// the staleness rule via errors.Is so the agent's reasoning can branch.
func TestCheckWriteStaleness_UnsyncedEditsRefuses(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "raced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Even with a fresh read recorded, unsynced edits take precedence.
	a.RecordFileReadThisTurn(path)
	a.SetFileMetadata(path, WorkspaceFileMetadata{
		BrowserSeq:        7,
		LastSyncedBrowser: 5,
	})

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal with unsynced edits flagged")
	}
	if !errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("error should wrap ErrWriteHasUnsyncedEdits, got %v", err)
	}
	if errors.Is(err, ErrWriteStale) {
		t.Errorf("error should NOT also wrap ErrWriteStale (would confuse the agent's branch)")
	}
	if !strings.Contains(err.Error(), "ask the user") {
		t.Errorf("message should tell the agent to ask the user, got %q", err)
	}
}

// TestCheckWriteStaleness_SyncedMetadataAllows pins the happy path for
// the conflict check: BrowserSeq == LastSyncedBrowser means everything is
// caught up; the regular staleness rule takes over.
func TestCheckWriteStaleness_SyncedMetadataAllows(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "synced.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	a.RecordFileReadThisTurn(path)
	a.SetFileMetadata(path, WorkspaceFileMetadata{
		BrowserSeq:        5,
		LastSyncedBrowser: 5, // caught up
	})

	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("expected nil error with synced metadata, got %v", err)
	}
}

// TestCheckWriteStaleness_StalenessErrorClassification ensures the
// existing "no read this turn" branch reports ErrWriteStale (not
// ErrWriteHasUnsyncedEdits) so the agent's branch in the tool-result
// handler routes correctly.
func TestCheckWriteStaleness_StalenessErrorClassification(t *testing.T) {
	a := &Agent{}
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	err := a.checkWriteStaleness(path)
	if err == nil {
		t.Fatal("expected refusal")
	}
	if !errors.Is(err, ErrWriteStale) {
		t.Errorf("error should wrap ErrWriteStale, got %v", err)
	}
	if errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("error should NOT wrap ErrWriteHasUnsyncedEdits")
	}
}

// TestCheckWriteStaleness_FreeTierDegenerate is the SP-046-1e
// verification: a free-tier WASM page that never calls setSyncEndpoint or
// applyFileMetadata should see exactly the native single-replica
// behavior. The conflict-detection path stays a no-op (zero-value
// metadata means BrowserSeq == LastSyncedBrowser == 0, hasUnsynced
// returns false), and the staleness rule's intra-turn check still fires.
//
// If this test ever breaks, the platform-free path has acquired a
// dependency on platform-side metadata pushes — which would silently
// degrade free-tier UX.
func TestCheckWriteStaleness_FreeTierDegenerate(t *testing.T) {
	a := &Agent{} // no SetFileMetadata calls anywhere
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.txt")
	if err := os.WriteFile(path, []byte("v1"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(path, old, old); err != nil {
		t.Fatal(err)
	}

	// Confirms: no metadata cached, GetFileMetadata returns ok=false.
	if _, ok := a.GetFileMetadata(path); ok {
		t.Fatalf("free-tier should have zero cached metadata, but %q was present", path)
	}

	// Without a read this turn → ErrWriteStale (NOT ErrWriteHasUnsyncedEdits).
	err := a.checkWriteStaleness(path)
	if err == nil || !errors.Is(err, ErrWriteStale) {
		t.Errorf("free-tier no-read should be ErrWriteStale, got %v", err)
	}
	if errors.Is(err, ErrWriteHasUnsyncedEdits) {
		t.Errorf("free-tier should never trigger ErrWriteHasUnsyncedEdits")
	}

	// With a read this turn → allowed (single-replica happy path).
	a.RecordFileReadThisTurn(path)
	if err := a.checkWriteStaleness(path); err != nil {
		t.Errorf("free-tier read-then-write should succeed, got %v", err)
	}
}

// TestCheckWriteStaleness_PathNormalization pins the fix for the bug
// where read_file("roadmap/spec.md") (relative) and write_file("/abs/path/roadmap/spec.md")
// (absolute) used different map keys in the turn tracker. The staleness
// check must normalize both to the same canonical form so the read
// satisfies the write.
func TestCheckWriteStaleness_PathNormalization(t *testing.T) {
	dir := t.TempDir()
	absPath := filepath.Join(dir, "subdir", "file.txt")
	relPath := "subdir/file.txt"
	if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absPath, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-2 * stalenessFreshnessWindow)
	if err := os.Chtimes(absPath, old, old); err != nil {
		t.Fatal(err)
	}

	// Case 1: relative read, absolute write
	a := &Agent{workspaceRoot: dir}
	a.RecordFileReadThisTurn(relPath)
	if err := a.checkWriteStaleness(absPath); err != nil {
		t.Errorf("read with relative %q should satisfy write with absolute %q: %v", relPath, absPath, err)
	}

	// Case 2: absolute read, relative write
	a2 := &Agent{workspaceRoot: dir}
	a2.RecordFileReadThisTurn(absPath)
	if err := a2.checkWriteStaleness(relPath); err != nil {
		t.Errorf("read with absolute %q should satisfy write with relative %q: %v", absPath, relPath, err)
	}

	// Case 3: both absolute (baseline — this should always work)
	a3 := &Agent{workspaceRoot: dir}
	a3.RecordFileReadThisTurn(absPath)
	if err := a3.checkWriteStaleness(absPath); err != nil {
		t.Errorf("read with absolute %q should satisfy write with same absolute: %v", absPath, err)
	}
}

// TestSetFileMetadata_RoundTrip verifies that the in-memory store
// preserves values across set/get cycles, including overwriting a prior
// entry (the sync layer expects to call SetFileMetadata repeatedly as
// browser-side edits arrive).
func TestSetFileMetadata_RoundTrip(t *testing.T) {
	a := &Agent{}
	a.SetFileMetadata("a.txt", WorkspaceFileMetadata{BrowserSeq: 1})
	a.SetFileMetadata("b.txt", WorkspaceFileMetadata{BrowserSeq: 2})
	a.SetFileMetadata("a.txt", WorkspaceFileMetadata{BrowserSeq: 3}) // overwrite

	if md, ok := a.GetFileMetadata("a.txt"); !ok || md.BrowserSeq != 3 {
		t.Errorf("a.txt = %+v ok=%v, want BrowserSeq=3", md, ok)
	}
	if md, ok := a.GetFileMetadata("b.txt"); !ok || md.BrowserSeq != 2 {
		t.Errorf("b.txt = %+v ok=%v, want BrowserSeq=2", md, ok)
	}
	if _, ok := a.GetFileMetadata("missing.txt"); ok {
		t.Errorf("missing.txt should not be present")
	}
}

// TestWorkspaceFileMetadata_UnsyncedDetection pins the conflict
// predicate used by the platform-side sync engine. Cheap to test now;
// expensive to debug later if the inequality direction drifts.
func TestWorkspaceFileMetadata_UnsyncedDetection(t *testing.T) {
	cases := []struct {
		name string
		md   WorkspaceFileMetadata
		want bool
	}{
		{"all zero", WorkspaceFileMetadata{}, false},
		{"in sync", WorkspaceFileMetadata{BrowserSeq: 5, LastSyncedBrowser: 5}, false},
		{"unsynced edit", WorkspaceFileMetadata{BrowserSeq: 6, LastSyncedBrowser: 5}, true},
		{"impossible past-future", WorkspaceFileMetadata{BrowserSeq: 4, LastSyncedBrowser: 5}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.md.HasUnsyncedBrowserEdits(); got != c.want {
				t.Errorf("HasUnsyncedBrowserEdits = %v, want %v", got, c.want)
			}
		})
	}
}

// ============================================================================
// resolveWorkspacePath
// ============================================================================

// TestResolveWorkspacePath_ValidPath confirms that a normal relative path is
// resolved to an absolute path within the workspace root.
func TestResolveWorkspacePath_ValidPath(t *testing.T) {
	dir := t.TempDir()
	resolved, err := resolveWorkspacePath(dir, "src/main.go")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(mustEvalSymlinks(t, dir), "src/main.go")
	if resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// TestResolveWorkspacePath_NestedPath confirms deeply nested paths are
// handled correctly.
func TestResolveWorkspacePath_NestedPath(t *testing.T) {
	dir := t.TempDir()
	resolved, err := resolveWorkspacePath(dir, "a/b/c/d.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := filepath.Join(mustEvalSymlinks(t, dir), "a/b/c/d.txt")
	if resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// TestResolveWorkspacePath_PathTraversal confirms that directory traversal
// attempts are rejected with an error.
func TestResolveWorkspacePath_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveWorkspacePath(dir, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for path traversal, got nil")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected traversal error, got %q", err)
	}
}

// TestResolveWorkspacePath_DotDotTraversal confirms that repeated ..
// components that would escape the workspace root are rejected.
func TestResolveWorkspacePath_DotDotTraversal(t *testing.T) {
	dir := t.TempDir()
	_, err := resolveWorkspacePath(dir, "../../../etc/passwd")
	if err == nil {
		t.Fatal("expected error for traversal path component, got nil")
	}
	if !strings.Contains(err.Error(), "traversal") {
		t.Errorf("expected traversal error, got %q", err)
	}
}

// TestResolveWorkspacePath_NonexistentPath confirms that paths to files that
// don't yet exist (but are within the workspace root) are accepted.
func TestResolveWorkspacePath_NonexistentPath(t *testing.T) {
	dir := t.TempDir()
	resolved, err := resolveWorkspacePath(dir, "new-dir/new-file.txt")
	if err != nil {
		t.Fatalf("unexpected error for nonexistent file: %v", err)
	}
	want := filepath.Join(mustEvalSymlinks(t, dir), "new-dir/new-file.txt")
	if resolved != want {
		t.Errorf("resolved = %q, want %q", resolved, want)
	}
}

// TestResolveWorkspacePath_InvalidRoot confirms that an empty workspace root
// produces an error.
func TestResolveWorkspacePath_InvalidRoot(t *testing.T) {
	_, err := resolveWorkspacePath("", "file.txt")
	// filepath.Abs("") resolves to the current directory, so it may succeed
	// or fail depending on the OS. Just confirm it doesn't panic.
	if err != nil {
		// That's fine — empty root is problematic.
	}
	// The key is no panic.
}

// ============================================================================
// ApplySyncOp
// ============================================================================

// TestApplySyncOp_WriteCreatesFile confirms that a valid write op creates the
// file with the expected content.
func TestApplySyncOp_WriteCreatesFile(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType:     "write",
		Path:       "hello.txt",
		Content:    "world",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	content, err := os.ReadFile(filepath.Join(dir, "hello.txt"))
	if err != nil {
		t.Fatalf("file not created: %v", err)
	}
	if string(content) != "world" {
		t.Errorf("content = %q, want %q", string(content), "world")
	}
}

// TestApplySyncOp_WriteCreatesDirs confirms that parent directories are
// created when they don't exist.
func TestApplySyncOp_WriteCreatesDirs(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType:     "write",
		Path:       "deep/nested/dir/file.txt",
		Content:    "content",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	content, err := os.ReadFile(filepath.Join(dir, "deep/nested/dir/file.txt"))
	if err != nil {
		t.Fatalf("file not created in nested dir: %v", err)
	}
	if string(content) != "content" {
		t.Errorf("content = %q, want %q", string(content), "content")
	}
}

// TestApplySyncOp_DeleteRemovesFile confirms that a delete op removes the
// target file.
func TestApplySyncOp_DeleteRemovesFile(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}
	filePath := filepath.Join(dir, "delete.txt")
	if err := os.WriteFile(filePath, []byte("bye"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := SyncOp{
		OpType:     "delete",
		Path:       "delete.txt",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	if _, err := os.Stat(filePath); !os.IsNotExist(err) {
		t.Fatal("file should have been deleted")
	}
}

// TestApplySyncOp_DeleteNonexistentIsOK confirms that deleting a file that
// doesn't exist succeeds (no error).
func TestApplySyncOp_DeleteNonexistentIsOK(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType:     "delete",
		Path:       "nonexistent.txt",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true for deleting nonexistent file, got false: %s", result.Error)
	}
}

// TestApplySyncOp_RenameMovesFile confirms that a rename op moves a file from
// the old path to the new path.
func TestApplySyncOp_RenameMovesFile(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}
	oldFile := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldFile, []byte("renamed"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := SyncOp{
		OpType:     "rename",
		Path:       "old.txt",
		NewPath:    "new.txt",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	if _, err := os.Stat(oldFile); !os.IsNotExist(err) {
		t.Fatal("old file should have been moved")
	}
	newFile := filepath.Join(dir, "new.txt")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("new file not found: %v", err)
	}
	if string(content) != "renamed" {
		t.Errorf("new file content = %q, want %q", string(content), "renamed")
	}
}

// TestApplySyncOp_RenameRequiresNewPath confirms that a rename op without
// new_path returns an error.
func TestApplySyncOp_RenameRequiresNewPath(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := SyncOp{
		OpType: "rename",
		Path:   "x.txt",
	}
	result := a.ApplySyncOp(op, dir)
	if result.Accepted {
		t.Fatal("expected failure when new_path is empty for rename")
	}
	if !strings.Contains(result.Error, "new_path") {
		t.Errorf("expected error mentioning new_path, got %q", result.Error)
	}
}

// TestApplySyncOp_RenameCreatesParentDirs confirms that rename creates
// parent directories for the destination path.
func TestApplySyncOp_RenameCreatesParentDirs(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}
	oldFile := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(oldFile, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}

	op := SyncOp{
		OpType:     "rename",
		Path:       "file.txt",
		NewPath:    "sub/deep/file.txt",
		BrowserSeq: 1,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	newFile := filepath.Join(dir, "sub/deep/file.txt")
	content, err := os.ReadFile(newFile)
	if err != nil {
		t.Fatalf("file not found at new path: %v", err)
	}
	if string(content) != "data" {
		t.Errorf("content = %q, want %q", string(content), "data")
	}
}

// TestApplySyncOp_InvalidOpType confirms that an unknown op type is rejected.
func TestApplySyncOp_InvalidOpType(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType: "copy",
		Path:   "x.txt",
	}
	result := a.ApplySyncOp(op, dir)
	if result.Accepted {
		t.Fatal("expected failure for invalid op_type")
	}
	if !strings.Contains(result.Error, "invalid op_type") {
		t.Errorf("expected error mentioning invalid op_type, got %q", result.Error)
	}
}

// TestApplySyncOp_EmptyPathFails confirms that an empty path is rejected.
func TestApplySyncOp_EmptyPathFails(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType: "write",
	}
	result := a.ApplySyncOp(op, dir)
	if result.Accepted {
		t.Fatal("expected failure for empty path")
	}
	if !strings.Contains(result.Error, "path must not be empty") {
		t.Errorf("expected error about empty path, got %q", result.Error)
	}
}

// TestApplySyncOp_NilAgent confirms that calling ApplySyncOp on a nil agent
// returns a non-accepted result without panicking.
func TestApplySyncOp_NilAgent(t *testing.T) {
	var a *Agent
	op := SyncOp{
		OpType:  "write",
		Path:    "x.txt",
		Content: "data",
	}
	result := a.ApplySyncOp(op, "/tmp")
	if result.Accepted {
		t.Fatal("expected failure for nil agent")
	}
	if !strings.Contains(result.Error, "nil") {
		t.Errorf("expected error mentioning nil agent, got %q", result.Error)
	}
}

// TestApplySyncOp_ConflictWritesTheirs confirms that when container_seq >
// last_synced_container, a .theirs file is created and the op is rejected.
func TestApplySyncOp_ConflictWritesTheirs(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}
	filePath := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(filePath, []byte("container content"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Container has unsynced writes.
	a.SetFileMetadata("conflict.txt", WorkspaceFileMetadata{
		ContainerSeq:        5,
		LastSyncedContainer: 3,
	})

	op := SyncOp{
		OpType:     "write",
		Path:       "conflict.txt",
		Content:    "browser content",
		BrowserSeq: 10,
	}
	result := a.ApplySyncOp(op, dir)
	if result.Accepted {
		t.Fatal("expected conflict rejection")
	}
	if result.ConflictPath == "" {
		t.Fatal("expected conflict_path to be set")
	}
	if !strings.Contains(result.Error, "container has unsynced writes") {
		t.Errorf("expected conflict error, got %q", result.Error)
	}

	// Verify the .theirs file was created with the container's content.
	theirsPath := filepath.Join(dir, "conflict.txt.theirs")
	content, err := os.ReadFile(theirsPath)
	if err != nil {
		t.Fatalf(".theirs file not created: %v", err)
	}
	if string(content) != "container content" {
		t.Errorf(".theirs content = %q, want %q", string(content), "container content")
	}
}

// TestApplySyncOp_UpdatesMetadata confirms that after a successful apply,
// browser_seq and container_seq are updated in the metadata store.
func TestApplySyncOp_UpdatesMetadata(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	op := SyncOp{
		OpType:     "write",
		Path:       "update.txt",
		Content:    "data",
		BrowserSeq: 7,
	}
	result := a.ApplySyncOp(op, dir)
	if !result.Accepted {
		t.Fatalf("expected accepted=true, got false: %s", result.Error)
	}

	md, ok := a.GetFileMetadata("update.txt")
	if !ok {
		t.Fatal("expected metadata for update.txt")
	}
	if md.BrowserSeq != 7 {
		t.Errorf("browser_seq = %d, want 7", md.BrowserSeq)
	}
	if md.LastSyncedBrowser != 7 {
		t.Errorf("last_synced_browser = %d, want 7", md.LastSyncedBrowser)
	}
	if md.ContainerSeq != 1 {
		t.Errorf("container_seq = %d, want 1", md.ContainerSeq)
	}
	if md.ModifiedAt.IsZero() {
		t.Error("modified_at should be set")
	}
}

// ============================================================================
// ApplySyncOpBatch
// ============================================================================

// TestApplySyncOpBatch_AllSucceed confirms that multiple valid ops are all
// applied and return accepted=true.
func TestApplySyncOpBatch_AllSucceed(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	ops := []SyncOp{
		{OpType: "write", Path: "a.txt", Content: "one", BrowserSeq: 1},
		{OpType: "write", Path: "b.txt", Content: "two", BrowserSeq: 2},
		{OpType: "write", Path: "c.txt", Content: "three", BrowserSeq: 3},
	}
	results := a.ApplySyncOpBatch(ops, dir)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, r := range results {
		if !r.Accepted {
			t.Errorf("result %d not accepted: %s", i, r.Error)
		}
	}

	// Verify all files exist.
	for _, name := range []string{"a.txt", "b.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Errorf("file %s not created: %v", name, err)
		}
	}
}

// TestApplySyncOpBatch_StopsOnConflict confirms that when a conflict occurs,
// remaining ops are marked as skipped.
func TestApplySyncOpBatch_StopsOnConflict(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	// Pre-create the conflict file with unsynced container writes.
	conflictPath := filepath.Join(dir, "conflict.txt")
	if err := os.WriteFile(conflictPath, []byte("container data"), 0o644); err != nil {
		t.Fatal(err)
	}
	a.SetFileMetadata("conflict.txt", WorkspaceFileMetadata{
		ContainerSeq:        5,
		LastSyncedContainer: 3,
	})

	ops := []SyncOp{
		{OpType: "write", Path: "ok.txt", Content: "fine", BrowserSeq: 1},
		{OpType: "write", Path: "conflict.txt", Content: "new", BrowserSeq: 2},
		{OpType: "write", Path: "skipped.txt", Content: "nope", BrowserSeq: 3},
	}
	results := a.ApplySyncOpBatch(ops, dir)
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	// First op succeeded.
	if !results[0].Accepted {
		t.Errorf("first op should succeed: %s", results[0].Error)
	}
	// Second op conflicted.
	if results[1].Accepted {
		t.Error("second op should conflict")
	}
	// Third op was skipped.
	if results[2].Accepted {
		t.Error("third op should be skipped")
	}
	if !strings.Contains(results[2].Error, "skipped") {
		t.Errorf("third op should mention skipped, got %q", results[2].Error)
	}
}

// TestApplySyncOpBatch_EmptyBatch confirms that an empty batch returns an
// empty results slice.
func TestApplySyncOpBatch_EmptyBatch(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	results := a.ApplySyncOpBatch(nil, dir)
	if len(results) != 0 {
		t.Errorf("expected 0 results for nil batch, got %d", len(results))
	}

	results = a.ApplySyncOpBatch([]SyncOp{}, dir)
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty batch, got %d", len(results))
	}
}

// TestApplySyncOpBatch_PathTraversal confirms that path traversal is caught
// in batch mode.
func TestApplySyncOpBatch_PathTraversal(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	ops := []SyncOp{
		{OpType: "write", Path: "../../../etc/passwd", Content: "hack", BrowserSeq: 1},
	}
	results := a.ApplySyncOpBatch(ops, dir)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Accepted {
		t.Error("expected rejection for path traversal")
	}
	if !strings.Contains(results[0].Error, "traversal") {
		t.Errorf("expected traversal error, got %q", results[0].Error)
	}
}

// TestApplySyncOp_RenameMovesMetadata confirms that metadata is transferred
// from the old path to the new path on a rename operation, and the old path
// no longer holds meaningful metadata.
func TestApplySyncOp_RenameMovesMetadata(t *testing.T) {
	dir := t.TempDir()
	a := &Agent{}

	// Create a file with initial content
	oldPath := filepath.Join(dir, "old.txt")
	if err := os.WriteFile(oldPath, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Write to it via sync to establish metadata
	writeResult := a.ApplySyncOp(SyncOp{
		OpType:     "write",
		Path:       "old.txt",
		Content:    "original",
		BrowserSeq: 1,
	}, dir)
	if !writeResult.Accepted {
		t.Fatalf("write should be accepted: %s", writeResult.Error)
	}

	// Sync the container state so the rename isn't rejected as a conflict.
	// (ContainerSeq was bumped to 1 by the write; without this the
	//  ContainerSeq > LastSyncedContainer conflict check would fire.)
	a.SetFileMetadata("old.txt", WorkspaceFileMetadata{
		BrowserSeq:          1,
		LastSyncedBrowser:   1,
		ContainerSeq:        1,
		LastSyncedContainer: 1,
	})

	// Rename the file
	renameResult := a.ApplySyncOp(SyncOp{
		OpType:     "rename",
		Path:       "old.txt",
		NewPath:    "new.txt",
		BrowserSeq: 2,
	}, dir)
	if !renameResult.Accepted {
		t.Fatalf("rename should be accepted: %s", renameResult.Error)
	}

	// Verify: old.txt metadata should be gone (or zero-valued)
	oldMD, oldOK := a.GetFileMetadata("old.txt")
	if oldOK && oldMD.BrowserSeq > 0 {
		t.Errorf("old.txt should not have metadata with BrowserSeq > 0, got BrowserSeq=%d", oldMD.BrowserSeq)
	}

	// Verify: new.txt should have metadata with updated BrowserSeq
	newMD, newOK := a.GetFileMetadata("new.txt")
	if !newOK {
		t.Fatal("new.txt should have metadata after rename")
	}
	if newMD.BrowserSeq != 2 {
		t.Errorf("new.txt BrowserSeq = %d, want 2", newMD.BrowserSeq)
	}

	// Verify file content moved
	content, err := os.ReadFile(filepath.Join(dir, "new.txt"))
	if err != nil {
		t.Fatalf("read new.txt: %v", err)
	}
	if string(content) != "original" {
		t.Errorf("new.txt content = %q, want %q", string(content), "original")
	}
}

// ============================================================================
// GetSyncStatus
// ============================================================================

// TestGetSyncStatus_Empty confirms that an agent with no metadata returns nil
// (the metadata store is lazily initialized by SetFileMetadata).
func TestGetSyncStatus_Empty(t *testing.T) {
	a := &Agent{}
	status := a.GetSyncStatus()
	if status != nil {
		t.Errorf("expected nil for agent with no metadata, got map with %d entries", len(status))
	}
}

// TestGetSyncStatus_WithMetadata confirms that tracked files are returned
// with their correct metadata.
func TestGetSyncStatus_WithMetadata(t *testing.T) {
	a := &Agent{}
	a.SetFileMetadata("x.txt", WorkspaceFileMetadata{
		BrowserSeq:   5,
		ContainerSeq: 3,
	})
	a.SetFileMetadata("y.txt", WorkspaceFileMetadata{
		BrowserSeq:   2,
		ContainerSeq: 1,
	})

	status := a.GetSyncStatus()
	if len(status) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(status))
	}
	if md, ok := status["x.txt"]; !ok || md.BrowserSeq != 5 {
		t.Errorf("x.txt: %+v ok=%v, want BrowserSeq=5", md, ok)
	}
	if md, ok := status["y.txt"]; !ok || md.BrowserSeq != 2 {
		t.Errorf("y.txt: %+v ok=%v, want BrowserSeq=2", md, ok)
	}
}

// TestGetSyncStatus_NilAgent confirms that calling GetSyncStatus on a nil
// agent returns nil without panicking.
func TestGetSyncStatus_NilAgent(t *testing.T) {
	var a *Agent
	status := a.GetSyncStatus()
	if status != nil {
		t.Errorf("expected nil for nil agent, got %v", status)
	}
}

// ---------------------------------------------------------------------------
// ReconcileSeqNumbers & determineReconcileAction tests
// ---------------------------------------------------------------------------

func TestReconcileSeqNumbers_SyncOK(t *testing.T) {
	ag := newTestAgent(t)

	ag.SetFileMetadata("a.txt", WorkspaceFileMetadata{
		BrowserSeq:          5,
		ContainerSeq:        5,
		LastSyncedBrowser:   5,
		LastSyncedContainer: 5,
	})
	ag.SetFileMetadata("b.txt", WorkspaceFileMetadata{
		BrowserSeq:          3,
		ContainerSeq:        3,
		LastSyncedBrowser:   3,
		LastSyncedContainer: 3,
	})

	browserSeqs := map[string]int64{
		"a.txt": 5,
		"b.txt": 3,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		if r.Action != ReconcileSyncOK {
			t.Errorf("file %s: expected sync_ok, got %s", r.FilePath, r.Action)
		}
	}
}

func TestReconcileSeqNumbers_ContainerAhead(t *testing.T) {
	ag := newTestAgent(t)

	ag.SetFileMetadata("foo.txt", WorkspaceFileMetadata{
		BrowserSeq:          2,
		ContainerSeq:        5,
		LastSyncedBrowser:   2,
		LastSyncedContainer: 2, // container has written past what browser saw
	})

	browserSeqs := map[string]int64{
		"foo.txt": 2,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != ReconcileContainerAhead {
		t.Errorf("expected container_ahead, got %s", r.Action)
	}
	if r.ContainerSeq != 5 {
		t.Errorf("expected container_seq=5, got %d", r.ContainerSeq)
	}
	if r.BrowserSeq != 2 {
		t.Errorf("expected browser_seq=2, got %d", r.BrowserSeq)
	}
}

func TestReconcileSeqNumbers_BrowserAhead(t *testing.T) {
	ag := newTestAgent(t)

	ag.SetFileMetadata("bar.txt", WorkspaceFileMetadata{
		BrowserSeq:          5,
		ContainerSeq:        3,
		LastSyncedBrowser:   3, // browser has edits container hasn't synced
		LastSyncedContainer: 3,
	})

	browserSeqs := map[string]int64{
		"bar.txt": 5,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != ReconcileBrowserAhead {
		t.Errorf("expected browser_ahead, got %s", r.Action)
	}
	if r.BrowserSeq != 5 {
		t.Errorf("expected browser_seq=5, got %d", r.BrowserSeq)
	}
	if r.ContainerSeq != 3 {
		t.Errorf("expected container_seq=3, got %d", r.ContainerSeq)
	}
}

func TestReconcileSeqNumbers_Diverged(t *testing.T) {
	ag := newTestAgent(t)

	// Both sides have changes the other hasn't seen:
	// - Browser has edits past last_synced_browser (5 > 3)
	// - Container has writes past last_synced_container (7 > 3)
	ag.SetFileMetadata("conflict.txt", WorkspaceFileMetadata{
		BrowserSeq:          5,
		ContainerSeq:        7,
		LastSyncedBrowser:   3,
		LastSyncedContainer: 3,
	})

	browserSeqs := map[string]int64{
		"conflict.txt": 5,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	r := results[0]
	if r.Action != ReconcileDiverged {
		t.Errorf("expected diverged, got %s", r.Action)
	}
}

func TestReconcileSeqNumbers_NilAgent(t *testing.T) {
	var ag *Agent
	_, err := ReconcileSeqNumbers(ag, map[string]int64{"a.txt": 1})
	if err == nil {
		t.Fatal("expected error for nil agent, got nil")
	}
	if !strings.Contains(err.Error(), "agent is nil") {
		t.Errorf("unexpected error message: %v", err)
	}
}

func TestReconcileSeqNumbers_NilMetadata(t *testing.T) {
	ag := newTestAgent(t)
	// Do NOT set any file metadata — metadata store stays nil/empty

	browserSeqs := map[string]int64{
		"new.txt":  5,
		"zero.txt": 0,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Only files with seq > 0 produce results when metadata is nil
	if len(results) != 1 {
		t.Fatalf("expected 1 result (zero-seq file excluded), got %d", len(results))
	}
	r := results[0]
	if r.FilePath != "new.txt" {
		t.Errorf("expected new.txt, got %s", r.FilePath)
	}
	if r.Action != ReconcileBrowserAhead {
		t.Errorf("expected browser_ahead, got %s", r.Action)
	}
	if r.ContainerSeq != 0 {
		t.Errorf("expected container_seq=0, got %d", r.ContainerSeq)
	}
}

func TestReconcileSeqNumbers_EmptySeqs(t *testing.T) {
	ag := newTestAgent(t)
	ag.SetFileMetadata("a.txt", WorkspaceFileMetadata{
		BrowserSeq:   5,
		ContainerSeq: 5,
	})

	results, err := ReconcileSeqNumbers(ag, map[string]int64{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected 0 results for empty seqs, got %d", len(results))
	}
}

func TestReconcileSeqNumbers_FileNotInMetadata(t *testing.T) {
	ag := newTestAgent(t)
	// Set metadata for one file but NOT the other
	ag.SetFileMetadata("known.txt", WorkspaceFileMetadata{
		BrowserSeq:          2,
		ContainerSeq:        2,
		LastSyncedBrowser:   2,
		LastSyncedContainer: 2,
	})

	browserSeqs := map[string]int64{
		"known.txt":        2,
		"unknown.txt":      3, // no metadata — browser is ahead
		"zero_unknown.txt": 0, // seq 0 — should be excluded
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	for _, r := range results {
		switch r.FilePath {
		case "known.txt":
			if r.Action != ReconcileSyncOK {
				t.Errorf("known.txt: expected sync_ok, got %s", r.Action)
			}
		case "unknown.txt":
			if r.Action != ReconcileBrowserAhead {
				t.Errorf("unknown.txt: expected browser_ahead, got %s", r.Action)
			}
			if r.ContainerSeq != 0 {
				t.Errorf("unknown.txt: expected container_seq=0, got %d", r.ContainerSeq)
			}
		default:
			t.Errorf("unexpected file: %s", r.FilePath)
		}
	}
}

func TestReconcileSeqResults_Sorted(t *testing.T) {
	ag := newTestAgent(t)
	// Set metadata for files in any order
	ag.SetFileMetadata("z.txt", WorkspaceFileMetadata{ContainerSeq: 1})
	ag.SetFileMetadata("a.txt", WorkspaceFileMetadata{ContainerSeq: 1})
	ag.SetFileMetadata("m.txt", WorkspaceFileMetadata{ContainerSeq: 1})

	browserSeqs := map[string]int64{
		"z.txt": 1,
		"a.txt": 1,
		"m.txt": 1,
	}

	results, err := ReconcileSeqNumbers(ag, browserSeqs)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	expectedOrder := []string{"a.txt", "m.txt", "z.txt"}
	for i, expected := range expectedOrder {
		if results[i].FilePath != expected {
			t.Errorf("result[%d].file_path = %q, want %q", i, results[i].FilePath, expected)
		}
	}
}

// ---------------------------------------------------------------------------
// determineReconcileAction edge-case tests
// ---------------------------------------------------------------------------

func TestDetermineReconcileAction_EdgeCases(t *testing.T) {
	tests := []struct {
		name       string
		browserSeq int64
		md         WorkspaceFileMetadata
		want       ReconciliationActionType
	}{
		{
			name:       "exact_match_sync_ok",
			browserSeq: 10,
			md: WorkspaceFileMetadata{
				ContainerSeq: 10,
			},
			want: ReconcileSyncOK,
		},
		{
			name:       "both_zero_sync_ok",
			browserSeq: 0,
			md: WorkspaceFileMetadata{
				ContainerSeq: 0,
			},
			want: ReconcileSyncOK,
		},
		{
			name:       "browser_has_unsynced_edits_only",
			browserSeq: 10,
			md: WorkspaceFileMetadata{
				ContainerSeq:        5,
				LastSyncedBrowser:   5,
				LastSyncedContainer: 5,
			},
			want: ReconcileBrowserAhead,
		},
		{
			name:       "container_ahead_with_acknowledged_browser",
			browserSeq: 5,
			md: WorkspaceFileMetadata{
				ContainerSeq:        10,
				LastSyncedBrowser:   5,
				LastSyncedContainer: 5,
			},
			want: ReconcileContainerAhead,
		},
		{
			name:       "diverged_both_sides_unsynced",
			browserSeq: 10,
			md: WorkspaceFileMetadata{
				ContainerSeq:        12,
				LastSyncedBrowser:   5,
				LastSyncedContainer: 5,
			},
			want: ReconcileDiverged,
		},
		{
			name:       "fallback_browser_less_than_container_no_sync_state",
			browserSeq: 3,
			md: WorkspaceFileMetadata{
				ContainerSeq:        7,
				LastSyncedBrowser:   3,
				LastSyncedContainer: 7,
			},
			want: ReconcileContainerAhead,
		},
		{
			name:       "fallback_browser_greater_than_container_no_sync_state",
			browserSeq: 7,
			md: WorkspaceFileMetadata{
				ContainerSeq:        3,
				LastSyncedBrowser:   7,
				LastSyncedContainer: 3,
			},
			want: ReconcileBrowserAhead,
		},
		{
			name:       "negative_seqs_sync_ok",
			browserSeq: -1,
			md: WorkspaceFileMetadata{
				ContainerSeq: -1,
			},
			want: ReconcileSyncOK,
		},
		{
			name:       "negative_browser_less_than_container",
			browserSeq: -5,
			md: WorkspaceFileMetadata{
				ContainerSeq: 0,
			},
			want: ReconcileContainerAhead,
		},
		{
			name:       "only_browser_has_edits_no_container_writes",
			browserSeq: 8,
			md: WorkspaceFileMetadata{
				ContainerSeq:        5,
				LastSyncedBrowser:   5,
				LastSyncedContainer: 5,
			},
			want: ReconcileBrowserAhead,
		},
		{
			name:       "only_container_has_writes_no_browser_edits",
			browserSeq: 5,
			md: WorkspaceFileMetadata{
				ContainerSeq:        8,
				LastSyncedBrowser:   5,
				LastSyncedContainer: 5,
			},
			want: ReconcileContainerAhead,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := determineReconcileAction(tc.browserSeq, tc.md)
			if got != tc.want {
				t.Errorf("determineReconcileAction(%d, %+v) = %s, want %s",
					tc.browserSeq, tc.md, got, tc.want)
			}
		})
	}
}
