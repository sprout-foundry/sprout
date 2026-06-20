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

// SP-068 Phase 3 removed the former Gate-1 → Gate-2 approval bridge
// (WithUserApproved/HasUserApproval context values, then
// recordGateApproval → consumeShellCommandApproval agent-scoped map).
// The unified risk resolver (UnifiedRiskResolver flag, default ON) runs
// a single gate, so no bridge plumbing is needed.

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
