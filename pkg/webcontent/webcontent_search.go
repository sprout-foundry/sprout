package webcontent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/apikeys"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

const jinaSearchURL = "https://s.jina.ai/search"

func FetchContextFromSearch(query string, cfg *config.Config) (string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep(fmt.Sprintf("Starting web content search for query: %s", query))
	// Ensure consistent log phrase for tests regardless of cache usage
	logger.LogProcessStep(fmt.Sprintf("Performing Jina AI search for query: %s", query))
	defer logger.LogProcessStep("Completed web content search")

	if strings.TrimSpace(query) == "" {
		logger.Log("No relevant content found for the query")
		return "", nil
	}

	// Fetch search results and content using Jina AI Search API
	results, err := fetchJinaSearchResults(query, cfg)
	if err != nil {
		logger.Logf("Error fetching Jina search results: %v", err)
		return "", fmt.Errorf("failed to fetch Jina search results: %w", err)
	}

	if len(results) == 0 {
		logger.Log("No relevant content found for the query")
		return "", nil
	}

	logger.Logf("Found %d relevant content items", len(results))
	var sb strings.Builder
	for url, content := range results {
		sb.WriteString(fmt.Sprintf("URL: %s\nContent:\n%s\n\n", url, content))
	}

	return sb.String(), nil
}

func getSearchResults(query string, cfg *config.Config) ([]JinaSearchResult, error) {
	fetcher := NewWebContentFetcher()
	logger := utils.GetLogger(cfg.SkipPrompt)
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

	// Get Jina API Key. This will prompt the user if the key is not found.
	jinaAPIKey, err := apikeys.GetAPIKey("JinaAI", !cfg.SkipPrompt)
	if err != nil {
		logger.Logf("Could not get Jina API key: %v. Proceeding without it, but may be rate limited.", err)
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
	client := &http.Client{Timeout: 120 * time.Second} // Increased from 10 seconds to 120 seconds
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
	return searchResponse.Data, nil
}

// fetchJinaSearchResults fetches search results using Jina AI Search API,
// selects relevant URLs using LLM, and fetches full content of selected URLs.
// uses embeddings to find the most relevant parts of the text for a given query.
func fetchJinaSearchResults(query string, cfg *config.Config) (map[string]string, error) {
	fetcher := NewWebContentFetcher()
	logger := utils.GetLogger(cfg.SkipPrompt)
	searchResponse, err := getSearchResults(query, cfg)
	if err != nil {
		logger.Logf("Failed to get search results: %v", err)
		return nil, fmt.Errorf("failed to get search results: %w", err)
	}

	selectedURLs, err := selectRelevantURLsWithLLM(query, searchResponse, cfg)
	if err != nil {
		logger.Logf("URL selection failed: %v", err)
		return nil, fmt.Errorf("failed to select relevant URLs: %w", err)
	}

	if len(selectedURLs) == 0 {
		logger.Log("No relevant URLs selected by LLM")
		return make(map[string]string), nil
	}

	logger.Logf("LLM selected %d URLs: %v", len(selectedURLs), selectedURLs)
	var wg sync.WaitGroup
	var mu sync.Mutex
	fetchedContent := make(map[string]string)

	for _, url := range selectedURLs {
		wg.Add(1)
		go func(url string) {
			defer wg.Done()
			logger.LogProcessStep(fmt.Sprintf("Fetching content from %s", url))
			content, err := fetcher.FetchWebContent(url, cfg)
			if err != nil {
				logger.Logf("Failed to fetch content from %s: %v", url, err)
				return
			}

			logger.LogProcessStep(fmt.Sprintf("Extracting relevant content from %s for query: %s", url, query))
			relevantContent, err := GetRelevantContentFromText(query, content, cfg)
			if err != nil {
				logger.Logf("Failed to get relevant content from %s: %v. Using full content as fallback.", url, err)
				mu.Lock()
				fetchedContent[url] = content
				mu.Unlock()
				return
			}

			logger.Logf("Extracted %d characters of relevant content from %s", len(relevantContent), url)
			mu.Lock()
			fetchedContent[url] = relevantContent
			mu.Unlock()
		}(url)
	}

	wg.Wait()
	logger.Logf("Successfully fetched content from %d/%d URLs", len(fetchedContent), len(selectedURLs))

	cacheEntry := &ReferenceCacheEntry{
		Query:          query,
		SearchResults:  searchResponse,
		SelectedURLs:   selectedURLs,
		FetchedContent: fetchedContent,
		Timestamp:      time.Now(),
	}
	if err := fetcher.saveReferenceCache(query, cacheEntry); err != nil {
		logger.Logf("Failed to save reference cache: %v", err)
	} else {
		logger.Log("Successfully cached search results")
	}

	return fetchedContent, nil
}

func selectRelevantURLsWithLLM(query string, results []JinaSearchResult, cfg *config.Config) ([]string, error) {
	logger := utils.GetLogger(cfg.SkipPrompt)
	logger.LogProcessStep("Selecting relevant URLs with LLM")
	defer logger.LogProcessStep("Completed URL selection")

	var sb strings.Builder
	sb.WriteString("Search Query: ")
	sb.WriteString(query)
	sb.WriteString("\n\nSearch Results:\n")
	for i, r := range results {
		sb.WriteString(fmt.Sprintf("%d. URL: %s\n   Title: %s\n   Description: %s\n", i+1, r.URL, r.Title, r.Description))
	}

	messages := prompts.BuildSearchResultsQueryMessages(sb.String(), query)

	logger.Log("Sending URL selection request to LLM")
	resp, _, err := llm.GetLLMResponse(cfg.WorkspaceModel, messages, "search_results_selector", cfg, 2*time.Minute)
	if err != nil {
		logger.Logf("LLM URL selection failed: %v", err)
		return nil, err
	}

	logger.Logf("Received LLM response for URL selection: %s", resp)
	if strings.ToLower(strings.TrimSpace(resp)) == "none" {
		logger.Log("LLM determined no relevant URLs")
		return []string{}, nil
	}

	var selectedURLs []string
	parts := strings.Split(resp, ",")
	for _, p := range parts {
		var num int
		_, err := fmt.Sscanf(strings.TrimSpace(p), "%d", &num)
		if err == nil && num > 0 && num <= len(results) {
			selectedURLs = append(selectedURLs, results[num-1].URL)
		}
	}

	return selectedURLs, nil
}
