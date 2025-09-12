package tools

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

// WebSearch performs a web search and returns relevant content from selected URLs.
// This replaces the need for the webcontent system to be called directly from context builders.
func WebSearch(query string, cfg *config.Config) (string, error) {
	if query == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	// Use the existing webcontent system for the actual search
	content, err := webcontent.FetchContextFromSearch(query, cfg)
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if content == "" {
		return "No relevant content found for the search query.", nil
	}

	return fmt.Sprintf("Web search results for query: %s\n\n%s", query, content), nil
}