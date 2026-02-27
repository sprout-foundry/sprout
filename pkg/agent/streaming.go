package agent

import (
	"sync"

	"github.com/alantheprice/ledit/pkg/events"
)

// SetStreamingEnabled enables or disables streaming responses
func (a *Agent) SetStreamingEnabled(enabled bool) {
	a.streamingEnabled = enabled
	if enabled && a.outputMutex == nil {
		a.outputMutex = &sync.Mutex{}
	}
}

// SetStreamingCallback sets a custom callback for streaming output
func (a *Agent) SetStreamingCallback(callback func(string)) {
	a.streamingCallback = callback
}

// EnableStreaming enables response streaming with a callback
func (a *Agent) EnableStreaming(callback func(string)) {
	a.streamingEnabled = true
	a.streamingCallback = callback
}

// DisableStreaming disables response streaming
func (a *Agent) DisableStreaming() {
	a.streamingEnabled = false
	a.streamingCallback = nil
	a.flushCallback = nil
}

// SetFlushCallback sets a callback to flush buffered output
func (a *Agent) SetFlushCallback(callback func()) {
	a.flushCallback = callback
}

// SetOutputMutex sets the output mutex for synchronized output
func (a *Agent) SetOutputMutex(mutex *sync.Mutex) {
	a.outputMutex = mutex
}

// IsStreamingEnabled returns whether streaming is enabled
func (a *Agent) IsStreamingEnabled() bool {
	return a.streamingEnabled
}

// PublishStreamChunk publishes a streaming chunk for real-time updates
func (a *Agent) PublishStreamChunk(chunk string) {
	a.publishEvent(events.EventTypeStreamChunk, events.StreamChunkEvent(chunk))
}
