package webcontent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/utils"
)

const jinaSearchURL = "https://s.jina.ai/search"

// GetSearchResults performs a web search using Jina AI and returns search results
func GetSearchResults(query string, cfg *configuration.Manager) ([]JinaSearchResult, error) {
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
	if jinaAPIKey == "" {
		logger.Log("Jina API key not found. Proceeding without it, but may be rate limited.")
	} else {
		logger.Log("Using Jina API key for search")
	}

	logger.LogProcessStep(fmt.Sprintf("Performing Jina AI search for query: %s", query))
	req, err := http.NewRequest("GET", jinaSearchURL, nil)
	if err != nil {
		logger.Logf("Failed to create Jina request: %v", err)
		return nil, fmt.Errorf("failed to create jina request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if jinaAPIKey != "" {
		req.Header.Set("Authorization", "Bearer "+jinaAPIKey)
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
		Data []JinaSearchResult `json:"data"`
	}
	if err := json.Unmarshal(body, &searchResponse); err != nil {
		logger.Logf("Failed to unmarshal Jina response: %v", err)
		return nil, fmt.Errorf("failed to unmarshal jina response: %w", err)
	}

	logger.Logf("Received %d search results from Jina", len(searchResponse.Data))

	// Cache the results
	cacheEntry := &ReferenceCacheEntry{
		Query:         query,
		SearchResults: searchResponse.Data,
		Timestamp:     time.Now(),
	}
	if err := fetcher.saveReferenceCache(query, cacheEntry); err != nil {
		logger.Logf("Failed to save reference cache: %v", err)
	}

	return searchResponse.Data, nil
}
