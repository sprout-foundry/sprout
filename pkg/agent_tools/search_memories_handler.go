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
)

// searchMemoriesHandler implements ToolHandler for the search_memories tool.
//
// Because ToolEnv does not carry an EmbeddingManager, this handler implements
// a text-based fallback: it lists all memory files and filters them by
// matching the query against each memory's name and first-line preview.
//
// When the ToolHandler path eventually gains embedding support (e.g. via a
// dedicated EmbeddingAccessor interface in ToolEnv), this can be extended to
// also run vector search.
type searchMemoriesHandler struct{}

func (h *searchMemoriesHandler) Name() string {
	return "search_memories"
}

func (h *searchMemoriesHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "search_memories",
		Description: "Search saved memories by name and content preview. Lists all memories and filters by text matching against the query. For semantic (vector) search, use the embedding_index tool to build the index first, then call search_memories through the agent.",
		Parameters: []ParameterDef{
			{
				Name:        "query",
				Type:        "string",
				Required:    true,
				Description: "Natural language description of what you're looking for",
			},
			{
				Name:        "top_k",
				Type:        "integer",
				Required:    false,
				Description: "Maximum number of results to return (default: 5)",
			},
			{
				Name:        "threshold",
				Type:        "number",
				Required:    false,
				Description: "Minimum relevance score 0.0-1.0 (default: 0.75). Lower values return more results.",
			},
		},
		Required: []string{"query"},
	}
}

func (h *searchMemoriesHandler) Validate(args map[string]any) error {
	query, err := extractString(args, "query")
	if err != nil {
		return err
	}
	if strings.TrimSpace(query) == "" {
		return agenterrors.NewValidation("parameter 'query' must not be empty", nil)
	}

	// Validate top_k if provided
	if tkRaw, exists := args["top_k"]; exists && tkRaw != nil {
		switch tkRaw.(type) {
		case int, float64:
			// Valid
		default:
			return agenterrors.NewValidation(fmt.Sprintf("parameter 'top_k' must be an integer, got %T", tkRaw), nil)
		}
	}

	// Validate threshold if provided
	if tRaw, exists := args["threshold"]; exists && tRaw != nil {
		switch tRaw.(type) {
		case float64, float32, int:
			// Valid
		default:
			return agenterrors.NewValidation(fmt.Sprintf("parameter 'threshold' must be a number, got %T", tRaw), nil)
		}
	}

	return nil
}

func (h *searchMemoriesHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
	query, err := extractString(args, "query")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, err
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

	// Perform text-based memory search
	results, err := searchMemoriesByText(query, topK, threshold)
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("memory search failed: %v", err),
			IsError: true,
		}, agenterrors.NewTool("search_memories", fmt.Sprintf("search memories: %v", err), err)
	}

	output := formatMemorySearchResults(query, results, threshold)

	// Write to output writer if available
	if env.OutputWriter != nil {
		io.WriteString(env.OutputWriter, output)
	}

	return ToolResult{
		Output:     output,
		TokenUsage: int64(estimateTokenUsage(output)),
	}, nil
}

func (h *searchMemoriesHandler) Aliases() []string      { return nil }
func (h *searchMemoriesHandler) Timeout() time.Duration { return 0 }
func (h *searchMemoriesHandler) MaxResultSize() int     { return 0 }
func (h *searchMemoriesHandler) SafeForParallel() bool  { return false }
func (h *searchMemoriesHandler) Interactive() bool      { return false }

// memorySearchResult holds a single result from a text-based memory search.
type memorySearchResult struct {
	Name    string
	Preview string
	Score   float64
	Content string
}

// searchMemoriesByText lists all memory files and scores them against the query
// using simple text matching. This is a fallback when no embedding index is available.
func searchMemoriesByText(query string, topK int, threshold float64) ([]memorySearchResult, error) {
	memoryDir := getMemoryDir()
	if memoryDir == "" {
		return nil, nil // No memory directory = no results, not an error
	}

	entries, err := os.ReadDir(memoryDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, agenterrors.NewTool("search_memories", fmt.Sprintf("read memories directory: %v", err), err)
	}

	queryLower := strings.ToLower(query)
	queryWords := strings.Fields(queryLower)

	var results []memorySearchResult

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
		content := string(contentBytes)

		// Get preview (first line or first 120 chars)
		preview := firstLine(content)
		if len(preview) > 120 {
			preview = preview[:117] + "..."
		}

		// Score based on name match + content match
		score := scoreMemoryMatch(name, preview, content, queryWords)

		if score >= threshold {
			results = append(results, memorySearchResult{
				Name:    name,
				Preview: preview,
				Score:   score,
				Content: content,
			})
		}
	}

	// Sort by score descending (simple bubble sort for small lists)
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Limit to topK
	if len(results) > topK {
		results = results[:topK]
	}

	return results, nil
}

// scoreMemoryMatch computes a relevance score (0.0-1.0) for a memory against the query.
func scoreMemoryMatch(name, preview, content string, queryWords []string) float64 {
	nameLower := strings.ToLower(name)
	previewLower := strings.ToLower(preview)
	contentLower := strings.ToLower(content)

	if len(queryWords) == 0 {
		return 0
	}

	matches := 0
	totalScore := float64(0)

	for _, word := range queryWords {
		if len(word) < 2 {
			continue
		}

		// Name match is weighted highest
		if strings.Contains(nameLower, word) {
			matches++
			totalScore += 0.5 // Name matches are very valuable
		}

		// Preview match is next
		if strings.Contains(previewLower, word) {
			matches++
			totalScore += 0.3
		}

		// Content match is lowest weight
		if strings.Contains(contentLower, word) {
			matches++
			totalScore += 0.2
		}
	}

	if len(queryWords) == 0 {
		return 0
	}

	// Normalize to 0-1 range
	// Maximum possible score per word is 1.0 (name + preview + content match)
	maxPossible := float64(len(queryWords))
	if maxPossible == 0 {
		return 0
	}

	score := totalScore / maxPossible
	return score
}

// firstLine extracts the first non-empty line from content.
func firstLine(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
	}
	return ""
}

// formatMemorySearchResults formats search results for display.
func formatMemorySearchResults(query string, results []memorySearchResult, threshold float64) string {
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
