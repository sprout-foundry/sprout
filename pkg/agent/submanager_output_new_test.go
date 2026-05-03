package agent

import (
	"sync"
	"testing"
)

func TestNewAgentOutputManager(t *testing.T) {
	om := NewAgentOutputManager()

	if om.IsStreamingEnabled() {
		t.Error("streaming should be disabled by default")
	}
	if om.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil by default")
	}
	if om.GetReasoningCallback() != nil {
		t.Error("reasoning callback should be nil by default")
	}
	if om.GetFlushCallback() != nil {
		t.Error("flush callback should be nil by default")
	}
	if om.GetOutputMutex() != nil {
		t.Error("output mutex should be nil by default")
	}
	if om.GetOutputRouter() != nil {
		t.Error("output router should be nil by default")
	}
	if om.GetAsyncOutput() != nil {
		t.Error("async output channel should be nil by default")
	}
	if om.GetEventMetadata() == nil {
		t.Error("event metadata should be initialized (non-nil) by default")
	}
	if om.GetEventMetadataMutex() == nil {
		t.Error("event metadata mutex should not be nil")
	}
}

func TestAgentOutputManager_StreamingEnabled(t *testing.T) {
	om := NewAgentOutputManager()

	if om.IsStreamingEnabled() {
		t.Error("should be false initially")
	}

	om.SetStreamingEnabled(true)
	if !om.IsStreamingEnabled() {
		t.Error("should be true after setting")
	}

	om.SetStreamingEnabled(false)
	if om.IsStreamingEnabled() {
		t.Error("should be false after resetting")
	}
}

func TestAgentOutputManager_StreamingCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetStreamingCallback() != nil {
		t.Error("streaming callback should be nil by default")
	}

	called := false
	cb := func(s string) { called = true }
	om.SetStreamingCallback(cb)
	if om.GetStreamingCallback() == nil {
		t.Error("GetStreamingCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetStreamingCallback()("test")
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_ReasoningCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetReasoningCallback() != nil {
		t.Error("reasoning callback should be nil by default")
	}

	called := false
	cb := func(s string) { called = true }
	om.SetReasoningCallback(cb)
	if om.GetReasoningCallback() == nil {
		t.Error("GetReasoningCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetReasoningCallback()("test")
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_FlushCallback(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetFlushCallback() != nil {
		t.Error("flush callback should be nil by default")
	}

	called := false
	cb := func() { called = true }
	om.SetFlushCallback(cb)
	if om.GetFlushCallback() == nil {
		t.Error("GetFlushCallback should return non-nil after setting")
	}
	// Call the callback to verify it works
	om.GetFlushCallback()()
	if !called {
		t.Error("callback should have been called")
	}
}

func TestAgentOutputManager_OutputMutex(t *testing.T) {
	om := NewAgentOutputManager()

	mu := &sync.Mutex{}
	om.SetOutputMutex(mu)
	if om.GetOutputMutex() != mu {
		t.Error("GetOutputMutex should return set mutex")
	}
}

func TestAgentOutputManager_StreamingBuffer(t *testing.T) {
	om := NewAgentOutputManager()

	buf := om.GetStreamingBuffer()
	if buf == nil {
		t.Error("GetStreamingBuffer should not be nil")
	}

	buf.WriteString("hello")
	if buf.String() != "hello" {
		t.Errorf("buffer content = %q, want hello", buf.String())
	}

	// Verify it's the same buffer (reference equality)
	buf2 := om.GetStreamingBuffer()
	if buf2.String() != "hello" {
		t.Error("should return the same buffer instance")
	}
}

func TestAgentOutputManager_ReasoningBuffer(t *testing.T) {
	om := NewAgentOutputManager()

	buf := om.GetReasoningBuffer()
	if buf == nil {
		t.Error("GetReasoningBuffer should not be nil")
	}

	buf.WriteString("reasoning")
	if buf.String() != "reasoning" {
		t.Errorf("buffer content = %q, want reasoning", buf.String())
	}
}

func TestAgentOutputManager_OutputRouter(t *testing.T) {
	om := NewAgentOutputManager()

	router := &OutputRouter{}
	om.SetOutputRouter(router)
	if om.GetOutputRouter() != router {
		t.Error("GetOutputRouter should return set router")
	}
}

func TestAgentOutputManager_AsyncOutput(t *testing.T) {
	om := NewAgentOutputManager()

	ch := make(chan string, 1)
	om.SetAsyncOutput(ch)
	if om.GetAsyncOutput() != ch {
		t.Error("GetAsyncOutput should return set channel")
	}
}

func TestAgentOutputManager_EnsureAsyncOutputWorker(t *testing.T) {
	om := NewAgentOutputManager()

	callCount := 0
	fn := func() {
		callCount++
	}

	// First call should execute the function
	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("first call should execute once, got %d", callCount)
	}

	// Subsequent calls should NOT execute (sync.Once behavior)
	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("second call should NOT execute (sync.Once), got %d", callCount)
	}

	om.EnsureAsyncOutputWorker(fn)
	if callCount != 1 {
		t.Errorf("third call should NOT execute (sync.Once), got %d", callCount)
	}
}

func TestAgentOutputManager_EnsureAsyncOutputWorker_DifferentFunctions(t *testing.T) {
	om := NewAgentOutputManager()

	count1 := 0
	count2 := 0

	fn1 := func() { count1++ }
	fn2 := func() { count2++ }

	// First function fires
	om.EnsureAsyncOutputWorker(fn1)
	// Second function should NOT fire (Once is already done)
	om.EnsureAsyncOutputWorker(fn2)

	if count1 != 1 {
		t.Errorf("fn1 called %d times, want 1", count1)
	}
	if count2 != 0 {
		t.Errorf("fn2 called %d times, want 0 (Once already triggered)", count2)
	}
}

func TestAgentOutputManager_AsyncBufferSize(t *testing.T) {
	om := NewAgentOutputManager()

	if om.GetAsyncBufferSize() != 0 {
		t.Errorf("default async buffer size = %d, want 0", om.GetAsyncBufferSize())
	}

	om.SetAsyncBufferSize(10)
	if om.GetAsyncBufferSize() != 10 {
		t.Errorf("GetAsyncBufferSize = %d, want 10", om.GetAsyncBufferSize())
	}
}

func TestAgentOutputManager_EventMetadata(t *testing.T) {
	om := NewAgentOutputManager()

	// Default is empty map
	meta := om.GetEventMetadata()
	if meta == nil {
		t.Error("default metadata should be non-nil")
	}
	if len(meta) != 0 {
		t.Errorf("default metadata should be empty, got %d entries", len(meta))
	}

	// Set new metadata
	om.SetEventMetadata(map[string]interface{}{"key": "value"})
	meta = om.GetEventMetadata()
	if meta["key"] != "value" {
		t.Errorf("metadata[key] = %v, want value", meta["key"])
	}
}

func TestAgentOutputManager_SetEventMetadataUnlocked(t *testing.T) {
	om := NewAgentOutputManager()

	// SetEventMetadataUnlocked sets without mutex
	om.SetEventMetadataUnlocked(map[string]interface{}{"unlocked": true})
	meta := om.GetEventMetadata()
	if meta["unlocked"] != true {
		t.Error("SetEventMetadataUnlocked should set the value")
	}
}

func TestAgentOutputManager_EventMetadataMutex(t *testing.T) {
	om := NewAgentOutputManager()

	mu := om.GetEventMetadataMutex()
	if mu == nil {
		t.Error("GetEventMetadataMutex should not be nil")
	}

	// Verify we can lock and unlock
	mu.Lock()
	mu.Unlock()
}

func TestAgentOutputManager_ConcurrentMetadataAccess(t *testing.T) {
	om := NewAgentOutputManager()

	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(n int) {
			defer wg.Done()
			om.SetEventMetadata(map[string]interface{}{"key": n})
		}(i)

		wg.Add(1)
		go func() {
			defer wg.Done()
			om.GetEventMetadata()
		}()
	}
	wg.Wait()

	// Should not have panicked
	meta := om.GetEventMetadata()
	if meta == nil {
		t.Error("metadata should still be non-nil after concurrent access")
	}
}

func TestAgentOutputManager_StreamingBufferIsolation(t *testing.T) {
	om := NewAgentOutputManager()

	streamingBuf := om.GetStreamingBuffer()
	reasoningBuf := om.GetReasoningBuffer()

	streamingBuf.WriteString("stream")
	reasoningBuf.WriteString("reason")

	if streamingBuf.String() != "stream" {
		t.Errorf("streaming buffer = %q, want stream", streamingBuf.String())
	}
	if reasoningBuf.String() != "reason" {
		t.Errorf("reasoning buffer = %q, want reason", reasoningBuf.String())
	}

	// Buffers should be different
	if streamingBuf.String() == reasoningBuf.String() {
		t.Error("streaming and reasoning buffers should be separate")
	}
}

func TestAgentOutputManager_BuffersPreallocate(t *testing.T) {
	om := NewAgentOutputManager()

	sb := om.GetStreamingBuffer()
	rb := om.GetReasoningBuffer()

	// Writers should start empty
	if sb.Len() != 0 {
		t.Errorf("streaming buffer length = %d, want 0", sb.Len())
	}
	if rb.Len() != 0 {
		t.Errorf("reasoning buffer length = %d, want 0", rb.Len())
	}

	// Multiple getters should return references to the same buffers
	sb.WriteString("data")
	if om.GetStreamingBuffer().String() != "data" {
		t.Error("should return same buffer reference")
	}
}
