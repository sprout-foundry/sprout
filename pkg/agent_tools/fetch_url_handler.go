// FetchURLHandler implements ToolHandler for the fetch_url tool.
//
// This is the new-style, registry-based handler that replaces the legacy
// switch-based dispatch in pkg/agent/tool_executor*.go.
package tools

import (
	"context"
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// FetchURLHandler implements ToolHandler for fetching and extracting content from URLs.
type FetchURLHandler struct{}

// NewFetchURLHandler returns a ready-to-use FetchURLHandler.
func NewFetchURLHandler() *FetchURLHandler {
	return &FetchURLHandler{}
}

// Name returns the tool name "fetch_url".
func (h *FetchURLHandler) Name() string {
	return "fetch_url"
}

// Definition returns the LLM-facing tool definition.
func (h *FetchURLHandler) Definition() api.Tool {
	return api.Tool{
		Type: "function",
		Function: struct {
			Name        string      `json:"name"`
			Description string      `json:"description"`
			Parameters  interface{} `json:"parameters"`
		}{
			Name:        "fetch_url",
			Description: "Fetch and extract content from a URL. For HTML/text content, extracts readable text. For images and PDFs (when the model supports vision), returns visual content directly.",
			Parameters: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"url": map[string]interface{}{
						"type":        "string",
						"description": "URL to fetch content from",
					},
				},
				"required":             []string{"url"},
				"additionalProperties": false,
			},
		},
	}
}

// Validate checks that the arguments are suitable for the fetch_url tool.
func (h *FetchURLHandler) Validate(args map[string]any) error {
	url, err := toString(args["url"], "url")
	if err != nil {
		return err
	}
	if url == "" {
		return fmt.Errorf("parameter 'url' is required")
	}
	return nil
}

// Execute fetches and extracts content from the specified URL.
func (h *FetchURLHandler) Execute(ctx context.Context, env *ToolEnv, args map[string]any) (*ToolResult, error) {
	url, _ := args["url"].(string)

	content, err := FetchURL(url, env.ConfigManager)
	if err != nil {
		return &ToolResult{ErrorMessage: err.Error()}, err
	}

	return &ToolResult{Output: content}, nil
}
