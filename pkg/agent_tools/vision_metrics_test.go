package tools

import (
	"sync"
	"testing"
)

// TestIncVisionImageTokens verifies the counter increments correctly and
// that the global state is independent of goroutine ordering (atomic.Add).
func TestIncVisionImageTokens(t *testing.T) {
	before := GetVisionMetrics()

	const totalDelta = 100
	const cachedDelta = 25

	IncVisionImageTokens(totalDelta, cachedDelta)

	after := GetVisionMetrics()
	if after.ImageTokensTotal-before.ImageTokensTotal != int64(totalDelta) {
		t.Errorf("ImageTokensTotal delta = %d, want %d",
			after.ImageTokensTotal-before.ImageTokensTotal, totalDelta)
	}
	if after.ImageTokensCachedTotal-before.ImageTokensCachedTotal != int64(cachedDelta) {
		t.Errorf("ImageTokensCachedTotal delta = %d, want %d",
			after.ImageTokensCachedTotal-before.ImageTokensCachedTotal, cachedDelta)
	}
}

// TestIncVisionImageTokens_NegativeIgnored ensures the function never
// decrements totals (negative deltas are dropped, not subtracted).
func TestIncVisionImageTokens_NegativeIgnored(t *testing.T) {
	before := GetVisionMetrics()

	IncVisionImageTokens(-50, -10) // both ignored

	after := GetVisionMetrics()
	if after.ImageTokensTotal != before.ImageTokensTotal {
		t.Errorf("negative delta should be ignored; got delta %d",
			after.ImageTokensTotal-before.ImageTokensTotal)
	}
	if after.ImageTokensCachedTotal != before.ImageTokensCachedTotal {
		t.Errorf("negative cached delta should be ignored; got delta %d",
			after.ImageTokensCachedTotal-before.ImageTokensCachedTotal)
	}
}

// TestVisionMetricsCountersConcurrent verifies that the atomic counters
// survive concurrent updates correctly (sanity check the atomicity).
func TestVisionMetricsCountersConcurrent(t *testing.T) {
	const goroutines = 8
	const incsPer = 100

	before := GetVisionMetrics()

	var wg sync.WaitGroup
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			for j := 0; j < incsPer; j++ {
				IncVisionEmbedCall()
				IncVisionOCRCall()
				IncVisionImageTokens(5, 1)
				IncVisionResizeEvent()
				IncVisionCacheHit()
				IncVisionCacheMiss()
			}
		}()
	}
	wg.Wait()

	after := GetVisionMetrics()
	wantEmbed := int64(goroutines * incsPer)
	wantOCR := int64(goroutines * incsPer)
	wantTokens := int64(goroutines * incsPer * 5)
	wantCached := int64(goroutines * incsPer)
	wantResize := int64(goroutines * incsPer)
	wantHit := int64(goroutines * incsPer)
	wantMiss := int64(goroutines * incsPer)

	if after.EmbedCallsTotal-before.EmbedCallsTotal != wantEmbed {
		t.Errorf("EmbedCallsTotal: got delta %d, want %d",
			after.EmbedCallsTotal-before.EmbedCallsTotal, wantEmbed)
	}
	if after.OCRCallsTotal-before.OCRCallsTotal != wantOCR {
		t.Errorf("OCRCallsTotal: got delta %d, want %d",
			after.OCRCallsTotal-before.OCRCallsTotal, wantOCR)
	}
	if after.ImageTokensTotal-before.ImageTokensTotal != wantTokens {
		t.Errorf("ImageTokensTotal: got delta %d, want %d",
			after.ImageTokensTotal-before.ImageTokensTotal, wantTokens)
	}
	if after.ImageTokensCachedTotal-before.ImageTokensCachedTotal != wantCached {
		t.Errorf("ImageTokensCachedTotal: got delta %d, want %d",
			after.ImageTokensCachedTotal-before.ImageTokensCachedTotal, wantCached)
	}
	if after.ResizeEvents-before.ResizeEvents != wantResize {
		t.Errorf("ResizeEvents: got delta %d, want %d",
			after.ResizeEvents-before.ResizeEvents, wantResize)
	}
	if after.CacheHits-before.CacheHits != wantHit {
		t.Errorf("CacheHits: got delta %d, want %d",
			after.CacheHits-before.CacheHits, wantHit)
	}
	if after.CacheMisses-before.CacheMisses != wantMiss {
		t.Errorf("CacheMisses: got delta %d, want %d",
			after.CacheMisses-before.CacheMisses, wantMiss)
	}
}

// TestGetVisionMetrics_StructSnapshot ensures the snapshot shape matches
// the JSON contract expected by /metrics endpoints.
func TestGetVisionMetrics_StructSnapshot(t *testing.T) {
	snap := GetVisionMetrics()
	type expected struct {
		ImageTokensTotal       int64
		ImageTokensCachedTotal int64
		EmbedCallsTotal        int64
		OCRCallsTotal          int64
		ResizeEvents           int64
		CacheHits              int64
		CacheMisses            int64
		RetryCount             int64
		OCRFallbackTotal       int64
		OCRFallbackSuccess     int64
		LatencyRequestMS       int64
		LatencyRetrySleepMS    int64
		LatencyFallbackMS      int64
		LatencyParseMS         int64
		FailuresByReason       map[string]int64
		BatchAttempts          int64
		BatchHits              int64
		BatchMisses            int64
		BatchPartialFailures   int64
	}
	_ = expected(snap) // compile-time shape check
}
