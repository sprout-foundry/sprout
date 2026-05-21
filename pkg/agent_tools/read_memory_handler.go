package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type readMemoryHandler struct{}

func (h *readMemoryHandler) Name() string {
	return "read_memory"
}

func (h *readMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "read_memory",
		Description: "Read a specific memory by name. Returns the full content of the memory file.",
		Parameters: []ParameterDef{
			{Name: "name", Type: "string", Description: "Name of the memory to read (without .md extension, e.g., 'git-safety')", Required: true},
		},
		Required: []string{"name"},
	}
}

func (h *readMemoryHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "name")
	return err
}

func (h *readMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

		name, err := extractString(args, "name")
		if err != nil {
			return ToolResult{
				Output:  err.Error(),
				IsError: true,
			}, nil
		}

		content, err := readMemoryFile(name)
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error reading memory '%s': %v", name, err),
				IsError: true,
			}, nil
		}

		return ToolResult{
			Output:    content,
			IsError:   false,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}

func readMemoryFile(name string) (string, error) {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}

	memoryDir := filepath.Join(configDir, "memories")
	filePath := filepath.Join(memoryDir, name+".md")

	content, err := os.ReadFile(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to read memory file %q: %w", name, err)
	}

	return string(content), nil
}
