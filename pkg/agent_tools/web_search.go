package tools

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

// WebSearch performs a web search and returns raw search results.
// The agent can then decide which URLs to fetch and process.
func WebSearch(query string, cfg *config.Config) (string, error) {
	if query == "" {
		return "", fmt.Errorf("search query cannot be empty")
	}

	// Get raw search results without LLM processing
	searchResults, err := webcontent.GetSearchResults(query, cfg)
	if err != nil {
		return "", fmt.Errorf("web search failed: %w", err)
	}

	if len(searchResults) == 0 {
		return "No search results found.", nil
	}

	// Format search results for the agent to process
	var results strings.Builder
	results.WriteString(fmt.Sprintf("Web search results for \"%s\":\n\n", query))
	
	for i, result := range searchResults {
		results.WriteString(fmt.Sprintf("%d. **%s**\n", i+1, result.Title))
		results.WriteString(fmt.Sprintf("   URL: %s\n", result.URL))
		if result.Description != "" {
			results.WriteString(fmt.Sprintf("   Description: %s\n", result.Description))
		}
		results.WriteString("\n")
	}
	
	results.WriteString(fmt.Sprintf("Found %d results. Use fetch_url to get content from specific URLs.", len(searchResults)))
	return results.String(), nil
}