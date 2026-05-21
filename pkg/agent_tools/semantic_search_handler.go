package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type semanticSearchHandler struct{}

func (h *semanticSearchHandler) Name() string { return "semantic_search" }

func (h *semanticSearchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "semantic_search",
		Description: "Search the codebase for semantically similar code using embedding vectors. Unlike text search, this finds code that does the same thing even with different names or implementations.",
		Required: []string{"query"},
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
	if env.EventBus != nil {
		env.EventBus.Publish(events.EventTypeToolStart, map[string]any{
			"tool":   toolName,
			"params": args,
		})
		defer func() {
			env.EventBus.Publish(events.EventTypeToolEnd, map[string]any{
				"tool":  toolName,
				"error": true,
			})
		}()
	}

	// TODO: Full implementation requires *Agent access for GetEmbeddingManager()
	// and embedding index access. This is a thin wrapper stub.
	return ToolResult{
		Output:  "semantic_search requires full *Agent refactoring for complete functionality. This handler cannot search embeddings without access to the Agent's embedding manager. Please use the legacy interface or complete the migration.",
		IsError: true,
	}, fmt.Errorf("semantic_search requires full *Agent refactoring")
}
