package agent

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// expectWorkspacePatchEvent waits for a workspace_patch event on the given
// channel, draining any non-workspace_patch events (e.g. file_changed) that
// arrive first. Both events are published for each write, so the subscriber
// channel sees both in order: file_changed, then workspace_patch.
func expectWorkspacePatchEvent(t *testing.T, ch <-chan events.UIEvent, expectedPath, expectedAction string) map[string]interface{} {
	t.Helper()

	for {
		select {
		case event := <-ch:
			if event.Type == events.EventTypeWorkspacePatch {
				data, ok := event.Data.(map[string]interface{})
				require.True(t, ok, "event data should be a map[string]interface{}")

				actualPath, ok := data["file_path"].(string)
				require.True(t, ok, "file_path should be a string")
				assert.Equal(t, expectedPath, actualPath, "file_path mismatch")

				actualAction, ok := data["action"].(string)
				require.True(t, ok, "action should be a string")
				assert.Equal(t, expectedAction, actualAction, "action mismatch")

				// Verify seq is present and positive
				seq, ok := data["seq"].(int64)
				require.True(t, ok, "seq should be int64")
				assert.Positive(t, seq, "seq should be positive")

				return data
			}
			// Not a workspace_patch event — skip it (e.g. file_changed)
			// and continue waiting.
		case <-time.After(500 * time.Millisecond):
			t.Fatal("timed out waiting for workspace_patch event")
			return nil
		}
	}
}

// expectNoWorkspacePatchEvent verifies that no workspace_patch event is
// published within the timeout window. Used for failure-path tests.
func expectNoWorkspacePatchEvent(t *testing.T, ch <-chan events.UIEvent) {
	t.Helper()

	select {
	case event := <-ch:
		// If we got an event, it should NOT be a workspace_patch.
		// It could be a file_changed or another event type.
		assert.NotEqual(t, events.EventTypeWorkspacePatch, event.Type,
			"expected no workspace_patch event but got one")
	case <-time.After(100 * time.Millisecond):
		// Good: no event published at all
	}
}

// TestNextPatchSeqMonotonic verifies that nextPatchSeq returns strictly
// increasing values on successive calls. The first value must be >= 1
// since atomic.AddInt64 starts from 0 and returns the new value.
func TestNextPatchSeqMonotonic(t *testing.T) {
	var prev int64
	for i := 0; i < 100; i++ {
		seq := nextPatchSeq()
		assert.Greater(t, seq, prev, "seq should be strictly increasing at call %d", i)
		prev = seq
	}
	// The first value should be >= 1 (counter starts at 0, AddInt64 returns post-increment)
	assert.GreaterOrEqual(t, prev, int64(100), "last seq should be at least 100 after 100 calls")
}

// TestWorkspacePatchEventCreation verifies that events.WorkspacePatchEvent
// constructs a properly shaped map with all required fields.
func TestWorkspacePatchEventCreation(t *testing.T) {
	data := events.WorkspacePatchEvent("/path/to/file.txt", "content", "write", 42)

	assert.Equal(t, "/path/to/file.txt", data["file_path"])
	assert.Equal(t, "content", data["content"])
	assert.Equal(t, "write", data["action"])
	assert.Equal(t, int64(42), data["seq"])
}

// TestWriteFileEmitsWorkspacePatchEvent verifies that handleWriteFile
// publishes a workspace_patch event with action "write" after a
// successful file write, including a positive sequence number.
func TestWriteFileEmitsWorkspacePatchEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_write_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "hello.txt")
	content := "hello world\n"

	result, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)
	assert.Contains(t, result, filePath, "result should mention the file path")

	// Expect the workspace_patch event (helper drains file_changed first)
	data := expectWorkspacePatchEvent(t, ch, filePath, "write")

	// Verify content matches
	assert.Equal(t, content, data["content"], "event content should match written content")
}

// TestEditFileEmitsWorkspacePatchEvent verifies that handleEditFile
// publishes a workspace_patch event with action "edit" after a
// successful file edit, including a positive sequence number.
func TestEditFileEmitsWorkspacePatchEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_edit_test")

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)
	filePath := filepath.Join(tmpDir, "config.txt")

	// Create initial file
	initialContent := "key = old_value\nother = data\n"
	err := os.WriteFile(filePath, []byte(initialContent), 0644)
	require.NoError(t, err)

	result, err := handleEditFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"old_str": "key = old_value",
		"new_str": "key = new_value",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Expect the workspace_patch event (helper drains file_changed first)
	data := expectWorkspacePatchEvent(t, ch, filePath, "edit")

	// Verify content reflects the edit
	assert.Contains(t, data["content"], "key = new_value", "event content should reflect the edit")
}

// TestWriteStructuredFileEmitsWorkspacePatchEvent verifies that
// handleWriteStructuredFile publishes a workspace_patch event with
// action "write" for both JSON and YAML files.
func TestWriteStructuredFileEmitsWorkspacePatchEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_structured_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "data.json")

	data := map[string]interface{}{
		"name":    "sprout",
		"version": 2,
		"tags":    []interface{}{"agent", "ai"},
	}

	result, err := handleWriteStructuredFile(context.Background(), agent, map[string]interface{}{
		"path":   filePath,
		"format": "json",
		"data":   data,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Expect the workspace_patch event
	eventData := expectWorkspacePatchEvent(t, ch, filePath, "write")

	// The content should contain the serialized JSON
	assert.Contains(t, eventData["content"], "sprout", "event content should contain the JSON data")
}

// TestPatchStructuredFileEmitsWorkspacePatchEvent verifies that
// handlePatchStructuredFile publishes a workspace_patch event after
// a successful patch operation. The action is "write" since patches
// go through writeFileContent.
func TestPatchStructuredFileEmitsWorkspacePatchEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_structured_patch_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "config.json")

	// Create initial JSON file
	initialContent := `{
  "name": "old-name",
  "version": 1,
  "enabled": true
}
`
	err := os.WriteFile(filePath, []byte(initialContent), 0644)
	require.NoError(t, err)

	// Mark the file as read so the staleness guard doesn't block the patch
	agent.RecordFileReadThisTurn(filePath)

	result, err := handlePatchStructuredFile(context.Background(), agent, map[string]interface{}{
		"path":   filePath,
		"format": "json",
		"patch_ops": []interface{}{
			map[string]interface{}{
				"op":    "replace",
				"path":  "/name",
				"value": "new-name",
			},
			map[string]interface{}{
				"op":    "add",
				"path":  "/description",
				"value": "a test agent",
			},
		},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Expect the workspace_patch event (action is "write" since it goes
	// through writeFileContent)
	eventData := expectWorkspacePatchEvent(t, ch, filePath, "write")

	// The content should contain the patched data
	assert.Contains(t, eventData["content"], "new-name", "event content should contain patched data")
}

// TestWorkspacePatchSeqIncrement verifies that when multiple files are
// written in sequence, the seq field in workspace_patch events is
// strictly increasing.
func TestWorkspacePatchSeqIncrement(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_seq_test")

	tmpDir := t.TempDir()
	fileA := filepath.Join(tmpDir, "a.txt")
	fileB := filepath.Join(tmpDir, "b.txt")

	// Write file A
	_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    fileA,
		"content": "content A",
	})
	require.NoError(t, err)

	// Write file B
	_, err = handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    fileB,
		"content": "content B",
	})
	require.NoError(t, err)

	// Expect workspace_patch for file A
	dataA := expectWorkspacePatchEvent(t, ch, fileA, "write")
	seqA, ok := dataA["seq"].(int64)
	require.True(t, ok, "seq A should be int64")

	// Expect workspace_patch for file B
	dataB := expectWorkspacePatchEvent(t, ch, fileB, "write")
	seqB, ok := dataB["seq"].(int64)
	require.True(t, ok, "seq B should be int64")

	// Seq numbers must be strictly increasing
	assert.Greater(t, seqB, seqA, "second workspace_patch seq (%d) should be greater than first (%d)", seqB, seqA)
}

// TestWorkspacePatchEventIncludesMetadata verifies that event metadata
// (client_id, chat_id) is merged into the workspace_patch event payload
// via decorateEventPayload, the same as file_changed events.
func TestWorkspacePatchEventIncludesMetadata(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_metadata_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "meta.txt")
	content := "test"

	// Set event metadata on the agent
	agent.SetEventMetadata(map[string]interface{}{
		"client_id": "test-client-123",
		"chat_id":   "chat-456",
	})

	_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)

	// Get the workspace_patch event (helper drains file_changed first)
	data := expectWorkspacePatchEvent(t, ch, filePath, "write")

	// Verify metadata was merged in by decorateEventPayload
	assert.Equal(t, "test-client-123", data["client_id"], "client_id should be merged from event metadata")
	assert.Equal(t, "chat-456", data["chat_id"], "chat_id should be merged from event metadata")
}

// TestWriteFileNoWorkspacePatchOnFailure verifies that when a write
// fails (e.g. writing to a directory that doesn't exist), no
// workspace_patch event is published.
func TestWriteFileNoWorkspacePatchOnFailure(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_failure_test")

	// Attempt to write to a deeply nested non-existent directory
	filePath := "/tmp/sprout-test-nonexistent-dir/" + filepath.Join("a", "b", "c", "d", "impossible.txt")

	_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": "this should fail",
	})
	require.Error(t, err, "writing to non-existent directory should fail")

	// No workspace_patch should be published on failure
	expectNoWorkspacePatchEvent(t, ch)
}

// TestWorkspacePatchRegisteredInOutboundTypes verifies that the
// workspace_patch event type string is correctly defined and can be
// added to the outbound registry. The actual presence in
// allowedOutboundMessageTypes is maintained by the sync contract
// (SP-034-6a) — this test ensures the event type constant and the
// outbound registry agree on the string value.
func TestWorkspacePatchRegisteredInOutboundTypes(t *testing.T) {
	// Verify the event type constant has the expected string value
	assert.Equal(t, "workspace_patch", events.EventTypeWorkspacePatch,
		"EventTypeWorkspacePatch should equal the literal used in the outbound registry")

	// The outbound registry (pkg/webui/websocket_outbound_registry.go) includes
	// events.EventTypeWorkspacePatch in allowedOutboundMessageTypes at init time.
	// This is verified indirectly: if the constant's value diverges, the
	// registry key would silently stop matching. A complementary test in
	// the webui package (or the existing registry-sync assertions) catches
	// stale entries. Here we verify the constant is what we expect.
}

// TestWriteFileEmitsBothEventsInOrder verifies that handleWriteFile
// publishes both file_changed and workspace_patch events, and that they
// arrive in the correct order: file_changed first, then workspace_patch.
// This ordering is important because the frontend may use file_changed
// for general notification and workspace_patch for the actual content.
func TestWriteFileEmitsBothEventsInOrder(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_both_order_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "order.txt")
	content := "ordered content\n"

	result, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)
	assert.Contains(t, result, filePath, "result should mention the file path")

	// First event should be file_changed
	event1 := <-ch
	assert.Equal(t, events.EventTypeFileChanged, event1.Type,
		"first event should be file_changed")
	data1, ok := event1.Data.(map[string]interface{})
	require.True(t, ok, "file_changed data should be a map")
	assert.Equal(t, filePath, data1["file_path"])
	assert.Equal(t, "write", data1["action"])

	// Second event should be workspace_patch
	event2 := <-ch
	assert.Equal(t, events.EventTypeWorkspacePatch, event2.Type,
		"second event should be workspace_patch")
	data2, ok := event2.Data.(map[string]interface{})
	require.True(t, ok, "workspace_patch data should be a map")
	assert.Equal(t, filePath, data2["file_path"])
	assert.Equal(t, "write", data2["action"])
	assert.Equal(t, content, data2["content"])
}

// expectWorkspacePatchConflict checks the workspace_patch event data for
// conflict-related fields. When expectConflict is true, it asserts that
// the event contains conflict=true and the given theirs_path. When false,
// it asserts that conflict and theirs_path keys are absent.
func expectWorkspacePatchConflict(t *testing.T, data map[string]interface{}, expectConflict bool, expectedTheirsPath string) {
	t.Helper()

	if expectConflict {
		assert.Contains(t, data, "conflict", "workspace_patch event should contain conflict key when browser has unsynced edits")
		assert.Equal(t, true, data["conflict"], "conflict should be true")
		assert.Contains(t, data, "theirs_path", "workspace_patch event should contain theirs_path key when browser has unsynced edits")
		assert.Equal(t, expectedTheirsPath, data["theirs_path"], "theirs_path should match expected value")
	} else {
		assert.NotContains(t, data, "conflict", "workspace_patch event should NOT contain conflict key when browser has no unsynced edits")
		assert.NotContains(t, data, "theirs_path", "workspace_patch event should NOT contain theirs_path key when browser has no unsynced edits")
	}
}

// TestWriteFileWithConflictRefused verifies that handleWriteFile REFUSES to
// write when the file has unsynced browser edits (checkWriteStaleness blocks
// it), so no workspace_patch event is emitted with conflict metadata. This
// is correct: the agent must ask the user before overwriting. The conflict
// detection via CheckPatchConflict in writeFileContent is only reachable when
// the write succeeds (no staleness block).
func TestWriteFileWithConflictRefused(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_write_refused_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "refused.txt")
	content := "conflict content\n"

	// Set up metadata showing the browser has unsynced edits
	agent.SetFileMetadata(filePath, WorkspaceFileMetadata{
		BrowserSeq:        10,
		ContainerSeq:      3,
		LastSyncedBrowser: 5,
	})

	// Write should be REFUSED because of unsynced browser edits
	_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.Error(t, err, "write should be refused when browser has unsynced edits")
	assert.Contains(t, err.Error(), "unsynced edits", "error should mention unsynced edits")

	// No events should be published since the write was refused
	select {
	case event := <-ch:
		t.Fatalf("no events expected after refused write, got %s", event.Type)
	case <-time.After(200 * time.Millisecond):
		// OK — no event published
	}
}

// TestWriteFileWithoutConflictEmitsNoConflictFields verifies that when
// a workspace_patch event is published for a file that has NO unsynced
// browser edits (BrowserSeq == LastSyncedBrowser), the event does NOT
// include conflict or theirs_path keys.
func TestWriteFileWithoutConflictEmitsNoConflictFields(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_no_conflict_write_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "noconflict.txt")
	content := "no conflict content\n"

	// Set up metadata showing browser is fully synced
	agent.SetFileMetadata(filePath, WorkspaceFileMetadata{
		BrowserSeq:        5,
		ContainerSeq:      3,
		LastSyncedBrowser: 5, // Equal to BrowserSeq → fully synced
	})

	result, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)
	assert.Contains(t, result, filePath, "result should mention the file path")

	// Expect the workspace_patch event WITHOUT conflict metadata
	data := expectWorkspacePatchEvent(t, ch, filePath, "write")

	// Verify conflict fields are NOT present
	expectWorkspacePatchConflict(t, data, false, "")
}

// TestWriteFileNoMetadataEmitsNoConflictFields verifies that when there
// is NO metadata for a file at all, the workspace_patch event does NOT
// include conflict or theirs_path keys.
func TestWriteFileNoMetadataEmitsNoConflictFields(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_no_metadata_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "nometa.txt")
	content := "no metadata content\n"

	// Do NOT set any metadata — file has no entry in the metadata store
	result, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)
	assert.Contains(t, result, filePath, "result should mention the file path")

	data := expectWorkspacePatchEvent(t, ch, filePath, "write")
	expectWorkspacePatchConflict(t, data, false, "")
}

// TestEditFileWithConflictEmitsConflictPatch verifies that when
// handleEditFile publishes a workspace_patch event for a file with
// unsynced browser edits, the event includes conflict metadata.
// Unlike handleWriteFile (which is blocked by checkWriteStaleness),
// handleEditFile does NOT call checkWriteStaleness, so edits can
// succeed and the conflict detection in the event emission path runs.
func TestEditFileWithConflictEmitsConflictPatch(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_conflict_edit_test")

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)
	filePath := filepath.Join(tmpDir, "edit_conflict.txt")

	// Create initial file
	initialContent := "old line\nkeep this\n"
	err := os.WriteFile(filePath, []byte(initialContent), 0644)
	require.NoError(t, err)

	// Set up metadata showing the browser has unsynced edits
	agent.SetFileMetadata(filePath, WorkspaceFileMetadata{
		BrowserSeq:        8,
		ContainerSeq:      2,
		LastSyncedBrowser: 3,
	})

	result, err := handleEditFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"old_str": "old line",
		"new_str": "new line",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Expect the workspace_patch event with conflict metadata
	data := expectWorkspacePatchEvent(t, ch, filePath, "edit")

	// Verify conflict fields are present
	expectWorkspacePatchConflict(t, data, true, filePath+".theirs")
}

// TestEditFileWithoutConflictEmitsNoConflictFields verifies that when
// handleEditFile publishes a workspace_patch event for a file with NO
// unsynced browser edits, the event does NOT include conflict fields.
func TestEditFileWithoutConflictEmitsNoConflictFields(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_no_conflict_edit_test")

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)
	filePath := filepath.Join(tmpDir, "edit_noconflict.txt")

	// Create initial file
	initialContent := "original value\nsome data\n"
	err := os.WriteFile(filePath, []byte(initialContent), 0644)
	require.NoError(t, err)

	// Set up metadata showing browser is fully synced
	agent.SetFileMetadata(filePath, WorkspaceFileMetadata{
		BrowserSeq:        4,
		ContainerSeq:      2,
		LastSyncedBrowser: 4,
	})

	result, err := handleEditFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"old_str": "original value",
		"new_str": "updated value",
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Expect the workspace_patch event WITHOUT conflict metadata
	data := expectWorkspacePatchEvent(t, ch, filePath, "edit")

	// Verify conflict fields are NOT present
	expectWorkspacePatchConflict(t, data, false, "")
}
