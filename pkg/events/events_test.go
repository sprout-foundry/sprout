package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWorkspacePatchEvent_NoConflictInfo verifies that calling
// WorkspacePatchEvent with only the required 4 arguments does NOT include
// conflict or theirs_path keys in the returned map.
func TestWorkspacePatchEvent_NoConflictInfo(t *testing.T) {
	data := WorkspacePatchEvent("/path/to/file.txt", "content", "write", 42)

	assert.Equal(t, "/path/to/file.txt", data["file_path"])
	assert.Equal(t, "content", data["content"])
	assert.Equal(t, "write", data["action"])
	assert.Equal(t, int64(42), data["seq"])

	// Must NOT contain conflict or theirs_path keys
	assert.NotContains(t, data, "conflict", "no conflict key when called without conflict info")
	assert.NotContains(t, data, "theirs_path", "no theirs_path key when called without conflict info")
}

// TestWorkspacePatchEvent_ConflictFalse verifies that when a
// PatchConflictInfo with Conflict=false is provided, the returned map
// does NOT include conflict or theirs_path keys.
func TestWorkspacePatchEvent_ConflictFalse(t *testing.T) {
	data := WorkspacePatchEvent(
		"/path/to/file.txt", "content", "edit", 10,
		PatchConflictInfo{Conflict: false, TheirsPath: ""},
	)

	assert.Equal(t, "/path/to/file.txt", data["file_path"])
	assert.Equal(t, "content", data["content"])
	assert.Equal(t, "edit", data["action"])
	assert.Equal(t, int64(10), data["seq"])

	// Conflict=false must NOT add conflict or theirs_path keys
	assert.NotContains(t, data, "conflict", "no conflict key when Conflict is false")
	assert.NotContains(t, data, "theirs_path", "no theirs_path key when Conflict is false")
}

// TestWorkspacePatchEvent_ConflictTrue verifies that when a
// PatchConflictInfo with Conflict=true and a TheirsPath is provided,
// the returned map includes conflict=true and theirs_path set to the
// provided theirs path.
func TestWorkspacePatchEvent_ConflictTrue(t *testing.T) {
	theirsPath := "/path/to/file.txt.theirs"
	data := WorkspacePatchEvent(
		"/path/to/file.txt", "content", "write", 55,
		PatchConflictInfo{Conflict: true, TheirsPath: theirsPath},
	)

	assert.Equal(t, "/path/to/file.txt", data["file_path"])
	assert.Equal(t, "content", data["content"])
	assert.Equal(t, "write", data["action"])
	assert.Equal(t, int64(55), data["seq"])

	// Conflict=true MUST include conflict and theirs_path keys
	assert.Contains(t, data, "conflict", "conflict key must be present when Conflict is true")
	assert.Equal(t, true, data["conflict"], "conflict must be true")
	assert.Contains(t, data, "theirs_path", "theirs_path key must be present when Conflict is true")
	assert.Equal(t, theirsPath, data["theirs_path"], "theirs_path must match the provided value")
}

// TestWorkspacePatchEvent_BackwardCompatibilityWithFourArgs verifies
// that calling WorkspacePatchEvent with just 4 arguments (the original
// signature) still works correctly — no panic, no missing required keys.
func TestWorkspacePatchEvent_BackwardCompatibilityWithFourArgs(t *testing.T) {
	data := WorkspacePatchEvent("foo.txt", "bar", "edit", 1)

	require.Contains(t, data, "file_path")
	require.Contains(t, data, "content")
	require.Contains(t, data, "action")
	require.Contains(t, data, "seq")

	assert.Equal(t, "foo.txt", data["file_path"])
	assert.Equal(t, "bar", data["content"])
	assert.Equal(t, "edit", data["action"])
	assert.Equal(t, int64(1), data["seq"])
	assert.NotContains(t, data, "conflict")
	assert.NotContains(t, data, "theirs_path")
}

func TestNewEventBus(t *testing.T) {
	eb := NewEventBus()
	assert.NotNil(t, eb)
	assert.NotNil(t, eb.subscribers)
}

func TestEventBus_Subscribe(t *testing.T) {
	eb := NewEventBus()

	ch := eb.Subscribe("test-subscriber")
	assert.NotNil(t, ch)

	// Verify subscriber was added
	eb.mutex.RLock()
	_, exists := eb.subscribers["test-subscriber"]
	eb.mutex.RUnlock()
	assert.True(t, exists)
}

func TestEventBus_Unsubscribe(t *testing.T) {
	eb := NewEventBus()

	// Subscribe and then unsubscribe
	eb.Subscribe("test-subscriber")
	eb.Unsubscribe("test-subscriber")

	// Verify subscriber was removed
	eb.mutex.RLock()
	_, exists := eb.subscribers["test-subscriber"]
	eb.mutex.RUnlock()
	assert.False(t, exists)
}

func TestEventBus_Publish(t *testing.T) {
	eb := NewEventBus()

	ch := eb.Subscribe("test-subscriber")

	// Publish an event
	testData := map[string]string{"key": "value"}
	eb.Publish(EventTypeQueryStarted, testData)

	// Verify event was received
	select {
	case event := <-ch:
		assert.Equal(t, EventTypeQueryStarted, event.Type)
		assert.NotNil(t, event.Data)
		assert.NotEmpty(t, event.ID)
		assert.False(t, event.Timestamp.IsZero())
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Expected to receive event but didn't")
	}
}

func TestEventBus_PublishToMultipleSubscribers(t *testing.T) {
	eb := NewEventBus()

	ch1 := eb.Subscribe("subscriber1")
	ch2 := eb.Subscribe("subscriber2")

	// Publish an event
	eb.Publish(EventTypeQueryProgress, QueryProgressEvent("test", 1, 100))

	// Both subscribers should receive the event
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		select {
		case event := <-ch1:
			assert.Equal(t, EventTypeQueryProgress, event.Type)
		case <-time.After(100 * time.Millisecond):
			t.Error("subscriber1 didn't receive event")
		}
	}()

	go func() {
		defer wg.Done()
		select {
		case event := <-ch2:
			assert.Equal(t, EventTypeQueryProgress, event.Type)
		case <-time.After(100 * time.Millisecond):
			t.Error("subscriber2 didn't receive event")
		}
	}()

	wg.Wait()
}

func TestEventBus_PublishToFullChannel(t *testing.T) {
	eb := NewEventBus()

	// Subscribe with a buffered channel that we won't read from
	ch := eb.Subscribe("test-subscriber")

	// Fill up the buffer
	for i := 0; i < 100; i++ {
		eb.Publish("test", nil)
	}

	// Publishing more events should not block (channels are buffered at 100)
	// and skipped when full
	done := make(chan bool)
	go func() {
		eb.Publish("test", nil)
		done <- true
	}()

	select {
	case <-done:
		// Good - didn't block
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Publish blocked on full channel")
	}

	// Drain a single event to verify at least one event was received
	select {
	case <-ch:
		// Good
	default:
		// Channel might be full, which is fine for this test
	}
}

func TestEventBus_UnsubscribeNonExistent(t *testing.T) {
	eb := NewEventBus()

	// Should not panic when unsubscribing non-existent subscriber
	eb.Unsubscribe("non-existent")

	// Verify no panic occurred and bus is still functional
	ch := eb.Subscribe("new-subscriber")
	eb.Publish("test", nil)

	select {
	case <-ch:
		// Good
	case <-time.After(100 * time.Millisecond):
		t.Fatal("EventBus not functional after unsubscribing non-existent subscriber")
	}
}

// TestRecallDiagnosticEvent (SP-066 Phase 3) verifies the recall-pass
// payload carries the embed duration, candidate counts, injection size,
// and the raw top-scores list. Subscribers tune the similarity threshold
// from this signal.
func TestRecallDiagnosticEvent(t *testing.T) {
	event := RecallDiagnosticEvent(12.5, 8, 2, 540, []float32{0.82, 0.71, 0.55})
	assert.Equal(t, 12.5, event["embed_duration_ms"])
	assert.Equal(t, 8, event["candidates_considered"])
	assert.Equal(t, 2, event["injected"])
	assert.Equal(t, 540, event["injected_chars"])
	scores := event["top_scores"].([]float64)
	if len(scores) != 3 {
		t.Fatalf("expected 3 scores, got %d", len(scores))
	}
	assert.InDelta(t, 0.82, scores[0], 1e-6)
	assert.NotEmpty(t, event["timestamp"])
}

// TestRecallDiagnosticEvent_EmptyScores reflects a recall pass that
// returned no candidates — the payload must still carry a valid (empty)
// top_scores list so consumers don't crash on a nil dereference.
func TestRecallDiagnosticEvent_EmptyScores(t *testing.T) {
	event := RecallDiagnosticEvent(3.0, 0, 0, 0, nil)
	scores := event["top_scores"].([]float64)
	if len(scores) != 0 {
		t.Fatalf("expected empty scores, got %d", len(scores))
	}
}

// TestContextManagementDiagnosticEvent (SP-066 Phase 1) verifies the
// diagnostic payload carries the model-aware trigger math fields the
// WebUI metrics panel and downstream telemetry expect, with the
// effective_max precomputed from max_tokens × trigger_fraction so
// consumers don't redo the arithmetic.
func TestContextManagementDiagnosticEvent(t *testing.T) {
	event := ContextManagementDiagnosticEvent(
		70000,           // current_tokens
		200000,          // max_tokens
		0.70,            // trigger_fraction
		0.15, 0.10, 0.05, // reserved response/thinking/tool_io
		3,    // iteration
		120,  // message_count
	)

	assert.Equal(t, 70000, event["current_tokens"])
	assert.Equal(t, 200000, event["max_tokens"])
	assert.Equal(t, 140000, event["effective_max"]) // 200000 * 0.70
	assert.Equal(t, 0.70, event["trigger_fraction"])
	assert.Equal(t, 0.15, event["reserved_response"])
	assert.Equal(t, 0.10, event["reserved_thinking"])
	assert.Equal(t, 0.05, event["reserved_tool_io"])
	assert.Equal(t, 3, event["iteration"])
	assert.Equal(t, 120, event["message_count"])
	assert.NotEmpty(t, event["timestamp"])
}

// TestContextManagementDiagnosticEvent_ZeroMaxTokens guards the
// degenerate case where max_tokens is zero (e.g. before the model is
// loaded). effective_max must not panic on the multiplication and
// should report 0 so downstream UIs don't render misleading values.
func TestContextManagementDiagnosticEvent_ZeroMaxTokens(t *testing.T) {
	event := ContextManagementDiagnosticEvent(0, 0, 0.70, 0.15, 0.10, 0.05, 0, 0)
	assert.Equal(t, 0, event["effective_max"])
}

// TestEventBus_PublishAfterUnsubscribeDoesNotPanic guards the race where a
// concurrent Unsubscribe closes a subscriber channel after Publish has
// snapshotted the subscriber list. Sending on the closed channel panics; the
// recover() guard in Publish must catch it. A prior implementation used
// `defer recover()` which does NOT recover (recover only returns non-nil
// when called inside a deferred function body), so a closed channel crashed
// the agent — observed in `sprout automate` runs.
func TestEventBus_PublishAfterUnsubscribeDoesNotPanic(t *testing.T) {
	t.Run("non-critical event", func(t *testing.T) {
		eb := NewEventBus()
		eb.Subscribe("racer")

		// Close the channel out from under Publish by reaching into the bus.
		// This deterministically simulates the race the recover() guards.
		eb.mutex.Lock()
		ch := eb.subscribers["racer"]
		delete(eb.subscribers, "racer")
		close(ch)
		// Re-register the now-closed channel so Publish sends to it.
		eb.subscribers["racer"] = ch
		eb.mutex.Unlock()

		assert.NotPanics(t, func() {
			eb.Publish(EventTypeAgentMessage, AgentMessageEvent("info", "hi", nil))
		})
	})

	t.Run("critical event", func(t *testing.T) {
		eb := NewEventBus()
		eb.Subscribe("racer")

		eb.mutex.Lock()
		ch := eb.subscribers["racer"]
		delete(eb.subscribers, "racer")
		close(ch)
		eb.subscribers["racer"] = ch
		eb.mutex.Unlock()

		assert.NotPanics(t, func() {
			eb.Publish(EventTypeSecurityApprovalRequest, nil)
		})

		// drainMu must be released even when the send panicked — otherwise
		// the next critical publish would deadlock.
		done := make(chan struct{})
		go func() {
			eb.drainMu.Lock()
			eb.drainMu.Unlock()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("drainMu leaked after panicked critical send")
		}
	})
}

func TestGenerateEventID(t *testing.T) {
	id1 := generateEventID(1)
	id2 := generateEventID(2)

	assert.NotEmpty(t, id1)
	assert.NotEmpty(t, id2)
	assert.NotEqual(t, id1, id2)
}

// Test helper functions for creating events

func TestQueryStartedEvent(t *testing.T) {
	event := QueryStartedEvent("test query", "test-provider", "test-model")

	assert.Equal(t, "test query", event["query"])
	assert.Equal(t, "test-provider", event["provider"])
	assert.Equal(t, "test-model", event["model"])
}

func TestQueryProgressEvent(t *testing.T) {
	event := QueryProgressEvent("working", 5, 500)

	assert.Equal(t, "working", event["message"])
	assert.Equal(t, 5, event["iteration"])
	assert.Equal(t, 500, event["tokens_used"])
}

func TestQueryCompletedEvent(t *testing.T) {
	event := QueryCompletedEvent("query?", "response!", 1000, 0.05, 2*time.Second)

	assert.Equal(t, "query?", event["query"])
	assert.Equal(t, "response!", event["response"])
	assert.Equal(t, 1000, event["tokens_used"])
	assert.Equal(t, 0.05, event["cost"])
	assert.Equal(t, int64(2000), event["duration_ms"])
}

func TestErrorEvent(t *testing.T) {
	event := ErrorEvent("something failed", assert.AnError)

	assert.Equal(t, "something failed", event["message"])
	assert.NotEmpty(t, event["error"])
}

func TestToolExecutionEvent(t *testing.T) {
	event := ToolExecutionEvent("read_file", "starting", map[string]interface{}{"path": "/test"})

	assert.Equal(t, "read_file", event["tool_name"])
	assert.Equal(t, "starting", event["action"])
	assert.Equal(t, "/test", event["path"])
}

func TestFileChangedEvent(t *testing.T) {
	event := FileChangedEvent("/path/to/file.go", "modified", "some content")

	assert.Equal(t, "/path/to/file.go", event["file_path"])
	assert.Equal(t, "modified", event["action"])
	// Whole-file content is intentionally NOT transmitted (only its size) so a
	// burst of file_changed events doesn't flood/overflow the event bus.
	assert.NotContains(t, event, "content", "file_changed must not carry file content")
	assert.Equal(t, len("some content"), event["size"])
}

func TestStreamChunkEvent(t *testing.T) {
	event := StreamChunkEvent("hello world", "assistant_text")

	assert.Equal(t, "hello world", event["chunk"])
	assert.Equal(t, "assistant_text", event["content_type"])
}

func TestMetricsUpdateEvent(t *testing.T) {
	event := MetricsUpdateEvent(1000, 500, 4096, 3, 0.15)

	assert.Equal(t, 1000, event["total_tokens"])
	assert.Equal(t, 500, event["context_tokens"])
	assert.Equal(t, 4096, event["max_context_tokens"])
	assert.Equal(t, 3, event["iteration"])
	assert.Equal(t, 0.15, event["total_cost"])
}

func TestAgentMessageEvent(t *testing.T) {
	extra := map[string]interface{}{"key": "value"}
	event := AgentMessageEvent("info", "test message", extra)

	assert.Equal(t, "info", event["category"])
	assert.Equal(t, "test message", event["message"])
	assert.Equal(t, "value", event["key"])
}

func TestAgentMessageEventNilExtra(t *testing.T) {
	event := AgentMessageEvent("warning", "caution", nil)

	assert.Equal(t, "warning", event["category"])
	assert.Equal(t, "caution", event["message"])
	assert.Len(t, event, 2) // Only category and message
}

// TestInputRequiredEventConstant verifies the constant string value.
func TestInputRequiredEventConstant(t *testing.T) {
	assert.Equal(t, "input_required", EventTypeInputRequired)
}

// TestInputRequiredEventFactory_WithRequestID verifies the payload carries
// both reason and request_id when the ID is non-empty.
func TestInputRequiredEventFactory_WithRequestID(t *testing.T) {
	event := InputRequiredEvent("some_reason", "req-123")

	assert.Equal(t, "some_reason", event["reason"])
	assert.Equal(t, "req-123", event["request_id"])
}

// TestInputRequiredEventFactory_EmptyRequestID verifies that when requestID
// is empty, the returned map contains reason but NOT request_id.
func TestInputRequiredEventFactory_EmptyRequestID(t *testing.T) {
	event := InputRequiredEvent("some_reason", "")

	assert.Equal(t, "some_reason", event["reason"])
	assert.NotContains(t, event, "request_id", "request_id key must not be present when empty")
	assert.NotEmpty(t, event["timestamp"], "timestamp must be present and non-empty")
}

// TestInputRequiredEventCritical verifies that input_required is treated as
// a critical event: when the subscriber channel is full, the critical event
// drains the stale event and delivers itself instead of being silently dropped.
func TestInputRequiredEventCritical(t *testing.T) {
	eb := NewEventBus()

	// Use a tiny buffer-1 channel so we can deterministically fill it.
	ch := make(chan UIEvent, 1)
	eb.mutex.Lock()
	eb.subscribers["crit-test"] = ch
	eb.mutex.Unlock()
	defer func() {
		eb.mutex.Lock()
		delete(eb.subscribers, "crit-test")
		eb.mutex.Unlock()
		close(ch)
	}()

	// Fill the channel with a non-critical event.
	eb.Publish(EventTypeAgentMessage, AgentMessageEvent("info", "old", nil))

	// Publish a critical input_required event — it should drain the channel
	// and deliver itself, not be silently dropped.
	eb.Publish(EventTypeInputRequired, InputRequiredEvent("test_reason", "req-1"))

	// Read the single event in the buffer — it should be the critical one.
	select {
	case evt := <-ch:
		assert.Equal(t, EventTypeInputRequired, evt.Type, "critical event should replace the drained event")
		data := evt.Data.(map[string]interface{})
		assert.Equal(t, "test_reason", data["reason"])
		assert.Equal(t, "req-1", data["request_id"])
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for critical input_required event")
	}

	// Channel should now be empty — only one slot, we already read it.
	select {
	case evt := <-ch:
		t.Fatalf("unexpected extra event in channel: %s", evt.Type)
	default:
		// Good — channel is empty.
	}
}
