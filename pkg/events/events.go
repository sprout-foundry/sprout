// Package events provides event system for ledit UI architecture
package events

import (
	"sync"
	"time"
)

// UIEvent represents an event that can be forwarded between CLI and Web UI
type UIEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Common event types
const (
	EventTypeQueryStarted   = "query_started"
	EventTypeQueryProgress  = "query_progress"
	EventTypeQueryCompleted = "query_completed"
	EventTypeError          = "error"
	EventTypeToolExecution  = "tool_execution"
	EventTypeFileChanged    = "file_changed"
	EventTypeStreamChunk    = "stream_chunk"
	EventTypeMetricsUpdate  = "metrics_update"
)

// EventBus manages event distribution between CLI and Web UI
type EventBus struct {
	subscribers map[string]chan UIEvent
	mutex       sync.RWMutex
	nextID      int64
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]chan UIEvent),
	}
}

// Subscribe adds a new subscriber to the event bus
func (eb *EventBus) Subscribe(name string) <-chan UIEvent {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	ch := make(chan UIEvent, 100) // Buffered channel
	eb.subscribers[name] = ch
	return ch
}

// Unsubscribe removes a subscriber from the event bus
func (eb *EventBus) Unsubscribe(name string) {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	if ch, exists := eb.subscribers[name]; exists {
		delete(eb.subscribers, name)
		close(ch)
	}
}

// Publish broadcasts an event to all subscribers
func (eb *EventBus) Publish(eventType string, data any) {
	eb.mutex.Lock()
	eb.nextID++
	event := UIEvent{
		ID:        generateEventID(eb.nextID),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
	subscribers := make([]chan UIEvent, 0, len(eb.subscribers))
	for _, ch := range eb.subscribers {
		subscribers = append(subscribers, ch)
	}
	eb.mutex.Unlock()

	// Publish to all subscribers without holding the lock
	for _, ch := range subscribers {
		select {
		case ch <- event:
		default:
			// Channel is full, skip this subscriber
			// This prevents blocking if a subscriber is slow
		}
	}
}

// generateEventID creates a unique event ID
func generateEventID(id int64) string {
	return time.Now().Format("20060102-150405") + "-" + string(rune(id))
}

// Helper functions for creating specific event types

// QueryStartedEvent creates a query started event
func QueryStartedEvent(query, provider, model string) map[string]interface{} {
	return map[string]interface{}{
		"query":    query,
		"provider": provider,
		"model":    model,
	}
}

// QueryProgressEvent creates a query progress event
func QueryProgressEvent(message string, iteration int, tokensUsed int) map[string]interface{} {
	return map[string]interface{}{
		"message":     message,
		"iteration":   iteration,
		"tokens_used": tokensUsed,
	}
}

// QueryCompletedEvent creates a query completed event
func QueryCompletedEvent(query, response string, tokensUsed int, cost float64, duration time.Duration) map[string]interface{} {
	return map[string]interface{}{
		"query":       query,
		"response":    response,
		"tokens_used": tokensUsed,
		"cost":        cost,
		"duration_ms": duration.Milliseconds(),
	}
}

// ErrorEvent creates an error event
func ErrorEvent(message string, err error) map[string]interface{} {
	return map[string]interface{}{
		"message": message,
		"error":   err.Error(),
	}
}

// ToolExecutionEvent creates a tool execution event
func ToolExecutionEvent(toolName, action string, details map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name": toolName,
		"action":    action,
	}
	for k, v := range details {
		data[k] = v
	}
	return data
}

// FileChangedEvent creates a file changed event
func FileChangedEvent(filePath, action string, content string) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"action":    action, // "created", "modified", "deleted"
		"content":   content,
	}
}

// StreamChunkEvent creates a stream chunk event
func StreamChunkEvent(chunk string) map[string]interface{} {
	return map[string]interface{}{
		"chunk": chunk,
	}
}

// MetricsUpdateEvent creates a metrics update event
func MetricsUpdateEvent(totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64) map[string]interface{} {
	return map[string]interface{}{
		"total_tokens":       totalTokens,
		"context_tokens":     contextTokens,
		"max_context_tokens": maxContextTokens,
		"iteration":          iteration,
		"total_cost":         totalCost,
	}
}
