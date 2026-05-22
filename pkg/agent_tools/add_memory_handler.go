package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type addMemoryHandler struct{}

func (h *addMemoryHandler) Name() string { return "add_memory" }

func (h *addMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "add_memory",
		Description: "Save a memory to persist across all conversations. Use this to remember user preferences, learned patterns, project-specific conventions, or anything useful for future sessions. Memories are stored as markdown files in ~/.config/sprout/memories/ and loaded into your system prompt automatically.",
		Required: []string{"name", "content"},
		Parameters: []ParameterDef{
			{Name: "name", Type: "string", Required: true, Description: "Short descriptive name for the memory (e.g., 'git-safety', 'test-conventions')"},
			{Name: "content", Type: "string", Required: true, Description: "Markdown content to store in the memory file"},
		},
	}
}

func (h *addMemoryHandler) Validate(args map[string]any) error {
	name, err := extractString(args, "name")
	if err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("parameter 'name' must not be empty")
	}
	_, err = extractString(args, "content")
	return err
}

func (h *addMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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
	content, _ := extractString(args, "content")

	// Sanitize the name (same logic as pkg/agent/memory.go:saveMemory)
	sanitized := sanitizeMemName(name)

	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to get config directory: %v", err), IsError: true}, nil
	}

	memoryDir := filepath.Join(configDir, "memories")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to create memories directory: %v", err), IsError: true}, nil
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")
	if err := os.WriteFile(filePath, []byte(content), 0600); err != nil {
		return ToolResult{Output: fmt.Sprintf("Failed to write memory file: %v", err), IsError: true}, nil
	}

	return ToolResult{Output: fmt.Sprintf("Memory %q saved successfully to %s", sanitized, filePath)}, nil
}

// sanitizeMemName sanitizes a memory name (replicated from pkg/agent/memory.go)
func sanitizeMemName(name string) string {
	name = strings.ToLower(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = regexp.MustCompile(`[^a-z0-9\-_]+`).ReplaceAllString(name, "")
	name = strings.Trim(name, "-_")
	if name == "" {
		name = "untitled"
	}
	return name
}
