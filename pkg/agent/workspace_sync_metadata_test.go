package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_True verifies that
// HasUnsyncedBrowserEdits returns true when BrowserSeq > LastSyncedBrowser.
// This is the core condition that drives conflict detection in both
// CheckPatchConflict (workspace_patch enrichment) and ApplySyncOp
// (browser→container conflict detection).
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_True(t *testing.T) {
	md := WorkspaceFileMetadata{
		BrowserSeq:        10,
		ContainerSeq:      5,
		LastSyncedBrowser: 3,
	}
	assert.True(t, md.HasUnsyncedBrowserEdits(), "should detect 7 unsynced browser edits")
}

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_False_Synced verifies that
// HasUnsyncedBrowserEdits returns false when BrowserSeq == LastSyncedBrowser
// (fully synced state).
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_False_Synced(t *testing.T) {
	md := WorkspaceFileMetadata{
		BrowserSeq:        5,
		ContainerSeq:      3,
		LastSyncedBrowser: 5,
	}
	assert.False(t, md.HasUnsyncedBrowserEdits(), "should not report unsynced edits when fully synced")
}

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_False_Zero verifies that
// zero-value metadata (fresh state) does not report unsynced edits.
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_False_Zero(t *testing.T) {
	md := WorkspaceFileMetadata{}
	assert.False(t, md.HasUnsyncedBrowserEdits(), "zero-value metadata should not report unsynced edits")
}

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_OneOff verifies
// detection with a minimal gap (BrowserSeq = LastSyncedBrowser + 1).
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_OneOff(t *testing.T) {
	md := WorkspaceFileMetadata{
		BrowserSeq:        1,
		ContainerSeq:      0,
		LastSyncedBrowser: 0,
	}
	assert.True(t, md.HasUnsyncedBrowserEdits(), "should detect single unsynced edit")
}

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_LargeSeq verifies
// detection with large sequence numbers (as might occur in long-running sessions).
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_LargeSeq(t *testing.T) {
	md := WorkspaceFileMetadata{
		BrowserSeq:        999999,
		ContainerSeq:      500000,
		LastSyncedBrowser: 999990,
	}
	assert.True(t, md.HasUnsyncedBrowserEdits(), "should detect 9 unsynced edits with large seq values")
}

// TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_ContainerDoesntMatter verifies
// that ContainerSeq vs LastSyncedContainer does NOT affect HasUnsyncedBrowserEdits.
// Only BrowserSeq vs LastSyncedBrowser matters.
func TestWorkspaceFileMetadata_HasUnsyncedBrowserEdits_ContainerDoesntMatter(t *testing.T) {
	md := WorkspaceFileMetadata{
		BrowserSeq:          5,
		ContainerSeq:        20,            // Container far ahead
		LastSyncedBrowser:   5,             // Browser fully synced
		LastSyncedContainer: 1,             // Browser hasn't seen latest container writes
	}
	assert.False(t, md.HasUnsyncedBrowserEdits(), "container-side unsynced writes should not affect HasUnsyncedBrowserEdits")
}

// TestAgentSetAndGetFileMetadata_RoundTrip verifies the SetFileMetadata /
// GetFileMetadata round-trip through the agent's metadata store.
func TestAgentSetAndGetFileMetadata_RoundTrip(t *testing.T) {
	a, _ := newTestAgentWithEventBus(t)

	testPath := "roundtrip.txt"
	original := WorkspaceFileMetadata{
		BrowserSeq:        42,
		ContainerSeq:      10,
		LastSyncedBrowser: 38,
		LastSyncedContainer: 8,
	}

	a.SetFileMetadata(testPath, original)

	retrieved, ok := a.GetFileMetadata(testPath)
	require.True(t, ok, "metadata should be found after setting it")
	assert.Equal(t, original, retrieved, "retrieved metadata should match what was set")
}

// TestAgentSetAndGetFileMetadata_MissingPath verifies that GetFileMetadata
// returns (zero-value, false) when no metadata exists for the given path.
func TestAgentSetAndGetFileMetadata_MissingPath(t *testing.T) {
	a, _ := newTestAgentWithEventBus(t)

	retrieved, ok := a.GetFileMetadata("missing.txt")
	assert.False(t, ok, "should not find metadata for a path that was never set")
	assert.Equal(t, WorkspaceFileMetadata{}, retrieved, "should return zero-value metadata for missing path")
}

// TestAgentSetAndGetFileMetadata_NilAgent verifies that GetFileMetadata is
// safe on a nil agent.
func TestAgentSetAndGetFileMetadata_NilAgent(t *testing.T) {
	var a *Agent = nil
	retrieved, ok := a.GetFileMetadata("anything.txt")
	assert.False(t, ok, "nil agent should return false")
	assert.Equal(t, WorkspaceFileMetadata{}, retrieved, "nil agent should return zero-value metadata")
}

// TestAgentSetFileMetadata_NilAgent verifies that SetFileMetadata is safe
// on a nil agent without panicking.
func TestAgentSetFileMetadata_NilAgent(t *testing.T) {
	var a *Agent = nil
	// Should not panic
	a.SetFileMetadata("anything.txt", WorkspaceFileMetadata{BrowserSeq: 1})
}

// TestAgentSetAndGetFileMetadata_Overwrite verifies that setting metadata
// for the same path a second time overwrites the previous value.
func TestAgentSetAndGetFileMetadata_Overwrite(t *testing.T) {
	a, _ := newTestAgentWithEventBus(t)

	testPath := "overwrite.txt"
	a.SetFileMetadata(testPath, WorkspaceFileMetadata{BrowserSeq: 1})
	a.SetFileMetadata(testPath, WorkspaceFileMetadata{BrowserSeq: 99, ContainerSeq: 50})

	retrieved, ok := a.GetFileMetadata(testPath)
	require.True(t, ok)
	assert.Equal(t, int64(99), retrieved.BrowserSeq, "should have the overwritten BrowserSeq")
	assert.Equal(t, int64(50), retrieved.ContainerSeq, "should have the overwritten ContainerSeq")
}

// TestAgentCheckPatchConflict_Integration is an end-to-end test that
// exercises the same conflict detection path used in workspace_patch
// event emission: set metadata with unsynced browser edits, then call
// CheckPatchConflict and verify the result matches what the tool handlers
// would see when publishing a workspace_patch event.
func TestAgentCheckPatchConflict_Integration(t *testing.T) {
	a, _ := newTestAgentWithEventBus(t)

	// Simulate the browser having written 5 edits that haven't been synced
	testPath := "agent_test.go"
	a.SetFileMetadata(testPath, WorkspaceFileMetadata{
		BrowserSeq:        100,
		ContainerSeq:      50,
		LastSyncedBrowser: 95, // 5 unsynced edits
	})

	conflict, theirsPath := a.CheckPatchConflict(testPath)
	require.True(t, conflict, "should detect conflict with 5 unsynced browser edits")
	assert.Equal(t, testPath+".theirs", theirsPath, "theirs_path should be <path>.theirs")
}

// TestAgentCheckPatchConflict_AfterSync verifies that after syncing
// (setting LastSyncedBrowser to equal BrowserSeq), the conflict goes away.
func TestAgentCheckPatchConflict_AfterSync(t *testing.T) {
	a, _ := newTestAgentWithEventBus(t)

	testPath := "synced_after.txt"
	// Start with unsynced edits
	a.SetFileMetadata(testPath, WorkspaceFileMetadata{
		BrowserSeq:        100,
		ContainerSeq:      50,
		LastSyncedBrowser: 95,
	})

	conflict, _ := a.CheckPatchConflict(testPath)
	require.True(t, conflict, "should detect conflict before sync")

	// Now sync: update LastSyncedBrowser to match BrowserSeq
	a.SetFileMetadata(testPath, WorkspaceFileMetadata{
		BrowserSeq:        100,
		ContainerSeq:      51,
		LastSyncedBrowser: 100, // Synced!
	})

	conflict, _ = a.CheckPatchConflict(testPath)
	assert.False(t, conflict, "should NOT detect conflict after syncing")
}

// TestWorkspaceFileMetadata_ZeroValue_Safe verifies that zero-value
// WorkspaceFileMetadata can be compared and used without issues.
func TestWorkspaceFileMetadata_ZeroValue_Safe(t *testing.T) {
	var md WorkspaceFileMetadata
	assert.False(t, md.HasUnsyncedBrowserEdits())
	assert.Equal(t, int64(0), md.BrowserSeq)
	assert.Equal(t, int64(0), md.ContainerSeq)
	assert.Equal(t, int64(0), md.LastSyncedBrowser)
	assert.Equal(t, int64(0), md.LastSyncedContainer)
}
