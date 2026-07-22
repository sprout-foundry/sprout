package agent

import (
	"context"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

func TestWithToolExecutionMetadata(t *testing.T) {
	ctx := withToolExecutionMetadata(context.Background(), "call-123", "read_file", "/workspace", "/workspace", nil)

	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	if toolCallID != "call-123" {
		t.Errorf("expected tool_call_id 'call-123'; got %q", toolCallID)
	}
	if toolName != "read_file" {
		t.Errorf("expected tool_name 'read_file'; got %q", toolName)
	}
}

func TestWithToolExecutionMetadata_ContainsWorkspaceRoot(t *testing.T) {
	ctx := withToolExecutionMetadata(context.Background(), "call-456", "shell_command", "/home/user/project", "/home/user/project", nil)

	workspaceRoot := filesystem.WorkspaceRootFromContext(ctx)

	if workspaceRoot != "/home/user/project" {
		t.Errorf("expected workspace root '/home/user/project'; got %q", workspaceRoot)
	}
}

func TestToolExecutionMetadataFromContext(t *testing.T) {
	ctx := withToolExecutionMetadata(context.Background(), "call-789", "write_file", "/workspace", "/workspace", nil)

	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	if toolCallID != "call-789" {
		t.Errorf("expected tool_call_id 'call-789'; got %q", toolCallID)
	}
	if toolName != "write_file" {
		t.Errorf("expected tool_name 'write_file'; got %q", toolName)
	}
}

func TestToolExecutionMetadataFromContext_NilContext(t *testing.T) {
	toolCallID, toolName := toolExecutionMetadataFromContext(nil)

	if toolCallID != "" {
		t.Errorf("expected empty tool_call_id for nil context; got %q", toolCallID)
	}
	if toolName != "" {
		t.Errorf("expected empty tool_name for nil context; got %q", toolName)
	}
}

func TestToolExecutionMetadataFromContext_MissingKeys(t *testing.T) {
	// Create a context without tool execution metadata
	ctx := context.Background()

	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	if toolCallID != "" {
		t.Errorf("expected empty tool_call_id for context without keys; got %q", toolCallID)
	}
	if toolName != "" {
		t.Errorf("expected empty tool_name for context without keys; got %q", toolName)
	}
}

func TestToolExecutionMetadataFromContext_WrongTypes(t *testing.T) {
	// Create a context where the values are the wrong type
	ctx := context.WithValue(context.Background(), toolExecutionContextKeyToolCallID, 12345)
	ctx = context.WithValue(ctx, toolExecutionContextKeyToolName, true)

	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	if toolCallID != "" {
		t.Errorf("expected empty tool_call_id for wrong type; got %q", toolCallID)
	}
	if toolName != "" {
		t.Errorf("expected empty tool_name for wrong type; got %q", toolName)
	}
}
