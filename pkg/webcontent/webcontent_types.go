package webcontent

import "time"

// JinaSearchResult represents a single search result from Jina AI Search API.
type JinaSearchResult struct {
	Title       string `json:"title"`
	URL         string `json:"url"`
	Description string `json:"description"` // This will be the snippet/description from search, not full content
}

// ReferenceCacheEntry stores cached search results and fetched content.
type ReferenceCacheEntry struct {
	Query          string             `json:"query"`
	SearchResults  []JinaSearchResult `json:"search_results"`  // Initial search results (snippets)
	SelectedURLs   []string           `json:"selected_urls"`   // URLs chosen by LLM
	FinalContent   string             `json:"final_content"`   // Full content fetched from selected URLs
	FetchedContent map[string]string  `json:"fetched_content"` // Full content for each selected URL
	Timestamp      time.Time          `json:"timestamp"`       // When this entry was cached
}

// URLCacheEntry stores cached content for individual URLs
type URLCacheEntry struct {
	URL       string    `json:"url"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"` // When this entry was cached
}
