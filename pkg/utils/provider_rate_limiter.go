package utils

import (
	"strings"
	"sync"
)

// ProviderRateLimiter manages a global registry of token buckets per provider.
// This ensures that all requests to the same provider are rate limited together,
// preventing cascading 429 errors when multiple subagents are running concurrently.
type ProviderRateLimiter struct {
	limiters sync.Map // maps provider name (lowercase) to *TokenBucket
}

var (
	globalProviderRateLimiter = &ProviderRateLimiter{}
)

// GetProviderRateLimiter returns the rate limiter for the specified provider.
// If no limiter exists for the provider, one is created with default rates.
// Provider names are case-insensitive.
//
// Default rates (tokens per second, burst):
// - openai: 1.0 tps (60 RPM), burst 5
// - openrouter: 2.0 tps (120 RPM), burst 10
// - deepinfra: 1.0 tps (60 RPM), burst 5
// - deepseek: 0.5 tps (30 RPM), burst 3
// - ollama/ollama-local/ollama-cloud: 10.0 tps (600 RPM), burst 20
// - zai: 2.0 tps (120 RPM), burst 10
// - chutes: 2.0 tps (120 RPM), burst 10
// - lmstudio: 10.0 tps (600 RPM), burst 20
// - mistral: 1.0 tps (60 RPM), burst 5
// - cerebras: 2.0 tps (120 RPM), burst 10
// - Default: 0.5 tps (30 RPM), burst 3
func GetProviderRateLimiter(providerName string) *TokenBucket {
	// Normalize provider name to lowercase
	providerKey := strings.ToLower(providerName)

	// Try to get existing limiter
	if limiter, ok := globalProviderRateLimiter.limiters.Load(providerKey); ok {
		return limiter.(*TokenBucket)
	}

	// Create new limiter with default rates
	rate, burst := getDefaultRateForProvider(providerName)

	// For test providers, use unlimited rate to avoid slowing down tests
	if strings.Contains(providerKey, "test") {
		rate = 0 // Unlimited
		burst = 0
	}

	limiter := NewTokenBucket(rate, burst)

	// Store it (use LoadOrStore for atomicity)
	actual, _ := globalProviderRateLimiter.limiters.LoadOrStore(providerKey, limiter)
	return actual.(*TokenBucket)
}

// SetProviderRate sets or updates the rate and burst for a specific provider.
// Provider names are case-insensitive.
// This can be used to override default rates based on configuration.
func SetProviderRate(providerName string, rate float64, burst int) {
	providerKey := strings.ToLower(providerName)

	// Use LoadOrStore for atomic creation
	val, _ := globalProviderRateLimiter.limiters.LoadOrStore(providerKey, NewTokenBucket(rate, burst))
	limiter := val.(*TokenBucket)

	// Update the rate
	limiter.UpdateRate(rate, burst)
}

// getDefaultRateForProvider returns default rate and burst for a provider.
// Rate is in tokens per second.
func getDefaultRateForProvider(providerName string) (rate float64, burst int) {
	providerLower := strings.ToLower(providerName)

	switch {
	case providerLower == "openai":
		return 1.0, 5 // 60 RPM

	case providerLower == "openrouter":
		return 2.0, 10 // 120 RPM

	case providerLower == "deepinfra":
		return 1.0, 5 // 60 RPM

	case providerLower == "deepseek":
		return 0.5, 3 // 30 RPM

	case providerLower == "ollama" || providerLower == "ollama-local" || providerLower == "ollama-cloud":
		return 10.0, 20 // 600 RPM

	case providerLower == "zai":
		return 2.0, 10 // 120 RPM

	case providerLower == "chutes":
		return 2.0, 10 // 120 RPM

	case providerLower == "lmstudio":
		return 10.0, 20 // 600 RPM (local, like ollama)

	case providerLower == "mistral":
		return 1.0, 5 // 60 RPM

	case providerLower == "cerebras":
		return 2.0, 10 // 120 RPM

	default:
		// Conservative default for unknown providers
		return 0.5, 3 // 30 RPM
	}
}

// GetAllProviderLimiters returns a snapshot of all provider limiters and their settings.
// This is useful for debugging and monitoring.
func GetAllProviderLimiters() map[string]TokenBucketInfo {
	result := make(map[string]TokenBucketInfo)

	globalProviderRateLimiter.limiters.Range(func(key, value interface{}) bool {
		provider := key.(string)
		limiter := value.(*TokenBucket)

		result[provider] = TokenBucketInfo{
			Rate:            limiter.GetRate(),
			Burst:           limiter.GetBurst(),
			AvailableTokens: limiter.GetAvailableTokens(),
		}
		return true
	})

	return result
}

// TokenBucketInfo contains information about a token bucket's state.
type TokenBucketInfo struct {
	Rate            float64 // tokens per second
	Burst           int     // maximum tokens
	AvailableTokens float64 // approximate tokens currently available
}

// RemoveProviderRateLimiter removes the rate limiter for a specific provider.
// This is primarily useful for testing.
func RemoveProviderRateLimiter(providerName string) {
	providerKey := strings.ToLower(providerName)
	globalProviderRateLimiter.limiters.Delete(providerKey)
}
