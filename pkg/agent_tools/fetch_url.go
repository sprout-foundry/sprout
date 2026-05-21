package tools

import (
	"fmt"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/webcontent"
)

// FetchURL fetches content from a specific URL using the webcontent fetcher.
// This provides direct URL access as an agent tool.
func FetchURL(url string, cfg *configuration.Manager) (string, error) {
	if url == "" {
		return "", fmt.Errorf("URL cannot be empty")
	}

	if cfg == nil {
		newCfg, err := configuration.NewManagerSilent()
		if err != nil {
			newCfg = configuration.NewManagerWithConfig(nil, nil)
		}
		cfg = newCfg
	}

	fetcher := webcontent.NewWebContentFetcher()
	content, err := fetcher.FetchWebContent(url, cfg)
	if err != nil {
		return "", fmt.Errorf("fetch URL %s: %w", url, err)
	}

	if content == "" {
		return fmt.Sprintf("No content found at URL: %s", url), nil
	}

	return content, nil
}
