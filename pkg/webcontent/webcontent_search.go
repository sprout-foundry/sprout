package webcontent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

const jinaSearchURL = "https://s.jina.ai/search"

// GetSearchResults performs a web search and returns search results
// It tries Jina AI first, then falls back to DuckDuckGo if Jina is not available
func GetSearchResults(query string, cfg *configuration.Manager) ([]SearchResult, error) {
	fetcher := NewWebContentFetcher()
	logger := utils.GetLogger(false)
	startTime := time.Now()
	defer func() {
		logger.Logf("Jina search results fetch completed in %v", time.Since(startTime))
	}()

	logger.LogProcessStep("Checking for cached search results")
	if cachedEntry, err := fetcher.loadReferenceCache(query); err == nil {
		logger.Logf("Using cached search results (age: %v)", time.Since(cachedEntry.Timestamp))
		return cachedEntry.SearchResults, nil
	} else {
		logger.Logf("Cache check result: %v", err)
	}

	// Get Jina API Key from configuration
	jinaAPIKey := cfg.GetAPIKeys().GetAPIKey("jinaai")

	// Try Jina AI search first if API key is available
	if jinaAPIKey != "" {
		logger.Log("Using Jina API key for search")
		results, err := performJinaSearch(query, jinaAPIKey, logger)
		if err == nil {
			// Cache the results
			cacheEntry := &ReferenceCacheEntry{
				Query:         query,
				SearchResults: results,
				Timestamp:     time.Now(),
			}
			if err := fetcher.saveReferenceCache(query, cacheEntry); err != nil {
				logger.Logf("Failed to save reference cache: %v", err)
			}
			return results, nil
		}
		logger.Logf("Jina search failed, falling back to DuckDuckGo: %v", err)
	}

	// Fallback to DuckDuckGo
	logger.Log("Jina API key not found or search failed. Using DuckDuckGo fallback.")
	results, err := performDuckDuckGoSearch(query, logger)
	if err != nil {
		return nil, fmt.Errorf("both Jina and DuckDuckGo searches failed: %w", err)
	}

	// Cache the results
	cacheEntry := &ReferenceCacheEntry{
		Query:         query,
		SearchResults: results,
		Timestamp:     time.Now(),
	}
	if err := fetcher.saveReferenceCache(query, cacheEntry); err != nil {
		logger.Logf("Failed to save reference cache: %v", err)
	}

	return results, nil
}

// performJinaSearch performs a search using Jina AI API
func performJinaSearch(query, apiKey string, logger *utils.Logger) ([]SearchResult, error) {
	logger.LogProcessStep(fmt.Sprintf("Performing Jina AI search for query: %s", query))
	req, err := http.NewRequest("GET", jinaSearchURL, nil)
	if err != nil {
		logger.Logf("Failed to create Jina request: %v", err)
		return nil, fmt.Errorf("failed to create jina request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+apiKey)
	}
	q := req.URL.Query()
	q.Add("q", query)
	req.URL.RawQuery = q.Encode()

	// Increase the timeout for search grounding
	client := &http.Client{Timeout: 120 * time.Second}
	logger.Logf("Making HTTP request to Jina API: %s", req.URL.String())
	resp, err := client.Do(req)
	if err != nil {
		logger.Logf("Jina search request failed: %v", err)
		return nil, fmt.Errorf("failed to perform jina search: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Logf("Failed to read Jina response body: %v", err)
		return nil, fmt.Errorf("failed to read jina response body: %w", err)
	}

	var searchResponse struct {
		Data []SearchResult `json:"data"`
	}
	if err := json.Unmarshal(body, &searchResponse); err != nil {
		logger.Logf("Failed to unmarshal Jina response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal jina response: %w", err)
	}

	logger.Logf("Received %d search results from Jina", len(searchResponse.Data))
	return searchResponse.Data, nil
}

// SearchProvider defines the interface for search providers
type SearchProvider interface {
	Name() string
	Search(query string, logger *utils.Logger) ([]SearchResult, error)
}

// JinaSearchProvider implements SearchProvider for Jina AI
type JinaSearchProvider struct{}

func (j *JinaSearchProvider) Name() string {
	return "Jina AI"
}

func (j *JinaSearchProvider) Search(query string, logger *utils.Logger) ([]SearchResult, error) {
	return performJinaSearch(query, "", logger) // API key will be passed separately
}

// DuckDuckGoSearchProvider implements SearchProvider for DuckDuckGo
type DuckDuckGoSearchProvider struct{}

func (d *DuckDuckGoSearchProvider) Name() string {
	return "DuckDuckGo"
}

func (d *DuckDuckGoSearchProvider) Search(query string, logger *utils.Logger) ([]SearchResult, error) {
	return performDuckDuckGoSearch(query, logger)
}

// performDuckDuckGoSearch performs a search using DuckDuckGo Instant Answer API
func performDuckDuckGoSearch(query string, logger *utils.Logger) ([]SearchResult, error) {
	logger.LogProcessStep(fmt.Sprintf("Performing DuckDuckGo search for query: %s", query))

	// Use DuckDuckGo Instant Answer API
	ddgURL := fmt.Sprintf("https://api.duckduckgo.com/?q=%s&format=json&no_html=1&skip_disambig=1", url.QueryEscape(query))

	req, err := http.NewRequest("GET", ddgURL, nil)
	if err != nil {
		logger.Logf("Failed to create DuckDuckGo request: %v", err)
		return nil, fmt.Errorf("failed to create duckduckgo request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; ledit/1.0)")
	req.Header.Set("Accept", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	logger.Logf("Making HTTP request to DuckDuckGo API: %s", req.URL.String())
	resp, err := client.Do(req)
	if err != nil {
		logger.Logf("DuckDuckGo search request failed: %v", err)
		return nil, fmt.Errorf("failed to perform duckduckgo search: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		logger.Logf("DuckDuckGo returned status %d", resp.StatusCode)
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		logger.Logf("Failed to read DuckDuckGo response body: %v", err)
		return nil, fmt.Errorf("failed to read duckduckgo response body: %w", err)
	}

	// Parse DuckDuckGo response
	results, err := parseDuckDuckGoResponse(body, query)
	if err != nil {
		logger.Logf("Failed to parse DuckDuckGo response: %v", err)
		return nil, fmt.Errorf("failed to parse duckduckgo response: %w", err)
	}

	logger.Logf("Received %d search results from DuckDuckGo", len(results))
	return results, nil
}

// parseDuckDuckGoResponse parses DuckDuckGo API response
func parseDuckDuckGoResponse(body []byte, query string) ([]SearchResult, error) {
	var ddgResponse struct {
		Abstract       string `json:"Abstract"`
		AbstractText   string `json:"AbstractText"`
		AbstractURL    string `json:"AbstractURL"`
		AbstractSource string `json:"AbstractSource"`
		RelatedTopics  []struct {
			FirstURL string `json:"FirstURL"`
			Text     string `json:"Text"`
			Result   string `json:"Result"`
		} `json:"RelatedTopics"`
	}

	if err := json.Unmarshal(body, &ddgResponse); err != nil {
		return nil, fmt.Errorf("failed to unmarshal duckduckgo response: %w", err)
	}

	var results []SearchResult

	// Add abstract as first result if available
	if ddgResponse.AbstractText != "" {
		results = append(results, SearchResult{
			Title:       fmt.Sprintf("About: %s", query),
			URL:         ddgResponse.AbstractURL,
			Description: ddgResponse.AbstractText,
		})
	}

	// Add related topics
	for i, topic := range ddgResponse.RelatedTopics {
		// Limit to first 10 results to avoid overwhelming
		if i >= 10 {
			break
		}

		// Extract clean text from HTML if present
		text := topic.Text
		if text == "" && topic.Result != "" {
			// Simple HTML stripping - remove tags
			text = strings.ReplaceAll(topic.Result, "<[^>]*>", "")
			text = strings.TrimSpace(text)
		}

		if text == "" {
			text = fmt.Sprintf("Related topic for: %s", query)
		}

		results = append(results, SearchResult{
			Title:       fmt.Sprintf("Result %d", i+1),
			URL:         topic.FirstURL,
			Description: text,
		})
	}

	// If no results found, provide a fallback
	if len(results) == 0 {
		results = append(results, SearchResult{
			Title:       fmt.Sprintf("Search results for: %s", query),
			URL:         fmt.Sprintf("https://duckduckgo.com/?q=%s", url.QueryEscape(query)),
			Description: "No specific results found. Use this URL to manually search DuckDuckGo.",
		})
	}

	return results, nil
}
