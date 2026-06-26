package tools

import (
	"context"
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/events"
)

type webSearchHandler struct{}

func (h *webSearchHandler) Name() string { return "web_search" }

func (h *webSearchHandler) Definition() ToolDefinition {
	return ToolDefinition{
		Name:        "web_search",
		Description: "Search web for relevant URLs",
		Required:    []string{"query"},
		Parameters: []ParameterDef{
			{Name: "query", Type: "string", Required: true, Description: "Search query to find relevant web content"},
		},
	}
}

func (h *webSearchHandler) Validate(args map[string]any) error {
	_, err := extractString(args, "query")
	return err
}

func (h *webSearchHandler) Execute(ctx context.Context, env ToolEnv, args map[string]any) (ToolResult, error) {
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

	// TODO: Full implementation requires *Agent access for GetSearchEngine() which
	// needs Google Custom Search API key from config. This is a thin wrapper stub.
	return ToolResult{
		Output:  "web_search requires full *Agent refactoring for complete functionality. This handler cannot perform web searches without access to the Agent's search engine (Google Custom Search API key). Please use the legacy interface or complete the migration.",
		IsError: true,
	}, fmt.Errorf("web_search requires full *Agent refactoring")
}
