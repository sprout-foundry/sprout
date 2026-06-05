package events

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Event type constants
// ---------------------------------------------------------------------------

// TestAutomateEventTypeConstants verifies that each automate event type
// constant maps to its expected string literal. These values are consumed
// by the WebUI and must not drift.
func TestAutomateEventTypeConstants(t *testing.T) {
	assert.Equal(t, "automate.session_started", EventTypeAutomateSessionStarted)
	assert.Equal(t, "automate.budget_update", EventTypeAutomateBudgetUpdate)
	assert.Equal(t, "automate.output_chunk", EventTypeAutomateOutputChunk)
	assert.Equal(t, "automate.session_ended", EventTypeAutomateSessionEnded)
}

// ---------------------------------------------------------------------------
// AutomateSessionStartedEvent
// ---------------------------------------------------------------------------

// TestAutomateSessionStartedEvent_NormalValues verifies that all expected
// fields are present and carry the correct values.
func TestAutomateSessionStartedEvent_NormalValues(t *testing.T) {
	data := AutomateSessionStartedEvent("sess-42", "workflow.json", "automate")

	assert.Equal(t, "sess-42", data["session_id"])
	assert.Equal(t, "workflow.json", data["workflow"])
	assert.Equal(t, "automate", data["kind"])
	assert.NotEmpty(t, data["timestamp"])

	// Verify timestamp is parseable as RFC3339 UTC.
	_, err := time.Parse(time.RFC3339, data["timestamp"].(string))
	assert.NoError(t, err, "timestamp should be valid RFC3339")
}

// TestAutomateSessionStartedEvent_EmptyStrings ensures the helper doesn't
// panic or produce unexpected keys when empty strings are passed.
func TestAutomateSessionStartedEvent_EmptyStrings(t *testing.T) {
	data := AutomateSessionStartedEvent("", "", "")

	assert.Equal(t, "", data["session_id"])
	assert.Equal(t, "", data["workflow"])
	assert.Equal(t, "", data["kind"])
	assert.NotEmpty(t, data["timestamp"])
}

// ---------------------------------------------------------------------------
// AutomateBudgetUpdateEvent
// ---------------------------------------------------------------------------

// TestAutomateBudgetUpdateEvent_NormalValues verifies the full payload
// structure including floating-point fields.
func TestAutomateBudgetUpdateEvent_NormalValues(t *testing.T) {
	data := AutomateBudgetUpdateEvent("sess-1", 0.50, 1.00, 0.5, 3)

	assert.Equal(t, "sess-1", data["session_id"])
	assert.Equal(t, 0.50, data["spent_usd"])
	assert.Equal(t, 1.00, data["budget_usd"])
	assert.Equal(t, 0.5, data["fraction"])
	assert.Equal(t, 3, data["iteration"])
	assert.NotEmpty(t, data["timestamp"])

	// Verify timestamp is parseable as RFC3339 UTC.
	_, err := time.Parse(time.RFC3339, data["timestamp"].(string))
	assert.NoError(t, err, "timestamp should be valid RFC3339")
}

// TestAutomateBudgetUpdateEvent_ZeroBudget covers the edge case where
// budget_usd is zero (unconfigured budget).
func TestAutomateBudgetUpdateEvent_ZeroBudget(t *testing.T) {
	data := AutomateBudgetUpdateEvent("sess-2", 0.0, 0.0, 0.0, 0)

	assert.Equal(t, "sess-2", data["session_id"])
	assert.Equal(t, 0.0, data["spent_usd"])
	assert.Equal(t, 0.0, data["budget_usd"])
	assert.Equal(t, 0.0, data["fraction"])
	assert.Equal(t, 0, data["iteration"])
	assert.NotEmpty(t, data["timestamp"])
}

// TestAutomateBudgetUpdateEvent_BudgetExceeded covers fraction > 1.0,
// meaning spending has exceeded the configured budget.
func TestAutomateBudgetUpdateEvent_BudgetExceeded(t *testing.T) {
	data := AutomateBudgetUpdateEvent("sess-3", 1.50, 1.00, 1.5, 10)

	assert.Equal(t, "sess-3", data["session_id"])
	assert.Equal(t, 1.50, data["spent_usd"])
	assert.Equal(t, 1.00, data["budget_usd"])
	assert.Equal(t, 1.5, data["fraction"])
	assert.Equal(t, 10, data["iteration"])
}

// ---------------------------------------------------------------------------
// AutomateOutputChunkEvent
// ---------------------------------------------------------------------------

// TestAutomateOutputChunkEvent_NormalValues verifies the payload structure
// and that chunk_len reflects the actual chunk length.
func TestAutomateOutputChunkEvent_NormalValues(t *testing.T) {
	chunk := "Hello, world!"
	data := AutomateOutputChunkEvent("sess-5", 100, chunk)

	assert.Equal(t, "sess-5", data["session_id"])
	assert.Equal(t, 100, data["offset"])
	assert.Equal(t, len(chunk), data["chunk_len"])
	assert.NotEmpty(t, data["timestamp"])

	// Verify timestamp is parseable as RFC3339 UTC.
	_, err := time.Parse(time.RFC3339, data["timestamp"].(string))
	assert.NoError(t, err, "timestamp should be valid RFC3339")
}

// TestAutomateOutputChunkEvent_EmptyChunk ensures chunk_len is 0 when
// the chunk string is empty — the helper must not panic or produce a
// negative length.
func TestAutomateOutputChunkEvent_EmptyChunk(t *testing.T) {
	data := AutomateOutputChunkEvent("sess-6", 0, "")

	assert.Equal(t, "sess-6", data["session_id"])
	assert.Equal(t, 0, data["offset"])
	assert.Equal(t, 0, data["chunk_len"])
	assert.NotEmpty(t, data["timestamp"])
}

// TestAutomateOutputChunkEvent_LargeChunk verifies that chunk_len tracks
// the length of a large string without overflow or truncation.
func TestAutomateOutputChunkEvent_LargeChunk(t *testing.T) {
	largeChunk := strings.Repeat("x", 10000)
	data := AutomateOutputChunkEvent("sess-7", 500, largeChunk)

	assert.Equal(t, "sess-7", data["session_id"])
	assert.Equal(t, 500, data["offset"])
	assert.Equal(t, 10000, data["chunk_len"])
}

// ---------------------------------------------------------------------------
// AutomateSessionEndedEvent
// ---------------------------------------------------------------------------

// TestAutomateSessionEndedEvent_NormalValues verifies the full payload
// for a successful session completion.
func TestAutomateSessionEndedEvent_NormalValues(t *testing.T) {
	data := AutomateSessionEndedEvent("sess-10", "workflow.json", "success", 0.75)

	assert.Equal(t, "sess-10", data["session_id"])
	assert.Equal(t, "workflow.json", data["workflow"])
	assert.Equal(t, "success", data["status"])
	assert.Equal(t, 0.75, data["total_cost"])
	assert.NotEmpty(t, data["timestamp"])

	// Verify timestamp is parseable as RFC3339 UTC.
	_, err := time.Parse(time.RFC3339, data["timestamp"].(string))
	assert.NoError(t, err, "timestamp should be valid RFC3339")
}

// TestAutomateSessionEndedEvent_ZeroCost covers the edge case where
// the session incurred no cost (e.g., failed early or dry-run).
func TestAutomateSessionEndedEvent_ZeroCost(t *testing.T) {
	data := AutomateSessionEndedEvent("sess-11", "workflow.json", "success", 0.0)

	assert.Equal(t, "sess-11", data["session_id"])
	assert.Equal(t, 0.0, data["total_cost"])
}

// TestAutomateSessionEndedEvent_DifferentStatuses verifies the helper
// handles various status strings without special-casing.
func TestAutomateSessionEndedEvent_DifferentStatuses(t *testing.T) {
	statuses := []string{"success", "error", "cancelled", "budget_exceeded", "timeout"}

	for _, status := range statuses {
		t.Run(status, func(t *testing.T) {
			data := AutomateSessionEndedEvent("sess-s", "wf", status, 0.1)
			assert.Equal(t, status, data["status"])
			assert.Equal(t, "sess-s", data["session_id"])
			assert.Equal(t, "wf", data["workflow"])
		})
	}
}

// ---------------------------------------------------------------------------
// EventBus integration — automate events
// ---------------------------------------------------------------------------

// TestEventBus_PublishAutomateSessionStarted verifies that publishing an
// automate session_started event through the EventBus delivers the correct
// type and payload to a subscriber.
func TestEventBus_PublishAutomateSessionStarted(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe("automate-test")

	data := AutomateSessionStartedEvent("sess-integration", "my-workflow", "automate")
	eb.Publish(EventTypeAutomateSessionStarted, data)

	select {
	case event := <-ch:
		assert.Equal(t, EventTypeAutomateSessionStarted, event.Type)
		payload, ok := event.Data.(map[string]interface{})
		require.True(t, ok, "event data should be a map[string]interface{}")
		assert.Equal(t, "sess-integration", payload["session_id"])
		assert.Equal(t, "my-workflow", payload["workflow"])
		assert.Equal(t, "automate", payload["kind"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Subscriber did not receive automate session_started event")
	}
}

// TestEventBus_PublishAutomateBudgetUpdate verifies that a budget_update
// event flows through the EventBus with correct numeric fields.
func TestEventBus_PublishAutomateBudgetUpdate(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe("budget-test")

	data := AutomateBudgetUpdateEvent("sess-b", 0.25, 1.00, 0.25, 5)
	eb.Publish(EventTypeAutomateBudgetUpdate, data)

	select {
	case event := <-ch:
		assert.Equal(t, EventTypeAutomateBudgetUpdate, event.Type)
		payload, ok := event.Data.(map[string]interface{})
		require.True(t, ok, "event data should be a map[string]interface{}")
		assert.Equal(t, "sess-b", payload["session_id"])
		assert.Equal(t, 0.25, payload["spent_usd"])
		assert.Equal(t, 1.00, payload["budget_usd"])
		assert.Equal(t, 0.25, payload["fraction"])
		assert.Equal(t, 5, payload["iteration"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Subscriber did not receive automate budget_update event")
	}
}

// TestEventBus_PublishAutomateOutputChunk verifies that an output_chunk
// event is delivered with the correct offset and chunk_len.
func TestEventBus_PublishAutomateOutputChunk(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe("chunk-test")

	data := AutomateOutputChunkEvent("sess-c", 200, "some output text")
	eb.Publish(EventTypeAutomateOutputChunk, data)

	select {
	case event := <-ch:
		assert.Equal(t, EventTypeAutomateOutputChunk, event.Type)
		payload, ok := event.Data.(map[string]interface{})
		require.True(t, ok, "event data should be a map[string]interface{}")
		assert.Equal(t, "sess-c", payload["session_id"])
		assert.Equal(t, 200, payload["offset"])
		assert.Equal(t, len("some output text"), payload["chunk_len"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Subscriber did not receive automate output_chunk event")
	}
}

// TestEventBus_PublishAutomateSessionEnded verifies that a session_ended
// event is delivered with correct status and cost fields.
func TestEventBus_PublishAutomateSessionEnded(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe("ended-test")

	data := AutomateSessionEndedEvent("sess-e", "my-workflow", "error", 0.99)
	eb.Publish(EventTypeAutomateSessionEnded, data)

	select {
	case event := <-ch:
		assert.Equal(t, EventTypeAutomateSessionEnded, event.Type)
		payload, ok := event.Data.(map[string]interface{})
		require.True(t, ok, "event data should be a map[string]interface{}")
		assert.Equal(t, "sess-e", payload["session_id"])
		assert.Equal(t, "my-workflow", payload["workflow"])
		assert.Equal(t, "error", payload["status"])
		assert.Equal(t, 0.99, payload["total_cost"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("Subscriber did not receive automate session_ended event")
	}
}

// TestEventBus_PublishAutomateEventSequence verifies the full automate
// lifecycle (started → budget_update → output_chunk → ended) is delivered
// in order to a subscriber.
func TestEventBus_PublishAutomateEventSequence(t *testing.T) {
	eb := NewEventBus()
	ch := eb.Subscribe("lifecycle-test")

	// Publish a complete automate session lifecycle.
	eb.Publish(EventTypeAutomateSessionStarted, AutomateSessionStartedEvent("seq-1", "wf", "automate"))
	eb.Publish(EventTypeAutomateBudgetUpdate, AutomateBudgetUpdateEvent("seq-1", 0.10, 1.00, 0.10, 1))
	eb.Publish(EventTypeAutomateOutputChunk, AutomateOutputChunkEvent("seq-1", 0, "output"))
	eb.Publish(EventTypeAutomateSessionEnded, AutomateSessionEndedEvent("seq-1", "wf", "success", 0.10))

	expectedTypes := []string{
		EventTypeAutomateSessionStarted,
		EventTypeAutomateBudgetUpdate,
		EventTypeAutomateOutputChunk,
		EventTypeAutomateSessionEnded,
	}

	for i, expectedType := range expectedTypes {
		select {
		case event := <-ch:
			assert.Equal(t, expectedType, event.Type, "event at index %d should match expected type", i)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("Subscriber did not receive event at index %d (expected %q)", i, expectedType)
		}
	}
}
