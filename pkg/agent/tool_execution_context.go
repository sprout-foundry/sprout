package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type toolExecutionContextKey string

const (
	toolExecutionContextKeyToolCallID toolExecutionContextKey = "tool_call_id"
	toolExecutionContextKeyToolName   toolExecutionContextKey = "tool_name"
	// toolExecutionContextKeyUserApproved is set when Gate 1 (the
	// static security classifier in tool_security.go) has already
	// shown a prompt and the user approved. The persona-cascade
	// gate (Gate 2, in tool_handlers_shell.go) checks this so it
	// doesn't re-prompt the same user for the same command (SP-058
	// follow-up — fixes double-prompt regression).
	toolExecutionContextKeyUserApproved toolExecutionContextKey = "user_approved"
)

// WithUserApproved marks the context as already having a user
// approval for the current tool call. Used by the static security
// gate to signal the persona cascade that re-prompting would be
// redundant.
func WithUserApproved(ctx context.Context) context.Context {
	return context.WithValue(ctx, toolExecutionContextKeyUserApproved, true)
}

// HasUserApproval reports whether an upstream gate already obtained
// user approval for the current tool execution.
func HasUserApproval(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, ok := ctx.Value(toolExecutionContextKeyUserApproved).(bool)
	return ok && v
}

func withToolExecutionMetadata(ctx context.Context, toolCallID, toolName, workspaceRoot string) context.Context {
	ctx = context.WithValue(ctx, toolExecutionContextKeyToolCallID, toolCallID)
	ctx = context.WithValue(ctx, toolExecutionContextKeyToolName, toolName)
	ctx = filesystem.WithWorkspaceRoot(ctx, workspaceRoot)
	return ctx
}

func toolExecutionMetadataFromContext(ctx context.Context) (toolCallID, toolName string) {
	if ctx == nil {
		return "", ""
	}
	if v, ok := ctx.Value(toolExecutionContextKeyToolCallID).(string); ok {
		toolCallID = v
	}
	if v, ok := ctx.Value(toolExecutionContextKeyToolName).(string); ok {
		toolName = v
	}
	return toolCallID, toolName
}
