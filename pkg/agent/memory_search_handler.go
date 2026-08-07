package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/embedding"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
)

func handleSearchMemories(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	query, ok := args["query"].(string)
	if !ok || query == "" {
		return "", agenterrors.NewValidation("query is required: provide a natural language description of what you're looking for", nil)
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

	threshold := float32(embedding.DefaultConversationSearchThreshold)
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

	if threshold < 0 {
		threshold = 0
	} else if threshold > 1 {
		threshold = 1
	}

	em := a.GetEmbeddingManager()
	if em == nil {
		results, err := tools.SearchMemoriesByText(query, topK, float64(threshold))
		if err != nil {
			return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold), nil
		}
		return tools.FormatMemorySearchResults(query, results, float64(threshold)), nil
	}

	store, err := em.GetConversationStore(ctx)
	if err != nil {
		return "", agenterrors.Wrap(err, "failed to get conversation store")
	}

	results, err := store.QueryMemories(ctx, query, topK, threshold)
	if err != nil {
		return "", agenterrors.Wrap(err, "memory search failed")
	}

	if len(results) == 0 {
		return fmt.Sprintf("No memories found matching: %q\n\nTry broadening your search or lowering the threshold (currently %.2f).", query, threshold), nil
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Found %d memory/memories matching: %q\n\n", len(results), query))

	for i, r := range results {
		name := r.Record.Name
		similarity := r.Similarity

		var preview string
		if r.Record.Metadata != nil {
			if p, ok := r.Record.Metadata["content_preview"].(string); ok {
				preview = p
			}
		}

		sb.WriteString(fmt.Sprintf("#%d — **%s** (relevance: %.2f)\n", i+1, name, similarity))
		if preview != "" {
			displayPreview := preview
			if len(displayPreview) > 150 {
				displayPreview = displayPreview[:147] + "..."
			}
			sb.WriteString(fmt.Sprintf("   Preview: %s\n", displayPreview))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("Use `manage_memory` with operation=\"read\" to view the full content of any memory.")

	return sb.String(), nil
}

func handleSearchMemoriesJSON(ctx context.Context, a *Agent, query string, topK int, threshold float32) (string, error) {
	em := a.GetEmbeddingManager()
	if em == nil {
		results, err := tools.SearchMemoriesByText(query, topK, float64(threshold))
		if err != nil {
			return "[]", nil
		}
		type memoryResult struct {
			Name      string  `json:"name"`
			Relevance float32 `json:"relevance"`
			Title     string  `json:"title,omitempty"`
		}
		var output []memoryResult
		for _, r := range results {
			preview := r.Preview
			if len(preview) > 120 {
				preview = preview[:117] + "..."
			}
			output = append(output, memoryResult{
				Name:      r.Name,
				Relevance: float32(r.Score),
				Title:     preview,
			})
		}
		data, err := json.Marshal(output)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}

	store, err := em.GetConversationStore(ctx)
	if err != nil {
		return "", err
	}

	results, err := store.QueryMemories(ctx, query, topK, threshold)
	if err != nil {
		return "", err
	}

	type memoryResult struct {
		Name      string  `json:"name"`
		Relevance float32 `json:"relevance"`
		Title     string  `json:"title,omitempty"`
	}

	var output []memoryResult
	for _, r := range results {
		title := ""
		if r.Record.Metadata != nil {
			if p, ok := r.Record.Metadata["content_preview"].(string); ok {
				title = p
				if len(title) > 120 {
					title = title[:117] + "..."
				}
			}
		}
		output = append(output, memoryResult{
			Name:      r.Record.Name,
			Relevance: r.Similarity,
			Title:     title,
		})
	}

	data, err := json.Marshal(output)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
