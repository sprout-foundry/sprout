package agent

import (
	"context"
	"fmt"
	"strings"
)

// handleAddMemory creates or overwrites a memory file
func handleAddMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", fmt.Errorf("name is required: %w", err)
	}

	content, err := getStringArg(args, "content")
	if err != nil {
		return "", fmt.Errorf("content is required: %w", err)
	}

	if err := SaveMemory(name, content); err != nil {
		return "", fmt.Errorf("failed to save memory: %w", err)
	}

	sanitized := sanitizeMemoryName(name)
	return fmt.Sprintf("Memory '%s' saved to ~/.ledit/memories/%s.md. This memory will be loaded in all future conversations.", sanitized, sanitized), nil
}

// handleReadMemory reads and returns the content of a specific memory
func handleReadMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", fmt.Errorf("name is required: %w", err)
	}

	content, err := LoadMemoryContent(name)
	if err != nil {
		return "", fmt.Errorf("failed to read memory '%s': %w", name, err)
	}

	return fmt.Sprintf("## Memory: %s\n\n%s", name, content), nil
}

// handleListMemories returns a formatted list of all saved memories
func handleListMemories(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	memories, err := ListMemories()
	if err != nil {
		return "", fmt.Errorf("failed to list memories: %w", err)
	}

	if len(memories) == 0 {
		return "No memories saved yet. Use `add_memory` to create a memory that persists across conversations.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Saved Memories (%d)\n\n", len(memories)))

	for _, m := range memories {
		// Truncate content to first line for the listing
		title := m.Content
		if len(title) > 120 {
			title = title[:117] + "..."
		}
		// Strip leading markdown heading markers for cleaner display
		title = strings.TrimLeft(title, "# ")
		title = strings.TrimSpace(title)
		sb.WriteString(fmt.Sprintf("- **%s** — %s\n", m.Name, title))
	}

	sb.WriteString("\nUse `read_memory` to view full content, or `add_memory`/`delete_memory` to manage memories.")
	return sb.String(), nil
}

// handleDeleteMemory deletes a memory file by name
func handleDeleteMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", fmt.Errorf("name is required: %w", err)
	}

	// Strip .md extension if provided
	name = strings.TrimSuffix(name, ".md")

	if err := DeleteMemory(name); err != nil {
		return "", fmt.Errorf("failed to delete memory '%s': %w", name, err)
	}

	return fmt.Sprintf("Memory '%s' deleted.", name), nil
}
