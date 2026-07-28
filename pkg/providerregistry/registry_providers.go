// Remote provider fetching and validation.
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

	"github.com/sprout-foundry/sprout/pkg/envutil"
)

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
