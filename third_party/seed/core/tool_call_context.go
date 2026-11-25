package core

import "context"

// Tool execution context keys. These let handlers look up the originating
// tool call without coupling to the registry. They are attached by
// ToolRegistry.executeSingle and survive the per-tool timeout wrapper in
// runWithTimeout (values propagate through context.WithTimeout).
type toolCallContextKey string

const (
	toolCallContextKeyCallID toolCallContextKey = "call_id"
	toolCallContextKeyName   toolCallContextKey = "tool_name"
	toolCallContextKeyIndex  toolCallContextKey = "call_index"
)

// WithToolCallMetadata returns a context carrying the executing tool call's
// metadata. Exported so hosts that invoke handlers outside the registry
// (custom ToolExecutor implementations) can provide the same metadata.
func WithToolCallMetadata(ctx context.Context, callID, toolName string, callIndex int) context.Context {
	ctx = context.WithValue(ctx, toolCallContextKeyCallID, callID)
	ctx = context.WithValue(ctx, toolCallContextKeyName, toolName)
	ctx = context.WithValue(ctx, toolCallContextKeyIndex, callIndex)
	return ctx
}

// ToolCallIDFromContext returns the ID of the tool call being executed, if
// the context came from ToolRegistry.Execute (or WithToolCallMetadata).
// Returns an empty string when the context carries no tool-call metadata —
// e.g. handlers invoked directly by a host's custom ToolExecutor.
func ToolCallIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(toolCallContextKeyCallID).(string); ok {
		return v
	}
	return ""
}

// ToolNameFromContext returns the resolved tool name being executed, or an
// empty string when the context carries no tool-call metadata.
func ToolNameFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(toolCallContextKeyName).(string); ok {
		return v
	}
	return ""
}

// ToolCallIndexFromContext returns the 0-based position of the tool call in
// the assistant message's tool_calls array, or -1 when the context carries
// no tool-call metadata.
func ToolCallIndexFromContext(ctx context.Context) int {
	if ctx == nil {
		return -1
	}
	if v, ok := ctx.Value(toolCallContextKeyIndex).(int); ok {
		return v
	}
	return -1
}
