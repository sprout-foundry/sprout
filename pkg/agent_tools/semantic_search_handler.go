package tools

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

type semanticSearchHandler struct{}

func (h *semanticSearchHandler) Name() string { return "semantic_search" }

func (h *semanticSearchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "semantic_search",
		Description: "Search the codebase for semantically similar code using embedding vectors. Unlike text search, this finds code that does the same thing even with different names or implementations.",
		Hidden:      true, // superseded by `search`; still callable by name
		// RequiresEmbeddings: without an embedding index this tool has nothing
		// to search — the registration path drops it entirely (the model never
		// sees it), and the nil-manager early return below keeps direct calls
		// from a replayed session silent instead of reporting the feature off.
		RequiresEmbeddings: true,
		Required:           []string{"query"},
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Required: true, Description: "Natural language description of what you're looking for"},
			{Name: "threshold", Type: "number", Description: "Minimum similarity score 0.0-1.0 (default: 0.4)"},
			{Name: "top_k", Type: "integer", Description: "Maximum results to return (default: 5)"},
		},
	}
}

func (h *semanticSearchHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "query")
	return err
}

func (h *semanticSearchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {

	// Extract query (required)
	query, err := extractString(args, "query")
	if err != nil {
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

	// Threshold default — resolved after the manager is acquired below, so
	// the right gate is used for the active provider (Jina 0.30, Gemma 0.40).
	var threshold float64
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
	//
	// Without ANY manager, fall back to a literal search. The registration
	// path drops this tool when the agent has no EmbeddingManager, so the
	// model never calls it — this branch guards direct calls from replayed
	// sessions or saved automations. Returning empty results for a query the
	// literal pass could answer is worse than answering it: the caller has no
	// way to tell "nothing matched" from "nothing was searched."
	mgr := env.EmbeddingMgr
	if mgr == nil {
		return literalFallbackSemantic(ctx, env, query), nil
	}

	if err := mgr.Init(ctx); err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Semantic search unavailable: %v\n\nThe embedding index could not be initialized. This is usually because the ONNX runtime is not available in this build, or the model has not been downloaded yet. Run `embedding_index operation=status` to check the current state.", err),
			IsError: true,
		}, nil
	}

	// Resolve the default threshold based on the active provider.
	threshold = float64(mgr.SemanticSearchThreshold())

	// Gate on the index actually holding data. Without this, an unbuilt or
	// still-building index returns zero hits and the formatter reports "No
	// results found ... try broadening your search" — a statement about the
	// codebase, when the truth is that nothing has been searched. The agent
	// acts on that as evidence the code does not exist.
	if r := mgr.Readiness(); !r.CanAnswerQueries() {
		switch {
		case r.Building:
			return ToolResult{
				Output: fmt.Sprintf("The embedding index is still building (%d records so far), so semantic search cannot answer yet.\n\n"+
					"This is not a statement about the codebase — nothing has been searched. Retry shortly, or use search_files for a literal match in the meantime.", r.Records),
			}, nil
		default:
			return ToolResult{
				Output: "The embedding index has not been built for this workspace, so semantic search cannot answer.\n\n" +
					"This is not a statement about the codebase — nothing has been searched. Enable indexing with the /index command, or use search_files for a literal match.",
			}, nil
		}
	}

	results, err := mgr.QuerySimilar(ctx, query, topK, float32(threshold))
	if err != nil {
		return ToolResult{
			Output:  fmt.Sprintf("Error searching embeddings: %v", err),
			IsError: true,
		}, nil
	}

	output := formatEmbeddingSearchResults(query, results, threshold)
	if r := mgr.Readiness(); r.Building {
		output = fmt.Sprintf("Note: the embedding index is still building (%d records indexed so far); these results are incomplete.\n\n%s", r.Records, output)
	}

	if env.OutputWriter != nil {
		_, _ = env.OutputWriter.Write([]byte(output))
	}

	return ToolResult{
		Output:     output,
		TokenUsage: int64(estimateTokenUsage(output)),
	}, nil
}

func (h *semanticSearchHandler) Aliases() []string      { return nil }
func (h *semanticSearchHandler) Timeout() time.Duration { return 0 }
func (h *semanticSearchHandler) MaxResultSize() int     { return 0 }
func (h *semanticSearchHandler) SafeForParallel() bool  { return false }
func (h *semanticSearchHandler) Interactive() bool      { return false }

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
