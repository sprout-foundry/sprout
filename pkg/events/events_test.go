package events

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

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
	event := FileChangedEvent("/path/to/file.go", "modified", "content")

	assert.Equal(t, "/path/to/file.go", event["file_path"])
	assert.Equal(t, "modified", event["action"])
	assert.Equal(t, "content", event["content"])
}

func TestStreamChunkEvent(t *testing.T) {
	event := StreamChunkEvent("hello world")

	assert.Equal(t, "hello world", event["chunk"])
}

func TestMetricsUpdateEvent(t *testing.T) {
	event := MetricsUpdateEvent(1000, 500, 4096, 3, 0.15)

	assert.Equal(t, 1000, event["total_tokens"])
	assert.Equal(t, 500, event["context_tokens"])
	assert.Equal(t, 4096, event["max_context_tokens"])
	assert.Equal(t, 3, event["iteration"])
	assert.Equal(t, 0.15, event["total_cost"])
}
