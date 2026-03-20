package webcontent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/alantheprice/ledit/pkg/configuration" // Updated to new config
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	// Max content size in bytes (1MB) - larger content gets truncated with warning
	maxContentSize = 1024 * 1024
	// Truncated suffix to append when content exceeds limit
	truncatedSuffix = "\n\n[CONTENT TRUNCATED: Original was larger than 1MB. Only first 1MB returned.]"
)

// globalBrowser is a lazily-initialized singleton BrowserRenderer shared by
// all fetchers. This avoids launching a separate Chromium process per
// WebContentFetcher. The singleton is protected by sync.Once and is never
// closed during normal operation (it lives for the process lifetime).
var (
	globalBrowser     BrowserRenderer
	globalBrowserOnce sync.Once
)

func getGlobalBrowser() BrowserRenderer {
	globalBrowserOnce.Do(func() {
		globalBrowser = NewBrowserRenderer()
	})
	return globalBrowser
}

// WebContentFetcher handles fetching content from URLs.
type WebContentFetcher struct {
	httpClient  *http.Client
	rateLimiter *rateLimiter
}

// NewWebContentFetcher creates a new WebContentFetcher instance.
func NewWebContentFetcher() *WebContentFetcher {
	return &WebContentFetcher{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		rateLimiter: newRateLimiter(1*time.Second, 1800*time.Millisecond),
	}
}

// urlBannerPrefix is used to detect whether cached content already includes
// the URL banner wrapper (produced by older versions that cached wrapped output).
const urlBannerPrefix = "\n--- Content from URL: "

// FetchWebContent fetches content from a given URL, using a cache to avoid refetching.
// It uses Jina Reader for external URLs if available, otherwise falls back to a direct HTTP GET.
// Content is always returned wrapped in URL banners.
func (w *WebContentFetcher) FetchWebContent(url string, cfg *configuration.Manager) (string, error) { // Use Manager instead of Config
	utils.GetLogger(false).LogProcessStep(fmt.Sprintf("Starting web content search for query: %s", url))
	// Check cache first
	if cachedEntry, found := w.loadURLCache(url); found {
		// Backwards-compatible: old cached entries may already include the banner.
		if !strings.HasPrefix(cachedEntry.Content, urlBannerPrefix) {
			return wrapWithBanner(url, cachedEntry.Content), nil
		}
		return cachedEntry.Content, nil
	}

	content, err := w.fetchContent(url, cfg)
	if err != nil {
		return "", err
	}

	// Cache the raw content so the same entry is valid regardless of how it was fetched.
	if err := w.saveURLCache(url, content); err != nil {
		// Log warning but don't fail the operation
		utils.GetLogger(false).LogError(err)
	}

	return wrapWithBanner(url, content), nil
}

// wrapWithBanner formats content with URL boundary markers.
func wrapWithBanner(url, content string) string {
	return fmt.Sprintf("\n--- Content from URL: %s ---\n\n%s\n--- End of content from URL: %s ---\n", url, content, url)
}

// fetchContent determines the best method to fetch content and retrieves it.
// The returned content is raw (no URL banners) — the caller is responsible
// for wrapping. This ensures both Jina and direct-fetch paths cache the
// same shape of data.
func (w *WebContentFetcher) fetchContent(url string, cfg *configuration.Manager) (string, error) { // Use Manager instead of Config
	isLocalhost := isLocalhostURL(url)
	jinaAPIKey := cfg.GetAPIKeys().GetAPIKey("jinaai")

	// Rate limit: wait before fetching from the same domain again.
	if !isLocalhost {
		if u, err := neturl.Parse(url); err == nil {
			w.rateLimiter.wait(u.Hostname())
		}
	}

	// GitHub blob URLs: rewrite to raw.githubusercontent.com for direct, Jina-free fetch.
	if isGitHubURL(url) {
		if rawURL := rewriteGitHubBlobToRaw(url); rawURL != "" {
			utils.GetLogger(false).Logf("Rewriting GitHub blob URL to raw: %s → %s", url, rawURL)
			return w.fetchDirectURL(rawURL)
		}
	}

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
		// If Jina returned suspiciously little content, try browser as fallback.
		trimmedContent := strings.TrimSpace(content)
		if len(trimmedContent) < 50 {
			utils.GetLogger(false).Logf("Jina returned very little content (%d chars) for %s, trying browser fallback", len(trimmedContent), url)
			directContent, directErr := w.fetchDirectURL(url)
			if directErr == nil && len(strings.TrimSpace(directContent)) > len(trimmedContent) {
				return directContent, nil
			}
		}
		return content, nil
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
	parsedURL, err := neturl.Parse(urlStr)
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
		// GitHub blob pages — prefer raw content over Jina-rendered HTML
		"github.com/blob/",
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
// Truncation is UTF-8 safe — it will not split multi-byte characters.
func (w *WebContentFetcher) truncateContent(content string) (string, error) {
	contentLen := len(content)
	if contentLen <= maxContentSize {
		return content, nil
	}

	// Find the last rune boundary at or before maxContentSize.
	boundary := maxContentSize
	for boundary > 0 && !utf8.RuneStart(content[boundary]) {
		boundary--
	}
	truncated := content[:boundary]
	return fmt.Sprintf("%s%s (original: %.1f MB)", truncated, truncatedSuffix, float64(contentLen)/(1024*1024)), nil
}

// isLocalhostURL returns true if the URL points to localhost.
func isLocalhostURL(url string) bool {
	lower := strings.ToLower(url)
	return strings.HasPrefix(lower, "http://localhost") ||
		strings.HasPrefix(lower, "https://localhost") ||
		strings.HasPrefix(lower, "http://127.0.0.1") ||
		strings.HasPrefix(lower, "https://127.0.0.1") ||
		strings.HasPrefix(lower, "http://[::1]") ||
		strings.HasPrefix(lower, "https://[::1]")
}

// localhostOrSPA returns a human-readable reason string for why browser rendering
// was attempted for a given URL.
func localhostOrSPA(url string) string {
	if isLocalhostURL(url) {
		return "localhost URL (JS likely needed)"
	}
	return "SPA shell detected"
}

// fetchDirectURL performs a direct HTTP GET request to the given URL.
// If the response Content-Type is text/html, the body is cleaned to extract
// visible text content before truncation.  For localhost URLs, a headless
// browser is always tried first for HTML content (since JS is likely needed).
// For non-localhost URLs, browser rendering is only triggered when the raw
// HTML appears to be an SPA shell (detected by NeedsRendering).
func (w *WebContentFetcher) fetchDirectURL(url string) (string, error) {
	resp, err := w.httpClient.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if isHTMLContent(resp.Header.Get("Content-Type")) {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			text := HTMLToText(string(body))
			text = strings.TrimSpace(text)
			const maxErrorLen = 500
			if len(text) > maxErrorLen {
				text = text[:maxErrorLen] + "..."
			}
			if text != "" {
				return "", fmt.Errorf("HTTP %d for URL: %s\n\nServer response:\n%s", resp.StatusCode, url, text)
			}
		} else {
			_, _ = io.ReadAll(resp.Body)
		}
		return "", fmt.Errorf("HTTP %d for URL: %s", resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxContentSize+1024))
	if err != nil {
		return "", err
	}

	content := string(body)

	// If the response is HTML, clean it to readable text before truncation.
	if isHTMLContent(resp.Header.Get("Content-Type")) {
		tryBrowser := isLocalhostURL(url) || NeedsRendering(content)
		if tryBrowser {
			if rendered, err := getGlobalBrowser().RenderPage(context.Background(), url); err == nil {
				utils.GetLogger(false).Logf("Browser rendering for %s (reason: %s)", url, localhostOrSPA(url))
				content = HTMLToText(rendered)
				return w.truncateContent(content)
			} else if _, ok := getGlobalBrowser().(*nopRenderer); !ok {
				utils.GetLogger(false).Logf("Browser render failed for %s: %v, falling back to raw HTML", url, err)
			}
		}
		content = HTMLToText(content)
	}

	// Truncate if content is too large
	truncated, err := w.truncateContent(content)
	if err != nil {
		return "", err
	}
	return truncated, nil
}

// isHTMLContent returns true if the Content-Type header indicates HTML.
func isHTMLContent(contentType string) bool {
	// Match text/html, application/xhtml+xml, etc. — anything containing "html"
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "text/html") || strings.Contains(ct, "application/xhtml")
}
