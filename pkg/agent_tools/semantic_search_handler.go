package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/embedding"
	"github.com/sprout-foundry/sprout/pkg/events"
)

type semanticSearchHandler struct{}

func (h *semanticSearchHandler) Name() string { return "semantic_search" }

func (h *semanticSearchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "semantic_search",
		Description: "Search the codebase for semantically similar code using embedding vectors. Unlike text search, this finds code that does the same thing even with different names or implementations.",
		Required:    []string{"query"},
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Required: true, Description: "Natural language description of what you're looking for"},
			{Name: "threshold", Type: "number", Description: "Minimum similarity score 0.0-1.0 (default: 0.75)"},
			{Name: "top_k", Type: "integer", Description: "Maximum results to return (default: 5)"},
		},
	}
}

func (h *semanticSearchHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "query")
	return err
}

func (h *semanticSearchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	// Extract query (required)
	query, err := extractString(args, "query")
	if err != nil {
		hadError = true
		return ToolResult{
			Output:  err.Error(),
			IsError: true,
		}, err
	}

	// Extract optional top_k (default: 5)
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

	// Extract optional threshold (default: 0.75)
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

	// Prefer the agent's long-lived embedding manager. It holds the loaded
	// ONNX model and an open HNSW handle; constructing a fresh one per call
	// re-downloads the model on first use, double-opens the HNSW store, and
	// can race the writer in the agent. Only fall back to a transient
	// manager when running outside an agent context (CLI tools, tests).
	mgr := env.EmbeddingMgr
	ownsMgr := false
	if mgr == nil {
		var cfg *configuration.Config
		if env.ConfigManager != nil {
			cfg = env.ConfigManager.GetConfig()
		} else {
			cfgMgr, err := configuration.NewManager()
			if err != nil {
				hadError = true
				return ToolResult{
					Output:  fmt.Sprintf("Error getting configuration: %v", err),
					IsError: true,
				}, nil
			}
			cfg = cfgMgr.GetConfig()
		}

		workspaceRoot := env.WorkspaceRoot
		if workspaceRoot == "" {
			workspaceRoot = "."
		}

		embeddingCfg := cfg.EmbeddingIndex
		if embeddingCfg == nil {
			embeddingCfg = &configuration.EmbeddingIndexConfig{}
		}

		mgr = embedding.NewEmbeddingManager(embeddingCfg, workspaceRoot)
		ownsMgr = true
	}
	if ownsMgr {
		defer mgr.Close()
	}

	if err := mgr.Init(ctx); err != nil {
		hadError = true
		return ToolResult{
			Output:  fmt.Sprintf("Semantic search unavailable: %v\n\nThe embedding index could not be initialized. This is usually because the ONNX runtime is not available in this build, or the model has not been downloaded yet. Run `embedding_index operation=status` to check the current state.", err),
			IsError: true,
		}, nil
	}

	results, err := mgr.QuerySimilar(ctx, query, topK, float32(threshold))
	if err != nil {
		hadError = true
		return ToolResult{
			Output:  fmt.Sprintf("Error searching embeddings: %v", err),
			IsError: true,
		}, nil
	}

	output := formatEmbeddingSearchResults(query, results, threshold)

	if env.OutputWriter != nil {
		_, _ = env.OutputWriter.Write([]byte(output))
	}

	return ToolResult{
		Output:     output,
		TokenUsage: int64(estimateTokenUsage(output)),
	}, nil
}

func (h *semanticSearchHandler) Aliases() []string         { return nil }
func (h *semanticSearchHandler) Timeout() time.Duration    { return 0 }
func (h *semanticSearchHandler) MaxResultSize() int        { return 0 }
func (h *semanticSearchHandler) SafeForParallel() bool     { return false }
func (h *semanticSearchHandler) Interactive() bool         { return false }

// formatEmbeddingSearchResults formats QueryResult entries into readable output.
func formatEmbeddingSearchResults(query string, results []embedding.QueryResult, threshold float64) string {
	if len(results) == 0 {
		return fmt.Sprintf("No results found matching: %q (threshold: %.2f)\n\nTry broadening your search query or lowering the threshold.", query, threshold)
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d result(s) matching: %q (threshold: %.2f)\n\n", len(results), query, threshold))

	for i, r := range results {
		rec := r.Record

		sb.WriteString(fmt.Sprintf("#%d — **%s** in `%s` (score: %.4f)\n", i+1, rec.Name, rec.File, r.Similarity))

		if rec.Language != "" {
			sb.WriteString(fmt.Sprintf("    Language: %s", rec.Language))
			if rec.StartLine > 0 {
				sb.WriteString(fmt.Sprintf(", lines %d-%d", rec.StartLine, rec.EndLine))
			}
			sb.WriteString("\n")
		}

		if rec.Signature != "" {
			// Truncate signature for display
			sig := rec.Signature
			if len(sig) > 200 {
				sig = sig[:197] + "..."
			}
			sb.WriteString(fmt.Sprintf("    Signature: %s\n", sig))
		}

		// Show relative path if possible
		if !filepath.IsAbs(rec.File) {
			sb.WriteString(fmt.Sprintf("    Path: %s\n", rec.File))
		}

		sb.WriteString("\n")
	}

	sb.WriteString(fmt.Sprintf("Use `read_file` to view the full content of any result."))
	return sb.String()
}
