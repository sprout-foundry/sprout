package agent

import (
	"context"
	"fmt"
	"strings"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// handleAddMemory creates or overwrites a memory file
func handleAddMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewTool("memory", "name is required", err)
	}

	content, err := getStringArg(args, "content")
	if err != nil {
		return "", agenterrors.NewTool("memory", "content is required", err)
	}

	// Redact secrets before persisting to memory files.
	content = redact.String(content)

	if err := SaveMemory(name, content); err != nil {
		return "", agenterrors.NewTool("memory", "failed to save memory", err)
	}

	// Embed the memory into the conversation store (best-effort)
	if a != nil {
		_ = EmbedMemory(ctx, a.GetEmbeddingManager(), name, content)
	}

	sanitized := sanitizeMemoryName(name)
	return fmt.Sprintf("Memory '%s' saved to ~/.config/sprout/memories/%s.md. This memory will be loaded in all future conversations.", sanitized, sanitized), nil
}

// handleReadMemory reads and returns the content of a specific memory
func handleReadMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewTool("memory", "name is required", err)
	}

	content, err := LoadMemoryContent(name)
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to read memory '%s'", name)
	}

	return fmt.Sprintf("## Memory: %s\n\n%s", name, content), nil
}

// handleListMemories returns a formatted list of all saved memories
func handleListMemories(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	memories, err := ListMemories()
	if err != nil {
		return "", agenterrors.NewTool("memory", "failed to list memories", err)
	}

	if len(memories) == 0 {
		return "No memories saved yet. Use `manage_memory` with operation=\"add\" to create a memory that persists across conversations.", nil
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

	sb.WriteString("\nUse `manage_memory` (operation=\"read\" to view full content; operation=\"add\"/\"delete\" to manage memories).")
	return sb.String(), nil
}

// handleDeleteMemory deletes a memory file by name
func handleDeleteMemory(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	name, err := getStringArg(args, "name")
	if err != nil {
		return "", agenterrors.NewTool("memory", "name is required", err)
	}

	// Strip .md extension if provided
	name = strings.TrimSuffix(name, ".md")

	if err := DeleteMemory(name); err != nil {
		return "", agenterrors.Wrapf(err, "failed to delete memory '%s'", name)
	}

	// Remove embedding from conversation store (best-effort)
	if a != nil {
		_ = DeleteMemoryEmbedding(a.GetEmbeddingManager(), name)
	}

	return fmt.Sprintf("Memory '%s' deleted.", name), nil
}
