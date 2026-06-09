package agent

import (
	"context"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

type toolExecutionContextKey string

const (
	toolExecutionContextKeyToolCallID toolExecutionContextKey = "tool_call_id"
	toolExecutionContextKeyToolName   toolExecutionContextKey = "tool_name"
)

// SP-068 collapsed the former WithUserApproved/HasUserApproval context-value
// bridge (which signalled a Gate-1 approval to the Gate-2 persona cascade)
// into the single agent-scoped recordGateApproval → consumeShellCommandApproval
// path in risk_prompt.go, so both dispatch architectures share one mechanism.

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
