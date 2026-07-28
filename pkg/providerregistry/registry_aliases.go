// Remote provider cache and configuration.
package providerregistry

import (
	"net/http"
	"strings"
	"sync"
	"time"

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

// ClearCache removes all cached entries.
func ClearCache() {
	mu.Lock()
	defer mu.Unlock()
	cache = make(map[string]*cachedConfig)
	negativeCache = make(map[string]time.Time)
}
