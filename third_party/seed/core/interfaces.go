package core

import "context"

// EventPublisher is the interface for publishing events.
// It is implemented by events.EventBus and can be satisfied by any
// custom event system, allowing seed to be used without the events package.
type EventPublisher interface {
	Publish(eventType string, data any)
}

// noopEventPublisher is a no-op implementation used when no publisher is provided.
type noopEventPublisher struct{}

func (noopEventPublisher) Publish(string, any) {}

// Generic event type constants used by the core package.
// These are the only event types that any consumer of core needs to handle.
const (
	// EventTypeQueryStarted is published when a new query begins.
	// Data map keys: "query" (string), "model" (string)
	EventTypeQueryStarted = "query_started"
	// EventTypeQueryCompleted is published when a query finishes.
	// Data map keys: "query", "response", "tokens", "cost", "duration_ms"
	EventTypeQueryCompleted = "query_completed"
	// EventTypeError is published on errors.
	// Data map keys: "message" (string), "error" (string)
	EventTypeError = "error"
	// EventTypeToolStart is published when a tool call begins.
	// Data map keys: "tool_name", "tool_call_id", "arguments", "tool_index"
	EventTypeToolStart = "tool_start"
	// EventTypeToolEnd is published when a tool call ends.
	// Data map keys: "tool_call_id", "tool_name", "status", "result", "duration_ms"
	EventTypeToolEnd = "tool_end"
	// EventTypeStreamChunk is published for each streaming content chunk.
	// Data map keys: "chunk" (string), "content_type" (string: "text"|"reasoning")
	EventTypeStreamChunk = "stream_chunk"
	// EventTypeMetricsUpdate is published with token usage updates.
	// Data map keys: "total_tokens", "context_tokens", "max_context_tokens", "iteration", "total_cost"
	EventTypeMetricsUpdate = "metrics_update"
	// EventTypeCompaction is published when context compaction occurs.
	// Data map keys: "strategy", "messages_before", "messages_after", "message_count_delta", "tokens_saved"
	EventTypeCompaction = "compaction"
)

// ProviderInfo contains metadata about a provider and its model.
type ProviderInfo struct {
	Model       string `json:"model"`
	ContextSize int    `json:"context_size"`
	HasVision   bool   `json:"has_vision"`
	// MaxOutputTokens, when > 0, is the model's hard cap on output tokens
	// per response. Used by the conversation loop to bound the derived
	// max_tokens request parameter — without it, on large-context models
	// we'd ask for ~ContextSize-input tokens of output, which providers
	// often reject as oversized. Zero means "no hint; cap derived value
	// at defaultMaxOutputCap instead.
	MaxOutputTokens int `json:"max_output_tokens,omitempty"`
}

// Provider represents an LLM provider that can be used for chat completions.
type Provider interface {
	// Chat sends a chat request and returns the response.
	Chat(ctx context.Context, req *ChatRequest) (*ChatResponse, error)

	// ChatStream sends a chat request and streams the response via the handler.
	ChatStream(ctx context.Context, req *ChatRequest, handler StreamHandler) error

	// Info returns metadata about the provider and its model.
	Info() ProviderInfo

	// EstimateTokens returns an approximate token count for the request.
	EstimateTokens(req *ChatRequest) int
}

// StreamHandler handles streamed responses from a Provider.
type StreamHandler interface {
	OnContent(content string)
	OnReasoning(reasoning string)
	OnDone(resp *ChatResponse)
	OnError(err error)
}

// ToolExecutor represents a system that can execute tool calls.
type ToolExecutor interface {
	// GetTools returns the list of available tools.
	GetTools() []Tool

	// Execute runs the given tool calls and returns the resulting messages.
	Execute(ctx context.Context, calls []ToolCall) []Message
}

// UI represents a user interface for prompting and output.
type UI interface {
	// Prompt displays a prompt and returns the user's input.
	Prompt(message string) (string, error)

	// Confirm displays a confirmation message and returns the user's choice.
	Confirm(message string) (bool, error)

	// Print writes output without a trailing newline.
	Print(message string)

	// PrintLine writes output with a trailing newline.
	PrintLine(message string)
}
