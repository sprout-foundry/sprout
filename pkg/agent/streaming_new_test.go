package agent

import (
	"strings"
	"sync"
	"testing"
)

// --- Enable/Disable streaming ---

func TestStreaming_EnableDisable(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}

	if a.IsStreamingEnabled() {
		t.Error("streaming should start disabled")
	}

	a.SetStreamingEnabled(true)
	if !a.IsStreamingEnabled() {
		t.Error("streaming should be enabled after SetStreamingEnabled(true)")
	}

	a.SetStreamingEnabled(false)
	if a.IsStreamingEnabled() {
		t.Error("streaming should be disabled after SetStreamingEnabled(false)")
	}
}

// --- Mutex auto-creation on enable ---

func TestStreaming_EnablesMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(nil)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu == nil {
		t.Error("expected output mutex to be created when streaming enabled and mutex was nil")
	}
}

func TestStreaming_DoesNotReplaceExistingMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	existingMutex := &sync.Mutex{}
	a.output.SetOutputMutex(existingMutex)

	a.SetStreamingEnabled(true)

	mu := a.output.GetOutputMutex()
	if mu != existingMutex {
		t.Error("expected existing mutex to be preserved, not replaced")
	}
}

// --- DisableStreaming clears state ---

func TestStreaming_DisableClearsState(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	a.output.SetStreamingCallback(func(s string) {})
	a.output.SetFlushCallback(func() {})
	a.output.SetStreamingEnabled(true)

	a.DisableStreaming()

	if a.IsStreamingEnabled() {
		t.Error("streaming should be disabled")
	}
	if a.output.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil after DisableStreaming")
	}
	if a.output.GetFlushCallback() != nil {
		t.Error("flush callback should be nil after DisableStreaming")
	}
}

// --- SetStreamingCallback / EnableStreaming ---

func TestStreaming_SetStreamingCallback(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var received string
	a.SetStreamingCallback(func(s string) { received = s })

	cb := a.output.GetStreamingCallback()
	if cb == nil {
		t.Fatal("expected callback to be set")
	}
	cb("test chunk")
	if received != "test chunk" {
		t.Errorf("expected 'test chunk', got %q", received)
	}
}

func TestStreaming_EnableStreaming(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var captured string
	a.EnableStreaming(func(s string) { captured = s })

	if !a.IsStreamingEnabled() {
		t.Error("streaming should be enabled after EnableStreaming")
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

// --- SetFlushCallback ---

func TestStreaming_SetFlushCallback(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var flushed bool
	a.SetFlushCallback(func() { flushed = true })

	cb := a.output.GetFlushCallback()
	if cb == nil {
		t.Fatal("expected flush callback to be set")
	}
	cb()
	if !flushed {
		t.Error("expected flush callback to have been called")
	}
}

// --- SetOutputMutex ---

func TestStreaming_SetOutputMutex(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	mu := &sync.Mutex{}
	a.SetOutputMutex(mu)
	if a.output.GetOutputMutex() != mu {
		t.Error("expected same mutex")
	}
}

// --- PublishStreamChunk: router path ---

func TestStreaming_PublishStreamChunk_WithRouter(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Assistant text should go to terminal callback via router
	a.PublishStreamChunk("hello world", "assistant_text")

	if len(terminalChunks) != 1 {
		t.Fatalf("expected 1 terminal chunk, got %d", len(terminalChunks))
	}
	if terminalChunks[0] != "hello world" {
		t.Errorf("expected 'hello world', got %q", terminalChunks[0])
	}
}

func TestStreaming_PublishStreamChunk_WithRouter_ReasoningNotToTerminal(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var terminalChunks []string
	a.output.SetStreamingCallback(func(s string) {
		terminalChunks = append(terminalChunks, s)
	})
	a.output.SetStreamingEnabled(true)

	router := NewOutputRouter(a, nil)
	a.output.SetOutputRouter(router)

	// Reasoning should NOT go to terminal callback via router
	a.PublishStreamChunk("reasoning step", "reasoning")

	if len(terminalChunks) != 0 {
		t.Errorf("expected 0 terminal chunks for reasoning, got %d: %v", len(terminalChunks), terminalChunks)
	}
}

func TestStreaming_PublishStreamChunk_WithRouter_DefaultContentType(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

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

// --- PublishStreamChunk: fallback path (no router) ---

func TestStreaming_PublishStreamChunk_NoRouter(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

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

func TestStreaming_PublishStreamChunk_NoRouter_DefaultContentType(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var received string
	a.output.SetStreamingCallback(func(s string) { received = s })

	// Empty content type should default to "assistant_text"
	a.PublishStreamChunk("test", "")
	if received != "test" {
		t.Errorf("expected 'test', got %q", received)
	}
}

func TestStreaming_PublishStreamChunk_NoRouter_ReasoningNotForwarded(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var callbackCalled bool
	a.output.SetStreamingCallback(func(s string) { callbackCalled = true })

	// Reasoning content should NOT go to streaming callback in fallback path
	a.PublishStreamChunk("reasoning", "reasoning")

	if callbackCalled {
		t.Error("reasoning content should not trigger streaming callback in fallback path")
	}
}

func TestStreaming_PublishStreamChunk_CallbackIntegration(t *testing.T) {
	a := &Agent{output: NewAgentOutputManager()}
	a.output.SetOutputMutex(&sync.Mutex{})

	var buf strings.Builder
	a.output.SetStreamingCallback(func(s string) { buf.WriteString(s) })
	a.output.SetStreamingEnabled(true)

	a.PublishStreamChunk("abc", "")
	if buf.String() != "abc" {
		t.Errorf("expected 'abc' in buffer, got %q", buf.String())
	}
}
