package changes

// Test seam for pkg/agent (SP-141 phase 2). Before the extraction,
// pkg/agent's tests built bare `&ChangeTracker{enabled: true, ...}`
// literals, which cannot reach the unexported fields across the package
// boundary. These helpers expose exactly the fixture surface those
// tests used — no more — and are documented as test-only. They avoid
// NewChangeTracker deliberately: the constructor calls
// history.InitializeHistoryPaths, a global side effect the old literals
// never triggered.

// NewTrackerForTesting returns a minimal enabled tracker for tests
// that exercise tracker methods directly: no agent view, no
// history-path initialization. revisionID may be empty.
func NewTrackerForTesting(revisionID string) *ChangeTracker {
	return &ChangeTracker{
		revisionID: revisionID,
		sessionID:  "test-session",
		enabled:    true,
	}
}

// SetTrackedChangesForTesting replaces the tracker's change buffer.
// Test seam: production code appends through the Track* methods.
func (ct *ChangeTracker) SetTrackedChangesForTesting(changes []TrackedFileChange) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.changes = changes
}
