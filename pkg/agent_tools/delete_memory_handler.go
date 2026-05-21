package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type deleteMemoryHandler struct{}

func (h *deleteMemoryHandler) Name() string { return "delete_memory" }

func (h *deleteMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "delete_memory",
		Description: "Delete a memory by name. Permanently removes the memory file from ~/.config/sprout/memories/.",
		Required: []string{"name"},
		Parameters: []ParameterDef{
			{Name: "name", Type: "string", Required: true, Description: "Name of the memory to delete (e.g., 'git-safety')"},
		},
	}
}

func (h *deleteMemoryHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "name")
	return err
}

func (h *deleteMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": false,
			})
		}()
	}

	name, _ := extractString(args, "name")

	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to get config directory: %v", err), IsError: true}, nil
	}

	memoryDir := filepath.Join(configDir, "memories")
	if !strings.HasSuffix(name, ".md") {
		name = name + ".md"
	}
	filePath := filepath.Join(memoryDir, name)

	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		return ToolResult{Output: fmt.Sprintf("Memory file %q does not exist", name), IsError: true}, nil
	}

	if err := os.Remove(filePath); err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to delete memory file %q: %v", name, err), IsError: true}, nil
	}

	return ToolResult{Output: fmt.Sprintf("Memory %q deleted successfully", strings.TrimSuffix(name, ".md"))}, nil
}
