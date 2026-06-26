package agent

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// TestCheckPatchConflict_NoMetadata verifies that when no metadata exists
// for the given path, CheckPatchConflict returns (false, "").
func TestCheckPatchConflict_NoMetadata(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)
	bus := events.NewEventBus()
	agent.SetEventBus(bus)

	conflict, theirsPath := agent.CheckPatchConflict("nonexistent.txt")
	assert.False(t, conflict, "should not report conflict when no metadata exists")
	assert.Empty(t, theirsPath, "theirs_path should be empty when no metadata exists")
}

// TestCheckPatchConflict_NoUnsyncedEdits verifies that when metadata exists
// but BrowserSeq == LastSyncedBrowser (fully synced), CheckPatchConflict
// returns (false, "").
func TestCheckPatchConflict_NoUnsyncedEdits(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	agent.SetFileMetadata("synced.txt", WorkspaceFileMetadata{
		BrowserSeq:        5,
		ContainerSeq:      3,
		LastSyncedBrowser: 5, // Equal to BrowserSeq → fully synced
	})

	conflict, theirsPath := agent.CheckPatchConflict("synced.txt")
	assert.False(t, conflict, "should not report conflict when browser edits are fully synced")
	assert.Empty(t, theirsPath, "theirs_path should be empty when fully synced")
}

// TestCheckPatchConflict_HasUnsyncedEdits verifies that when
// BrowserSeq > LastSyncedBrowser (unsynced browser edits exist),
// CheckPatchConflict returns (true, path+".theirs").
func TestCheckPatchConflict_HasUnsyncedEdits(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	testPath := "conflict.txt"
	agent.SetFileMetadata(testPath, WorkspaceFileMetadata{
		BrowserSeq:        10,
		ContainerSeq:      5,
		LastSyncedBrowser: 7, // Less than BrowserSeq → 3 unsynced edits
	})

	conflict, theirsPath := agent.CheckPatchConflict(testPath)
	require.True(t, conflict, "should report conflict when browser has unsynced edits")
	assert.Equal(t, testPath+".theirs", theirsPath, "theirs_path should be <path>.theirs")
}

// TestCheckPatchConflict_NilAgent verifies that CheckPatchConflict is safe
// to call on a nil agent and returns (false, "") without panicking.
func TestCheckPatchConflict_NilAgent(t *testing.T) {
	var agent *Agent = nil
	conflict, theirsPath := agent.CheckPatchConflict("anything.txt")
	assert.False(t, conflict, "nil agent should not report conflict")
	assert.Empty(t, theirsPath, "nil agent should return empty theirs_path")
}

// TestCheckPatchConflict_EqualNonZeroSeqs verifies that when BrowserSeq and
// LastSyncedBrowser are both non-zero but equal, there is no conflict.
func TestCheckPatchConflict_EqualNonZeroSeqs(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	agent.SetFileMetadata("equal.txt", WorkspaceFileMetadata{
		BrowserSeq:        99,
		ContainerSeq:      50,
		LastSyncedBrowser: 99,
	})

	conflict, theirsPath := agent.CheckPatchConflict("equal.txt")
	assert.False(t, conflict)
	assert.Empty(t, theirsPath)
}

// TestCheckPatchConflict_ZeroSeqs verifies that when all seq values are zero
// (freshly created metadata), there is no conflict.
func TestCheckPatchConflict_ZeroSeqs(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	agent.SetFileMetadata("fresh.txt", WorkspaceFileMetadata{})

	conflict, theirsPath := agent.CheckPatchConflict("fresh.txt")
	assert.False(t, conflict, "zero-value metadata should not indicate a conflict")
	assert.Empty(t, theirsPath)
}

// TestCheckPatchConflict_LargeSeqGap verifies conflict detection with a large
// gap between BrowserSeq and LastSyncedBrowser.
func TestCheckPatchConflict_LargeSeqGap(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	testPath := "largegap.txt"
	agent.SetFileMetadata(testPath, WorkspaceFileMetadata{
		BrowserSeq:        10000,
		ContainerSeq:      1,
		LastSyncedBrowser: 1,
	})

	conflict, theirsPath := agent.CheckPatchConflict(testPath)
	require.True(t, conflict, "should report conflict with large seq gap")
	assert.Equal(t, testPath+".theirs", theirsPath)
}

// TestCheckPatchConflict_ContainerSeqDoesntMatter verifies that ContainerSeq
// vs LastSyncedContainer does NOT trigger a conflict in CheckPatchConflict —
// only BrowserSeq vs LastSyncedBrowser matters (the container-side conflict
// is handled in ApplySyncOp, not here).
func TestCheckPatchConflict_ContainerSeqDoesntMatter(t *testing.T) {
	agent, _ := newTestAgentWithEventBus(t)

	agent.SetFileMetadata("container.txt", WorkspaceFileMetadata{
		BrowserSeq:          5,
		ContainerSeq:        10, // Container ahead of browser
		LastSyncedBrowser:   5,  // Browser fully synced
		LastSyncedContainer: 3,  // Browser hasn't seen latest container writes
	})

	// CheckPatchConflict only checks browser-side unsynced edits.
	// Container-side unsynced writes are NOT a conflict for patch emission.
	conflict, theirsPath := agent.CheckPatchConflict("container.txt")
	assert.False(t, conflict, "container-side unsynced writes should not trigger CheckPatchConflict")
	assert.Empty(t, theirsPath)
}
