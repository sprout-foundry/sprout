package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// manageMemoryHandler implements ToolHandler for the consolidated manage_memory tool.
//
// Dispatches on `operation` to add/read/list/delete/search memories. Replaces
// the legacy add_memory / read_memory / list_memories / delete_memory /
// search_memories tools so the LLM only sees one entry for memory management.
type manageMemoryHandler struct{}

func (h *manageMemoryHandler) Name() string { return "manage_memory" }

func (h *manageMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "manage_memory",
		Description: "Persistent markdown memories at ~/.config/sprout/memories/ that auto-load into the system prompt every future conversation. Operations:\n\n• `add` — create/overwrite. Required: `name` (slug, e.g. 'git-safety'), `content` (markdown).\n• `read` — full content of one memory. Required: `name`.\n• `list` — every saved memory with first-line title.\n• `delete` — remove a memory file. Required: `name`.\n• `search` — semantic search via embedding similarity. Required: `query`. Optional: `threshold` (0.0–1.0, default 0.75), `top_k` (default 5).\n\n**Use `add`** when the user shares a durable preference or convention. **Use `search`/`read`** to recall prior notes. **Use `delete`** when the user says to forget something specific. Memories auto-load — explicit reads are only for verification.",
		Required:   []string{"operation"},
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "One of: 'add', 'read', 'list', 'delete', 'search'."},
			{Name: "name", Type: "string", Description: "Memory name (required for add/read/delete). Short descriptive slug, no .md extension."},
			{Name: "content", Type: "string", Description: "Markdown content (required for add)."},
			{Name: "query", Type: "string", Description: "Natural-language search query (required for search)."},
			{Name: "threshold", Type: "number", Description: "Search-only: minimum similarity 0.0–1.0 (default 0.75)."},
			{Name: "top_k", Type: "integer", Description: "Search-only: maximum results to return (default 5)."},
		},
	}
}

func (h *manageMemoryHandler) Validate(args map[string]any) error {
	op, err := extractString(args, "operation")
	if err != nil {
		return err
	}
	op = strings.TrimSpace(strings.ToLower(op))
	switch op {
	case "add", "read", "list", "delete", "search":
		// valid
	default:
		return agenterrors.NewValidation(fmt.Sprintf("manage_memory: unknown operation %q (want add, read, list, delete, or search)", op), nil)
	}

	// Validate operation-specific required params
	switch op {
	case "add":
		if _, err := extractString(args, "name"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'name' is required for add", nil)
		}
		if _, err := extractString(args, "content"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'content' is required for add", nil)
		}
	case "read", "delete":
		if _, err := extractString(args, "name"); err != nil {
			return agenterrors.NewValidation(fmt.Sprintf("manage_memory: 'name' is required for %s", op), nil)
		}
	case "search":
		if _, err := extractString(args, "query"); err != nil {
			return agenterrors.NewValidation("manage_memory: 'query' is required for search", nil)
		}
	}

	return nil
}

func (h *manageMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	toolName := h.Name()
	var hadError bool
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": hadError,
			})
		}()
	}

	op, _ := extractString(args, "operation")
	op = strings.TrimSpace(strings.ToLower(op))

	switch op {
	case "add":
		return h.executeAdd(env, args)
	case "read":
		return h.executeRead(args)
	case "list":
		return h.executeList()
	case "delete":
		return h.executeDelete(args)
	case "search":
		return h.executeSearch(env, args)
	default:
		hadError = true
		return ToolResult{
			Output:  fmt.Sprintf("manage_memory: unknown operation %q", op),
			IsError: true,
		}, nil
	}
}

func (h *manageMemoryHandler) Aliases() []string         { return nil }
func (h *manageMemoryHandler) Timeout() time.Duration    { return 0 }
func (h *manageMemoryHandler) MaxResultSize() int        { return 0 }
func (h *manageMemoryHandler) SafeForParallel() bool     { return false }
func (h *manageMemoryHandler) Interactive() bool         { return false }

// executeAdd handles the "add" operation: create/overwrite a memory file.
func (h *manageMemoryHandler) executeAdd(env ToolEnv, args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")
	content, _ := extractString(args, "content")

	// Redact secrets before persisting to memory files
	content = redact.String(content)

	sanitized := sanitizeMemoryName(name)
	result, err := saveMemoryToDisk(sanitized, content)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("failed to save memory '%s': %v", name, err),
			IsError: true,
		}, nil
	}

	// Embed into conversation store (best-effort) if embedding manager is available
	if env.EmbeddingMgr != nil && env.EmbeddingMgr.IsInitialized() {
		ctx := context.Background()
		if convoStore, err := env.EmbeddingMgr.GetConversationStore(ctx); err == nil && convoStore != nil {
			_ = convoStore.StoreMemory(ctx, sanitized, content)
		}
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// executeRead handles the "read" operation: return full content of a memory.
func (h *manageMemoryHandler) executeRead(args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")
	sanitized := sanitizeMemoryName(name)

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "unable to locate config directory for memories", IsError: true}, nil
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")
	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: fmt.Sprintf("Memory '%s' not found", sanitized)}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to read memory '%s': %v", sanitized, err),
			IsError: true,
		}, nil
	}

	return ToolResult{
		Output:     string(data),
		TokenUsage: int64(estimateTokenUsage(string(data))),
	}, nil
}

// executeList handles the "list" operation: list all saved memories with previews.
func (h *manageMemoryHandler) executeList() (ToolResult, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "No memories directory found."}, nil
	}

	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: "No memories found."}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to read memories directory: %v", err),
			IsError: true,
		}, nil
	}

	type memoryEntry struct {
		name    string
		preview string
	}

	var memories []memoryEntry
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}

		name := strings.TrimSuffix(entry.Name(), ".md")
		contentPath := filepath.Join(memoryDir, entry.Name())

		contentBytes, err := os.ReadFile(contentPath)
		if err != nil {
			continue // Skip unreadable files
		}

		preview := firstLine(string(contentBytes))
		if len(preview) > 120 {
			preview = preview[:117] + "..."
		}

		memories = append(memories, memoryEntry{name: name, preview: preview})
	}

	if len(memories) == 0 {
		return ToolResult{Output: "No memories found."}, nil
	}

	// Sort alphabetically for deterministic output
	sort.Slice(memories, func(i, j int) bool {
		return memories[i].name < memories[j].name
	})

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories:\n\n", len(memories)))
	for i, m := range memories {
		sb.WriteString(fmt.Sprintf("#%d — **%s**\n", i+1, m.name))
		if m.preview != "" {
			sb.WriteString(fmt.Sprintf("   %s\n", m.preview))
		}
		sb.WriteString("\n")
	}

	return ToolResult{
		Output:     sb.String(),
		TokenUsage: int64(estimateTokenUsage(sb.String())),
	}, nil
}

// executeDelete handles the "delete" operation: remove a memory file.
func (h *manageMemoryHandler) executeDelete(args map[string]any) (ToolResult, error) {
	name, _ := extractString(args, "name")
	sanitized := sanitizeMemoryName(name)

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return ToolResult{Output: "unable to locate config directory for memories", IsError: true}, nil
	}

	filePath := filepath.Join(memoryDir, sanitized+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return ToolResult{Output: fmt.Sprintf("Memory '%s' not found", sanitized)}, nil
		}
		return ToolResult{
			Output:  fmt.Sprintf("failed to delete memory '%s': %v", sanitized, err),
			IsError: true,
		}, nil
	}

	result := fmt.Sprintf("Memory '%s' deleted.", sanitized)

	// Remove embedding from conversation store (best-effort)
	// This is handled by the embedding manager's background cleanup, so
	// we don't block the user on it. The memory file is already deleted.

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

// executeSearch handles the "search" operation: search memories by text matching
// or semantic search via the embedding manager.
func (h *manageMemoryHandler) executeSearch(env ToolEnv, args map[string]any) (ToolResult, error) {
	query, _ := extractString(args, "query")

	topK := 5
	if tkRaw, exists := args["top_k"]; exists && tkRaw != nil {
		switch v := tkRaw.(type) {
		case int:
			topK = v
		case float64:
			topK = int(v)
		}
	}
	if topK < 1 {
		topK = 1
	}

	threshold := 0.75
	if tRaw, exists := args["threshold"]; exists && tRaw != nil {
		switch v := tRaw.(type) {
		case float64:
			threshold = v
		case float32:
			threshold = float64(v)
		case int:
			threshold = float64(v)
		}
	}
	if threshold < 0 {
		threshold = 0
	}
	if threshold > 1 {
		threshold = 1
	}

	// Try semantic (embedding-based) search first if the embedding manager is
	// available and initialized.
	if env.EmbeddingMgr != nil && env.EmbeddingMgr.IsInitialized() {
		output, err := h.semanticSearch(env, query, topK, float32(threshold))
		if err == nil {
			return ToolResult{
				Output:     output,
				TokenUsage: int64(estimateTokenUsage(output)),
			}, nil
		}
	}

	// Fall back to text-based search
	results, err := searchMemoriesByText(query, topK, threshold)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("memory search failed: %v", err),
			IsError: true,
		}, nil
	}

	output := formatMemorySearchResults(query, results, threshold)
	return ToolResult{
		Output:     output,
		TokenUsage: int64(estimateTokenUsage(output)),
	}, nil
}

// semanticSearch performs semantic search using the embedding manager's
// ConversationStore. It embeds the query, retrieves matching memory records,
// and formats the results for display.
func (h *manageMemoryHandler) semanticSearch(env ToolEnv, query string, topK int, threshold float32) (string, error) {
	ctx := context.Background()

	convoStore, err := env.EmbeddingMgr.GetConversationStore(ctx)
	if err != nil {
		return "", fmt.Errorf("conversation store unavailable: %w", err)
	}
	if convoStore == nil {
		return "", fmt.Errorf("conversation store is nil")
	}

	results, err := convoStore.QueryMemories(ctx, query, topK, threshold)
	if err != nil {
		return "", fmt.Errorf("semantic search failed: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories via semantic search for: %q\n\n", len(results), query))

	for i, r := range results {
		preview := ""
		if md, ok := r.Record.Metadata["content_preview"].(string); ok {
			preview = md
		}
		if len(preview) > 120 {
			preview = preview[:117] + "..."
		}
		sb.WriteString(fmt.Sprintf("#%d — **%s** (similarity: %.2f)\n", i+1, r.Record.Name, r.Similarity))
		if preview != "" {
			sb.WriteString(fmt.Sprintf("   Preview: %s\n", preview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Use `manage_memory` with operation=\"read\" to view the full content of any memory.")
	return sb.String(), nil
}
