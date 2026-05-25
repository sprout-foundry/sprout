package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/embedding"
)

// handleEmbeddingIndex manages the embedding index for duplicate detection
// and semantic search. Operations: build, update, status.
func handleEmbeddingIndex(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	operation, ok := args["operation"].(string)
	if !ok || operation == "" {
		return "", fmt.Errorf("operation is required: must be one of 'build', 'update', 'status'")
	}

	em := a.GetEmbeddingManager()
	if em == nil {
		return "Embedding index is not enabled. Use the /index command to enable workspace indexing,\n" +
			"or click the index badge in the UI.", nil
	}

	switch operation {
	case "build":
		return handleEmbeddingIndexBuild(ctx, a, em)
	case "update":
		return handleEmbeddingIndexUpdate(ctx, a, em)
	case "status":
		return handleEmbeddingIndexStatus(a, em)
	default:
		return "", fmt.Errorf("unknown operation '%s'; must be 'build', 'update', or 'status'", operation)
	}
}

// handleEmbeddingIndexBuild starts an index build in a background goroutine
// so it does not block the HTTP handler. It returns immediately with a
// confirmation message and emits progress events via the event bus as the
// build proceeds.
func handleEmbeddingIndexBuild(ctx context.Context, a *Agent, em *embedding.EmbeddingManager) (string, error) {
	// Run the build in the background via the manager's non-blocking API.
	resultCh := em.BuildIndexBackground(ctx)

	// Publish a start event so the UI knows a build is underway.
	a.PublishToolExecution("embedding_index", "build_started", map[string]interface{}{
		"status":  "running",
		"message": "Index build started in background",
	})

	// Launch a goroutine to collect the result and report it without blocking
	// the HTTP response.
	go func() {
		result := <-resultCh
		if result.Err != nil {
			a.Logger().Error("embedding: index build failed: %v", result.Err)
			a.PublishToolExecution("embedding_index", "build_failed", map[string]interface{}{
				"error": result.Err.Error(),
			})
			return
		}

		stats := result.Stats
		a.PublishToolExecution("embedding_index", "build_completed", map[string]interface{}{
			"filesProcessed":  stats.FilesProcessed,
			"unitsExtracted":  stats.UnitsExtracted,
			"unitsEmbedded":   stats.UnitsEmbedded,
			"duration":        stats.Duration.String(),
			"durationSeconds": stats.Duration.Seconds(),
		})
	}()

	return "Index build started in background. Progress will be reported via events.", nil
}

func handleEmbeddingIndexUpdate(ctx context.Context, a *Agent, em *embedding.EmbeddingManager) (string, error) {
	stats, err := em.UpdateFromGitDiff(ctx)
	if err != nil {
		return "", fmt.Errorf("index update failed: %w", err)
	}

	var sb strings.Builder
	sb.WriteString("✅ Embedding index updated successfully.\n")
	sb.WriteString(fmt.Sprintf("  Files processed: %d\n", stats.FilesProcessed))
	sb.WriteString(fmt.Sprintf("  Duration: %s\n", stats.Duration.Round(time.Millisecond)))
	return sb.String(), nil
}

func handleEmbeddingIndexStatus(a *Agent, em *embedding.EmbeddingManager) (string, error) {
	cfg := a.GetConfig()
	enabled := false
	if cfg != nil && cfg.EmbeddingIndex != nil {
		enabled = cfg.EmbeddingIndex.Enabled
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Embedding manager initialized: %v\n", em.IsInitialized()))
	sb.WriteString(fmt.Sprintf("Embedding index enabled in config: %v\n", enabled))
	sb.WriteString(fmt.Sprintf("Index records: %d\n", em.IndexSize()))

	if em.IsInitialized() {
		sb.WriteString("\nIndex is ready for semantic search and duplicate detection.")
	} else if enabled {
		sb.WriteString("\nIndex is not yet initialized. Run with operation 'build' to create the index.")
	} else {
		sb.WriteString("\nEmbedding index is disabled in config.")
	}
	return sb.String(), nil
}

// handleSemanticSearch searches the codebase for semantically similar code
// using embedding vectors.
func handleSemanticSearch(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", fmt.Errorf("query is required: provide a natural language description of what you're looking for")
	}

	topK := 5
	if tk, ok := args["top_k"]; ok {
		switch v := tk.(type) {
		case int:
			topK = v
		case float64:
			topK = int(v)
		}
	}

	threshold := float32(0.75)
	if t, ok := args["threshold"]; ok {
		switch v := t.(type) {
		case float64:
			threshold = float32(v)
		case float32:
			threshold = v
		case int:
			threshold = float32(v)
		}
	}
	// Clamp threshold to [0.0, 1.0] (MUST_FIX #3).
	if threshold < 0 {
		threshold = 0
	} else if threshold > 1 {
		threshold = 1
	}

	em := a.GetEmbeddingManager()
	if em == nil {
		return "Embedding index is not enabled. Use the /index command to enable workspace indexing,\n" +
			"or click the index badge in the UI.", nil
	}

	results, err := em.QuerySimilar(ctx, query, topK, threshold)
	if err != nil {
		return "", fmt.Errorf("semantic search failed: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("No semantically similar code found for query: %q\n\nTry broadening your search query or lowering the threshold (currently %.2f).", query, threshold), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d result(s) similar to: %q\n\n", len(results), query))

	for i, r := range results {
		sb.WriteString(fmt.Sprintf("#%d — %s (similarity: %.4f)\n", i+1, r.Record.Name, r.Similarity))
		sb.WriteString(fmt.Sprintf("   File: %s\n", r.Record.File))
		sb.WriteString(fmt.Sprintf("   Lines: %d–%d\n", r.Record.StartLine, r.Record.EndLine))
		if r.Record.Signature != "" {
			sb.WriteString(fmt.Sprintf("   Signature: %s\n", r.Record.Signature))
		}
		sb.WriteString("\n")
	}

	return sb.String(), nil
}
