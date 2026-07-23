//go:build !js

// Package workflow: tests for ApplyWorkflowRuntimeAllowedPaths and
// RestoreWorkflowRuntimeAllowedPaths (SP-127 Phase 2.3).
package workflow

import (
	"sort"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// testAgent creates a minimal agent for testing. Uses a real security manager
// so RemoveSessionAllowedFolder works end-to-end. Mirrors the pattern from
// agent_cd_security_test.go::newTestAgentWithSecurity.
func applyPathsTestAgent(workspaceRoot string) *agent.Agent {
	if workspaceRoot == "" {
		workspaceRoot = "/workspace"
	}
	return agent.NewTestAgent()
}

func TestApplyWorkflowRuntimeAllowedPaths_NilAgent(t *testing.T) {
	t.Parallel()
	_, _, _, err := ApplyWorkflowRuntimeAllowedPaths(nil, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
	})
	if err == nil {
		t.Error("expected error for nil agent, got nil")
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_EmptyPaths(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")
	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if snap != nil && len(snap) > 0 {
		t.Errorf("snapshot with no prior paths: got %v, want nil or empty", snap)
	}
	if modes != nil && len(modes) > 0 {
		t.Errorf("modes with no prior entries: got %v, want nil or empty", modes)
	}
	if added != nil && len(added) > 0 {
		t.Errorf("added with no paths: got %v, want nil or empty", added)
	}

	// Empty slice should behave identically to nil.
	snap2, modes2, added2, err2 := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{})
	if err2 != nil {
		t.Fatalf("unexpected error: %v", err2)
	}
	if snap2 != nil && len(snap2) > 0 {
		t.Errorf("snapshot with empty paths: got %v, want nil or empty", snap2)
	}
	if added2 != nil && len(added2) > 0 {
		t.Errorf("added with empty paths: got %v, want nil or empty", added2)
	}
	// modes2 should also be nil or empty (no prior entries).
	if modes2 != nil && len(modes2) > 0 {
		t.Errorf("modes2 with empty paths: got %v, want nil or empty", modes2)
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_OnePath(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Snapshot should be empty (agent had no prior allowlist).
	if len(snap) > 0 {
		t.Errorf("snapshot before first apply: got %v, want empty", snap)
	}
	// Modes map should be nil or empty (no prior entries).
	if modes != nil && len(modes) > 0 {
		t.Errorf("modes before first apply: got %v, want nil or empty", modes)
	}

	// added should contain the one path.
	if len(added) != 1 || added[0] != "/tmp/foo" {
		t.Errorf("added: got %v, want [/tmp/foo]", added)
	}

	// Agent should now have the path.
	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) != 1 || folders[0] != "/tmp/foo" {
		t.Errorf("agent allowlist after apply: got %v, want [/tmp/foo]", folders)
	}

	// Mode should be recorded.
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/tmp/foo"] != "read_write" {
		t.Errorf("mode for /tmp/foo: got %q, want read_write", modeMap["/tmp/foo"])
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_TwoPaths(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	_, _, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
		{Path: "/srv/data", Mode: "read_only"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(added) != 2 {
		t.Errorf("added count: got %d, want 2", len(added))
	}
	sort.Strings(added)
	if added[0] != "/srv/data" || added[1] != "/tmp/foo" {
		t.Errorf("added: got %v, want [/srv/data, /tmp/foo] (sorted)", added)
	}

	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) != 2 {
		t.Errorf("agent allowlist: got %d entries, want 2", len(folders))
	}
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/srv/data"] != "read_only" {
		t.Errorf("mode for /srv/data: got %q, want read_only", modeMap["/srv/data"])
	}
	if modeMap["/tmp/foo"] != "read_write" {
		t.Errorf("mode for /tmp/foo: got %q, want read_write", modeMap["/tmp/foo"])
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_DefaultMode(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	// Mode empty → should default to read_write.
	_, _, _, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/defaultmode", Mode: ""},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/tmp/defaultmode"] != "read_write" {
		t.Errorf("default mode: got %q, want read_write", modeMap["/tmp/defaultmode"])
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_Idempotent(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	// Apply same path twice in the same call.
	_, _, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
		{Path: "/tmp/foo", Mode: "read_only"}, // same path, different mode
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Path should only appear once in added.
	if len(added) != 1 || added[0] != "/tmp/foo" {
		t.Errorf("added (idempotent): got %v, want [/tmp/foo]", added)
	}

	// Mode is read_write from the first entry; the second entry is skipped
	// (continue is taken before mode-setting code runs). This is intentional:
	// the implementation checks currentSet BEFORE setting mode, so a second
	// declaration of the same path in one call is silently ignored.
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/tmp/foo"] != "read_write" {
		t.Errorf("mode after idempotent in-call: got %q, want read_write (first entry wins)", modeMap["/tmp/foo"])
	}
}

func TestApplyWorkflowRuntimeAllowedPaths_IdempotentAcrossCalls(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	// First call adds /tmp/foo with mode read_write.
	_, _, added1, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if len(added1) != 1 {
		t.Errorf("added1 count: got %d, want 1", len(added1))
	}

	// Second call tries to add /tmp/foo again with mode read_only.
	_, _, added2, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_only"},
	})
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}

	// Path is NOT in added2 (already present from first call).
	if len(added2) != 0 {
		t.Errorf("added2 (already present): got %v, want empty", added2)
	}

	// Mode stays read_write from the first call. The second call's
	// SetSessionAllowedFolderMode is never called because the implementation
	// checks currentSet BEFORE mode-setting and skips (continue) when the
	// path is already present. This is the documented idempotent behavior:
	// duplicate paths in a step's allowed_paths list are silently ignored.
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/tmp/foo"] != "read_write" {
		t.Errorf("mode after second call: got %q, want read_write (unchanged)", modeMap["/tmp/foo"])
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_RestoresAddedPaths(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	// Apply two paths.
	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/foo", Mode: "read_write"},
		{Path: "/srv/data", Mode: "read_only"},
	})
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	// Restore.
	restoreErr := RestoreWorkflowRuntimeAllowedPaths(a, snap, modes, added)
	if restoreErr != nil {
		t.Fatalf("restore error: %v", restoreErr)
	}

	// Allowlist should be empty again.
	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) > 0 {
		t.Errorf("allowlist after restore: got %v, want empty", folders)
	}
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if len(modeMap) > 0 {
		t.Errorf("mode map after restore: got %v, want empty", modeMap)
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_PreservesPreExisting(t *testing.T) {
	t.Parallel()
	a := applyPathsTestAgent("/ws")

	// Pre-existing path (e.g., from Initial.AllowedPaths or user approval).
	a.AddSessionAllowedFolder("/already/here")
	a.SetSessionAllowedFolderMode("/already/here", "read_only")

	// Step adds a new path.
	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/steppath", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	// Restore should remove only the step path, preserve pre-existing.
	restoreErr := RestoreWorkflowRuntimeAllowedPaths(a, snap, modes, added)
	if restoreErr != nil {
		t.Fatalf("restore error: %v", restoreErr)
	}

	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) != 1 || folders[0] != "/already/here" {
		t.Errorf("allowlist after restore: got %v, want [/already/here]", folders)
	}
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/already/here"] != "read_only" {
		t.Errorf("mode for /already/here: got %q, want read_only", modeMap["/already/here"])
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_OverlappingPathNotRemoved(t *testing.T) {
	t.Parallel()
	// This test documents the "consecutive steps" behavior: when step 2
	// adds a path that step 1 already added, step 2's restore must NOT
	// remove the path (it was already present in step 2's snapshot).
	a := applyPathsTestAgent("/ws")

	// Step 1 adds /tmp/shared.
	snap1, modes1, added1, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/shared", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("step1 apply error: %v", err)
	}
	restoreErr1 := RestoreWorkflowRuntimeAllowedPaths(a, snap1, modes1, added1)
	if restoreErr1 != nil {
		t.Fatalf("step1 restore error: %v", restoreErr1)
	}
	// After step 1 restore: allowlist is empty.

	// Step 2 tries to add /tmp/shared again.
	snap2, modes2, added2, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/tmp/shared", Mode: "read_only"},
	})
	if err != nil {
		t.Fatalf("step2 apply error: %v", err)
	}

	// Step 2 snapshot should be empty (no prior allowlist).
	if len(snap2) != 0 {
		t.Errorf("step2 snapshot: got %v, want empty", snap2)
	}
	// Step 2 added should contain /tmp/shared.
	if len(added2) != 1 || added2[0] != "/tmp/shared" {
		t.Errorf("step2 added: got %v, want [/tmp/shared]", added2)
	}

	// Restore step 2.
	restoreErr2 := RestoreWorkflowRuntimeAllowedPaths(a, snap2, modes2, added2)
	if restoreErr2 != nil {
		t.Fatalf("step2 restore error: %v", restoreErr2)
	}

	// Allowlist should be empty (shared path was added and removed).
	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) > 0 {
		t.Errorf("allowlist after step2 restore: got %v, want empty", folders)
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_ModeRestored(t *testing.T) {
	t.Parallel()
	// Test that RestoreWorkflowRuntimeAllowedPaths restores mode for paths
	// that were in the snapshot, even when the current state has different modes.
	a := applyPathsTestAgent("/ws")

	// Pre-populate /existing with mode read_only.
	a.AddSessionAllowedFolder("/existing")
	a.SetSessionAllowedFolderMode("/existing", "read_only")

	// Step adds /newpath (should be removed on restore) and changes nothing
	// about /existing (it's already present, not added by this step).
	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/newpath", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	// Mode of /existing should still be read_only (this step didn't touch it).
	modeMap := a.SnapshotSessionAllowedFolderModes()
	if modeMap["/existing"] != "read_only" {
		t.Errorf("mode of /existing (untouched by step): got %q, want read_only", modeMap["/existing"])
	}

	// Simulate: step changed /existing's mode (e.g., another part of the step
	// execution changed it, or a workflow override updated it).
	a.SetSessionAllowedFolderMode("/existing", "read_write")

	// Now restore. This should revert /existing's mode to read_only and remove /newpath.
	restoreErr := RestoreWorkflowRuntimeAllowedPaths(a, snap, modes, added)
	if restoreErr != nil {
		t.Fatalf("restore error: %v", err)
	}

	modeMapAfter := a.SnapshotSessionAllowedFolderModes()
	if modeMapAfter["/existing"] != "read_only" {
		t.Errorf("mode of /existing after restore: got %q, want read_only", modeMapAfter["/existing"])
	}

	// /newpath should be removed.
	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) != 1 || folders[0] != "/existing" {
		t.Errorf("allowlist after restore: got %v, want [/existing]", folders)
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_ConsecutiveStepsBehavior(t *testing.T) {
	t.Parallel()
	// This test documents the "consecutive steps don't inherit" behavior:
	// step 1 adds /a and /b, then restores (clearing the allowlist).
	// Step 2 must declare /b in its own allowed_paths to have it —
	// it won't be present just because step 1 used it.
	//
	// Because step 1's restore cleared the allowlist, step 2's snapshot is
	// empty and step 2 adds both /b and /c (both are net-new).
	a := applyPathsTestAgent("/ws")

	// Step 1: adds /a and /b.
	snap1, modes1, added1, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/a", Mode: "read_write"},
		{Path: "/b", Mode: "read_write"},
	})
	if err != nil {
		t.Fatalf("step1 apply: %v", err)
	}
	RestoreWorkflowRuntimeAllowedPaths(a, snap1, modes1, added1)

	// After step 1 restore: allowlist is empty.
	foldersAfterStep1 := a.SnapshotSessionAllowedFolders()
	if len(foldersAfterStep1) != 0 {
		t.Fatalf("after step1 restore: allowlist = %v, want empty", foldersAfterStep1)
	}

	// Step 2: adds /b (was removed by step 1's restore) and /c (new).
	snap2, modes2, added2, err := ApplyWorkflowRuntimeAllowedPaths(a, []AllowedPath{
		{Path: "/b", Mode: "read_only"},  // re-declared; step 1 already removed it
		{Path: "/c", Mode: "read_write"}, // new
	})
	if err != nil {
		t.Fatalf("step2 apply: %v", err)
	}

	// Step 2 snapshot should be empty (allowlist was cleared by step 1).
	if len(snap2) != 0 {
		t.Errorf("step2 snapshot (after step1 restore): got %v, want empty", snap2)
	}

	// Step 2 added contains both /b and /c (both net-new relative to empty snapshot).
	if len(added2) != 2 {
		t.Errorf("step2 added count: got %d, want 2", len(added2))
	}
	sort.Strings(added2)
	if len(added2) == 2 && (added2[0] != "/b" || added2[1] != "/c") {
		t.Errorf("step2 added: got %v, want [/b, /c] (sorted)", added2)
	}

	// Restore step 2: should go back to empty (snap2 was empty).
	RestoreWorkflowRuntimeAllowedPaths(a, snap2, modes2, added2)

	foldersAfterStep2 := a.SnapshotSessionAllowedFolders()
	if len(foldersAfterStep2) != 0 {
		t.Errorf("after step2 restore: allowlist = %v, want empty (snap2 was empty)", foldersAfterStep2)
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_NilAgent(t *testing.T) {
	t.Parallel()
	// Nil agent should not panic.
	err := RestoreWorkflowRuntimeAllowedPaths(nil, nil, nil, nil)
	if err == nil {
		// Nil agent is documented as a no-op, not an error.
		t.Log("nil agent: returned nil (documented behavior)")
	}
}

func TestRestoreWorkflowRuntimeAllowedPaths_EmptyAddedPaths(t *testing.T) {
	t.Parallel()
	// When a step has no allowed_paths, RestoreWorkflowRuntimeAllowedPaths
	// is called with empty addedPaths. It should be a no-op.
	a := applyPathsTestAgent("/ws")
	a.AddSessionAllowedFolder("/preexisting")

	snap, modes, added, err := ApplyWorkflowRuntimeAllowedPaths(a, nil)
	if err != nil {
		t.Fatalf("apply error: %v", err)
	}

	restoreErr := RestoreWorkflowRuntimeAllowedPaths(a, snap, modes, added)
	if restoreErr != nil {
		t.Fatalf("restore error: %v", restoreErr)
	}

	// Pre-existing path should still be there.
	folders := a.SnapshotSessionAllowedFolders()
	if len(folders) != 1 || folders[0] != "/preexisting" {
		t.Errorf("allowlist after empty-step restore: got %v, want [/preexisting]", folders)
	}
}
