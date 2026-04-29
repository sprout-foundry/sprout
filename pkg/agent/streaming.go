package agent

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// SetStreamingEnabled enables or disables streaming responses
func (a *Agent) SetStreamingEnabled(enabled bool) {
	a.output.SetStreamingEnabled(enabled)
	if enabled && a.output.GetOutputMutex() == nil {
		a.output.SetOutputMutex(&sync.Mutex{})
	}
}

// SetStreamingCallback sets a custom callback for streaming output
func (a *Agent) SetStreamingCallback(callback func(string)) {
	a.output.SetStreamingCallback(callback)
}

// EnableStreaming enables response streaming with a callback
func (a *Agent) EnableStreaming(callback func(string)) {
	a.output.SetStreamingEnabled(true)
	a.output.SetStreamingCallback(callback)
}

// DisableStreaming disables response streaming
func (a *Agent) DisableStreaming() {
	a.output.SetStreamingEnabled(false)
	a.output.SetStreamingCallback(nil)
	a.output.SetFlushCallback(nil)
}

// SetFlushCallback sets a callback to flush buffered output
func (a *Agent) SetFlushCallback(callback func()) {
	a.output.SetFlushCallback(callback)
}

// SetOutputMutex sets the output mutex for synchronized output
func (a *Agent) SetOutputMutex(mutex *sync.Mutex) {
	a.output.SetOutputMutex(mutex)
}

// IsStreamingEnabled returns whether streaming is enabled
func (a *Agent) IsStreamingEnabled() bool {
	return a.output.IsStreamingEnabled()
}

// PublishStreamChunk publishes a streaming chunk for real-time updates
func (a *Agent) PublishStreamChunk(chunk string, contentType string) {
	if contentType == "" {
		contentType = "assistant_text"
	}
	// Route through OutputRouter (single source: publishes event + writes terminal)
	if a.output.GetOutputRouter() != nil {
		a.output.GetOutputRouter().RouteStreamChunk(chunk, contentType)
		return
	}
	// Fallback for when router isn't initialized: publish event and write terminal
	a.publishEvent(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk, contentType))
	if contentType != "reasoning" {
		if a.output.GetStreamingCallback() != nil {
			a.output.GetStreamingCallback()(chunk)
		}
	}
}
