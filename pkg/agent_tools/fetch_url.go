package tools

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/webcontent"
)

// FetchURL fetches content from a specific URL using the webcontent fetcher.
// This provides direct URL access as an agent tool.
func FetchURL(url string, cfg *configuration.Config) (string, error) {
	if url == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}

	fetcher := webcontent.NewWebContentFetcher()
	content, err := fetcher.FetchWebContent(url, cfg)
	if err != nil {
		return "", fmt.Errorf("failed to fetch URL %s: %w", url, err)
	}

	if content == "" {
		return fmt.Sprintf("No content found at URL: %s", url), nil
	}

	return content, nil
}
