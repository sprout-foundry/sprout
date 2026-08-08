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

// The unified risk resolver (UnifiedRiskResolver flag, default ON) runs
// a single gate, so no bridge plumbing is needed.

func withToolExecutionMetadata(ctx context.Context, toolCallID, toolName, workspaceRoot string, effectiveCwd string, sessionFolders []string, auditLogger filesystem.AuditLogger) context.Context {
	ctx = context.WithValue(ctx, toolExecutionContextKeyToolCallID, toolCallID)
	ctx = context.WithValue(ctx, toolExecutionContextKeyToolName, toolName)
	ctx = filesystem.WithWorkspaceRoot(ctx, workspaceRoot)
	ctx = filesystem.WithAgentContext(ctx, effectiveCwd, sessionFolders)
	ctx = filesystem.WithAuditLogger(ctx, auditLogger)
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
