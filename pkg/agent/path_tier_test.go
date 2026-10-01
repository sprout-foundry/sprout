// Package agent: session-allowlist (AgentSecurityManager) tests.
package agent

import (
	"testing"
)

// TestSessionAllowedFolders exercises the per-folder allowlist API
// on the security manager: add, prefix-match lookup, snapshot, dedup.
func TestSessionAllowedFolders(t *testing.T) {
	sm := NewAgentSecurityManager()

	if sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Fatal("fresh manager should not allow any folder")
	}
	if sm.IsSecurityBypassApproved() {
		t.Fatal("fresh manager: IsSecurityBypassApproved should be false")
	}

	sm.AddSessionAllowedFolder("/tmp/foo")

	if !sm.IsFolderSessionAllowed("/tmp/foo/bar.txt") {
		t.Error("path under allowed folder should be allowed")
	}
	if !sm.IsFolderSessionAllowed("/tmp/foo") {
		t.Error("the exact folder itself should be allowed")
	}
	if !sm.IsFolderSessionAllowed("/tmp/foo/sub/dir/x.txt") {
		t.Error("deep path under allowed folder should be allowed")
	}
	if sm.IsFolderSessionAllowed("/tmp/other/y.txt") {
		t.Error("path outside any allowed folder should not be allowed")
	}
	if sm.IsFolderSessionAllowed("/tmp/foobar/x.txt") {
		t.Error("component-boundary safety: /tmp/foobar should not match /tmp/foo")
	}
	if !sm.IsSecurityBypassApproved() {
		t.Error("with a folder allowlisted, the coarse signal should be true")
	}

	// Dedup: re-adding the same folder doesn't grow the list.
	sm.AddSessionAllowedFolder("/tmp/foo")
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 1 {
		t.Errorf("expected 1 entry after dedup, got %d", got)
	}

	// Snapshot is independent — mutating it doesn't change the manager.
	snap := sm.SnapshotSessionAllowedFolders()
	snap[0] = "/mutated"
	if !sm.IsFolderSessionAllowed("/tmp/foo/x.txt") {
		t.Error("mutating the snapshot should not affect the manager")
	}
}

// TestSessionAllowedFolders_EmptyAndAbsentInputs guards the helpers
// against nil/empty edge cases.
func TestSessionAllowedFolders_EmptyAndAbsentInputs(t *testing.T) {
	sm := NewAgentSecurityManager()
	if sm.IsFolderSessionAllowed("") {
		t.Error("empty path must not be approved")
	}
	sm.AddSessionAllowedFolder("")
	if got := len(sm.SnapshotSessionAllowedFolders()); got != 0 {
		t.Errorf("adding empty folder should be a no-op, got %d entries", got)
	}
}
