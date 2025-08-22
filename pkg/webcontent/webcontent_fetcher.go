package webcontent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/alantheprice/ledit/pkg/apikeys" // Changed import from pkg/llm to pkg/apikeys
	"github.com/alantheprice/ledit/pkg/config"  // Added import for config
	"github.com/alantheprice/ledit/pkg/utils"
)

// WebContentFetcher handles fetching content from URLs.
type WebContentFetcher struct{}

// NewWebContentFetcher creates a new WebContentFetcher instance.
func NewWebContentFetcher() *WebContentFetcher {
	return &WebContentFetcher{}
}

// FetchWebContent fetches content from a given URL, using a cache to avoid refetching.
// It uses Jina Reader for external URLs if available, otherwise falls back to a direct HTTP GET.
func (w *WebContentFetcher) FetchWebContent(url string, cfg *config.Config) (string, error) { // Added cfg parameter
	utils.GetLogger(cfg.SkipPrompt).LogProcessStep(fmt.Sprintf("Starting web content search for query: %s", url))
	// Check cache first
	if cachedEntry, found := w.loadURLCache(url); found {
		return cachedEntry.Content, nil
	}

	content, err := w.fetchContent(url, cfg) // Pass cfg
	if err != nil {
		return "", err
	}

	if err := w.saveURLCache(url, content); err != nil {
		// Log warning but don't fail the operation
		fmt.Printf("Warning: Failed to cache content for URL %s: %v\n", url, err)
	}

	return content, nil
}

// fetchContent determines the best method to fetch content and retrieves it.
func (w *WebContentFetcher) fetchContent(url string, cfg *config.Config) (string, error) { // Added cfg parameter
	isLocalhost := strings.HasPrefix(url, "http://localhost") || strings.HasPrefix(url, "https://localhost")
	jinaAPIKey, err := apikeys.GetAPIKey("JinaAI", !cfg.SkipPrompt) // Changed call to apikeys.GetAPIKey and passed cfg

	useJina := !isLocalhost && err == nil && jinaAPIKey != ""
	if useJina {
		content, err := w.fetchWithJinaReader(url, cfg) // Pass cfg
		if err != nil {
			return "", fmt.Errorf("failed to fetch with Jina Reader: %w", err)
		}
		return fmt.Sprintf("\n--- Content from URL: %s ---\n\n%s\n--- End of content from URL: %s ---\n", url, content, url), nil
	}

	// Fallback to direct fetch for localhost or if Jina is not configured.
	if !isLocalhost {
		// Get your Jina AI API key for free: https://jina.ai/?sui=apikey
		fmt.Printf("Warning: Jina AI API key not found or provided. Jina Reader will not be used for URL: %s. Falling back to direct HTTP GET. Error: %v\n", url, err)
	}
	return w.fetchDirectURL(url)
}

// fetchWithJinaReader fetches content using the Jina Reader API.
func (w *WebContentFetcher) fetchWithJinaReader(url string, cfg *config.Config) (string, error) { // Added cfg parameter
	apiKey, err := apikeys.GetAPIKey("JinaAI", !cfg.SkipPrompt) // Get API key here, passing cfg
	if err != nil {
		return "", fmt.Errorf("failed to get Jina AI API key: %w", err)
	}

	req, err := createJinaRequest(url, apiKey)
	if err != nil {
		return "", err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to make request to Jina Reader: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("jina Reader API returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	return parseJinaResponse(resp.Body)
}

// createJinaRequest creates an HTTP request for the Jina Reader API.
func createJinaRequest(url, apiKey string) (*http.Request, error) {
	const jinaURL = "https://r.jina.ai/"

	requestBody := map[string]string{"url": url}
	data, err := json.Marshal(requestBody)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal Jina request body: %w", err)
	}

	req, err := http.NewRequest("POST", jinaURL, bytes.NewBuffer(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create request for Jina Reader: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")
	// req.Header.Set("X-Engine", "browser")
	req.Header.Set("X-Return-Format", "markdown")
	// req.Header.Set("X-Respond-With", "readerlm-v2") // Use readerlm-v2 for better quality content

	return req, nil
}

// parseJinaResponse decodes the Jina Reader API response and extracts the content.
func parseJinaResponse(body io.Reader) (string, error) {
	var jinaResponse struct {
		Data struct {
			Content string `json:"content"`
		} `json:"data"`
	}
	if err := json.NewDecoder(body).Decode(&jinaResponse); err != nil {
		return "", fmt.Errorf("failed to decode Jina Reader response: %w", err)
	}

	if jinaResponse.Data.Content == "" {
		return "", fmt.Errorf("jina Reader response did not contain expected 'data.content'")
	}

	return jinaResponse.Data.Content, nil
}

// fetchDirectURL performs a direct HTTP GET request to the given URL.
func (w *WebContentFetcher) fetchDirectURL(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
