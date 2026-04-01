package agent

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewOutputRouter_NilEventBus verifies that creating a router with nil eventBus
// results in terminal-only mode
func TestNewOutputRouter_NilEventBus(t *testing.T) {
	router := NewOutputRouter(nil, nil)
	assert.Equal(t, OutputModeTerminal, router.Mode())
}

// TestNewOutputRouter_WithEventBus verifies that creating a router with eventBus
// results in event-sourced mode
func TestNewOutputRouter_WithEventBus(t *testing.T) {
	bus := events.NewEventBus()
	router := NewOutputRouter(nil, bus)
	assert.Equal(t, OutputModeEventSourced, router.Mode())
}

// TestSetEventBus_UpdatesMode verifies that SetEventBus correctly changes the mode
func TestSetEventBus_UpdatesMode(t *testing.T) {
	// Start with terminal mode (no event bus)
	router := NewOutputRouter(nil, nil)
	assert.Equal(t, OutputModeTerminal, router.Mode())

	// Add event bus - should switch to event-sourced mode
	bus := events.NewEventBus()
	router.SetEventBus(bus)
	assert.Equal(t, OutputModeEventSourced, router.Mode())

	// Remove event bus - should switch back to terminal mode
	router.SetEventBus(nil)
	assert.Equal(t, OutputModeTerminal, router.Mode())
}

// TestRouteStreamChunk_PublishesToEventBus verifies that stream chunks are published
func TestRouteStreamChunk_PublishesToEventBus(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	router.RouteStreamChunk("hello world", "assistant_text")

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeStreamChunk, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "hello world", data["chunk"])
		assert.Equal(t, "assistant_text", data["content_type"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteStreamChunk_CallsStreamingCallback verifies that streamingCallback is invoked
func TestRouteStreamChunk_CallsStreamingCallback(t *testing.T) {
	var callbackCalled bool
	var receivedChunk string
	var callbackMu sync.Mutex

	callback := func(chunk string) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
		receivedChunk = chunk
	}

	agent := &Agent{
		streamingEnabled:  true,
		streamingCallback: callback,
		outputMutex:       &sync.Mutex{},
	}
	router := NewOutputRouter(agent, nil)

	router.RouteStreamChunk("test chunk", "assistant_text")

	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.True(t, callbackCalled, "streamingCallback should be called")
	assert.Equal(t, "test chunk", receivedChunk)
}

// TestRouteStreamChunk_WritesToTerminalFallback verifies fallback to fmt.Print
func TestRouteStreamChunk_WritesToTerminalFallback(t *testing.T) {
	// Capture stdout using pipe
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create router without callback (no agent)
	router := NewOutputRouter(nil, nil)
	router.RouteStreamChunk("fallback output", "assistant_text")

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	assert.Contains(t, output, "fallback output")
}

// TestRouteStreamChunk_SkipsReasoningInTerminalFallback verifies reasoning is not printed
func TestRouteStreamChunk_SkipsReasoningInTerminalFallback(t *testing.T) {
	// Capture stdout using pipe
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create router without callback (no agent)
	router := NewOutputRouter(nil, nil)
	router.RouteStreamChunk("reasoning content", "reasoning")

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Reasoning should NOT be printed to terminal
	assert.Equal(t, "", output, "reasoning content should not be printed to terminal")
}

// TestRouteStreamChunk_SkipsReasoningWithStreamingCallbackByDefault verifies reasoning is
// published to the event bus but does not reach the normal terminal callback by default.
func TestRouteStreamChunk_SkipsReasoningWithStreamingCallbackByDefault(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	var callbackCalled bool
	agent := &Agent{
		streamingEnabled: true,
		streamingCallback: func(string) {
			callbackCalled = true
		},
		outputMutex: &sync.Mutex{},
	}
	router := NewOutputRouter(agent, bus)

	router.RouteStreamChunk("reasoning content", "reasoning")

	assert.False(t, callbackCalled, "reasoning content should not reach the terminal callback by default")

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeStreamChunk, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "reasoning content", data["chunk"])
		assert.Equal(t, "reasoning", data["content_type"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected reasoning stream chunk to be published")
	}
}

// TestRouteStreamChunk_AllowsReasoningWithOptIn verifies reasoning can be rendered to the
// terminal when explicitly enabled.
func TestRouteStreamChunk_AllowsReasoningWithOptIn(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")

	var callbackCalled bool
	var receivedChunk string
	agent := &Agent{
		streamingEnabled: true,
		streamingCallback: func(chunk string) {
			callbackCalled = true
			receivedChunk = chunk
		},
		outputMutex: &sync.Mutex{},
	}
	router := NewOutputRouter(agent, bus)
	router.SetReasoningTerminalEnabled(true)

	router.RouteStreamChunk("reasoning content", "reasoning")

	assert.True(t, callbackCalled, "reasoning content should reach the terminal callback when enabled")
	assert.Equal(t, "reasoning content", receivedChunk)

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeStreamChunk, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "reasoning content", data["chunk"])
		assert.Equal(t, "reasoning", data["content_type"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected reasoning stream chunk to be published")
	}
}

// TestRouteAgentMessage_PublishesToEventBus verifies agent messages are published
func TestRouteAgentMessage_PublishesToEventBus(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	extra := map[string]interface{}{"key": "value"}
	router.RouteAgentMessage("info", "test message", extra)

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeAgentMessage, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "info", data["category"])
		assert.Equal(t, "test message", data["message"])
		assert.Equal(t, "value", data["key"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteAgentMessage_CallsStreamingCallback verifies callback invocation
func TestRouteAgentMessage_CallsStreamingCallback(t *testing.T) {
	var callbackCalled bool
	var receivedMessage string
	var callbackMu sync.Mutex

	callback := func(message string) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
		receivedMessage = message
	}

	agent := &Agent{
		streamingEnabled:  true,
		streamingCallback: callback,
		outputMutex:       &sync.Mutex{},
	}
	router := NewOutputRouter(agent, nil)

	message := "system message"
	router.RouteAgentMessage("info", message, nil)

	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.True(t, callbackCalled, "streamingCallback should be called")
	assert.Contains(t, receivedMessage, message)
	assert.Contains(t, receivedMessage, "\n", "message should have newline")
}

// TestRouteToolLog_PublishesCorrectEvent verifies tool log event structure
func TestRouteToolLog_PublishesCorrectEvent(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	agent := &Agent{
		currentIteration:     3,
		maxContextTokens:     1000,
		currentContextTokens: 500,
	}
	router := NewOutputRouter(agent, bus)

	router.RouteToolLog("read_file", "/path/to/file.go")

	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeAgentMessage, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "tool_log", data["category"])
		assert.Equal(t, "read_file", data["action"])
		assert.Equal(t, "/path/to/file.go", data["target"])
		assert.Equal(t, 3, data["iteration"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteToolLog_HandlesNilAgent verifies no panic when agent is nil
func TestRouteToolLog_HandlesNilAgent(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	// Should not panic
	assert.NotPanics(t, func() {
		router.RouteToolLog("test_action", "test_target")
	})

	// Event should still be published
	select {
	case event := <-ch:
		assert.Equal(t, events.EventTypeAgentMessage, event.Type)
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "tool_log", data["category"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published even with nil agent")
	}
}

// TestRouteToolLog_FormatsTerminalOutput verifies ANSI formatting
func TestRouteToolLog_FormatsTerminalOutput(t *testing.T) {
	// Capture stdout using pipe
	old := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Create router without callback to test terminal fallback path
	router := NewOutputRouter(nil, nil)

	router.RouteToolLog("read_file", "/path/to/file.go")

	// Restore stdout
	w.Close()
	os.Stdout = old

	var buf bytes.Buffer
	io.Copy(&buf, r)
	output := buf.String()

	// Should contain ANSI codes
	assert.Contains(t, output, "\033[90m", "should contain dark gray ANSI code")
	assert.Contains(t, output, "\033[0m", "should contain reset ANSI code")
	assert.Contains(t, output, "read_file", "should contain tool name")
	assert.Contains(t, output, "/path/to/file.go", "should contain target")
}

// TestRouteToolLog_MultipleSubscribers verifies multiple subscribers receive events
func TestRouteToolLog_MultipleSubscribers(t *testing.T) {
	bus := events.NewEventBus()
	ch1 := bus.Subscribe("subscriber1")
	ch2 := bus.Subscribe("subscriber2")
	router := NewOutputRouter(nil, bus)

	router.RouteToolLog("test_action", "test_target")

	// Both subscribers should receive the event
	var event1, event2 events.UIEvent
	var received1, received2 bool

	select {
	case event1 = <-ch1:
		received1 = true
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case event2 = <-ch2:
		received2 = true
	case <-time.After(100 * time.Millisecond):
	}

	assert.True(t, received1, "first subscriber should receive event")
	assert.True(t, received2, "second subscriber should receive event")
	assert.Equal(t, event1.Type, event2.Type, "both events should have same type")
}

// TestRouteStreamChunk_MultipleSubscribers verifies multiple subscribers for stream chunks
func TestRouteStreamChunk_MultipleSubscribers(t *testing.T) {
	bus := events.NewEventBus()
	ch1 := bus.Subscribe("stream1")
	ch2 := bus.Subscribe("stream2")
	router := NewOutputRouter(nil, bus)

	router.RouteStreamChunk("test chunk", "assistant_text")

	// Both subscribers should receive the event
	var event1, event2 events.UIEvent
	var received1, received2 bool

	select {
	case event1 = <-ch1:
		received1 = true
	case <-time.After(100 * time.Millisecond):
	}

	select {
	case event2 = <-ch2:
		received2 = true
	case <-time.After(100 * time.Millisecond):
	}

	assert.True(t, received1, "first subscriber should receive stream event")
	assert.True(t, received2, "second subscriber should receive stream event")
	assert.Equal(t, events.EventTypeStreamChunk, event1.Type)
	assert.Equal(t, events.EventTypeStreamChunk, event2.Type)
}

// TestOutputRouter_ModeThreadSafety verifies mode access is thread-safe
func TestOutputRouter_ModeThreadSafety(t *testing.T) {
	bus := events.NewEventBus()
	router := NewOutputRouter(nil, bus)

	var wg sync.WaitGroup
	iterations := 100

	// Goroutine 1: constantly read mode
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = router.Mode()
		}
	}()

	// Goroutine 2: constantly change mode
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				router.SetEventBus(bus)
			} else {
				router.SetEventBus(nil)
			}
		}
	}()

	wg.Wait()
	// If we get here without panic, thread safety is working
}

// TestOutputRouter_PublishAndSetEventBusConcurrently ensures publish path is safe when
// the event bus is swapped during active streaming.
func TestOutputRouter_PublishAndSetEventBusConcurrently(t *testing.T) {
	busA := events.NewEventBus()
	busB := events.NewEventBus()
	router := NewOutputRouter(nil, busA)

	var wg sync.WaitGroup
	iterations := 500

	// Stream publisher goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			router.RouteStreamChunk(fmt.Sprintf("chunk-%d", i), "assistant_text")
		}
	}()

	// Event bus swapper goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				router.SetEventBus(busB)
			} else {
				router.SetEventBus(busA)
			}
		}
	}()

	wg.Wait()
}

// TestRouteAgentMessage_WithExtraFields verifies extra fields are preserved
func TestRouteAgentMessage_WithExtraFields(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	extra := map[string]interface{}{
		"tool_name":    "test_tool",
		"tool_call_id": "call_123",
		"is_critical":  true,
		"priority":     5,
	}
	router.RouteAgentMessage("warning", "Warning message", extra)

	select {
	case event := <-ch:
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "warning", data["category"])
		assert.Equal(t, "Warning message", data["message"])
		assert.Equal(t, "test_tool", data["tool_name"])
		assert.Equal(t, "call_123", data["tool_call_id"])
		assert.Equal(t, true, data["is_critical"])
		assert.Equal(t, 5, data["priority"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteStreamChunk_CallbackWithModeChange verifies callback works after mode switch
func TestRouteStreamChunk_CallbackWithModeChange(t *testing.T) {
	var callbackCalled bool
	var callbackMu sync.Mutex

	callback := func(chunk string) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
	}

	agent := &Agent{
		streamingEnabled:  true,
		streamingCallback: callback,
		outputMutex:       &sync.Mutex{},
	}

	// Start with event bus
	bus := events.NewEventBus()
	router := NewOutputRouter(agent, bus)

	// Switch to terminal mode
	router.SetEventBus(nil)

	// Callback should still work
	router.RouteStreamChunk("after mode change", "assistant_text")

	callbackMu.Lock()
	defer callbackMu.Unlock()
	assert.True(t, callbackCalled, "callback should work after mode change")
}

// TestNewOutputRouter_AgentFieldPreserved verifies agent field is correctly set
func TestNewOutputRouter_AgentFieldPreserved(t *testing.T) {
	agent := &Agent{
		currentIteration: 5,
	}
	router := NewOutputRouter(agent, nil)

	// Access the agent through the router
	// Note: agent field is private, so we test indirectly via RouteToolLog
	assert.NotNil(t, router)
}

// TestRouteToolLog_EmptyTarget verifies handling of empty target
func TestRouteToolLog_EmptyTarget(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	// Should not panic with empty target
	assert.NotPanics(t, func() {
		router.RouteToolLog("test_action", "")
	})

	select {
	case event := <-ch:
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "tool_log", data["category"])
		assert.Equal(t, "test_action", data["action"])
		// Target should be empty
		assert.Equal(t, "", data["target"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteAgentMessage_EmptyMessage verifies empty message handling
func TestRouteAgentMessage_EmptyMessage(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	router := NewOutputRouter(nil, bus)

	// Should still publish event even with empty message
	router.RouteAgentMessage("info", "", nil)

	select {
	case event := <-ch:
		data, ok := event.Data.(map[string]interface{})
		require.True(t, ok)
		assert.Equal(t, "info", data["category"])
		assert.Equal(t, "", data["message"])
	case <-time.After(100 * time.Millisecond):
		t.Fatal("expected event to be published")
	}
}

// TestRouteStreamChunk_ContentTypeVariations verifies different content types
func TestRouteStreamChunk_ContentTypeVariations(t *testing.T) {
	bus := events.NewEventBus()
	router := NewOutputRouter(nil, bus)

	testCases := []string{
		"assistant_text",
		"reasoning",
		"tool_response",
		"user_message",
		"system_message",
	}

	for _, contentType := range testCases {
		ch := bus.Subscribe(fmt.Sprintf("test_%s", contentType))
		router.RouteStreamChunk("test", contentType)

		select {
		case event := <-ch:
			data, ok := event.Data.(map[string]interface{})
			require.True(t, ok)
			assert.Equal(t, contentType, data["content_type"])
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("expected event for content type %s", contentType)
		}
	}
}

// TestRouteTerminalOnly_DoesNotPublishEvent verifies that RouteTerminalOnly
// writes to the terminal callback but does NOT publish to the event bus.
func TestRouteTerminalOnly_DoesNotPublishEvent(t *testing.T) {
	bus := events.NewEventBus()
	ch := bus.Subscribe("test")
	defer bus.Unsubscribe("test")

	var callbackCalled bool
	var receivedMessage string
	var callbackMu sync.Mutex

	callback := func(message string) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCalled = true
		receivedMessage = message
	}

	agent := &Agent{
		streamingEnabled:  true,
		streamingCallback: callback,
		outputMutex:       &sync.Mutex{},
	}
	router := NewOutputRouter(agent, bus)

	router.RouteTerminalOnly("hello terminal")

	// Verify terminal callback was invoked
	callbackMu.Lock()
	assert.True(t, callbackCalled, "streamingCallback should be called for terminal output")
	assert.Contains(t, receivedMessage, "hello terminal")
	callbackMu.Unlock()

	// Verify no event was published
	select {
	case event := <-ch:
		t.Fatalf("expected no event on bus, got: %s", event.Type)
	case <-time.After(50 * time.Millisecond):
		// Good: no event published
	}
}

// TestRouteTerminalOnly_NilRouter_Fallback verifies that Agent.PrintTerminalOnly
// falls back gracefully when the output router is nil.
func TestPrintTerminalOnly_NilRouter_Fallback(t *testing.T) {
	a := &Agent{}
	// Should not panic
	a.PrintTerminalOnly("test\n")
}
