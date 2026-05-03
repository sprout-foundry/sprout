package agent

import (
	"sync"
	"testing"
)

func TestSetStreamingEnabled(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	a.SetStreamingEnabled(true)
	if !a.IsStreamingEnabled() {
		t.Error("expected streaming enabled")
	}

	a.SetStreamingEnabled(false)
	if a.IsStreamingEnabled() {
		t.Error("expected streaming disabled")
	}
}

func TestSetStreamingEnabled_EnablesMutex(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	// Ensure no mutex exists initially
	a.output.SetOutputMutex(nil)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu == nil {
		t.Error("expected output mutex to be created when streaming enabled and mutex was nil")
	}
}

func TestSetStreamingEnabled_DoesNotReplaceExistingMutex(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	existingMutex := &sync.Mutex{}
	a.output.SetOutputMutex(existingMutex)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu != existingMutex {
		t.Error("expected existing mutex to be preserved, not replaced")
	}
}

func TestSetStreamingCallback(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var received string
	a.SetStreamingCallback(func(s string) {
		received = s
	})

	cb := a.output.GetStreamingCallback()
	if cb == nil {
		t.Fatal("expected callback to be set")
	}

	cb("test chunk")
	if received != "test chunk" {
		t.Errorf("expected 'test chunk', got %q", received)
	}
}

func TestEnableStreaming(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var captured string
	a.EnableStreaming(func(s string) {
		captured = s
	})

	if !a.IsStreamingEnabled() {
		t.Error("expected streaming enabled")
	}

	cb := a.output.GetStreamingCallback()
	if cb == nil {
		t.Fatal("expected callback to be set")
	}

	cb("enable-streaming-test")
	if captured != "enable-streaming-test" {
		t.Errorf("expected 'enable-streaming-test', got %q", captured)
	}
}

func TestDisableStreaming(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	a.output.SetStreamingCallback(func(s string) {})
	a.output.SetFlushCallback(func() {})
	a.output.SetStreamingEnabled(true)

	a.DisableStreaming()

	if a.IsStreamingEnabled() {
		t.Error("expected streaming disabled")
	}
	if a.output.GetStreamingCallback() != nil {
		t.Error("expected streaming callback to be nil")
	}
	if a.output.GetFlushCallback() != nil {
		t.Error("expected flush callback to be nil")
	}
}

func TestSetFlushCallback(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var flushed bool
	a.SetFlushCallback(func() {
		flushed = true
	})

	cb := a.output.GetFlushCallback()
	if cb == nil {
		t.Fatal("expected flush callback to be set")
	}
	cb()

	if !flushed {
		t.Error("expected flush callback to have been called")
	}
}

func TestSetOutputMutex(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	mu := &sync.Mutex{}
	a.SetOutputMutex(mu)

	if a.output.GetOutputMutex() != mu {
		t.Error("expected same mutex")
	}
}

func TestIsStreamingEnabled_Default(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	if a.IsStreamingEnabled() {
		t.Error("expected streaming disabled by default")
	}
}

func TestPublishStreamChunk_WithRouter(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	// Set up a streaming callback to capture terminal output
	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Assistant text should go to terminal callback
	a.PublishStreamChunk("hello world", "assistant_text")

	if len(terminalChunks) != 1 {
		t.Fatalf("expected 1 terminal chunk, got %d", len(terminalChunks))
	}
	if terminalChunks[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", terminalChunks[0])
	}
}

func TestPublishStreamChunk_WithRouter_ReasoningNotToTerminal(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Reasoning should NOT go to terminal callback (default)
	a.PublishStreamChunk("reasoning step", "reasoning")

	if len(terminalChunks) != 0 {
		t.Errorf("expected 0 terminal chunks for reasoning, got %d: %v", len(terminalChunks), terminalChunks)
	}
}

func TestPublishStreamChunk_DefaultContentType(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Empty contentType should default to assistant_text
	a.PublishStreamChunk("test content", "")

	if len(terminalChunks) != 1 {
		t.Fatalf("expected 1 terminal chunk, got %d", len(terminalChunks))
	}
	if terminalChunks[0] != "test content" {
		t.Errorf("expected 'test content', got %q", terminalChunks[0])
	}
}

func TestPublishStreamChunk_WithoutRouter_WithCallback(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var callbackChunks []string
	a.output.SetStreamingCallback(func(s string) {
		callbackChunks = append(callbackChunks, s)
	})
	// No router set — should use fallback path

	a.PublishStreamChunk("fallback chunk", "assistant_text")

	if len(callbackChunks) != 1 {
		t.Fatalf("expected 1 callback chunk, got %d", len(callbackChunks))
	}
	if callbackChunks[0] != "fallback chunk" {
		t.Errorf("expected 'fallback chunk', got %q", callbackChunks[0])
	}
}

func TestPublishStreamChunk_WithoutRouter_Reasoning_NoCallback(t *testing.T) {
	a := &Agent{}
	a.initSubManagers()

	var callbackCalled bool
	a.output.SetStreamingCallback(func(s string) {
		callbackCalled = true
	})
	// No router set

	// Reasoning content should NOT go to streaming callback in fallback path
	a.PublishStreamChunk("reasoning", "reasoning")

	if callbackCalled {
		t.Error("reasoning content should not trigger streaming callback in fallback path")
	}
}
