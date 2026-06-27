package tools

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// editFileHandler implements ToolHandler for the edit_file tool.
type editFileHandler struct{}

func (h *editFileHandler) Name() string {
	return "edit_file"
}

func (h *editFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "edit_file",
		Description: "Edit a file by replacing old string with new string",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to edit",
			},
			{
				Name:        "old_str",
				Type:        "string",
				Required:    true,
				Description: "String to replace",
			},
			{
				Name:        "new_str",
				Type:        "string",
				Required:    true,
				Description: "Replacement string",
			},
		},
		Required: []string{"path", "old_str", "new_str"},
	}
}

func (h *editFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("parameter 'path' must not be empty")
	}

	oldStr, err := extractString(args, "old_str")
	if err != nil {
		return err
	}
	if strings.TrimSpace(oldStr) == "" {
		return fmt.Errorf("parameter 'old_str' must not be empty")
	}

	newStr, err := extractString(args, "new_str")
	if err != nil {
		return err
	}
	_ = newStr // new_str can be empty (replacing with nothing)

	return nil
}

func (h *editFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	oldStr, err := extractString(args, "old_str")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	newStr, err := extractString(args, "new_str")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-046-2: Check staleness before editing
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool": "edit_file",
			"path": path,
		})
	}

	result, err := EditFile(ctx, path, oldStr, newStr)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("edit file %q: %w", path, err)
	}

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":   "edit_file",
			"path":   path,
			"tokens": estimateTokenUsage(result),
		})
	}

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}
