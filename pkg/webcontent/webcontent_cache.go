package webcontent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/utils"
)

const (
	referencesDir = "search_cache"
	urlCacheDir   = "url_cache"
	cacheExpiry   = 90 * 24 * time.Hour // 90 days
)

func getHomeSettingsPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".ledit"), nil
}

func getPathWithFallback(folderName string) string {
	settings, err := getHomeSettingsPath()
	if err == nil {
		return filepath.Join(settings, folderName)
	}
	// Fallback to current working directory if home directory is not accessible
	cwd, err := os.Getwd()
	if err != nil {
		return filepath.Join(".ledit", folderName)
	}
	return filepath.Join(cwd, ".ledit", folderName)

}

// getReferenceCachePath returns the full path to the cache directory for references.
func getReferenceCachePath() string {
	return getPathWithFallback(referencesDir)
}

// getURLCachePath returns the full path to the cache directory for URL content.
func getURLCachePath() string {
	return getPathWithFallback(urlCacheDir)
}

// getReferenceCacheFilePath returns the full path for a specific cached query.
func getReferenceCacheFilePath(query string) (string, error) {
	cacheDir := getReferenceCachePath()
	queryHash := utils.GenerateRequestHash(query)
	return filepath.Join(cacheDir, queryHash+".json"), nil
}

// getURLCacheFilePath returns the full path for a specific cached URL.
func getURLCacheFilePath(url string) (string, error) {
	cacheDir := getURLCachePath()
	urlHash := utils.GenerateRequestHash(url)
	return filepath.Join(cacheDir, urlHash+".json"), nil
}

// loadURLCache attempts to load cached content for the given URL.
func (w *WebContentFetcher) loadURLCache(url string) (*URLCacheEntry, bool) {
	filePath, err := getURLCacheFilePath(url)
	if err != nil {
		fmt.Printf("Error getting URL cache file path: %v\n", err)
		return nil, false
	}

	data, err := filesystem.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false // Cache file does not exist
		}
		fmt.Printf("Error reading URL cache file %s: %v\n", filePath, err)
		return nil, false
	}

	var entry URLCacheEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		fmt.Printf("Error unmarshaling URL cache data from %s: %v\n", filePath, err)
		os.Remove(filePath) // Delete corrupted cache file
		return nil, false
	}

	if time.Since(entry.Timestamp) > cacheExpiry {
		fmt.Printf("Cached content for URL \"%s\" is expired. Deleting and re-fetching.\n", url)
		os.Remove(filePath) // Delete expired cache
		return nil, false
	}

	fmt.Printf("Using cached content for URL: \"%s\" (cached on %s)\n", url, entry.Timestamp.Format("2006-01-02"))
	return &entry, true
}

// saveURLCache saves the URL content to a cache file.
func (w *WebContentFetcher) saveURLCache(url string, content string) error {
	cacheDir := getURLCachePath()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create URL cache directory %s: %w", cacheDir, err)
	}

	filePath, err := getURLCacheFilePath(url)
	if err != nil {
		return err
	}

	entry := URLCacheEntry{
		URL:       url,
		Content:   content,
		Timestamp: time.Now(),
	}

	data, err := json.MarshalIndent(entry, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal URL cache entry: %w", err)
	}

	if err := filesystem.SaveFile(filePath, string(data)); err != nil {
		return fmt.Errorf("failed to save URL cache file %s: %w", filePath, err)
	}
	fmt.Printf("Saved URL content to cache: %s\n", filePath)
	return nil
}

// loadReferenceCache attempts to load a cached search result for the given query.
// It returns the entry and true if found and not expired, otherwise nil and false.
func (w *WebContentFetcher) loadReferenceCache(query string) (*ReferenceCacheEntry, error) {
	filePath, err := getReferenceCacheFilePath(query)
	if err != nil {
		fmt.Printf("Error getting cache file path: %v\n", err)
		return nil, err
	}

	data, err := filesystem.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, err // Cache file does not exist
		}
		fmt.Printf("Error reading cache file %s: %v\n", filePath, err)
		return nil, err
	}

	var entry ReferenceCacheEntry
	if err := json.Unmarshal([]byte(data), &entry); err != nil {
		fmt.Printf("Error unmarshaling cache data from %s: %v\n", filePath, err)
		// Consider deleting corrupted cache file
		os.Remove(filePath) // Delete corrupted cache file
		return nil, fmt.Errorf("corrupted cache file")
	}

	if time.Since(entry.Timestamp) > cacheExpiry {
		fmt.Printf("Cached entry for query \"%s\" is expired. Deleting and re-fetching.\n", query)
		os.Remove(filePath) // Delete expired cache
		return nil, fmt.Errorf("expired cache")
	}

	fmt.Printf("Using cached content for query: \"%s\" (cached on %s)\n", query, entry.Timestamp.Format("2006-01-02"))
	return &entry, nil
}

// saveReferenceCache saves the search results and fetched content to a cache file.
func (w *WebContentFetcher) saveReferenceCache(query string, entry *ReferenceCacheEntry) error {
	cacheDir := getReferenceCachePath()
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		return fmt.Errorf("failed to create cache directory %s: %w", cacheDir, err)
	}

	filePath, err := getReferenceCacheFilePath(query)
	if err != nil {
		return err
	}

	entry.Timestamp = time.Now() // Update timestamp before saving

	data, err := json.MarshalIndent(entry, "", "    ")
	if err != nil {
		return fmt.Errorf("failed to marshal cache entry: %w", err)
	}

	if err := filesystem.SaveFile(filePath, string(data)); err != nil {
		return fmt.Errorf("failed to save cache file %s: %w", filePath, err)
	}
	fmt.Printf("Saved search results to cache: %s\n", filePath)
	return nil
}
