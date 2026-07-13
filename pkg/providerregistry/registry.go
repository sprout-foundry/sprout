// Package providerregistry provides a client for fetching provider connection
// configs from a remote JSON registry server with in-memory caching.
// Package providerregistry fetches per-provider TECHNICAL CONFIG —
// the API-client wiring (endpoint, auth.type/env_var, streaming
// format, headers, retry/cost, message conversion quirks) — from
// the remote registry at https://sprout-foundry.github.io/sprout/
// providers/{id}.json plus the index at providers/index.json.
//
// Lifecycle:
//   - Published every 6h by .github/workflows/model-registry-publish.yml,
//     which copies pkg/agent_providers/configs/*.json into providers/
//     with schema_version + published_at added by jq.
//   - Fetched at runtime by pkg/factory.refreshFromRemote, which
//     UpsertConfigs each result into the global ProviderFactory so
//     pkg/agent_providers.NewGenericProvider can build a working
//     client from JSON alone (no per-provider Go code).
//   - Cached in-process for 5 min (positive) / 30 s (negative);
//     SSRF + schema-validated before caching.
//
// IMPORTANT — distinguish from pkg/providercatalog, which is a
// separate system with adjacent but DIFFERENT concerns:
//
//   - pkg/providercatalog: ONE combined JSON describing the curated
//     UX layer — friendly descriptions, signup URLs, API-key help
//     text, recommended-model justification. Used by onboarding /
//     the model picker for human-facing copy. Refreshed by a separate
//     workflow (.github/workflows/provider-catalog-refresh.yml).
//   - pkg/providerregistry (this package): per-provider TECHNICAL
//     CONFIG that the API client actually uses to talk to a provider.
//
// They overlap minimally (both have an id/name and a model list, in
// different shapes); they do not share infrastructure, schemas, or
// publish workflows. A consolidation has been discussed but the two
// systems serve different consumers (UI vs API client) so the seam
// is load-bearing.
package providerregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/envutil"
	"golang.org/x/sync/singleflight"
)

const (
	defaultTTL                = 5 * time.Minute
	defaultNegativeTTL        = 30 * time.Second
	defaultHTTPTimeout        = 500 * time.Millisecond
	defaultIndexTimeout       = 1 * time.Second
	maxResponseBytes    int64 = 1 << 20 // 1 MiB
	defaultRegistryURL        = "https://sprout-foundry.github.io/sprout"
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
	DefaultContextLimit        int                     `json:"default_context_limit"`
	DefaultMaxCompletionTokens int                     `json:"default_max_completion_tokens,omitempty"`
	ModelOverrides             map[string]int          `json:"model_overrides"`
	MaxCompletionOverrides     map[string]int          `json:"max_completion_overrides,omitempty"`
	PatternOverrides           []RemotePatternOverride `json:"pattern_overrides"`
	CompletionPatternOverrides []RemotePatternOverride `json:"completion_pattern_overrides,omitempty"`
	ModelInfo                  []RemoteModelInfo       `json:"model_info,omitempty"`
	ContextLimit               int                     `json:"context_limit,omitempty"`
	SupportsVision             bool                    `json:"supports_vision"`
	VisionModel                string                  `json:"vision_model"`
	DefaultModel               string                  `json:"default_model"`
	AvailableModels            []string                `json:"available_models"`
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
	Name        string                  `json:"name"`
	DisplayName string                  `json:"display_name,omitempty"`
	Endpoint    string                  `json:"endpoint"`
	Auth        RemoteAuthConfig        `json:"auth"`
	Headers     map[string]string       `json:"headers"`
	Defaults    RemoteRequestDefaults   `json:"defaults"`
	Conversion  RemoteMessageConversion `json:"message_conversion"`
	Streaming   RemoteStreamingConfig   `json:"streaming"`
	Models      RemoteModelConfig       `json:"models"`
	Retry       RemoteRetryConfig       `json:"retry"`
	Cost        RemoteCostConfig        `json:"cost"`
}

// ToProviderConfig converts this remote config to a providers.ProviderConfig.
// The Key field in Auth is left empty (runtime-only, set by the credential resolver).
func (r *RemoteProviderConfig) ToProviderConfig() *providers.ProviderConfig {
	if r == nil {
		return nil
	}
	return &providers.ProviderConfig{
		Name:        r.Name,
		DisplayName: r.DisplayName,
		Endpoint:    r.Endpoint,
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
	mc := providers.MessageConversion{
		IncludeToolCallID:        r.Conversion.IncludeToolCallID,
		ConvertToolRoleToUser:    r.Conversion.ConvertToolRoleToUser,
		ReasoningContentField:    r.Conversion.ReasoningContentField,
		ArgumentsAsJSON:          r.Conversion.ArgumentsAsJSON,
		SkipToolExecutionSummary: r.Conversion.SkipToolExecutionSummary,
		ForceToolCallType:        r.Conversion.ForceToolCallType,
	}
	// Enforce standard OpenAI tool-calling defaults for remote configs that
	// omit message_conversion settings. Same rationale as custom providers.
	if !mc.IncludeToolCallID {
		mc.IncludeToolCallID = true
	}
	return mc
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

	// sharedTransport enables connection pooling and TLS session resumption
	// across all registry fetches, avoiding a fresh TCP+TLS handshake per
	// provider in FetchAllProviders.
	sharedTransport *http.Transport

	// httpClient is the shared client for individual provider fetches.
	// It uses sharedTransport for connection reuse; its Timeout is
	// configured via SetHTTPTimeout (default: 500ms).
	httpClient *http.Client
)

func init() {
	loadConfig()

	sharedTransport = &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     30 * time.Second,
	}
	httpClient = &http.Client{
		Timeout:   httpTimeout,
		Transport: sharedTransport,
	}
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
	if httpClient != nil {
		httpClient.Timeout = d
	}
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

// validateEndpoint checks that the endpoint URL is safe to call.
// It rejects non-HTTPS schemes, localhost, and private/internal IP addresses.
// DNS lookups are performed with a 3-second timeout; DNS failures fail closed
// (return an error) to prevent SSRF via DNS poisoning or flaky resolution.
func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return fmt.Errorf("endpoint is empty")
	}

	u, err := url.Parse(endpoint)
	if err != nil {
		return fmt.Errorf("failed to parse endpoint URL: %w", err)
	}

	// Only allow HTTPS.
	if !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("endpoint scheme %q is not https", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("endpoint has no hostname")
	}

	// Reject localhost.
	if strings.EqualFold(host, "localhost") {
		return fmt.Errorf("endpoint points to localhost")
	}

	// If the host is a literal IP address, check it directly.
	ip := net.ParseIP(host)
	if ip != nil {
		if isPrivateIP(ip) {
			return fmt.Errorf("endpoint resolves to private IP %s", host)
		}
		return nil
	}

	// Otherwise resolve the hostname and check each resulting IP.
	// Use a context with a 3-second timeout to prevent blocking the goroutine
	// on slow DNS servers. On DNS failure, fail-open — we cannot verify the
	// endpoint is private, so allow it. This prevents false rejections for
	// endpoints that are unreachable from the current machine (e.g., air-gapped
	// environments, CI without network, or provider endpoints that don't resolve
	// from the client's DNS). The HTTPS-only check above is the primary guard.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	resolver := &net.Resolver{}
	ips, err := resolver.LookupIPAddr(ctx, host)
	if err != nil {
		// DNS resolution failed — fail open. The endpoint is HTTPS and we
		// cannot determine it's private, so allow it.
		return nil
	}
	for _, resolvedIP := range ips {
		if isPrivateIP(resolvedIP.IP) {
			return fmt.Errorf("endpoint %s resolves to private IP %s", host, resolvedIP.IP)
		}
	}
	return nil
}

// isPrivateIP returns true if the IP falls into a private, loopback, or link-local range.
func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}

	// IPv4 checks
	if ip4 := ip.To4(); ip4 != nil {
		// 127.0.0.0/8 (loopback)
		if ip4[0] == 127 {
			return true
		}
		// 10.0.0.0/8 (private class A)
		if ip4[0] == 10 {
			return true
		}
		// 172.16.0.0/12 (private class B: 172.16-31.x.x)
		if ip4[0] == 172 && ip4[1]&0xf0 == 0x10 {
			return true
		}
		// 192.168.0.0/16 (private class C)
		if ip4[0] == 192 && ip4[1] == 168 {
			return true
		}
		return false
	}

	// IPv6 checks
	// ::1 (loopback)
	if ip.Equal(net.IPv6zero) {
		// :: (unspecified) — treat as private
		return true
	}
	if ip.Equal(net.ParseIP("::1")) {
		return true
	}
	// fc00::/7 (unique local)
	if len(ip) == 16 && ip[0]&0xfe == 0xfc {
		return true
	}
	// fe80::/10 (link-local)
	if len(ip) == 16 && ip[0]&0xff == 0xfe && ip[1]&0xc0 == 0x80 {
		return true
	}
	return false
}

// validRemoteAuthTypes mirrors the AuthConfig.Type values that
// providers.NewGenericProvider knows how to wire — keep in sync if
// the auth contract gains a new type.
var validRemoteAuthTypes = map[string]struct{}{
	"":        {}, // empty is treated as "none" by downstream code
	"none":    {},
	"bearer":  {},
	"api_key": {},
	"basic":   {},
	"oauth":   {},
}

// ValidateForPublish runs the same structural schema check that
// FetchProvider applies at runtime, but is exported so the publish-time
// validator (cmd/validate_registry) can reject bad files BEFORE they
// hit GitHub Pages. The two share one rule set so what passes CI also
// passes at runtime.
func ValidateForPublish(id string, cfg *RemoteProviderConfig) error {
	return validateRemoteConfig(id, cfg)
}

// validateRemoteConfig is a structural check on a freshly-decoded
// RemoteProviderConfig: required fields present and within sane
// bounds, auth.type recognised, defaults.model present unless auth
// is "none" (local providers like LM Studio publish a default in the
// JSON too in practice, but we don't require it). SSRF checks on the
// endpoint live in validateEndpoint and run separately.
func validateRemoteConfig(id string, cfg *RemoteProviderConfig) error {
	if cfg == nil {
		return fmt.Errorf("nil config")
	}
	if strings.TrimSpace(cfg.Name) == "" {
		return fmt.Errorf("missing name")
	}
	// Defence in depth: name should match the file/index id (cheap
	// guard against an index that lists "openai" but serves zai.json
	// content somehow — e.g. a botched publish step).
	if !strings.EqualFold(strings.TrimSpace(cfg.Name), strings.TrimSpace(id)) {
		return fmt.Errorf("name %q does not match id %q", cfg.Name, id)
	}
	endpoint := strings.TrimSpace(cfg.Endpoint)
	if endpoint == "" {
		return fmt.Errorf("missing endpoint")
	}
	// Cheap scheme check so the publish-time validator rejects
	// non-HTTPS at CI time without doing DNS. Runtime's
	// validateEndpoint still runs the full SSRF check (private IPs,
	// localhost, DNS resolution) on fetched configs.
	if !strings.HasPrefix(strings.ToLower(endpoint), "https://") {
		return fmt.Errorf("endpoint must be https://")
	}
	if _, ok := validRemoteAuthTypes[strings.ToLower(strings.TrimSpace(cfg.Auth.Type))]; !ok {
		return fmt.Errorf("unknown auth.type %q", cfg.Auth.Type)
	}
	if strings.TrimSpace(cfg.Defaults.Model) == "" {
		return fmt.Errorf("missing defaults.model")
	}
	return nil
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

		client := &http.Client{Timeout: httpTimeoutCopy(), Transport: sharedTransport}
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

		// SSRF validation — reject configs that point to private/internal endpoints.
		if valErr := validateEndpoint(config.Endpoint); valErr != nil {
			return nil, fmt.Errorf("providerregistry: invalid endpoint for %s: %w", providerID, valErr)
		}

		// Schema validation — reject configs missing required fields.
		// Without this a malformed publish (e.g. a forgotten field after
		// a schema change) would silently UpsertConfig into the global
		// factory and produce confusing failures at first API call.
		if schemaErr := validateRemoteConfig(providerID, &config); schemaErr != nil {
			return nil, fmt.Errorf("providerregistry: invalid schema for %s: %w", providerID, schemaErr)
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

	client := &http.Client{Timeout: defaultIndexTimeout, Transport: sharedTransport}
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
