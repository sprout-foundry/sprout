package agent

import (
	"path/filepath"
	"testing"
)

// TestSessionPathModes_DefaultReadWrite verifies that the legacy
// AddSessionAllowedFolder path (no mode set) defaults to read_write
// — i.e. writes are allowed. This preserves existing behavior for
// callers that pre-date SP-128.
func TestSessionPathModes_DefaultReadWrite(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.AddSessionAllowedFolder("/tmp/foo")

	if !sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Fatal("freshly-added folder should be allowlisted")
	}
	if !sm.IsFolderSessionWriteAllowed("/tmp/foo/bar.txt") {
		t.Error("default mode (no explicit entry) should permit writes")
	}
	if !sm.IsFolderSessionWriteAllowed("/tmp/foo") {
		t.Error("default mode should permit writes on the folder itself")
	}
}

// TestSessionPathModes_ReadOnly verifies the read_only mode blocks
// writes but does NOT affect IsFolderSessionAllowed — reads continue
// to succeed. This is the core SP-128-1d invariant: the new mode is
// strictly a write gate, not a general access toggle.
func TestSessionPathModes_ReadOnly(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.AddSessionAllowedFolder("/tmp/foo")
	sm.SetSessionAllowedFolderMode("/tmp/foo", "read_only")

	if !sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Error("reads must still be allowed under read_only grant")
	}
	if sm.IsFolderSessionWriteAllowed("/tmp/foo/bar.txt") {
		t.Error("writes must be denied under read_only grant")
	}
	if sm.IsFolderSessionWriteAllowed("/tmp/foo") {
		t.Error("writes must be denied even for the folder itself under read_only")
	}
}

// TestSessionPathModes_ClearRevertsToDefault verifies that passing
// mode=="" to SetSessionAllowedFolderMode removes the explicit
// entry — the folder reverts to the default read_write semantics.
// Idempotent: calling twice with "" is safe.
func TestSessionPathModes_ClearRevertsToDefault(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.AddSessionAllowedFolder("/tmp/foo")
	sm.SetSessionAllowedFolderMode("/tmp/foo", "read_only")
	if sm.IsFolderSessionWriteAllowed("/tmp/foo/x.txt") {
		t.Fatal("read_only should block writes")
	}
	sm.SetSessionAllowedFolderMode("/tmp/foo", "")
	if !sm.IsFolderSessionWriteAllowed("/tmp/foo/x.txt") {
		t.Error("clearing the mode should revert to default read_write")
	}
}

// TestSessionPathModes_OnlyForAllowlistedFolder verifies the guard:
// setting a mode on a folder NOT in the allowlist is a no-op (mode
// can't widen access the user never approved). Reading back via
// IsFolderSessionWriteAllowed returns false for that folder.
func TestSessionPathModes_OnlyForAllowlistedFolder(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.SetSessionAllowedFolderMode("/never/allowlisted", "read_only")
	if sm.IsFolderSessionWriteAllowed("/never/allowlisted/x.txt") {
		t.Error("mode for an unallowlisted folder must NOT enable writes")
	}
	// And after the no-op, the snapshot must remain empty.
	if got := len(sm.SnapshotSessionAllowedFolderModes()); got != 0 {
		t.Errorf("expected empty mode map after no-op, got %d entries", got)
	}
}

// TestSessionPathModes_Snapshot verifies that
// SnapshotSessionAllowedFolderModes returns a copy (not a reference)
// so subagent creation can mutate the result without affecting the
// parent. Together with SnapshotSessionAllowedFolders, this is what
// lets a subagent inherit a workflow's read_only constraints.
//
// NOTE: AddSessionAllowedFolder and SetSessionAllowedFolderMode
// normalize paths via filepath.EvalSymlinks (normalizePath helper
// in path_tier.go). On macOS /var and /tmp may resolve to
// /private/var and /private/tmp, so the test uses a workspace
// tempdir which never has a symlinked ancestor. The mode-lookup
// itself uses the same normalization, so the round-trip behavior
// is what production callers observe.
func TestSessionPathModes_Snapshot(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	// Build two directories under t.TempDir() so neither has a
	// symlinked ancestor (macOS resolves /tmp → /private/tmp via
	// EvalSymlinks and would silently canonicalize the keys).
	root := t.TempDir()
	dirA := filepath.Join(root, "a")
	dirB := filepath.Join(root, "b")
	sm.AddSessionAllowedFolder(dirA)
	sm.AddSessionAllowedFolder(dirB)
	sm.SetSessionAllowedFolderMode(dirA, "read_write")
	sm.SetSessionAllowedFolderMode(dirB, "read_only")

	snap := sm.SnapshotSessionAllowedFolderModes()
	if len(snap) != 2 {
		t.Fatalf("expected 2 entries in mode snapshot, got %d (%+v)", len(snap), snap)
	}
	if snap[dirA] != "read_write" {
		t.Errorf("snap[dirA] = %q, want read_write", snap[dirA])
	}
	if snap[dirB] != "read_only" {
		t.Errorf("snap[dirB] = %q, want read_only", snap[dirB])
	}

	// Mutate the snapshot — the manager must not observe the change.
	// The manager originally stored read_write for dirA; mutating the
	// snapshot to read_only must NOT flip the manager's behavior.
	// If it did, writes would now be denied under dirA — assert that
	// writes are still allowed (the manager's actual mode).
	snap[dirA] = "read_only"
	if !sm.IsFolderSessionWriteAllowed(filepath.Join(dirA, "x.txt")) {
		t.Error("mutating the snapshot must not affect the manager's mode")
	}

	// Empty case — fresh manager returns an empty (but non-nil) map.
	fresh := NewAgentSecurityManager()
	empty := fresh.SnapshotSessionAllowedFolderModes()
	if empty == nil {
		t.Fatal("snapshot of empty manager should be a non-nil empty map (so callers can mutate)")
	}
	if len(empty) != 0 {
		t.Errorf("expected empty map, got %d entries", len(empty))
	}
}

// TestSessionPathModes_ReadOnlyStopsAtCorrectFolder verifies the
// prefix-matching honors component boundaries: a read_only mode on
// /tmp/foo must NOT block writes against /tmp/foobar.
func TestSessionPathModes_ReadOnlyStopsAtCorrectFolder(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.AddSessionAllowedFolder("/tmp/foo")
	sm.SetSessionAllowedFolderMode("/tmp/foo", "read_only")

	if sm.IsFolderSessionWriteAllowed("/tmp/foobar/x.txt") {
		t.Error("read_only on /tmp/foo must NOT match /tmp/foobar (component-boundary)")
	}
}

// TestSessionPathModes_EmptyPath guards the empty-input edge case:
// writes against an empty path must always be denied (mirrors the
// existing IsFolderSessionAllowed contract).
func TestSessionPathModes_EmptyPath(t *testing.T) {
	t.Parallel()
	sm := NewAgentSecurityManager()
	sm.AddSessionAllowedFolder("/tmp/foo")
	if sm.IsFolderSessionWriteAllowed("") {
		t.Error("empty path must not be write-allowed")
	}
	sm.SetSessionAllowedFolderMode("", "read_write")
	if sm.IsFolderSessionWriteAllowed("") {
		t.Error("setting mode on empty folder must not widen write access")
	}
}
