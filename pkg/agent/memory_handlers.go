package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/embedding"
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
	return fmt.Sprintf("Memory '%s' saved to ~/.config/sprout/memories/%s.md. This memory will be loaded in all future conversations.", sanitized, sanitized), nil
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

// handleSearchMemories searches saved memories semantically by query
func handleSearchMemories(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	query, err := getStringArg(args, "query")
	if err != nil {
		return "", fmt.Errorf("query is required: %w", err)
	}

	// Extract max_results with default of 5, clamped to [1, 100]
	maxResults := 5
	if mr, ok := args["max_results"]; ok {
		switch v := mr.(type) {
		case int:
			maxResults = v
		case float64:
			maxResults = int(v)
		}
	}
	if maxResults < 1 {
		maxResults = 1
	}
	if maxResults > 100 {
		maxResults = 100
	}

	// Get embedding manager (nil-safe)
	var em *embedding.EmbeddingManager
	if a != nil {
		em = a.GetEmbeddingManager()
	}
	if em == nil {
		return "Memory search is not available. Embedding index is not enabled.", nil
	}

	// Get conversation store
	store, err := em.GetConversationStore(ctx)
	if err != nil {
		return "", fmt.Errorf("failed to get conversation store: %w", err)
	}
	if store == nil {
		return "Memory search is not available. Conversation store is not initialized.", nil
	}

	// Embed the query
	emb, err := store.Provider().Embed(ctx, query)
	if err != nil {
		return "", fmt.Errorf("failed to embed query: %w", err)
	}

	// Since the store uses a linear scan for TopK, request all records.
	// This ensures memory records are never missed when the store contains
	// a mix of memory and non-memory (e.g., conversation turn) records.
	// We then filter to only memory type and cap at maxResults.
	results, err := store.Query(emb, store.Size(), 0.0)
	if err != nil {
		return "", fmt.Errorf("failed to search memories: %w", err)
	}

	// Filter to only memory-type records, capping at maxResults
	var memoryResults []embedding.QueryResult
	for _, r := range results {
		if r.Record.Type == "memory" {
			memoryResults = append(memoryResults, r)
			if len(memoryResults) >= maxResults {
				break
			}
		}
	}

	// If no memories found, return helpful message
	if len(memoryResults) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nUse `list_memories` to see all saved memories, or `add_memory` to create new ones.", query), nil
	}

	// Format results as markdown list
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Memory Search Results for: %q\n\n", query))
	sb.WriteString(fmt.Sprintf("Found %d result(s):\n\n", len(memoryResults)))

	for i, r := range memoryResults {
		name := r.Record.Name
		title := ""
		if r.Record.Metadata != nil {
			if t, ok := r.Record.Metadata["title"].(string); ok {
				// Strip leading markdown heading markers for cleaner display
				title = strings.TrimLeft(t, "# ")
				title = strings.TrimSpace(title)
			}
		}
		sb.WriteString(fmt.Sprintf("%d. **%s** — %s (relevance: %.2f)\n", i+1, name, title, r.Similarity))
	}

	sb.WriteString("\nUse `read_memory` to view full content of any memory.")
	return sb.String(), nil
}
