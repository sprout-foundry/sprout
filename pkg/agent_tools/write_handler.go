package tools

import (
	"context"
	"fmt"
	"io"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// writeFileHandler implements ToolHandler for the write_file tool.
type writeFileHandler struct{}

func (h *writeFileHandler) Name() string {
	return "write_file"
}

func (h *writeFileHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "write_file",
		Description: "Write content to a file",
		Parameters: []ParameterDef{
			{
				Name:        "path",
				Type:        "string",
				Required:    true,
				Description: "Path to the file to write",
			},
			{
				Name:        "content",
				Type:        "string",
				Required:    true,
				Description: "Content to write to the file",
			},
		},
		Required: []string{"path", "content"},
	}
}

func (h *writeFileHandler) Validate(args map[string]any) error {
	path, err := extractString(args, "path")
	if err != nil {
		return err
	}
	if strings.TrimSpace(path) == "" {
		return fmt.Errorf("parameter 'path' must not be empty")
	}

	content, err := extractString(args, "content")
	if err != nil {
		return err
	}
	_ = content // content is validated by the write function itself

	return nil
}

func (h *writeFileHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	path, err := extractString(args, "path")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	content, err := extractString(args, "content")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// SP-046-2: Check staleness before writing
	if err := CheckStaleness(path); err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	// Publish tool start event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool": "write_file",
			"path": path,
		})
	}

	result, err := WriteFile(ctx, path, content)
	if err != nil {
		return ToolResult{
			Output:  "",
			IsError: true,
		}, fmt.Errorf("write file %q: %w", path, err)
	}

	// Publish tool end event
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
			"tool":   "write_file",
			"path":   path,
			"bytes":  len(content),
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
