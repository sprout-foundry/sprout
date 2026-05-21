package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type rollbackChangesHandler struct{}

func (h *rollbackChangesHandler) Name() string {
	return "rollback_changes"
}

func (h *rollbackChangesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "rollback_changes",
		Description: "Preview or perform a rollback of tracked revisions",
		Parameters: []ParameterDef{
			{Name: "revision_id", Type: "string", Description: "Revision ID to rollback (leave blank to list revisions)"},
			{Name: "file_path", Type: "string", Description: "Rollback only this file from the revision"},
			{Name: "confirm", Type: "boolean", Description: "Set to true to execute the rollback"},
		},
		Required: []string{},
	}
}

func (h *rollbackChangesHandler) Validate(args map[string]any) error {
	if confirmVal, exists := args["confirm"]; exists && confirmVal != nil {
		if _, ok := confirmVal.(bool); !ok {
			return fmt.Errorf("parameter 'confirm' must be a boolean, got %T", confirmVal)
		}
	}
	if rid, exists := args["revision_id"]; exists && rid != nil {
		if _, ok := rid.(string); !ok {
			return fmt.Errorf("parameter 'revision_id' must be a string, got %T", rid)
		}
	}
	if fp, exists := args["file_path"]; exists && fp != nil {
		if _, ok := fp.(string); !ok {
			return fmt.Errorf("parameter 'file_path' must be a string, got %T", fp)
		}
	}
	return nil
}

func (h *rollbackChangesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":     toolName,
			"params":   args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()

		revisionID, _ := extractString(args, "revision_id")
		filePath, _ := extractString(args, "file_path")
		confirm := getBoolArg(args, "confirm")

		result, err := RollbackChanges(revisionID, filePath, confirm)
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error during rollback: %v", err),
				IsError: true,
			}, nil
		}

		return ToolResult{
			Output:        result.Output,
			IsError:       false,
			StructuredOut: result.Metadata,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}
