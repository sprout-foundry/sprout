package utils

import (
	"sync"
	"testing"
)

func TestGetProviderRateLimiterSameProvider(t *testing.T) {
	// Clean up any existing limiters for this test
	RemoveProviderRateLimiter("providerA")

	// Get limiter for same provider multiple times
	limiter1 := GetProviderRateLimiter("ProviderA")
	limiter2 := GetProviderRateLimiter("providera")
	limiter3 := GetProviderRateLimiter("PROVIDERA")

	// Should return the same limiter (case-insensitive)
	if limiter1 != limiter2 || limiter1 != limiter3 {
		t.Error("Expected same limiter instance for same provider (case-insensitive)")
	}

	// Should have default rate for unknown provider (0.5 tps, burst 3)
	if limiter1.GetRate() != 0.5 {
		t.Errorf("Expected default rate 0.5, got %f", limiter1.GetRate())
	}
	if limiter1.GetBurst() != 3 {
		t.Errorf("Expected default burst 3, got %d", limiter1.GetBurst())
	}

	// Cleanup
	RemoveProviderRateLimiter("providera")
}

func TestGetProviderRateLimiterDifferentProviders(t *testing.T) {
	// Clean up any existing limiters
	RemoveProviderRateLimiter("provider1")
	RemoveProviderRateLimiter("provider2")

	limiter1 := GetProviderRateLimiter("provider1")
	limiter2 := GetProviderRateLimiter("provider2")

	// Should return different limiters for different providers
	if limiter1 == limiter2 {
		t.Error("Expected different limiter instances for different providers")
	}

	// Both should have default settings for unknown providers
	if limiter1.GetRate() != limiter2.GetRate() {
		t.Error("Expected same default rate for unknown providers")
	}

	// Cleanup
	RemoveProviderRateLimiter("provider1")
	RemoveProviderRateLimiter("provider2")
}

func TestGetProviderRateLimiterDefaults(t *testing.T) {
	testCases := []struct {
		provider      string
		expectedRate  float64
		expectedBurst int
	}{
		{"openai", 1.0, 5},
		{"OPENAI", 1.0, 5}, // case insensitive
		{"openrouter", 2.0, 10},
		{"OpenRouter", 2.0, 10},
		{"deepinfra", 1.0, 5},
		{"deepseek", 0.5, 3},
		{"ollama", 10.0, 20},
		{"ollama-local", 10.0, 20},
		{"ollama-cloud", 10.0, 20},
		{"zai", 2.0, 10},
		{"chutes", 2.0, 10},
		{"lmstudio", 10.0, 20},
		{"mistral", 1.0, 5},
		{"cerebras", 2.0, 10},
		{"unknown-provider", 0.5, 3},
	}

	for _, tc := range testCases {
		t.Run(tc.provider, func(t *testing.T) {
			RemoveProviderRateLimiter(tc.provider)
			defer RemoveProviderRateLimiter(tc.provider)

			limiter := GetProviderRateLimiter(tc.provider)

			if limiter.GetRate() != tc.expectedRate {
				t.Errorf("Provider %s: expected rate %f, got %f",
					tc.provider, tc.expectedRate, limiter.GetRate())
			}

			if limiter.GetBurst() != tc.expectedBurst {
				t.Errorf("Provider %s: expected burst %d, got %d",
					tc.provider, tc.expectedBurst, limiter.GetBurst())
			}
		})
	}
}

func TestSetProviderRate(t *testing.T) {
	provider := "set-rate-provider"
	RemoveProviderRateLimiter(provider)
	defer RemoveProviderRateLimiter(provider)

	// Get default limiter
	limiter := GetProviderRateLimiter(provider)
	if limiter.GetRate() != 0.5 || limiter.GetBurst() != 3 {
		t.Errorf("Expected default rate/burst, got %f/%d", limiter.GetRate(), limiter.GetBurst())
	}

	// Update rate
	SetProviderRate(provider, 5.0, 20)

	// Get limiter again - should be the same instance with updated rate
	limiter2 := GetProviderRateLimiter(provider)
	if limiter != limiter2 {
		t.Error("Expected same limiter instance after rate update")
	}

	if limiter.GetRate() != 5.0 {
		t.Errorf("Expected rate 5.0 after update, got %f", limiter.GetRate())
	}

	if limiter.GetBurst() != 20 {
		t.Errorf("Expected burst 20 after update, got %d", limiter.GetBurst())
	}
}

func TestSetProviderRateNewProvider(t *testing.T) {
	provider := "new-provider-rate"
	RemoveProviderRateLimiter(provider)
	defer RemoveProviderRateLimiter(provider)

	// Set rate for non-existent provider
	SetProviderRate(provider, 3.0, 10)

	// Get limiter - should have the specified rate
	limiter := GetProviderRateLimiter(provider)

	if limiter.GetRate() != 3.0 {
		t.Errorf("Expected rate 3.0, got %f", limiter.GetRate())
	}

	if limiter.GetBurst() != 10 {
		t.Errorf("Expected burst 10, got %d", limiter.GetBurst())
	}
}

func TestGetAllProviderLimiters(t *testing.T) {
	// Clean up test providers
	RemoveProviderRateLimiter("providerA")
	RemoveProviderRateLimiter("providerB")
	RemoveProviderRateLimiter("providerC")
	defer func() {
		RemoveProviderRateLimiter("providerA")
		RemoveProviderRateLimiter("providerB")
		RemoveProviderRateLimiter("providerC")
	}()

	// Create limiters for multiple providers
	GetProviderRateLimiter("providerA")
	GetProviderRateLimiter("providerB")
	GetProviderRateLimiter("providerC")

	// Override one provider's rate
	SetProviderRate("providerB", 5.0, 15)

	// Get all limiters
	all := GetAllProviderLimiters()

	// Check that our providers are present
	if _, ok := all["providera"]; !ok {
		t.Error("Expected providera to be in limiters")
	}
	if _, ok := all["providerb"]; !ok {
		t.Error("Expected providerb to be in limiters")
	}
	if _, ok := all["providerc"]; !ok {
		t.Error("Expected providerc to be in limiters")
	}

	// Check providerB's overridden rate
	providerBInfo := all["providerb"]
	if providerBInfo.Rate != 5.0 {
		t.Errorf("Expected providerB rate 5.0, got %f", providerBInfo.Rate)
	}
	if providerBInfo.Burst != 15 {
		t.Errorf("Expected providerB burst 15, got %d", providerBInfo.Burst)
	}

	// Check that available tokens are reported
	if providerBInfo.AvailableTokens <= 0 {
		t.Error("Expected positive available tokens")
	}
}

func TestProviderRateLimiterConcurrency(t *testing.T) {
	provider := "concurrent-provider"
	RemoveProviderRateLimiter(provider)
	defer RemoveProviderRateLimiter(provider)

	const numGoroutines = 50
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// Concurrently get limiter for the same provider
	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			limiter := GetProviderRateLimiter(provider)

			// Update rate concurrently
			SetProviderRate(provider, 2.0, 10)

			// Read rate
			_ = limiter.GetRate()
		}()
	}

	wg.Wait()

	// Verify we got one limiter with consistent rate
	limiter := GetProviderRateLimiter(provider)
	if limiter.GetRate() != 2.0 {
		t.Errorf("Expected rate 2.0 after concurrent updates, got %f", limiter.GetRate())
	}
	if limiter.GetBurst() != 10 {
		t.Errorf("Expected burst 10 after concurrent updates, got %d", limiter.GetBurst())
	}
}

func TestRemoveProviderRateLimiter(t *testing.T) {
	provider := "remove-provider"

	// Create limiter
	limiter1 := GetProviderRateLimiter(provider)

	// Remove it
	RemoveProviderRateLimiter(provider)

	// Get new limiter - should be different instance
	limiter2 := GetProviderRateLimiter(provider)

	if limiter1 == limiter2 {
		t.Error("Expected different limiter instance after removal")
	}

	// Cleanup
	RemoveProviderRateLimiter(provider)
}

func TestProviderRateLimiterIsolation(t *testing.T) {
	provider1 := "isolation-provider-1"
	provider2 := "isolation-provider-2"

	RemoveProviderRateLimiter(provider1)
	RemoveProviderRateLimiter(provider2)
	defer func() {
		RemoveProviderRateLimiter(provider1)
		RemoveProviderRateLimiter(provider2)
	}()

	// Get limiters for two providers
	limiter1 := GetProviderRateLimiter(provider1)
	limiter2 := GetProviderRateLimiter(provider2)

	// Update one provider's rate
	SetProviderRate(provider1, 10.0, 30)

	// Verify limiter1 was updated
	if limiter1.GetRate() != 10.0 {
		t.Errorf("Expected limiter1 rate 10.0, got %f", limiter1.GetRate())
	}

	// Verify limiter2 still has default rate
	if limiter2.GetRate() != 0.5 {
		t.Errorf("Expected limiter2 default rate 0.5, got %f", limiter2.GetRate())
	}
}

func TestGetProviderRateLimiterRealProviders(t *testing.T) {
	// Test with real provider names that will be used in production
	providers := []string{
		"openai",
		"openrouter",
		"deepinfra",
		"deepseek",
		"ollama",
		"zai",
		"chutes",
		"lmstudio",
		"mistral",
		"cerebras",
	}

	for _, provider := range providers {
		t.Run(provider, func(t *testing.T) {
			RemoveProviderRateLimiter(provider)
			defer RemoveProviderRateLimiter(provider)

			limiter := GetProviderRateLimiter(provider)

			// Verify it's not nil
			if limiter == nil {
				t.Fatalf("Expected non-nil limiter for %s", provider)
			}

			// Verify rate is positive
			if limiter.GetRate() <= 0 {
				t.Errorf("Expected positive rate for %s, got %f", provider, limiter.GetRate())
			}

			// Verify burst is positive
			if limiter.GetBurst() <= 0 {
				t.Errorf("Expected positive burst for %s, got %d", provider, limiter.GetBurst())
			}
		})
	}
}
