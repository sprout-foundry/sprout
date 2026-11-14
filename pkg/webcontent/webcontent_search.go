package webcontent

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/net/html"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/utils"
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
	if len(results) == 0 {
		return results, nil
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

// duckDuckGoHTMLURL is DuckDuckGo's HTML results page, which lists web
// results; its Instant Answer API (api.duckduckgo.com) answers only a few
// encyclopedic queries and returns nothing for most searches.
var duckDuckGoHTMLURL = "https://html.duckduckgo.com/html/"

// duckDuckGoMaxBody bounds the results page read.
const duckDuckGoMaxBody = 2 << 20

// performDuckDuckGoSearch searches DuckDuckGo's HTML results page. No result
// is an empty list, never a made-up one.
func performDuckDuckGoSearch(query string, logger *utils.Logger) ([]SearchResult, error) {
	logger.LogProcessStep(fmt.Sprintf("Performing DuckDuckGo search for query: %s", query))

	form := url.Values{"q": {query}}
	req, err := http.NewRequest(http.MethodPost, duckDuckGoHTMLURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create duckduckgo request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("User-Agent", "Mozilla/5.0 (compatible; sprout/1.0)")
	req.Header.Set("Accept", "text/html")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		logger.Logf("DuckDuckGo search request failed: %v", err)
		return nil, fmt.Errorf("failed to perform duckduckgo search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("duckduckgo returned status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, duckDuckGoMaxBody))
	if err != nil {
		return nil, fmt.Errorf("failed to read duckduckgo response body: %w", err)
	}
	results := parseDuckDuckGoHTML(string(body))
	logger.Logf("Received %d search results from DuckDuckGo", len(results))
	return results, nil
}

// parseDuckDuckGoHTML reads the results page: each title link (class
// result__a, its address behind DuckDuckGo's redirect) and the snippet after
// it (class result__snippet).
func parseDuckDuckGoHTML(src string) []SearchResult {
	doc, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return nil
	}
	hasClass := func(n *html.Node, class string) bool {
		for _, a := range n.Attr {
			if a.Key == "class" {
				for _, c := range strings.Fields(a.Val) {
					if c == class {
						return true
					}
				}
			}
		}
		return false
	}
	attr := func(n *html.Node, key string) string {
		for _, a := range n.Attr {
			if a.Key == key {
				return a.Val
			}
		}
		return ""
	}
	text := func(n *html.Node) string {
		var b strings.Builder
		var walk func(*html.Node)
		walk = func(n *html.Node) {
			if n.Type == html.TextNode {
				b.WriteString(n.Data)
			}
			for c := n.FirstChild; c != nil; c = c.NextSibling {
				walk(c)
			}
		}
		walk(n)
		return strings.Join(strings.Fields(b.String()), " ")
	}
	var results []SearchResult
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.Data == "a" {
			switch {
			case hasClass(n, "result__a"):
				results = append(results, SearchResult{Title: text(n), URL: duckDuckGoTarget(attr(n, "href"))})
			case hasClass(n, "result__snippet") && len(results) > 0 && results[len(results)-1].Description == "":
				results[len(results)-1].Description = text(n)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	out := results[:0]
	for _, r := range results {
		if r.URL != "" && r.Title != "" {
			out = append(out, r)
		}
	}
	return out
}

// duckDuckGoTarget is the address behind a result link: DuckDuckGo wraps it
// as //duckduckgo.com/l/?uddg=<address>.
func duckDuckGoTarget(href string) string {
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if target := u.Query().Get("uddg"); target != "" {
		return target
	}
	if u.Scheme == "https" || u.Scheme == "http" {
		return u.String()
	}
	return ""
}
