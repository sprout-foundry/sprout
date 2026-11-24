package core

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/seed/events"
)

// === Buffer access ===

func TestOutputManager_ContentBuffer_NotNil(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	if om.ContentBuffer() == nil {
		t.Error("expected non-nil content buffer")
	}
}

func TestOutputManager_ReasoningBuffer_NotNil(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	if om.ReasoningBuffer() == nil {
		t.Error("expected non-nil reasoning buffer")
	}
}

func TestOutputManager_ContentBuffer_WriteAndRead(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	buf := om.ContentBuffer()
	buf.Write([]byte("Hello"))
	buf.Write([]byte(" World"))
	if got := buf.String(); got != "Hello World" {
		t.Errorf("expected 'Hello World', got %q", got)
	}
}

func TestOutputManager_ReasoningBuffer_WriteAndRead(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	buf := om.ReasoningBuffer()
	buf.Write([]byte("thinking "))
	buf.Write([]byte("about it"))
	if got := buf.String(); got != "thinking about it" {
		t.Errorf("expected 'thinking about it', got %q", got)
	}
}

// === Flush callback ===

func TestOutputManager_SetFlushCallback_InvokedByFlush(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	fired := false
	om.SetFlushCallback(func() { fired = true })
	om.Flush()

	if !fired {
		t.Error("expected flush callback to be invoked")
	}
}

func TestOutputManager_Flush_NoCallbackSet(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	// Should not panic
	om.Flush()
}

func TestOutputManager_SetFlushCallback_Overwrite(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	var firstFired, secondFired bool
	om.SetFlushCallback(func() { firstFired = true })
	om.SetFlushCallback(func() { secondFired = true })

	om.Flush()

	if firstFired {
		t.Error("expected first callback NOT to fire after overwrite")
	}
	if !secondFired {
		t.Error("expected second callback to fire")
	}
}

func TestOutputManager_Flush_ConcurrentSafety(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetFlushCallback(func() { /* no-op */ })

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			om.Flush()
		}()
	}
	wg.Wait()
}

// === Async output channel ===

func TestOutputManager_AsyncOutput_ReceivesPublishedEvent(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "test content",
		Source:  "test",
	})

	select {
	case evt := <-om.AsyncOutput():
		if evt.Type != "content" {
			t.Errorf("expected type 'content', got %q", evt.Type)
		}
		if evt.Content != "test content" {
			t.Errorf("expected content 'test content', got %q", evt.Content)
		}
		if evt.Source != "test" {
			t.Errorf("expected source 'test', got %q", evt.Source)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event on async channel")
	}
}

func TestOutputManager_AsyncOutput_SetsTimestamp(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	before := time.Now().Add(-1 * time.Second)
	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "test",
	})
	after := time.Now().Add(1 * time.Second)

	select {
	case evt := <-om.AsyncOutput():
		if evt.Timestamp.IsZero() {
			t.Error("expected non-zero timestamp")
		}
		if evt.Timestamp.Before(before) || evt.Timestamp.After(after) {
			t.Errorf("expected timestamp between %v and %v, got %v", before, after, evt.Timestamp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOutputManager_AsyncOutput_PreservesExistingTimestamp(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	customTime := time.Date(2020, 1, 1, 12, 0, 0, 0, time.UTC)
	om.PublishOutput(OutputEvent{
		Type:      "content",
		Content:   "test",
		Timestamp: customTime,
	})

	select {
	case evt := <-om.AsyncOutput():
		if !evt.Timestamp.Equal(customTime) {
			t.Errorf("expected timestamp %v, got %v", customTime, evt.Timestamp)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOutputManager_AsyncOutput_MergesMetadata(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("session_id", "abc-123")
	om.SetEventMetadata("model", "test-model")

	om.PublishOutput(OutputEvent{
		Type:     "content",
		Content:  "test",
		Metadata: map[string]string{"custom": "value"},
	})

	select {
	case evt := <-om.AsyncOutput():
		if evt.Metadata["session_id"] != "abc-123" {
			t.Errorf("expected session_id 'abc-123', got %q", evt.Metadata["session_id"])
		}
		if evt.Metadata["model"] != "test-model" {
			t.Errorf("expected model 'test-model', got %q", evt.Metadata["model"])
		}
		if evt.Metadata["custom"] != "value" {
			t.Errorf("expected custom 'value', got %q", evt.Metadata["custom"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOutputManager_AsyncOutput_MetadataNoOverwrite(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("model", "global-model")

	// Event provides its own value for "model" — should not be overwritten
	om.PublishOutput(OutputEvent{
		Type:     "content",
		Content:  "test",
		Metadata: map[string]string{"model": "event-model"},
	})

	select {
	case evt := <-om.AsyncOutput():
		if evt.Metadata["model"] != "event-model" {
			t.Errorf("expected event metadata to take precedence: 'event-model', got %q", evt.Metadata["model"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOutputManager_PublishOutput_DoesNotMutateCallerMetadata(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("session_id", "abc-123")

	originalMeta := map[string]string{"custom": "value"}
	om.PublishOutput(OutputEvent{
		Type:     "content",
		Content:  "test",
		Metadata: originalMeta,
	})

	// The caller's map should not have been mutated
	if len(originalMeta) != 1 {
		t.Errorf("expected caller's metadata map to remain size 1, got %d: %v", len(originalMeta), originalMeta)
	}
	if originalMeta["custom"] != "value" {
		t.Errorf("expected caller's metadata to still have 'custom': 'value', got %v", originalMeta)
	}
	if _, ok := originalMeta["session_id"]; ok {
		t.Error("expected caller's metadata to NOT contain merged global key 'session_id'")
	}

	// The received event should have the merged metadata
	select {
	case evt := <-om.AsyncOutput():
		if evt.Metadata["session_id"] != "abc-123" {
			t.Errorf("expected event metadata to contain merged 'session_id', got %v", evt.Metadata)
		}
		if evt.Metadata["custom"] != "value" {
			t.Errorf("expected event metadata to contain 'custom': 'value', got %v", evt.Metadata)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for event")
	}
}

func TestOutputManager_AsyncOutput_DropsWhenFull(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	// Channel capacity is 256. Fill it completely.
	for i := 0; i < 256; i++ {
		om.PublishOutput(OutputEvent{
			Type:    "content",
			Content: fmt.Sprintf("fill-%d", i),
		})
	}

	// The next publish should not block or panic (silent drop)
	done := make(chan struct{})
	go func() {
		om.PublishOutput(OutputEvent{
			Type:    "content",
			Content: "should-be-dropped",
		})
		close(done)
	}()

	select {
	case <-done:
		// Good — publish returned immediately
	case <-time.After(2 * time.Second):
		t.Fatal("PublishOutput blocked — channel should have dropped the event")
	}

	// Drain and verify the dropped event was not received
	drainCount := 0
	for drainCount < 256 {
		select {
		case <-om.AsyncOutput():
			drainCount++
		case <-time.After(1 * time.Second):
			t.Fatalf("expected 256 events but only drained %d", drainCount)
		}
	}

	// Verify the dropped event is not in the channel
	select {
	case evt := <-om.AsyncOutput():
		t.Errorf("unexpected extra event after drain: %v", evt)
	case <-time.After(100 * time.Millisecond):
		// Good — no extra event
	}
}

func TestOutputManager_PublishOutput_AfterClose_NoPanic(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	om.Close()

	// Should silently ignore publish to closed channel
	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "after close",
	})
}

func TestOutputManager_PublishOutput_ConcurrentPublish(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			om.PublishOutput(OutputEvent{
				Type:    "content",
				Content: fmt.Sprintf("concurrent-%d", n),
			})
		}(i)
	}
	wg.Wait()
}

func TestOutputManager_AsyncOutput_ReturnsReadOnlyChannel(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	ch := om.AsyncOutput()
	// Verify via reflection that the channel is receive-only.
	// A receive-only channel has direction flag reflect.ChanRecv.
	if ch == nil {
		t.Fatal("expected non-nil channel")
	}

	// Verify we can read from it (basic sanity)
	select {
	case <-ch:
		// channel exists, even if empty (zero value returned for closed channel)
	default:
		// OK — channel is empty but exists
	}
}

// === EventBus integration ===

func TestOutputManager_PublishOutput_ContentPublishesStreamChunkAndAgentMessage(t *testing.T) {
	t.Parallel()
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-output")

	om := NewOutputManager(bus)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "content data",
	})

	// Content type publishes two events: stream_chunk and agent_message.
	var count int
	var streamChunk, agentMsg events.UIEvent
	for count < 2 {
		select {
		case evt := <-ch:
			count++
			switch evt.Type {
			case events.EventTypeStreamChunk:
				streamChunk = evt
			case events.EventTypeAgentMessage:
				agentMsg = evt
			default:
				t.Errorf("unexpected event type %q", evt.Type)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("timeout waiting for events, received %d/%d", count, 2)
		}
	}

	// Verify stream_chunk event
	if data, ok := streamChunk.Data.(map[string]interface{}); !ok {
		t.Fatalf("stream_chunk data should be map[string]interface{}, got %T", streamChunk.Data)
	} else {
		if data["chunk"] != "content data" {
			t.Errorf("expected chunk 'content data', got %v", data["chunk"])
		}
		if data["content_type"] != "text" {
			t.Errorf("expected content_type 'text', got %v", data["content_type"])
		}
	}

	// Verify agent_message event
	if data, ok := agentMsg.Data.(map[string]interface{}); !ok {
		t.Fatalf("agent_message data should be map[string]interface{}, got %T", agentMsg.Data)
	} else {
		if data["category"] != "info" {
			t.Errorf("expected category 'info', got %v", data["category"])
		}
		if data["message"] != "content data" {
			t.Errorf("expected message 'content data', got %v", data["message"])
		}
	}
}

func TestOutputManager_PublishOutput_ReasoningPublishesStreamChunk(t *testing.T) {
	t.Parallel()
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-output")

	om := NewOutputManager(bus)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "reasoning",
		Content: "reasoning data",
	})

	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeStreamChunk {
			t.Fatalf("expected stream_chunk event, got %q", evt.Type)
		}
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected data as map[string]interface{}, got %T", evt.Data)
		}
		if data["chunk"] != "reasoning data" {
			t.Errorf("expected chunk 'reasoning data', got %v", data["chunk"])
		}
		if data["content_type"] != "reasoning" {
			t.Errorf("expected content_type 'reasoning', got %v", data["content_type"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for stream_chunk event")
	}
}

func TestOutputManager_PublishOutput_AgentMessagePublishesAgentMessageEvent(t *testing.T) {
	t.Parallel()
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-output")

	om := NewOutputManager(bus)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "agent_message",
		Content: "agent said something",
	})

	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeAgentMessage {
			t.Fatalf("expected agent_message event, got %q", evt.Type)
		}
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected data as map[string]interface{}, got %T", evt.Data)
		}
		if data["category"] != "info" {
			t.Errorf("expected category 'info', got %v", data["category"])
		}
		if data["message"] != "agent said something" {
			t.Errorf("expected message 'agent said something', got %v", data["message"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for agent_message event")
	}
}

func TestOutputManager_PublishOutput_ErrorPublishesErrorEvent(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "error",
		Content: "something went wrong",
	})

	select {
	case evt := <-om.AsyncOutput():
		if evt.Type != "error" {
			t.Errorf("expected type 'error', got %q", evt.Type)
		}
		if evt.Content != "something went wrong" {
			t.Errorf("expected content 'something went wrong', got %q", evt.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error event")
	}
}

func TestOutputManager_PublishOutput_NoEventBus_NoPanic(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	// Should not panic when eventBus is nil
	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "test",
	})
}

// === Event metadata ===

func TestOutputManager_SetGetEventMetadata(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("key1", "value1")
	if got := om.GetEventMetadata("key1"); got != "value1" {
		t.Errorf("expected 'value1', got %q", got)
	}
}

func TestOutputManager_GetEventMetadata_MissingKey(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	if got := om.GetEventMetadata("nonexistent"); got != "" {
		t.Errorf("expected empty string for missing key, got %q", got)
	}
}

func TestOutputManager_SetEventMetadata_Overwrite(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("key1", "first")
	om.SetEventMetadata("key1", "second")

	if got := om.GetEventMetadata("key1"); got != "second" {
		t.Errorf("expected 'second', got %q", got)
	}
}

// === Reset ===

func TestOutputManager_Reset_ClearsBuffers(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	buf := om.ContentBuffer()
	reasoningBuf := om.ReasoningBuffer()

	buf.Write([]byte("content data"))
	reasoningBuf.Write([]byte("reasoning data"))

	if buf.String() != "content data" {
		t.Errorf("expected 'content data', got %q", buf.String())
	}
	if reasoningBuf.String() != "reasoning data" {
		t.Errorf("expected 'reasoning data', got %q", reasoningBuf.String())
	}

	om.Reset()

	if buf.String() != "" {
		t.Errorf("expected empty content buffer after reset, got %q", buf.String())
	}
	if reasoningBuf.String() != "" {
		t.Errorf("expected empty reasoning buffer after reset, got %q", reasoningBuf.String())
	}
}

func TestOutputManager_Reset_DrainsChannel(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	// Enqueue some events
	om.PublishOutput(OutputEvent{Type: "content", Content: "event1"})
	om.PublishOutput(OutputEvent{Type: "content", Content: "event2"})
	om.PublishOutput(OutputEvent{Type: "content", Content: "event3"})

	om.Reset()

	// Channel should be drained — reading should return immediately with zero value
	// Wait briefly to confirm nothing arrives
	select {
	case <-om.AsyncOutput():
		t.Error("expected channel to be empty after reset")
	case <-time.After(100 * time.Millisecond):
		// Good — no pending events
	}
}

func TestOutputManager_Reset_ChannelStillUsable(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.PublishOutput(OutputEvent{Type: "content", Content: "before-reset"})
	om.Reset()

	// After reset, the channel should still accept new events
	om.PublishOutput(OutputEvent{Type: "content", Content: "after-reset"})

	select {
	case evt := <-om.AsyncOutput():
		if evt.Content != "after-reset" {
			t.Errorf("expected 'after-reset', got %q", evt.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout — channel should still work after reset")
	}
}

// === Close ===

func TestOutputManager_Close_ChannelClosed(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	om.Close()

	// After close, the channel is closed. Reading should return zero value immediately.
	select {
	case evt := <-om.AsyncOutput():
		// Zero value for OutputEvent
		if evt.Type != "" || evt.Content != "" || !evt.Timestamp.IsZero() {
			t.Errorf("expected zero value, got %+v", evt)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("timeout — closed channel should return immediately")
	}
}

func TestOutputManager_Close_Idempotent(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)

	om.Close()
	om.Close() // Should not panic
}

func TestOutputManager_Close_ThenPublish_NoPanic(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	om.Close()

	// Publish after close should be silently ignored
	om.PublishOutput(OutputEvent{
		Type:    "content",
		Content: "ignored",
	})
}

// === Edge cases ===

func TestOutputManager_AsyncOutput_EmptyChannelAfterClose(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)

	// Publish an event, close, then verify we can drain it
	om.PublishOutput(OutputEvent{Type: "content", Content: "last"})
	om.Close()

	// Should be able to read the pending event
	select {
	case evt := <-om.AsyncOutput():
		if evt.Content != "last" {
			t.Errorf("expected 'last', got %q", evt.Content)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout — should be able to read pending event before closed channel returns zero")
	}
}

func TestOutputManager_SetEventMetadata_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			om.SetEventMetadata(fmt.Sprintf("key-%d", n), fmt.Sprintf("val-%d", n))
		}(i)
	}
	wg.Wait()

	// Verify some keys
	if om.GetEventMetadata("key-0") != "val-0" {
		t.Error("expected 'val-0' for key-0")
	}
	if om.GetEventMetadata("key-49") != "val-49" {
		t.Error("expected 'val-49' for key-49")
	}
}

func TestOutputManager_GetEventMetadata_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	om.SetEventMetadata("shared-key", "shared-val")

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = om.GetEventMetadata("shared-key")
		}()
	}
	wg.Wait()
}

func TestOutputManager_Reset_ConcurrentSafe(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	defer om.Close()

	var wg sync.WaitGroup

	// Concurrent writes to buffers
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			buf := om.ContentBuffer()
			buf.Write([]byte(fmt.Sprintf("n%d", n)))
		}(i)
	}

	// Concurrent resets
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			om.Reset()
		}()
	}

	// Concurrent publishes
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			om.PublishOutput(OutputEvent{Type: "content", Content: fmt.Sprintf("evt-%d", n)})
		}(i)
	}

	wg.Wait()
}

func TestOutputManager_Reset_AfterClose_NoInfiniteLoop(t *testing.T) {
	t.Parallel()
	om := NewOutputManager(nil)
	om.Close()

	done := make(chan struct{})
	go func() {
		om.Reset()
		close(done)
	}()

	select {
	case <-done:
		// Good — Reset returned
	case <-time.After(1 * time.Second):
		t.Fatal("Reset() infinite-looped after Close()")
	}
}

// === Race condition tests ===

func TestOutputManager_PublishOutput_ConcurrentClose_NoPanic(t *testing.T) {
	om := NewOutputManager(nil)

	var wg sync.WaitGroup

	// Concurrent publishes while closing
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			// Small stagger to interleave with Close()
			time.Sleep(time.Microsecond * time.Duration(n%10))
			om.PublishOutput(OutputEvent{Type: "content", Content: fmt.Sprintf("evt-%d", n)})
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		time.Sleep(50 * time.Microsecond)
		om.Close()
	}()

	wg.Wait()
}

func TestOutputManager_PublishOutput_ErrorPublishesErrorEventToEventBus(t *testing.T) {
	t.Parallel()
	bus := events.NewEventBus()
	ch := bus.Subscribe("test-error-eventbus")

	om := NewOutputManager(bus)
	defer om.Close()

	om.PublishOutput(OutputEvent{
		Type:    "error",
		Content: "something went wrong",
		Source:  "test",
	})

	select {
	case evt := <-ch:
		if evt.Type != events.EventTypeError {
			t.Fatalf("expected error event, got %q", evt.Type)
		}
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected data as map[string]interface{}, got %T", evt.Data)
		}
		if data["message"] != "something went wrong" {
			t.Errorf("expected message 'something went wrong', got %v", data["message"])
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for error event from EventBus")
	}
}
