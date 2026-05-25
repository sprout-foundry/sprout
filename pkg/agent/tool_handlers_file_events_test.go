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

// newTestAgentWithEventBus creates a minimal Agent configured for testing
// tool handlers that emit events via the event bus.
func newTestAgentWithEventBus(t *testing.T) (*Agent, *events.EventBus) {
	t.Helper()

	bus := events.NewEventBus()

	agent := &Agent{
		output:       NewAgentOutputManager(),
		state:        NewAgentStateManager(false),
		security:     NewAgentSecurityManager(),
		interruptCtx: context.Background(),
	}
	agent.SetEventBus(bus)

	// Enable unsafe mode so file operations outside the workspace root
	// don't trigger security approval prompts.
	agent.SetUnsafeMode(true)

	return agent, bus
}

// expectFileChangedEvent waits for a file_changed event on the given channel
// and verifies its type, file_path, and action fields.
func expectFileChangedEvent(t *testing.T, ch <-chan events.UIEvent, expectedPath, expectedAction string) map[string]interface{} {
	t.Helper()

	select {
	case event := <-ch:
		require.Equal(t, events.EventTypeFileChanged, event.Type, "expected file_changed event type")
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok, "event data should be a map[string]interface{}")

		actualPath, ok := data["file_path"].(string)
		require.True(t, ok, "file_path should be a string")
		assert.Equal(t, expectedPath, actualPath, "file_path mismatch")

		actualAction, ok := data["action"].(string)
		require.True(t, ok, "action should be a string")
		assert.Equal(t, expectedAction, actualAction, "action mismatch")

		return data
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for file_changed event")
		return nil
	}
}

// TestWriteFileEmitsFileChangedEvent verifies that handleWriteFile publishes
// a file_changed event with action "write" after a successful file write.
func TestWriteFileEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("write_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "hello.txt")
	content := "hello world\n"

	result, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": content,
	})
	require.NoError(t, err)
	assert.Contains(t, result, filePath, "result should mention the file path")

	// Verify file was actually written on disk
	written, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, content, string(written))

	// Verify the file_changed event was published
	data := expectFileChangedEvent(t, ch, filePath, "write")

	// The content field should be the written content
	actualContent, ok := data["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Equal(t, content, actualContent, "event content should match written content")
}

// TestEditFileEmitsFileChangedEvent verifies that handleEditFile publishes
// a file_changed event with action "edit" after a successful file edit.
func TestEditFileEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("edit_test")

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

	// Verify file was actually edited on disk
	edited, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(edited), "key = new_value")
	assert.NotContains(t, string(edited), "key = old_value")

	// Verify the file_changed event was published
	data := expectFileChangedEvent(t, ch, filePath, "edit")

	// The content field should reflect the edited file content
	actualContent, ok := data["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Contains(t, actualContent, "key = new_value", "event content should reflect the edit")
}

// TestWriteStructuredFileEmitsFileChangedEvent verifies that
// handleWriteStructuredFile publishes a file_changed event for JSON files.
func TestWriteStructuredFileEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("structured_write_test")

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

	// Verify file was actually written on disk
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Verify the file_changed event was published
	eventData := expectFileChangedEvent(t, ch, filePath, "write")

	// The content should contain the JSON output
	actualContent, ok := eventData["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Contains(t, actualContent, "sprout", "event content should contain the JSON data")
}

// TestWriteStructuredFileYAMLEmitsFileChangedEvent verifies that
// handleWriteStructuredFile publishes a file_changed event for YAML files.
func TestWriteStructuredFileYAMLEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("yaml_write_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "config.yaml")

	data := map[string]interface{}{
		"app": "sprout",
		"port": 8080,
	}

	result, err := handleWriteStructuredFile(context.Background(), agent, map[string]interface{}{
		"path":   filePath,
		"format": "yaml",
		"data":   data,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Verify file was actually written on disk
	_, err = os.Stat(filePath)
	require.NoError(t, err)

	// Verify the file_changed event was published
	eventData := expectFileChangedEvent(t, ch, filePath, "write")

	// The content should contain the YAML output
	actualContent, ok := eventData["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Contains(t, actualContent, "sprout", "event content should contain the YAML data")
}

// TestPatchStructuredFileEmitsFileChangedEvent verifies that
// handlePatchStructuredFile publishes a file_changed event after a successful patch.
func TestPatchStructuredFileEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_test")

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

	// Mark file as read this turn to satisfy the staleness check
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

	// Verify file was actually patched on disk
	patched, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Contains(t, string(patched), "new-name")
	assert.Contains(t, string(patched), "a test agent")

	// Verify the file_changed event was published
	eventData := expectFileChangedEvent(t, ch, filePath, "write")

	// The content should contain the patched JSON
	actualContent, ok := eventData["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Contains(t, actualContent, "new-name", "event content should contain patched data")
}

// TestPatchStructuredFileAsWriteEmitsFileChangedEvent verifies that when
// patch_structured_file is called with 'data' instead of 'patch_ops'
// (compatibility path), it still emits a file_changed event.
func TestPatchStructuredFileAsWriteEmitsFileChangedEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("patch_as_write_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "data.json")

	data := map[string]interface{}{
		"key":  "value",
		"num":  42,
	}

	result, err := handlePatchStructuredFile(context.Background(), agent, map[string]interface{}{
		"path":   filePath,
		"format": "json",
		"data":   data,
	})
	require.NoError(t, err)
	assert.NotEmpty(t, result)

	// Verify the file_changed event was published (this goes through write path)
	eventData := expectFileChangedEvent(t, ch, filePath, "write")

	actualContent, ok := eventData["content"].(string)
	require.True(t, ok, "content should be a string")
	assert.Contains(t, actualContent, "value", "event content should contain the data")
}

// TestWriteFileEmitsFileChangedEventWithMetadata verifies that event metadata
// (e.g. client_id, chat_id) is merged into the published event payload.
func TestWriteFileEmitsFileChangedEventWithMetadata(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("metadata_test")

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

	select {
	case event := <-ch:
		require.Equal(t, events.EventTypeFileChanged, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)

		// Verify core file_changed fields
		assert.Equal(t, filePath, data["file_path"])
		assert.Equal(t, "write", data["action"])

		// Verify metadata was merged in by decorateEventPayload
		assert.Equal(t, "test-client-123", data["client_id"], "client_id should be merged from event metadata")
		assert.Equal(t, "chat-456", data["chat_id"], "chat_id should be merged from event metadata")
	case <-time.After(500 * time.Millisecond):
		t.Fatal("timed out waiting for file_changed event with metadata")
	}
}

// TestWriteFileNoEventBusDoesNotPanic verifies that handleWriteFile does not
// panic when the agent has no event bus set (CLI-only mode).
func TestWriteFileNoEventBusDoesNotPanic(t *testing.T) {
	agent := &Agent{
		output:       NewAgentOutputManager(),
		state:        NewAgentStateManager(false),
		security:     NewAgentSecurityManager(),
		interruptCtx: context.Background(),
	}
	agent.SetUnsafeMode(true)

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)
	filePath := filepath.Join(tmpDir, "no-bus.txt")

	assert.NotPanics(t, func() {
		_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
			"path":    filePath,
			"content": "hello",
		})
		assert.NoError(t, err)
	})
}

// TestEditFileNoEventBusDoesNotPanic verifies that handleEditFile does not
// panic when the agent has no event bus set.
func TestEditFileNoEventBusDoesNotPanic(t *testing.T) {
	agent := &Agent{
		output:       NewAgentOutputManager(),
		state:        NewAgentStateManager(false),
		security:     NewAgentSecurityManager(),
		interruptCtx: context.Background(),
	}
	agent.SetUnsafeMode(true)

	tmpDir := t.TempDir()
	agent.SetWorkspaceRoot(tmpDir)
	filePath := filepath.Join(tmpDir, "no-bus-edit.txt")

	err := os.WriteFile(filePath, []byte("original content"), 0644)
	require.NoError(t, err)

	assert.NotPanics(t, func() {
		_, err := handleEditFile(context.Background(), agent, map[string]interface{}{
			"path":    filePath,
			"old_str": "original",
			"new_str": "edited",
		})
		assert.NoError(t, err)
	})
}

// TestWriteFileMultipleEvents verifies that multiple writes emit multiple
// file_changed events, each with the correct path and content.
func TestWriteFileMultipleEvents(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("multi_test")

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

	// Expect event for file A
	dataA := expectFileChangedEvent(t, ch, fileA, "write")
	assert.Equal(t, "content A", dataA["content"])

	// Expect event for file B
	dataB := expectFileChangedEvent(t, ch, fileB, "write")
	assert.Equal(t, "content B", dataB["content"])
}

// TestWriteStructuredFileDisallowRawWriteInTextFile verifies that write_file
// is not allowed for structured files (.yaml, .yml) and returns an error
// without emitting a file_changed event.
// Note: .json files are routed through the structured write path in handleWriteFile,
// so we test .yaml here which goes through writeFileContent with the disallow check.
func TestWriteStructuredFileDisallowRawWriteInTextFile(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("disallow_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "bad.yaml")

	_, err := handleWriteFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"content": "key: value",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not allowed for structured files")

	// No file_changed event should be published for failed writes
	select {
	case event := <-ch:
		t.Fatalf("expected no event, got: %s", event.Type)
	case <-time.After(100 * time.Millisecond):
		// Good: no event published
	}
}

// TestEditFileInvalidOldStrNoEvent verifies that when handleEditFile fails
// (e.g. old_str not found), no file_changed event is published.
func TestEditFileInvalidOldStrNoEvent(t *testing.T) {
	agent, bus := newTestAgentWithEventBus(t)
	ch := bus.Subscribe("edit_fail_test")

	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "fail-edit.txt")

	err := os.WriteFile(filePath, []byte("some content"), 0644)
	require.NoError(t, err)

	_, err = handleEditFile(context.Background(), agent, map[string]interface{}{
		"path":    filePath,
		"old_str": "this string does not exist in the file",
		"new_str": "replacement",
	})
	require.Error(t, err)

	// No event should be published for failed edits
	select {
	case event := <-ch:
		t.Fatalf("expected no event on failure, got: %s", event.Type)
	case <-time.After(100 * time.Millisecond):
		// Good: no event published
	}
}
