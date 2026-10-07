package tools

import (
	"context"
	"fmt"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

type checkpointHandler struct{}

func (h *checkpointHandler) Name() string {
	return "checkpoint"
}

func (h *checkpointHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name: "checkpoint",
		Description: "Create, list, and restore project checkpoints. A checkpoint is a restorable marker " +
			"of a project state, recorded automatically at a passing verification and a completed deploy. " +
			"Restoring is one action and is itself recorded as a checkpoint, so the timeline shows it; nothing is lost.",
		Parameters: []ParameterDef{
			{Name: "action", Type: "string", Description: "One of 'list' (default), 'create', or 'restore'"},
			{Name: "checkpoint_id", Type: "string", Description: "Checkpoint ID to restore (from 'list')"},
			{Name: "revision_id", Type: "string", Description: "Restore the checkpoint that captured this revision (alternative to checkpoint_id)"},
			{Name: "summary", Type: "string", Description: "Optional summary for a created checkpoint"},
			{Name: "confirm", Type: "boolean", Description: "Set to true to execute a restore"},
		},
		Required: []string{},
	}
}

func (h *checkpointHandler) Validate(args map[string]any) error {
	if v, exists := args["action"]; exists && v != nil {
		if _, ok := v.(string); !ok {
			return agenterrors.NewValidation(fmt.Sprintf("parameter 'action' must be a string, got %T", v), nil)
		}
	}
	if v, exists := args["confirm"]; exists && v != nil {
		if _, ok := v.(bool); !ok {
			return agenterrors.NewValidation(fmt.Sprintf("parameter 'confirm' must be a boolean, got %T", v), nil)
		}
	}
	return nil
}

func (h *checkpointHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	action, _ := extractString(args, "action")
	id, _ := extractString(args, "checkpoint_id")
	revisionID, _ := extractString(args, "revision_id")
	summary, _ := extractString(args, "summary")
	confirm := getBoolArg(args, "confirm")

	result, err := ManageCheckpoints(env.WorkspaceRoot, action, id, revisionID, summary, confirm)
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("checkpoint: %v", err), IsError: true}, nil
	}

	return ToolResult{
		Output:        result.Output,
		IsError:       !result.Success,
		StructuredOut: result.Metadata,
	}, nil
}

func (h *checkpointHandler) Aliases() []string      { return nil }
func (h *checkpointHandler) Timeout() time.Duration { return 0 }
func (h *checkpointHandler) MaxResultSize() int     { return 4096 }
func (h *checkpointHandler) SafeForParallel() bool  { return false }
func (h *checkpointHandler) Interactive() bool      { return false }
