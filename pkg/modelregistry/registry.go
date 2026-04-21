// Package modelregistry provides a client for fetching per-provider model lists
// from a static JSON model registry server with in-memory caching.
package modelregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	defaultTTL         = 5 * time.Minute
	defaultHTTPTimeout = 500 * time.Millisecond
	maxResponseBytes   int64 = 1 << 20 // 1 MiB — matches pkg/providercatalog limit
	envRegistryURL     = "LEDIT_MODEL_REGISTRY_URL"
	envRegistryTTL     = "LEDIT_MODEL_REGISTRY_TTL"
	envRegistryTimeout = "LEDIT_MODEL_REGISTRY_TIMEOUT"
)

// ModelInfo mirrors api.ModelInfo for registry JSON responses.
// NOTE: Intentionally duplicated to avoid import cycles between
// modelregistry and agent_api. Keep in sync with pkg/agent_api/models.go ModelInfo.
type ModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Size          string   `json:"size,omitempty"`
	Cost          float64  `json:"cost,omitempty"`
	InputCost     float64  `json:"input_cost,omitempty"`
	OutputCost    float64  `json:"output_cost,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// RawModel is a provider-agnostic model representation used for cache storage
// and inter-package transfer without creating import cycles.
type RawModel struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	Provider      string   `json:"provider,omitempty"`
	Size          string   `json:"size,omitempty"`
	Cost          float64  `json:"cost,omitempty"`
	InputCost     float64  `json:"input_cost,omitempty"`
	OutputCost    float64  `json:"output_cost,omitempty"`
	ContextLength int      `json:"context_length,omitempty"`
	Tags          []string `json:"tags,omitempty"`
}

// providerResponse is the JSON schema for a per-provider model file.
type providerResponse struct {
	UpdatedAt string      `json:"updated_at"`
	Models    []ModelInfo `json:"models"`
}

type cacheEntry struct {
	models    []ModelInfo
	fetchedAt time.Time
}

var (
	mu          sync.RWMutex
	cache       = make(map[string]cacheEntry)
	baseURL     string
	ttl         = defaultTTL
	httpTimeout = defaultHTTPTimeout
	once        sync.Once
	sf          singleflight.Group
)

func init() {
	once.Do(loadConfig)
}

func loadConfig() {
	if v := strings.TrimSpace(os.Getenv(envRegistryURL)); v != "" {
		baseURL = strings.TrimRight(v, "/")
	}
	if v := strings.TrimSpace(os.Getenv(envRegistryTTL)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ttl = d
		}
	}
	if v := strings.TrimSpace(os.Getenv(envRegistryTimeout)); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			httpTimeout = d
		}
	}
}

// SetBaseURL sets the registry base URL (useful for testing).
func SetBaseURL(url string) {
	mu.Lock()
	defer mu.Unlock()
	baseURL = strings.TrimRight(url, "/")
}

// SetTTL sets the cache TTL (useful for testing).
func SetTTL(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	ttl = d
}

// SetHTTPTimeout sets the HTTP client timeout (useful for testing).
func SetHTTPTimeout(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	httpTimeout = d
}

// IsEnabled returns true if the registry URL is configured.
func IsEnabled() bool {
	mu.RLock()
	defer mu.RUnlock()
	return baseURL != ""
}

// baseURLCopy returns a copy of the base URL under read lock.
func baseURLCopy() string {
	mu.RLock()
	defer mu.RUnlock()
	return baseURL
}

// ttlCopy returns a copy of the TTL under read lock.
func ttlCopy() time.Duration {
	mu.RLock()
	defer mu.RUnlock()
	return ttl
}

// httpTimeoutCopy returns a copy of the HTTP timeout under read lock.
func httpTimeoutCopy() time.Duration {
	mu.RLock()
	defer mu.RUnlock()
	return httpTimeout
}

// isValidProviderID checks that a provider ID contains only safe characters.
func isValidProviderID(id string) bool {
	if len(id) == 0 || len(id) > 128 {
		return false
	}
	for _, r := range id {
		if !((r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_') {
			return false
		}
	}
	return true
}

// FetchModels returns raw model data for a provider from the registry.
// Returns nil, nil if the registry is not enabled.
// Returns cached models if available and not expired.
// Falls through to a network fetch on cache miss or expiry.
// Uses singleflight to deduplicate concurrent requests for the same provider.
func FetchModels(ctx context.Context, providerID string) ([]RawModel, error) {
	if !IsEnabled() {
		return nil, nil
	}

	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if !isValidProviderID(providerID) {
		return nil, fmt.Errorf("modelregistry: invalid provider ID %q", providerID)
	}

	// Check cache
	mu.RLock()
	entry, ok := cache[providerID]
	mu.RUnlock()
	if ok && time.Since(entry.fetchedAt) < ttlCopy() {
		return convertToRaw(entry.models), nil
	}

	// Use singleflight to deduplicate concurrent requests for the same provider.
	result, err, _ := sf.Do(providerID, func() (interface{}, error) {
		// Double-check cache after acquiring singleflight lock.
		mu.RLock()
		entry, ok := cache[providerID]
		mu.RUnlock()
		if ok && time.Since(entry.fetchedAt) < ttlCopy() {
			return convertToRaw(entry.models), nil
		}

		// Fetch from registry.
		url := baseURLCopy() + "/models/" + providerID + ".json"

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("modelregistry: create request: %w", reqErr)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "ledit-model-registry/1.0")

		client := &http.Client{Timeout: httpTimeoutCopy()}
		resp, fetchErr := client.Do(req)
		if fetchErr != nil {
			return nil, fmt.Errorf("modelregistry: fetch %s: %w", providerID, fetchErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			// Log debug information if debug mode is enabled
			if os.Getenv("LEDIT_DEBUG_REGISTRY") != "" {
				log.Printf("[modelregistry] provider %q not found in registry (404), falling back to provider API", providerID)
			}
			return nil, nil
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("modelregistry: fetch %s: HTTP %d", providerID, resp.StatusCode)
		}

		var payload providerResponse
		if decodeErr := json.NewDecoder(io.LimitReader(resp.Body, maxResponseBytes)).Decode(&payload); decodeErr != nil {
			return nil, fmt.Errorf("modelregistry: decode %s: %w", providerID, decodeErr)
		}

		// Store in cache.
		mu.Lock()
		cache[providerID] = cacheEntry{models: payload.Models, fetchedAt: time.Now()}
		mu.Unlock()

		return convertToRaw(payload.Models), nil
	})

	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.([]RawModel), nil
}

// ClearCache removes all cached entries.
func ClearCache() {
	mu.Lock()
	defer mu.Unlock()
	cache = make(map[string]cacheEntry)
}

func convertToRaw(models []ModelInfo) []RawModel {
	out := make([]RawModel, len(models))
	for i, m := range models {
		out[i] = RawModel{
			ID:            m.ID,
			Name:          m.Name,
			Description:   m.Description,
			Provider:      m.Provider,
			Size:          m.Size,
			Cost:          m.Cost,
			InputCost:     m.InputCost,
			OutputCost:    m.OutputCost,
			ContextLength: m.ContextLength,
			Tags:          append([]string(nil), m.Tags...),
		}
	}
	return out
}
