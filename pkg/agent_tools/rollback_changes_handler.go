package tools

import (
	"context"
	"fmt"
	"strings"

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

		output := result.Output
		// Append metadata if available
		if result.Metadata != nil {
			var metaParts []string
			for k, v := range result.Metadata {
				metaParts = append(metaParts, fmt.Sprintf("%s: %v", k, v))
			}
			if len(metaParts) > 0 {
				output += "\n" + strings.Join(metaParts, ", ")
			}
		}

		return ToolResult{
			Output:        output,
			IsError:       false,
			StructuredOut: result.Metadata,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}
