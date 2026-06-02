// Package providerregistry provides a client for fetching provider connection
// configs from a remote JSON registry server with in-memory caching.
package providerregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"golang.org/x/sync/singleflight"
)

const (
	defaultTTL               = 5 * time.Minute
	defaultNegativeTTL       = 30 * time.Second
	defaultHTTPTimeout       = 500 * time.Millisecond
	defaultIndexTimeout      = 1 * time.Second
	maxResponseBytes   int64 = 1 << 20 // 1 MiB
	defaultRegistryURL       = "https://sprout-foundry.github.io/sprout"
)

// RemoteAuthConfig duplicates AuthConfig without the runtime-only Key field.
type RemoteAuthConfig struct {
	Type   string `json:"type"`
	EnvVar string `json:"env_var"`
}

// RemoteRequestDefaults duplicates RequestDefaults.
type RemoteRequestDefaults struct {
	Model       string                 `json:"model"`
	Temperature *float64               `json:"temperature"`
	MaxTokens   *int                   `json:"max_tokens"`
	TopP        *float64               `json:"top_p"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

// RemoteMessageConversion duplicates MessageConversion.
type RemoteMessageConversion struct {
	IncludeToolCallID        bool   `json:"include_tool_call_id"`
	ConvertToolRoleToUser    bool   `json:"convert_tool_role_to_user"`
	ReasoningContentField    string `json:"reasoning_content_field"`
	ArgumentsAsJSON          bool   `json:"arguments_as_json"`
	SkipToolExecutionSummary bool   `json:"skip_tool_execution_summary"`
	ForceToolCallType        string `json:"force_tool_call_type"`
}

// RemoteStreamingConfig duplicates StreamingConfig.
type RemoteStreamingConfig struct {
	Format         string `json:"format"`
	ChunkTimeoutMs int    `json:"chunk_timeout_ms"`
	DoneMarker     string `json:"done_marker"`
}

// RemotePatternOverride duplicates PatternOverride.
type RemotePatternOverride struct {
	Pattern      string `json:"pattern"`
	ContextLimit int    `json:"context_limit"`
}

// RemoteModelInfo duplicates ModelInfo.
type RemoteModelInfo struct {
	ID            string   `json:"id"`
	Name          string   `json:"name,omitempty"`
	Description   string   `json:"description,omitempty"`
	ContextLength int      `json:"context_length"`
	Tags          []string `json:"tags,omitempty"`
}

// RemoteModelConfig duplicates ModelConfig.
type RemoteModelConfig struct {
	DefaultContextLimit        int                   `json:"default_context_limit"`
	DefaultMaxCompletionTokens int                   `json:"default_max_completion_tokens,omitempty"`
	ModelOverrides             map[string]int        `json:"model_overrides"`
	MaxCompletionOverrides     map[string]int        `json:"max_completion_overrides,omitempty"`
	PatternOverrides           []RemotePatternOverride `json:"pattern_overrides"`
	CompletionPatternOverrides []RemotePatternOverride `json:"completion_pattern_overrides,omitempty"`
	ModelInfo                  []RemoteModelInfo     `json:"model_info,omitempty"`
	ContextLimit               int                   `json:"context_limit,omitempty"`
	SupportsVision             bool                  `json:"supports_vision"`
	VisionModel                string                `json:"vision_model"`
	DefaultModel               string                `json:"default_model"`
	AvailableModels            []string              `json:"available_models"`
}

// RemoteRetryConfig duplicates RetryConfig.
type RemoteRetryConfig struct {
	MaxAttempts       int      `json:"max_attempts"`
	BaseDelayMs       int      `json:"base_delay_ms"`
	BackoffMultiplier float64  `json:"backoff_multiplier"`
	MaxDelayMs        int      `json:"max_delay_ms"`
	RetryableErrors   []string `json:"retryable_errors"`
}

// RemoteCostConfig duplicates CostConfig.
type RemoteCostConfig struct {
	InputTokenCost  float64 `json:"input_token_cost"`
	OutputTokenCost float64 `json:"output_token_cost"`
	Currency        string  `json:"currency"`
}

// RemoteProviderConfig duplicates ProviderConfig for remote JSON consumption.
type RemoteProviderConfig struct {
	Name       string                 `json:"name"`
	Endpoint   string                 `json:"endpoint"`
	Auth       RemoteAuthConfig       `json:"auth"`
	Headers    map[string]string      `json:"headers"`
	Defaults   RemoteRequestDefaults  `json:"defaults"`
	Conversion RemoteMessageConversion `json:"message_conversion"`
	Streaming  RemoteStreamingConfig  `json:"streaming"`
	Models     RemoteModelConfig      `json:"models"`
	Retry      RemoteRetryConfig      `json:"retry"`
	Cost       RemoteCostConfig       `json:"cost"`
}

// ToProviderConfig converts this remote config to a providers.ProviderConfig.
// The Key field in Auth is left empty (runtime-only, set by the credential resolver).
func (r *RemoteProviderConfig) ToProviderConfig() *providers.ProviderConfig {
	if r == nil {
		return nil
	}
	return &providers.ProviderConfig{
		Name:     r.Name,
		Endpoint: r.Endpoint,
		Auth: providers.AuthConfig{
			Type:   r.Auth.Type,
			EnvVar: r.Auth.EnvVar,
		},
		Headers:    copyStringMap(r.Headers),
		Defaults:   r.defaultsToNative(),
		Conversion: r.conversionToNative(),
		Streaming:  r.streamingToNative(),
		Models:     r.modelsToNative(),
		Retry:      r.retryToNative(),
		Cost:       r.costToNative(),
	}
}

func (r *RemoteProviderConfig) defaultsToNative() providers.RequestDefaults {
	rd := providers.RequestDefaults{
		Model:      r.Defaults.Model,
		Parameters: copyInterfaceMap(r.Defaults.Parameters),
	}
	if r.Defaults.Temperature != nil {
		v := *r.Defaults.Temperature
		rd.Temperature = &v
	}
	if r.Defaults.MaxTokens != nil {
		v := *r.Defaults.MaxTokens
		rd.MaxTokens = &v
	}
	if r.Defaults.TopP != nil {
		v := *r.Defaults.TopP
		rd.TopP = &v
	}
	return rd
}

func (r *RemoteProviderConfig) conversionToNative() providers.MessageConversion {
	return providers.MessageConversion{
		IncludeToolCallID:        r.Conversion.IncludeToolCallID,
		ConvertToolRoleToUser:    r.Conversion.ConvertToolRoleToUser,
		ReasoningContentField:    r.Conversion.ReasoningContentField,
		ArgumentsAsJSON:          r.Conversion.ArgumentsAsJSON,
		SkipToolExecutionSummary: r.Conversion.SkipToolExecutionSummary,
		ForceToolCallType:        r.Conversion.ForceToolCallType,
	}
}

func (r *RemoteProviderConfig) streamingToNative() providers.StreamingConfig {
	return providers.StreamingConfig{
		Format:         r.Streaming.Format,
		ChunkTimeoutMs: r.Streaming.ChunkTimeoutMs,
		DoneMarker:     r.Streaming.DoneMarker,
	}
}

func (r *RemoteProviderConfig) modelsToNative() providers.ModelConfig {
	mc := providers.ModelConfig{
		DefaultContextLimit:        r.Models.DefaultContextLimit,
		DefaultMaxCompletionTokens: r.Models.DefaultMaxCompletionTokens,
		ModelOverrides:             copyIntMap(r.Models.ModelOverrides),
		MaxCompletionOverrides:     copyIntMap(r.Models.MaxCompletionOverrides),
		ContextLimit:               r.Models.ContextLimit,
		SupportsVision:             r.Models.SupportsVision,
		VisionModel:                r.Models.VisionModel,
		DefaultModel:               r.Models.DefaultModel,
		AvailableModels:            copyStringSlice(r.Models.AvailableModels),
	}

	for _, po := range r.Models.PatternOverrides {
		mc.PatternOverrides = append(mc.PatternOverrides, providers.PatternOverride{
			Pattern:      po.Pattern,
			ContextLimit: po.ContextLimit,
		})
	}
	for _, po := range r.Models.CompletionPatternOverrides {
		mc.CompletionPatternOverrides = append(mc.CompletionPatternOverrides, providers.PatternOverride{
			Pattern:      po.Pattern,
			ContextLimit: po.ContextLimit,
		})
	}
	for _, mi := range r.Models.ModelInfo {
		mc.ModelInfo = append(mc.ModelInfo, providers.ModelInfo{
			ID:            mi.ID,
			Name:          mi.Name,
			Description:   mi.Description,
			ContextLength: mi.ContextLength,
			Tags:          copyStringSlice(mi.Tags),
		})
	}
	return mc
}

func (r *RemoteProviderConfig) retryToNative() providers.RetryConfig {
	return providers.RetryConfig{
		MaxAttempts:       r.Retry.MaxAttempts,
		BaseDelayMs:       r.Retry.BaseDelayMs,
		BackoffMultiplier: r.Retry.BackoffMultiplier,
		MaxDelayMs:        r.Retry.MaxDelayMs,
		RetryableErrors:   copyStringSlice(r.Retry.RetryableErrors),
	}
}

func (r *RemoteProviderConfig) costToNative() providers.CostConfig {
	return providers.CostConfig{
		InputTokenCost:  r.Cost.InputTokenCost,
		OutputTokenCost: r.Cost.OutputTokenCost,
		Currency:        r.Cost.Currency,
	}
}

// Helper copy functions to avoid mutating shared data.
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyIntMap(m map[string]int) map[string]int {
	if m == nil {
		return nil
	}
	out := make(map[string]int, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyInterfaceMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

func copyStringSlice(s []string) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s))
	copy(out, s)
	return out
}

var (
	mu            sync.RWMutex
	cache         = make(map[string]*cachedConfig)
	negativeCache = make(map[string]time.Time)
	baseURL       string
	ttl           = defaultTTL
	negativeTTL   = defaultNegativeTTL
	httpTimeout   = defaultHTTPTimeout
	sf            singleflight.Group
)

func init() {
	loadConfig()
}

func loadConfig() {
	if v := strings.TrimSpace(envutil.GetEnvSimple("PROVIDER_REGISTRY_URL")); v != "" {
		lower := strings.ToLower(v)
		if lower == "off" || lower == "none" || lower == "disabled" {
			baseURL = ""
		} else {
			baseURL = strings.TrimRight(v, "/")
		}
	} else {
		baseURL = defaultRegistryURL
	}
	if v := strings.TrimSpace(envutil.GetEnvSimple("PROVIDER_REGISTRY_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			ttl = d
		}
	}
	if v := strings.TrimSpace(envutil.GetEnvSimple("PROVIDER_REGISTRY_NEGATIVE_TTL")); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			negativeTTL = d
		}
	}
	if v := strings.TrimSpace(envutil.GetEnvSimple("PROVIDER_REGISTRY_TIMEOUT")); v != "" {
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

// SetNegativeTTL sets the negative cache TTL for 404 responses (useful for testing).
func SetNegativeTTL(d time.Duration) {
	mu.Lock()
	defer mu.Unlock()
	negativeTTL = d
}

// IsEnabled returns true if the registry URL is configured and not disabled.
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

// negativeTTLCopy returns a copy of the negative cache TTL under read lock.
func negativeTTLCopy() time.Duration {
	mu.RLock()
	defer mu.RUnlock()
	return negativeTTL
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

// FetchProviderConfig returns a provider connection config from the remote registry.
//
// Return values:
//   - (config, nil): config from registry or cache
//   - (nil, nil): registry disabled, provider not found (404/negative cache)
//   - (nil, err): hard error (invalid provider ID, non-404 HTTP errors)
//
// Caching behavior:
//   - Successful responses are cached for the configured TTL (default 5 minutes)
//   - 404 responses are cached in a negative cache for negativeTTL (default 30 seconds)
//   - Singleflight deduplicates concurrent requests for the same provider
//   - Use ClearCache() to manually invalidate all cached entries
func FetchProviderConfig(ctx context.Context, providerID string) (*RemoteProviderConfig, error) {
	if !IsEnabled() {
		return nil, nil
	}

	providerID = strings.TrimSpace(strings.ToLower(providerID))
	if !isValidProviderID(providerID) {
		return nil, fmt.Errorf("providerregistry: invalid provider ID %q", providerID)
	}

	// Check cache and negative cache under a single lock to avoid TOCTOU window.
	mu.RLock()
	entry, ok := cache[providerID]
	if ok && time.Since(entry.fetchedAt) < ttlCopy() {
		mu.RUnlock()
		return cloneConfig(entry), nil
	}
	negHit, negOk := negativeCache[providerID]
	mu.RUnlock()
	if negOk && time.Since(negHit) < negativeTTLCopy() {
		return nil, nil
	}

	// Use singleflight to deduplicate concurrent requests for the same provider.
	result, err, _ := sf.Do(providerID, func() (interface{}, error) {
		// Double-check cache and negative cache after acquiring singleflight lock.
		mu.RLock()
		entry, ok := cache[providerID]
		if ok && time.Since(entry.fetchedAt) < ttlCopy() {
			mu.RUnlock()
			return cloneConfig(entry), nil
		}
		negHit, negOk := negativeCache[providerID]
		mu.RUnlock()
		if negOk && time.Since(negHit) < negativeTTLCopy() {
			return nil, nil
		}

		// Fetch from registry.
		url := baseURLCopy() + "/providers/" + providerID + ".json"

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if reqErr != nil {
			return nil, fmt.Errorf("providerregistry: create request: %w", reqErr)
		}
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", "sprout-provider-registry/1.0")

		client := &http.Client{Timeout: httpTimeoutCopy()}
		resp, fetchErr := client.Do(req)
		if fetchErr != nil {
			return nil, fmt.Errorf("providerregistry: fetch %s: %w", providerID, fetchErr)
		}
		defer resp.Body.Close()

		if resp.StatusCode == http.StatusNotFound {
			if envutil.GetEnvSimple("DEBUG_REGISTRY") != "" {
				log.Printf("[providerregistry] provider %q not found at %s/providers/%s.json (404), falling back to embedded config", providerID, baseURLCopy(), providerID)
			}
			mu.Lock()
			negativeCache[providerID] = time.Now()
			mu.Unlock()
			return nil, nil
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("providerregistry: fetch %s: HTTP %d", providerID, resp.StatusCode)
		}

		body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
		if readErr != nil {
			return nil, fmt.Errorf("providerregistry: read %s: %w", providerID, readErr)
		}

		var config RemoteProviderConfig
		if decodeErr := json.Unmarshal(body, &config); decodeErr != nil {
			return nil, fmt.Errorf("providerregistry: decode %s: %w", providerID, decodeErr)
		}

		// Store in cache.
		cached := &cachedConfig{config: config, fetchedAt: time.Now()}
		mu.Lock()
		cache[providerID] = cached
		mu.Unlock()

		return cloneConfig(cached), nil
	})

	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, nil
	}
	return result.(*RemoteProviderConfig), nil
}

// FetchAllProviders fetches all provider configs from the registry.
//
// It first fetches the index file ({baseURL}/providers/index.json), then
// concurrently fetches each provider file. Individual failures are silently
// skipped (partial results OK). If the index fetch fails, returns nil map
// and nil error (graceful degradation).
//
// Returns a map keyed by provider ID.
func FetchAllProviders(ctx context.Context) (map[string]*RemoteProviderConfig, error) {
	if !IsEnabled() {
		return nil, nil
	}

	// Fetch index.
	url := baseURLCopy() + "/providers/index.json"

	req, reqErr := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if reqErr != nil {
		return nil, nil
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sprout-provider-registry/1.0")

	client := &http.Client{Timeout: defaultIndexTimeout}
	resp, fetchErr := client.Do(req)
	if fetchErr != nil {
		if envutil.GetEnvSimple("DEBUG_REGISTRY") != "" {
			log.Printf("[providerregistry] index fetch failed: %v", fetchErr)
		}
		return nil, nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		if envutil.GetEnvSimple("DEBUG_REGISTRY") != "" {
			log.Printf("[providerregistry] index fetch returned HTTP %d", resp.StatusCode)
		}
		return nil, nil
	}

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if readErr != nil {
		return nil, nil
	}

	var index struct {
		Providers []string `json:"providers"`
	}
	if decodeErr := json.Unmarshal(body, &index); decodeErr != nil {
		return nil, nil
	}

	if len(index.Providers) == 0 {
		return nil, nil
	}

	// Batch-fetch all provider files concurrently.
	results := make(map[string]*RemoteProviderConfig)
	resultsMu := sync.Mutex{}

	var wg sync.WaitGroup
	for _, pid := range index.Providers {
		wg.Add(1)
		go func(pid string) {
			defer wg.Done()
			cfg, err := FetchProviderConfig(ctx, pid)
			if err != nil {
				if envutil.GetEnvSimple("DEBUG_REGISTRY") != "" {
					log.Printf("[providerregistry] provider %q fetch error: %v", pid, err)
				}
				return // Hard error — skip this provider
			}
			if cfg == nil {
				if envutil.GetEnvSimple("DEBUG_REGISTRY") != "" {
					log.Printf("[providerregistry] provider %q not found (404 or negative cache)", pid)
				}
				return // Not found or cached miss — skip
			}
			resultsMu.Lock()
			results[pid] = cfg
			resultsMu.Unlock()
		}(pid)
	}
	wg.Wait()

	if len(results) == 0 {
		return nil, nil
	}
	return results, nil
}

// cachedConfig wraps a RemoteProviderConfig with its fetch time.
type cachedConfig struct {
	config    RemoteProviderConfig
	fetchedAt time.Time
}

// cloneConfig returns a deep copy of the cached config to avoid shared mutations.
func cloneConfig(c *cachedConfig) *RemoteProviderConfig {
	data, _ := json.Marshal(&c.config)
	var out RemoteProviderConfig
	_ = json.Unmarshal(data, &out)
	return &out
}

// ClearCache removes all cached entries.
func ClearCache() {
	mu.Lock()
	defer mu.Unlock()
	cache = make(map[string]*cachedConfig)
	negativeCache = make(map[string]time.Time)
}
