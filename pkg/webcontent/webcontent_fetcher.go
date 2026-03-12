package webcontent

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration" // Updated to new config
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	// Max content size in bytes (1MB) - larger content gets truncated with warning
	maxContentSize = 1024 * 1024
	// Truncated suffix to append when content exceeds limit
	truncatedSuffix = "\n\n[CONTENT TRUNCATED: Original was larger than 1MB. Only first 1MB returned.]"
)

// WebContentFetcher handles fetching content from URLs.
type WebContentFetcher struct {
	httpClient *http.Client
}

// NewWebContentFetcher creates a new WebContentFetcher instance.
func NewWebContentFetcher() *WebContentFetcher {
	return &WebContentFetcher{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// FetchWebContent fetches content from a given URL, using a cache to avoid refetching.
// It uses Jina Reader for external URLs if available, otherwise falls back to a direct HTTP GET.
func (w *WebContentFetcher) FetchWebContent(url string, cfg *configuration.Manager) (string, error) { // Use Manager instead of Config
	utils.GetLogger(false).LogProcessStep(fmt.Sprintf("Starting web content search for query: %s", url))
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
		utils.GetLogger(false).LogError(err)
	}

	return content, nil
}

// fetchContent determines the best method to fetch content and retrieves it.
func (w *WebContentFetcher) fetchContent(url string, cfg *configuration.Manager) (string, error) { // Use Manager instead of Config
	isLocalhost := strings.HasPrefix(url, "http://localhost") || strings.HasPrefix(url, "https://localhost")
	jinaAPIKey := cfg.GetAPIKeys().GetAPIKey("jinaai")

	// Check if this is a URL type that should bypass Jina (JSON, APIs, static assets)
	if w.shouldBypassJina(url) {
		return w.fetchDirectURL(url)
	}

	useJina := !isLocalhost && jinaAPIKey != ""
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
		utils.GetLogger(false).Logf("Jina AI API key not found or provided. Jina Reader will not be used for URL: %s. Falling back to direct HTTP GET.", url)
	}
	return w.fetchDirectURL(url)
}

// shouldBypassJina checks if the URL should bypass Jina Reader and use direct fetch.
// This is important for JSON files, APIs, static assets, and other non-HTML content.
func (w *WebContentFetcher) shouldBypassJina(urlStr string) bool {
	parsedURL, err := url.Parse(urlStr)
	if err != nil {
		// If we can't parse the URL, fall back to direct fetch to be safe
		return true
	}

	lowerPath := strings.ToLower(parsedURL.Path)
	lowerRawQuery := strings.ToLower(parsedURL.RawQuery)
	lowerURL := lowerPath + lowerRawQuery

	// List of file extensions and patterns that should bypass Jina
	bypassPatterns := []string{
		// JSON and data files
		".json",
		".yaml",
		".yml",
		".xml",
		".csv",
		// API endpoints (often return JSON)
		"/api/",
		// Static assets that don't need markdown conversion
		".css",
		".js",
		".map",       // source maps
		".ico",
		".png",
		".jpg",
		".jpeg",
		".gif",
		".svg",
		".webp",
		".pdf",
		".zip",
		".tar",
		".gz",
	}

	for _, pattern := range bypassPatterns {
		if strings.Contains(lowerURL, pattern) {
			return true
		}
	}

	// Additional check: look for query params that indicate JSON/API
	if strings.Contains(lowerRawQuery, "format=json") ||
		strings.Contains(lowerRawQuery, "format=xml") ||
		strings.Contains(lowerRawQuery, "?json") ||
		strings.Contains(lowerRawQuery, "&json") ||
		strings.Contains(lowerRawQuery, "?xml") ||
		strings.Contains(lowerRawQuery, "&xml") {
		return true
	}

	return false
}

// fetchWithJinaReader fetches content using the Jina Reader API.
func (w *WebContentFetcher) fetchWithJinaReader(url string, cfg *configuration.Manager) (string, error) { // Use Manager instead of Config
	apiKey := cfg.GetAPIKeys().GetAPIKey("jinaai")
	if apiKey == "" {
		return "", fmt.Errorf("JinaAI API key not found")
	}

	req, err := createJinaRequest(url, apiKey)
	if err != nil {
		return "", err
	}

	resp, err := w.httpClient.Do(req)
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

// truncateContent truncates content if it exceeds max size and adds a warning.
func (w *WebContentFetcher) truncateContent(content string) (string, error) {
	contentLen := len(content)
	if contentLen <= maxContentSize {
		return content, nil
	}

	// Truncate and add warning
	truncated := content[:maxContentSize]
	return fmt.Sprintf("%s%s (original: %.1f MB)", truncated, truncatedSuffix, float64(contentLen)/(1024*1024)), nil
}

// fetchDirectURL performs a direct HTTP GET request to the given URL.
func (w *WebContentFetcher) fetchDirectURL(url string) (string, error) {
	resp, err := w.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		_, _ = io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d for URL: %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}

	// Truncate if content is too large
	truncated, err := w.truncateContent(string(body))
	if err != nil {
		return "", err
	}
	return truncated, nil
}
