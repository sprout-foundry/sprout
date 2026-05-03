package agent

import (
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

func TestAgent_EnableChangeTracking_ResetsExistingTracker(t *testing.T) {
	a := &Agent{
		state: NewAgentStateManager(false),
	}
	a.EnableChangeTracking("first instructions")
	firstID := a.GetRevisionID()

	a.EnableChangeTracking("second instructions")
	secondID := a.GetRevisionID()

	if firstID == secondID {
		t.Error("revision ID should change when re-enabling with different instructions")
	}
	if !a.IsChangeTrackingEnabled() {
		t.Error("should still be enabled after re-enable")
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
