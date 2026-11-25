package core

import (
	"bytes"
	"strings"
	"sync"
)

// StreamingBuffer captures streamed output for controlled display.
type StreamingBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

// NewStreamingBuffer creates a new streaming buffer.
func NewStreamingBuffer() *StreamingBuffer {
	return &StreamingBuffer{}
}

// Write appends content to the buffer.
func (b *StreamingBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Write(p)
}

// String returns the current buffer contents.
func (b *StreamingBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.String()
}

// Len returns the current buffer length.
func (b *StreamingBuffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buffer.Len()
}

// Reset clears the buffer.
func (b *StreamingBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buffer.Reset()
}

// AgentStreamHandler is a concrete StreamHandler implementation that writes
// streamed content to the agent's buffers and publishes events via the
// agent's eventPublisher.
type AgentStreamHandler struct {
	agent    *Agent
	state    *State
	response *ChatResponse

	// content and reasoning are what this one stream delivered. The agent's
	// buffers span the whole query (every model call in it), so the response
	// is filled from these instead.
	content   strings.Builder
	reasoning strings.Builder
}

// NewAgentStreamHandler creates a new StreamHandler for the given agent.
func NewAgentStreamHandler(agent *Agent, state *State) *AgentStreamHandler {
	return &AgentStreamHandler{
		agent: agent,
		state: state,
	}
}

// OnContent handles a streaming content chunk: writes to the stream buffer,
// delegates to OutputManager.PublishOutput for event publishing, and invokes
// the flush callback. Empty content chunks are silently ignored.
func (h *AgentStreamHandler) OnContent(content string) {
	if content == "" {
		return
	}
	h.content.WriteString(content)
	h.agent.outputMgr.ContentBuffer().Write([]byte(content))
	h.agent.outputMgr.PublishOutput(OutputEvent{Type: "content", Content: content, Source: "stream"})
	h.agent.outputMgr.Flush()
}

// OnReasoning handles a streaming reasoning chunk: writes to the reasoning
// buffer, delegates to OutputManager.PublishOutput for event publishing, and
// invokes the flush callback. Empty reasoning chunks are silently ignored.
func (h *AgentStreamHandler) OnReasoning(reasoning string) {
	if reasoning == "" {
		return
	}
	h.reasoning.WriteString(reasoning)
	h.agent.outputMgr.ReasoningBuffer().Write([]byte(reasoning))
	h.agent.outputMgr.PublishOutput(OutputEvent{Type: "reasoning", Content: reasoning, Source: "stream"})
	h.agent.outputMgr.Flush()
}

// OnDone handles the end of streaming: records token usage, publishes
// metrics, and fills in what the provider's response left out from what this
// stream delivered: the content when the response has none, and the reasoning
// as the message's ReasoningContent. A provider whose ChatStream builds no
// message text still yields the streamed text. Reasoning stays out of
// Content, so a reasoning-only reply is still recognised as one.
func (h *AgentStreamHandler) OnDone(resp *ChatResponse) {
	if resp == nil {
		return
	}
	h.response = resp
	if len(resp.Choices) == 0 {
		if h.agent.eventPublisher != nil {
			h.agent.eventPublisher.Publish(EventTypeError,
				map[string]interface{}{"message": "stream returned zero choices", "error": "zero_choices"})
		}
		return
	}

	choice := &resp.Choices[0]
	if choice.Message.Content == "" {
		choice.Message.Content = h.content.String()
	}
	if choice.Message.ReasoningContent == "" {
		choice.Message.ReasoningContent = h.reasoning.String()
	}

	if resp.Usage.TotalTokens > 0 {
		h.state.AddTokens(resp.Usage.PromptTokens,
			resp.Usage.CompletionTokens, resp.Usage.TotalTokens)

		// Publish metrics update event (matches non-streaming path)
		if h.agent.eventPublisher != nil {
			h.agent.eventPublisher.Publish(EventTypeMetricsUpdate, map[string]interface{}{
				"total_tokens":       h.state.TotalTokens(),
				"context_tokens":     resp.Usage.PromptTokens,
				"max_context_tokens": h.agent.provider.Info().ContextSize,
				"iteration":          0, // iteration index not tracked in stream handler
				"total_cost":         h.state.TotalCost(),
			})
		}
	}
}

// Response returns the final ChatResponse from OnDone, or nil if not yet set.
func (h *AgentStreamHandler) Response() *ChatResponse {
	return h.response
}

// OnError handles streaming errors: publishes an error event via the bus.
func (h *AgentStreamHandler) OnError(err error) {
	if h.agent.eventPublisher != nil {
		h.agent.eventPublisher.Publish(EventTypeError,
			map[string]interface{}{"message": "chat stream failed", "error": err.Error()})
	}
}
