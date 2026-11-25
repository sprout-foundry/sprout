package core

import (
	"sync"
	"time"
)

// OutputEvent represents a generic output event emitted through the async
// output channel.
type OutputEvent struct {
	Type      string // "content", "reasoning", "tool_result", "agent_message", "error"
	Content   string
	Source    string // origin of the output (e.g., "stream", "provider", "tool")
	Timestamp time.Time
	Metadata  map[string]string
}

// OutputManager manages all output streams from the agent, including content
// and reasoning buffers, async output delivery, flush callbacks, and event
// metadata. The eventPublisher parameter may be nil (no events) or any
// EventPublisher implementation.
type OutputManager interface {
	// Buffer access
	ContentBuffer() *StreamingBuffer
	ReasoningBuffer() *StreamingBuffer

	// Flush callback management
	SetFlushCallback(fn func())
	Flush()

	// Async output channel for goroutine-safe background output delivery
	AsyncOutput() <-chan OutputEvent
	PublishOutput(event OutputEvent)

	// Event metadata (session ID, model, etc.) attached to output events
	SetEventMetadata(key string, value string)
	GetEventMetadata(key string) string

	// Reset clears all buffers and pending async output
	Reset()

	// Close shuts down the async output channel
	Close()
}

// defaultOutputManager is the concrete implementation of OutputManager.
type defaultOutputManager struct {
	contentBuf    *StreamingBuffer
	reasoningBuf  *StreamingBuffer
	flushCallback func()
	flushMu       sync.Mutex

	asyncChan chan OutputEvent

	metadata map[string]string
	mu       sync.RWMutex

	eventPublisher EventPublisher

	closeMu sync.RWMutex // RLock for publish, Lock for close — zero data races
	closed  bool         // guarded by closeMu
}

// NewOutputManager creates a new OutputManager with the given optional event publisher.
func NewOutputManager(eventBus EventPublisher) OutputManager {
	return &defaultOutputManager{
		contentBuf:     NewStreamingBuffer(),
		reasoningBuf:   NewStreamingBuffer(),
		asyncChan:      make(chan OutputEvent, 256),
		metadata:       make(map[string]string),
		eventPublisher: eventBus,
	}
}

// ContentBuffer returns the content streaming buffer.
func (om *defaultOutputManager) ContentBuffer() *StreamingBuffer {
	return om.contentBuf
}

// ReasoningBuffer returns the reasoning streaming buffer.
func (om *defaultOutputManager) ReasoningBuffer() *StreamingBuffer {
	return om.reasoningBuf
}

// SetFlushCallback registers a callback to be invoked on Flush().
func (om *defaultOutputManager) SetFlushCallback(fn func()) {
	om.flushMu.Lock()
	defer om.flushMu.Unlock()
	om.flushCallback = fn
}

// Flush invokes the registered flush callback if one is set.
func (om *defaultOutputManager) Flush() {
	om.flushMu.Lock()
	cb := om.flushCallback
	om.flushMu.Unlock()

	if cb != nil {
		cb()
	}
}

// AsyncOutput returns the read-only async output channel.
func (om *defaultOutputManager) AsyncOutput() <-chan OutputEvent {
	return om.asyncChan
}

// PublishOutput publishes an OutputEvent to the async channel non-blocking.
// If the async channel (capacity 256) is full, the event is silently dropped.
// Once Close() has been called, this method returns immediately without
// attempting to send on the channel.
// It also publishes a corresponding EventBus event when the bus is configured.
// The caller's event.Metadata map is not mutated — a copy is created for
// merging with global metadata.
func (om *defaultOutputManager) PublishOutput(event OutputEvent) {
	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	// Create a copy of event metadata, then merge global metadata.
	// This avoids mutating the caller's map.
	om.mu.RLock()
	mergedMeta := make(map[string]string, len(event.Metadata)+len(om.metadata))
	for k, v := range event.Metadata {
		mergedMeta[k] = v
	}
	for k, v := range om.metadata {
		if _, exists := mergedMeta[k]; !exists {
			mergedMeta[k] = v
		}
	}
	om.mu.RUnlock()
	event.Metadata = mergedMeta

	// Publish to async channel (non-blocking; dropped if full or closed).
	// The async channel is used by tests and optional consumers.
	om.closeMu.RLock()
	if om.closed {
		om.closeMu.RUnlock()
	} else {
		select {
		case om.asyncChan <- event:
		default:
			// Channel is full, silently drop
		}
		om.closeMu.RUnlock()
	}

	// Publish to eventPublisher independently of async channel state.
	// The eventPublisher is the primary delivery path; asyncChan is secondary.
	// Do not gate eventPublisher delivery on asyncChan capacity.
	if om.eventPublisher != nil {
		switch event.Type {
		case "content":
			om.eventPublisher.Publish(EventTypeStreamChunk,
				map[string]interface{}{"chunk": event.Content, "content_type": "text"})
			om.eventPublisher.Publish("agent_message",
				map[string]interface{}{"category": "info", "message": event.Content})
		case "reasoning":
			om.eventPublisher.Publish(EventTypeStreamChunk,
				map[string]interface{}{"chunk": event.Content, "content_type": "reasoning"})
		case "agent_message":
			om.eventPublisher.Publish("agent_message",
				map[string]interface{}{"category": "info", "message": event.Content})
		case "error":
			om.eventPublisher.Publish(EventTypeError,
				map[string]interface{}{"message": event.Content, "error": event.Content})
			// tool_result and other types: no eventPublisher event (tool results are
			// already communicated via EventTypeToolEnd from the executor).
		}
	}
}

// SetEventMetadata sets a key-value pair in the global event metadata.
func (om *defaultOutputManager) SetEventMetadata(key string, value string) {
	om.mu.Lock()
	defer om.mu.Unlock()
	om.metadata[key] = value
}

// GetEventMetadata returns the value for the given key from the global event metadata.
func (om *defaultOutputManager) GetEventMetadata(key string) string {
	om.mu.RLock()
	defer om.mu.RUnlock()
	return om.metadata[key]
}

// Reset clears both streaming buffers and any pending events on the async channel.
// It drains currently-pending events but does not guarantee draining events
// arriving concurrently during the reset. If the manager has been closed,
// Reset returns immediately without attempting to drain the channel.
func (om *defaultOutputManager) Reset() {
	om.contentBuf.Reset()
	om.reasoningBuf.Reset()

	// Guard against closed state — receiving from a closed channel in Go
	// succeeds immediately and returns the zero value, which would cause
	// the drain loop below to spin forever.
	om.closeMu.RLock()
	closed := om.closed
	om.closeMu.RUnlock()
	if closed {
		return
	}

	// Drain pending events without closing the channel
	for {
		select {
		case <-om.asyncChan:
		default:
			return
		}
	}
}

// Close shuts down the async output channel. It is idempotent — calling it
// multiple times has no additional effect after the first call.
func (om *defaultOutputManager) Close() {
	om.closeMu.Lock()
	defer om.closeMu.Unlock()

	if om.closed {
		return
	}
	om.closed = true
	close(om.asyncChan)
}
