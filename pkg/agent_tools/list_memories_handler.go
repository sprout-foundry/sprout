package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type listMemoriesHandler struct{}

func (h *listMemoriesHandler) Name() string {
	return "list_memories"
}

func (h *listMemoriesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "list_memories",
		Description: "List all saved memories. Returns memory names and their first lines (titles).",
		Parameters:  []ParameterDef{},
		Required:    []string{},
	}
}

func (h *listMemoriesHandler) Validate(args map[string]any) error {
	return nil
}

func (h *listMemoriesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

		memories, err := loadMemoryList()
		if err != nil {
			return ToolResult{
				Output:  fmt.Sprintf("Error listing memories: %v", err),
				IsError: true,
			}, nil
		}

		return ToolResult{
			Output:    formatMemoryList(memories),
			IsError:   false,
		}, nil
	}

	return ToolResult{
		Output:  "No results",
		IsError: false,
	}, nil
}

type memoryEntry struct {
	Name  string
	Title string
}

func loadMemoryList() ([]memoryEntry, error) {
	configDir, err := configuration.GetConfigDir()
	if err != nil {
		return nil, fmt.Errorf("failed to get config directory: %w", err)
	}

	memoryDir := filepath.Join(configDir, "memories")
	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return []memoryEntry{}, nil
		}
		return nil, fmt.Errorf("failed to read memories directory: %w", err)
	}

	var memories []memoryEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		filePath := filepath.Join(memoryDir, entry.Name())
		content, err := os.ReadFile(filePath)
		if err != nil {
			memories = append(memories, memoryEntry{Name: name, Title: "(unreadable)"})
			continue
		}

		// Extract first non-empty line as title
		title := "(no title)"
		for _, line := range strings.Split(string(content), "\n") {
			trimmed := strings.TrimSpace(line)
			if trimmed != "" {
				title = trimmed
				break
			}
		}

		memories = append(memories, memoryEntry{Name: name, Title: title})
	}

	sort.Slice(memories, func(i, j int) bool {
		return memories[i].Name < memories[j].Name
	})

	return memories, nil
}

func formatMemoryList(memories []memoryEntry) string {
	if len(memories) == 0 {
		return "No memories found."
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory(ies):\n\n", len(memories)))
	sb.WriteString("| Name | Title |\n")
	sb.WriteString("| ---- | ----- |\n")
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("| %s | %s |\n", m.Name, m.Title))
	}
	sb.WriteString("\nUse read_memory to view the full content of a memory.\n")
	return sb.String()
}
