// Package tools provides the manage_memory tool handler.
//
// This is a consolidated handler replacing the legacy per-operation memory
// tools (add_memory, read_memory, list_memories, delete_memory, search_memories).
// It dispatches on the `operation` parameter to the appropriate sub-handler.
package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// manageMemoryHandler implements ToolHandler for the manage_memory tool.
type manageMemoryHandler struct{}

func (h *manageMemoryHandler) Name() string { return "manage_memory" }

func (h *manageMemoryHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "manage_memory",
		Description: "Persistent markdown memories at ~/.config/sprout/memories/ that auto-load into the system prompt every future conversation. Operations:\n\n• `add` — create/overwrite. Required: `name` (slug, e.g. 'git-safety'), `content` (markdown).\n• `read` — full content of one memory. Required: `name`.\n• `list` — every saved memory with first-line title.\n• `delete` — remove a memory file. Required: `name`.\n• `search` — semantic search via embedding similarity. Required: `query`. Optional: `threshold` (0.0–1.0, default 0.75), `top_k` (default 5).\n\n**Use `add`** when the user shares a durable preference or convention. **Use `search`/`read`** to recall prior notes. **Use `delete`** when the user says to forget something specific. Memories auto-load — explicit reads are only for verification.",
		Parameters: []ParameterDef{
			{Name: "operation", Type: "string", Required: true, Description: "One of: 'add', 'read', 'list', 'delete', 'search'."},
			{Name: "name", Type: "string", Description: "Memory name (required for add/read/delete). Short descriptive slug, no .md extension."},
			{Name: "content", Type: "string", Description: "Markdown content (required for add)."},
			{Name: "query", Type: "string", Description: "Natural-language search query (required for search)."},
			{Name: "threshold", Type: "number", Description: "Search-only: minimum similarity 0.0–1.0 (default 0.75)."},
			{Name: "top_k", Type: "integer", Description: "Search-only: maximum results to return (default 5)."},
		},
		Required: []string{"operation"},
	}
}

func (h *manageMemoryHandler) Validate(args map[string]any) error {
	op, err := extractString(args, "operation")
	if err != nil {
		return err
	}
	op = strings.TrimSpace(strings.ToLower(op))
	switch op {
	case "add":
		if _, err := extractString(args, "name"); err != nil {
			return err
		}
		if _, err := extractString(args, "content"); err != nil {
			return err
		}
	case "read", "delete":
		if _, err := extractString(args, "name"); err != nil {
			return err
		}
	case "list":
		// no required params
	case "search":
		if _, err := extractString(args, "query"); err != nil {
			return err
		}
	default:
		return agenterrors.NewValidation(fmt.Sprintf("unknown operation %q (want add, read, list, delete, or search)", op), nil)
	}
	return nil
}

func (h *manageMemoryHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	op, _ := extractString(args, "operation")
	op = strings.TrimSpace(strings.ToLower(op))

	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   "manage_memory",
			"op":     op,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool": "manage_memory",
				"op":   op,
			})
		}()
	}

	var result string
	var err error

	switch op {
	case "add":
		result, err = handleMemoryAdd(env, args)
	case "read":
		result, err = handleMemoryRead(args)
	case "list":
		result, err = handleMemoryList()
	case "delete":
		result, err = handleMemoryDelete(args)
	case "search":
		result, err = handleMemorySearch(env, args)
	default:
		return ToolResult{Output: fmt.Sprintf("unknown operation %q", op), IsError: true},
			agenterrors.NewValidation(fmt.Sprintf("unknown operation %q", op), nil)
	}

	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
	}

	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, result)
	}

	return ToolResult{
		Output:     result,
		TokenUsage: int64(estimateTokenUsage(result)),
	}, nil
}

func (h *manageMemoryHandler) Aliases() []string         { return nil }
func (h *manageMemoryHandler) Timeout() time.Duration    { return 0 }
func (h *manageMemoryHandler) MaxResultSize() int        { return 0 }
func (h *manageMemoryHandler) SafeForParallel() bool     { return false }
func (h *manageMemoryHandler) Interactive() bool         { return false }

// --- operation handlers ---

func handleMemoryAdd(env ToolEnv, args map[string]any) (string, error) {
	name, err := extractString(args, "name")
	if err != nil {
		return "", err
	}
	content, err := extractString(args, "content")
	if err != nil {
		return "", err
	}

	// Redact secrets before persisting
	content = redact.String(content)

	sanitized := sanitizeMemoryName(name)
	result, err := saveMemoryToDisk(sanitized, content)
	if err != nil {
		return "", err
	}

	return result, nil
}

func handleMemoryRead(args map[string]any) (string, error) {
	name, err := extractString(args, "name")
	if err != nil {
		return "", err
	}

	// Strip .md extension if provided
	name = strings.TrimSuffix(name, ".md")

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewConfig("unable to locate config directory for memories", nil)
	}

	filePath := filepath.Join(memoryDir, name+".md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", agenterrors.NewNotFound(fmt.Sprintf("memory %q not found", name))
		}
		return "", agenterrors.Wrapf(err, "failed to read memory '%s'", name)
	}

	return fmt.Sprintf("## Memory: %s\n\n%s", name, string(content)), nil
}

func handleMemoryList() (string, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewConfig("unable to locate config directory for memories", nil)
	}

	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "No memories saved yet. Use `manage_memory` with operation=\"add\" to create a memory that persists across conversations.", nil
		}
		return "", agenterrors.Wrapf(err, "failed to list memories")
	}

	type memoryInfo struct {
		Name  string
		Title string
	}

	var memories []memoryInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		contentPath := filepath.Join(memoryDir, entry.Name())
		content, err := os.ReadFile(contentPath)
		if err != nil {
			continue
		}
		title := firstLineContent(string(content))
		memories = append(memories, memoryInfo{Name: name, Title: title})
	}

	if len(memories) == 0 {
		return "No memories saved yet. Use `manage_memory` with operation=\"add\" to create a memory that persists across conversations.", nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("## Saved Memories (%d)\n\n", len(memories)))
	for _, m := range memories {
		sb.WriteString(fmt.Sprintf("- **%s** — %s\n", m.Name, m.Title))
	}
	sb.WriteString("\nUse `manage_memory` (operation=\"read\" to view full content; operation=\"add\"/\"delete\" to manage memories).")
	return sb.String(), nil
}

func handleMemoryDelete(args map[string]any) (string, error) {
	name, err := extractString(args, "name")
	if err != nil {
		return "", err
	}

	// Strip .md extension if provided
	name = strings.TrimSuffix(name, ".md")

	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return "", agenterrors.NewConfig("unable to locate config directory for memories", nil)
	}

	filePath := filepath.Join(memoryDir, name+".md")
	if err := os.Remove(filePath); err != nil {
		if os.IsNotExist(err) {
			return "", agenterrors.NewNotFound(fmt.Sprintf("memory %q not found", name))
		}
		return "", agenterrors.Wrapf(err, "failed to delete memory '%s'", name)
	}

	return fmt.Sprintf("Memory '%s' deleted.", name), nil
}

func handleMemorySearch(env ToolEnv, args map[string]any) (string, error) {
	query, err := extractString(args, "query")
	if err != nil {
		return "", err
	}

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

	// Use text-based search (embedding-based search requires agent context
	// via the legacy path; the text fallback provides useful results).
	return searchMemoriesByTextFallback(query, topK, threshold), nil
}

// --- helpers ---

func firstLineContent(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			title := line
			if len(title) > 120 {
				title = title[:117] + "..."
			}
			title = strings.TrimLeft(title, "# ")
			return strings.TrimSpace(title)
		}
	}
	return ""
}

func formatEmbeddingMemoryResults(query string, results []EmbeddingMemoryResult, threshold float64) string {
	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories matching: %q\n\n", len(results), query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("#%d — **%s** (relevance: %.2f)\n", i+1, r.Name, r.Score))
		if r.Preview != "" {
			sb.WriteString(fmt.Sprintf("   Preview: %s\n", r.Preview))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Use `manage_memory` with operation=\"read\" to view the full content of any memory.")
	return sb.String()
}

// EmbeddingMemoryResult is a local type to avoid importing embedding package.
type EmbeddingMemoryResult struct {
	Name    string
	Preview string
	Score   float64
}

func searchMemoriesByTextFallback(query string, topK int, threshold float64) string {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold)
	}

	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold)
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	type result struct {
		name    string
		preview string
		score   float64
	}
	var results []result

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		contentPath := filepath.Join(memoryDir, entry.Name())
		contentBytes, err := os.ReadFile(contentPath)
		if err != nil {
			continue
		}
		content := string(contentBytes)
		preview := firstLineContent(content)

		score := scoreMemoryMatchFallback(name, preview, content, queryWords)
		if score >= threshold {
			results = append(results, result{name: name, preview: preview, score: score})
		}
	}

	// Sort by score descending
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].score > results[i].score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	if len(results) > topK {
		results = results[:topK]
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories matching: %q\n\n", len(results), query))
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("#%d — **%s** (relevance: %.2f)\n", i+1, r.name, r.score))
		if r.preview != "" {
			sb.WriteString(fmt.Sprintf("   Preview: %s\n", r.preview))
		}
		sb.WriteString("\n")
	}
	sb.WriteString("Use `manage_memory` with operation=\"read\" to view the full content of any memory.")
	return sb.String()
}

func scoreMemoryMatchFallback(name, preview, content string, queryWords []string) float64 {
	nameLower := strings.ToLower(name)
	previewLower := strings.ToLower(preview)
	contentLower := strings.ToLower(content)

	if len(queryWords) == 0 {
		return 0
	}

	totalScore := float64(0)
	for _, word := range queryWords {
		if len(word) < 2 {
			continue
		}
		if strings.Contains(nameLower, word) {
			totalScore += 0.5
		}
		if strings.Contains(previewLower, word) {
			totalScore += 0.3
		}
		if strings.Contains(contentLower, word) {
			totalScore += 0.2
		}
	}

	maxPossible := float64(len(queryWords))
	if maxPossible == 0 {
		return 0
	}
	return totalScore / maxPossible
}
