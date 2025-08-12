package llm

import (
	"testing"
)

func TestEstimateTokensCaching(t *testing.T) {
	// 1. Clear cache to ensure clean state
	ClearTokenCache()

	// 2. First call with specific string (expected cache miss)
	testString := "This is a test string for token estimation"
	tokens1 := EstimateTokens(testString)

	// 3. Assert cache stats show 0 hits and 1 miss
	stats := GetCacheStats()
	if stats.Hits != 0 || stats.Misses != 1 {
		t.Errorf("Expected 0 hits and 1 miss, got %d hits and %d misses", stats.Hits, stats.Misses)
	}

	// 4. Second call with same string (expected cache hit)
	tokens2 := EstimateTokens(testString)

	// 5. Assert cache stats show 1 hit and 1 miss
	stats = GetCacheStats()
	if stats.Hits != 1 || stats.Misses != 1 {
		t.Errorf("Expected 1 hit and 1 miss, got %d hits and %d misses", stats.Hits, stats.Misses)
	}

	// 6. Third call with different string (expected cache miss)
	differentString := "This is a different test string"
	tokens3 := EstimateTokens(differentString)

	// 7. Assert cache stats show 1 hit and 2 misses
	stats = GetCacheStats()
	if stats.Hits != 1 || stats.Misses != 2 {
		t.Errorf("Expected 1 hit and 2 misses, got %d hits and %d misses", stats.Hits, stats.Misses)
	}

	// 8. Verify token counts for identical strings are consistent
	if tokens1 != tokens2 {
		t.Errorf("Token counts for identical strings should be consistent: %d != %d", tokens1, tokens2)
	}

	// Verify tokens3 is different (different string should have different count)
	if tokens1 == tokens3 {
		t.Errorf("Token counts for different strings should be different: %d == %d", tokens1, tokens3)
	}
}
