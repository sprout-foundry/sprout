package tools

import (
	"context"
	"time"
)

// webSearchHandler performs web searches via the SearchEngine dependency.
// It returns formatted search results — it does NOT replicate the legacy
// handler's captureWebText side-effect (that remains in the legacy path).
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
	query, err := extractString(args, "query")
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	if env.SearchEngine == nil {
		return ToolResult{Output: "search engine not available: SearchEngine is not configured", IsError: true}, nil
	}

	result, err := env.SearchEngine.Search(ctx, query)
	if err != nil {
		return ToolResult{Output: err.Error(), IsError: true}, nil
	}

	return ToolResult{Output: result}, nil
}

func (h *webSearchHandler) Aliases() []string      { return nil }
func (h *webSearchHandler) Timeout() time.Duration { return 0 }
func (h *webSearchHandler) MaxResultSize() int     { return 0 }
func (h *webSearchHandler) SafeForParallel() bool  { return false }
func (h *webSearchHandler) Interactive() bool      { return false }
