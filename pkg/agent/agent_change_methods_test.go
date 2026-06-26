package agent

import (
	"path/filepath"
	"testing"
)

func TestAgent_GetRevisionID_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	if got := a.GetRevisionID(); got != "" {
		t.Errorf("GetRevisionID() = %q, expected empty when no tracker", got)
	}
}

func TestAgent_GetTrackedFiles_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	files := a.GetTrackedFiles()
	if len(files) != 0 {
		t.Errorf("GetTrackedFiles() = %v, expected empty when no tracker", files)
	}
}

func TestAgent_GetChangeCount_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	if got := a.GetChangeCount(); got != 0 {
		t.Errorf("GetChangeCount() = %d, expected 0 when no tracker", got)
	}
}

func TestAgent_GetChangesSummary_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	summary := a.GetChangesSummary()
	if summary != "Change tracking is not enabled" {
		t.Errorf("GetChangesSummary() = %q, expected 'Change tracking is not enabled'", summary)
	}
}

func TestAgent_IsChangeTrackingEnabled_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	if a.IsChangeTrackingEnabled() {
		t.Error("IsChangeTrackingEnabled() = true, expected false when no tracker")
	}
}

func TestAgent_EnableChangeTracking_CreatesTracker(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	if a.IsChangeTrackingEnabled() {
		t.Error("should not be enabled before calling EnableChangeTracking")
	}

	a.EnableChangeTracking("test instructions")
	if !a.IsChangeTrackingEnabled() {
		t.Error("should be enabled after calling EnableChangeTracking")
	}

	// Verify tracker was created with revision ID
	revisionID := a.GetRevisionID()
	if revisionID == "" {
		t.Error("GetRevisionID() should be non-empty after enabling")
	}

	// Verify tracked files returns empty slice, not nil
	files := a.GetTrackedFiles()
	if files == nil {
		t.Error("GetTrackedFiles() should return empty slice, not nil")
	}
}

// TestAgent_EnableChangeTracking_PreservesExistingTracker verifies the
// SESSION-SCOPING contract: calling EnableChangeTracking on an existing
// tracker does NOT reset the buffer or change the revision ID. The
// first call (new session) establishes identity; subsequent calls
// (every ProcessQuery in a daemon chat) must preserve accumulated
// changes so list_changes / recover_file / revert_my_changes reflect
// the whole session, not just the current turn.
//
// Previously EnableChangeTracking called Reset() on re-enable, which
// wiped prior turns' edits — a cross-turn footgun. See
// memory: off-rails-revert-detection for the incident that surfaced it.
func TestAgent_EnableChangeTracking_PreservesExistingTracker(t *testing.T) {
	ws := t.TempDir()
	a := &Agent{
		state:         NewAgentStateManager(false),
		workspaceRoot: ws,
	}
	a.EnableChangeTracking("first instructions")
	firstID := a.GetRevisionID()

	// Record a change after first enable.
	a.TrackFileWrite(filepath.Join(ws, "a.go"), "content")
	if got := a.GetChangeCount(); got != 1 {
		t.Fatalf("expected 1 change after first enable, got %d", got)
	}

	// Re-enable (simulating a new ProcessQuery in the same session).
	a.EnableChangeTracking("second instructions")
	secondID := a.GetRevisionID()

	// The revision ID MUST be stable — it's the session identity used
	// to scope persisted history entries. Changing it per-turn would
	// orphan prior commits and break list_changes' session filter.
	if firstID != secondID {
		t.Errorf("revision ID changed on re-enable: %q -> %q (must stay stable within a session)", firstID, secondID)
	}
	if !a.IsChangeTrackingEnabled() {
		t.Error("should still be enabled after re-enable")
	}

	// The buffer MUST be preserved — the whole point of Fix B. If this
	// regresses to wiping, list_changes returns count:0 at the start of
	// every new turn, exactly the bug that motivated this change.
	if got := a.GetChangeCount(); got != 1 {
		t.Errorf("re-enable wiped the buffer: change count = %d, want 1 (session buffer must be preserved)", got)
	}
}

func TestAgent_DisableChangeTracking(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.EnableChangeTracking("test")
	if !a.IsChangeTrackingEnabled() {
		t.Error("should be enabled after EnableChangeTracking")
	}

	a.DisableChangeTracking()
	if a.IsChangeTrackingEnabled() {
		t.Error("should not be enabled after DisableChangeTracking")
	}
}

func TestAgent_DisableChangeTracking_NoTracker(t *testing.T) {
	a := &Agent{}
	// Should not panic
	a.DisableChangeTracking()
}

func TestAgent_GetChangeTracker_NoTracker(t *testing.T) {
	t.Parallel()
	a := &Agent{}
	if got := a.GetChangeTracker(); got != nil {
		t.Error("GetChangeTracker() should be nil when no tracker")
	}
}

func TestAgent_GetChangeTracker_AfterEnable(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.EnableChangeTracking("instructions")
	if got := a.GetChangeTracker(); got == nil {
		t.Error("GetChangeTracker() should be non-nil after enabling")
	}
}

func TestAgent_GetChangeTracker_AfterDisable(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.EnableChangeTracking("instructions")
	a.DisableChangeTracking()
	tracker := a.GetChangeTracker()
	if tracker == nil {
		t.Error("GetChangeTracker() should still return tracker after disable (just disabled)")
	}
	if tracker.IsEnabled() {
		t.Error("tracker should be disabled")
	}
}

func TestHandleListChanges_IncludeCrossSession(t *testing.T) {
	// Verify include_cross_session flag merges persisted entries from
	// ALL sessions when true, and filters to THIS session only when false.
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.EnableChangeTracking("test-instructions")

	// Track a file change in this session.
	tracker := a.GetChangeTracker()
	if tracker == nil {
		t.Fatal("expected tracker to be non-nil")
	}
	err := tracker.TrackFileWrite("test.txt", "content")
	if err != nil {
		t.Fatalf("TrackFileWrite: %v", err)
	}

	// Test 1: include_cross_session=false (default) should NOT
	// include persisted entries from other sessions. Since there are
	// no persisted entries yet, this just verifies the default path
	// doesn't break.
	result, err := handleListChanges(nil, a, map[string]interface{}{
		"include_cross_session": false,
	})
	if err != nil {
		t.Fatalf("handleListChanges(include_cross_session=false): %v", err)
	}
	if len(result) == 0 {
		t.Fatal("expected non-empty response for default path")
	}

	// Test 2: include_cross_session=true should also work and produce
	// a valid response. This path exercises the metadata-only scan of
	// all persisted entries across sessions.
	result2, err := handleListChanges(nil, a, map[string]interface{}{
		"include_cross_session": true,
	})
	if err != nil {
		t.Fatalf("handleListChanges(include_cross_session=true): %v", err)
	}
	if len(result2) == 0 {
		t.Fatal("expected non-empty response for cross-session path")
	}

	// Both should have the same in-memory file since persisted history
	// is empty. The key differentiator is that cross-session would
	// include additional entries from other revisions when they exist.
	t.Logf("include_cross_session=false: %d bytes", len(result))
	t.Logf("include_cross_session=true:  %d bytes", len(result2))
}
