package webcontent

import "time"

// SearchResult represents a single search result from any search provider.
type SearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"` // This will be the snippet/description from search, not full content
}

// JinaSearchResult represents a single search result from Jina AI Search API.
// Deprecated: Use SearchResult instead
type JinaSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"` // This will be the snippet/description from search, not full content
}

// ReferenceCacheEntry stores cached search results and fetched content.
type ReferenceCacheEntry struct {
	Query          string            `json:"query"`
	SearchResults  []SearchResult    `json:"search_results"`  // Initial search results (snippets)
	SelectedURLs   []string          `json:"selected_urls"`   // URLs chosen by LLM
	FinalContent   string            `json:"final_content"`   // Full content fetched from selected URLs
	FetchedContent map[string]string `json:"fetched_content"` // Full content for each selected URL
	Timestamp      time.Time         `json:"timestamp"`       // When this entry was cached
}

// URLCacheEntry stores cached content for individual URLs
type URLCacheEntry struct {
	URL       string    `json:"url"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"` // When this entry was cached
}
