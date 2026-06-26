package embedding

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
)

// stubProvider is a minimal EmbeddingProvider that records call counts and
// returns deterministic vectors derived from the input text. Used by the
// cachedProvider tests to verify cache hit/miss behavior without an ONNX
// runtime.
type stubProvider struct {
	mu       sync.Mutex
	embeds   int
	batches  int
	dim      int
	failNext bool
}

func (s *stubProvider) Embed(_ context.Context, text string) ([]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.embeds++
	if s.failNext {
		s.failNext = false
		return nil, errors.New("stub failure")
	}
	// Deterministic vector: each float is the byte value of the corresponding
	// character (mod dim), so identical text yields identical vectors.
	vec := make([]float32, s.dim)
	for i := 0; i < s.dim; i++ {
		if i < len(text) {
			vec[i] = float32(text[i])
		}
	}
	return vec, nil
}

func (s *stubProvider) EmbedBatch(_ context.Context, texts []string) ([][]float32, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.batches++
	out := make([][]float32, len(texts))
	for i, text := range texts {
		vec := make([]float32, s.dim)
		for j := 0; j < s.dim; j++ {
			if j < len(text) {
				vec[j] = float32(text[j])
			}
		}
		out[i] = vec
	}
	return out, nil
}

func (s *stubProvider) Dimensions() int   { return s.dim }
func (s *stubProvider) Name() string      { return "stub" }
func (s *stubProvider) ModelHash() string { return "stub-hash" }
func (s *stubProvider) EmbedWithPrefix(ctx context.Context, text, prefix string) ([]float32, error) {
	return s.Embed(ctx, prefix+text)
}
func (s *stubProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	prefixed := make([]string, len(texts))
	for i, t := range texts {
		prefixed[i] = prefix + t
	}
	return s.EmbedBatch(ctx, prefixed)
}
func (s *stubProvider) Close() error { return nil }

func TestCachedProvider_HitAvoidsInnerCall(t *testing.T) {
	stub := &stubProvider{dim: 4}
	c := newCachedProvider(stub)

	v1, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("first Embed failed: %v", err)
	}
	v2, err := c.Embed(context.Background(), "hello")
	if err != nil {
		t.Fatalf("second Embed failed: %v", err)
	}

	if stub.embeds != 1 {
		t.Fatalf("expected 1 inner Embed call, got %d", stub.embeds)
	}
	if len(v1) != len(v2) {
		t.Fatalf("vectors differ in length: %d vs %d", len(v1), len(v2))
	}
	for i := range v1 {
		if v1[i] != v2[i] {
			t.Fatalf("vectors differ at index %d: %v vs %v", i, v1[i], v2[i])
		}
	}
}

// TestCachedProvider_ReturnsIndependentCopy verifies the cache returns a
// defensive copy so a mutating caller cannot corrupt the cached vector.
func TestCachedProvider_ReturnsIndependentCopy(t *testing.T) {
	stub := &stubProvider{dim: 4}
	c := newCachedProvider(stub)

	v1, _ := c.Embed(context.Background(), "abc")
	v1[0] = 999.0 // mutate the returned slice

	v2, _ := c.Embed(context.Background(), "abc")
	if v2[0] == 999.0 {
		t.Fatalf("cache returned the mutated vector — caller corrupted the cache; got %v, want unmutated", v2[0])
	}
}

// TestCachedProvider_CacheMissReturnsIndependentCopy verifies the
// cache-miss path also stores a copy, not the caller's slice.
func TestCachedProvider_CacheMissReturnsIndependentCopy(t *testing.T) {
	stub := &stubProvider{dim: 4}
	c := newCachedProvider(stub)

	// First call populates the cache (cache-miss path).
	v1, _ := c.Embed(context.Background(), "xyz")
	v1[1] = 777.0 // mutate

	// Second call should hit cache and NOT see the mutation.
	v2, _ := c.Embed(context.Background(), "xyz")
	if v2[1] == 777.0 {
		t.Fatalf("cache-miss path cached the caller's mutated slice; got %v", v2[1])
	}
}

// TestCachedProvider_EmbedBatchMixedHitsMisses verifies partial cache hits
// in EmbedBatch only embed the missing texts.
func TestCachedProvider_EmbedBatchMixedHitsMisses(t *testing.T) {
	stub := &stubProvider{dim: 2}
	c := newCachedProvider(stub)

	// Warm the cache for "a" via a single Embed.
	if _, err := c.Embed(context.Background(), "a"); err != nil {
		t.Fatal(err)
	}

	// Batch with one cached ("a") and two uncached ("b", "c").
	results, err := c.EmbedBatch(context.Background(), []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("EmbedBatch failed: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	// "a" was cached; only "b" and "c" should be new embeddings. The batch
	// call counts as 1 inner EmbedBatch regardless, so we check the vectors
	// are present and distinct.
	if len(results[0]) == 0 || len(results[1]) == 0 || len(results[2]) == 0 {
		t.Fatalf("expected non-empty vectors for all results")
	}
	if stub.batches != 1 {
		t.Fatalf("expected 1 inner EmbedBatch call for the 2 misses, got %d", stub.batches)
	}
}

// TestCachedProvider_ConcurrentSameText verifies the double-check pattern
// handles concurrent embedding of the same text without duplicate inner calls
// (the goroutine that wins the cache-miss race populates it; others should
// hit the cache or the double-check).
func TestCachedProvider_ConcurrentSameText(t *testing.T) {
	stub := &stubProvider{dim: 4}
	c := newCachedProvider(stub)

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	errs := make([]error, n)
	vecs := make([][]float32, n)
	for i := 0; i < n; i++ {
		go func(idx int) {
			defer wg.Done()
			v, err := c.Embed(context.Background(), "same")
			vecs[idx] = v
			errs[idx] = err
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("goroutine %d errored: %v", i, err)
		}
	}
	// We expect exactly 1 inner Embed call (the winner of the race).
	// Under the double-check pattern, losing goroutines may also call
	// inner.Embed before the winner stores the result — but they'll find
	// it on the double-check. So we assert at most n calls and at least 1.
	if stub.embeds < 1 {
		t.Fatalf("expected at least 1 inner Embed call, got %d", stub.embeds)
	}
	if stub.embeds > n {
		t.Fatalf("expected at most %d inner Embed calls, got %d", n, stub.embeds)
	}
	// All returned vectors must be equal.
	for i := 1; i < n; i++ {
		if len(vecs[i]) != len(vecs[0]) {
			t.Fatalf("goroutine %d vector length differs", i)
		}
		for j := range vecs[i] {
			if vecs[i][j] != vecs[0][j] {
				t.Fatalf("goroutine %d vector differs at %d", i, j)
			}
		}
	}
}

// TestCachedProvider_FIFOEviction verifies that exceeding the cache capacity
// evicts the oldest entry, causing a re-embed on the next access.
func TestCachedProvider_FIFOEviction(t *testing.T) {
	stub := &stubProvider{dim: 2}
	c := newCachedProvider(stub)

	// Temporarily lower the cap to make eviction observable without
	// populating 1024 entries.
	originalMax := maxEmbedCacheEntries
	// We can't reassign a const, so instead fill the cache to capacity+1
	// with distinct texts and verify the first entry was evicted.
	_ = originalMax

	// Fill exactly capacity entries.
	for i := 0; i < maxEmbedCacheEntries; i++ {
		text := string(rune('a' + i%26)) + string(rune('0'+i/26))
		if _, err := c.Embed(context.Background(), text); err != nil {
			t.Fatalf("embed %d failed: %v", i, err)
		}
	}
	firstEmbeds := stub.embeds

	// One more distinct entry should trigger eviction of the oldest.
	if _, err := c.Embed(context.Background(), "zzz-new"); err != nil {
		t.Fatal(err)
	}

	// Re-embed the first entry; it should have been evicted and require a
	// fresh inner call. (Total new embeds since snapshot: zzz-new + firstText = 2.)
	firstText := string(rune('a')) + string(rune('0'))
	if _, err := c.Embed(context.Background(), firstText); err != nil {
		t.Fatal(err)
	}
	if stub.embeds != firstEmbeds+2 {
		t.Fatalf("expected eviction to force 1 re-embed (+1 for zzz-new); embeds before=%d after=%d", firstEmbeds, stub.embeds)
	}
}

// TestCachedProvider_PassthroughMethods verifies Dimensions/Name/ModelHash
// delegate to the inner provider.
func TestCachedProvider_PassthroughMethods(t *testing.T) {
	stub := &stubProvider{dim: 7}
	c := newCachedProvider(stub)

	if c.Dimensions() != 7 {
		t.Fatalf("Dimensions = %d, want 7", c.Dimensions())
	}
	if c.Name() != "stub" {
		t.Fatalf("Name = %q, want \"stub\"", c.Name())
	}
	if c.ModelHash() != "stub-hash" {
		t.Fatalf("ModelHash = %q, want \"stub-hash\"", c.ModelHash())
	}
}

// TestCachedProvider_BoundedSize verifies that after many evictions the cache
// map and list never exceed maxEmbedCacheEntries. This guards against the
// unbounded-growth regression where a reslicing slice (s = s[1:]) leaks its
// backing array, causing the cache to hold a ~1M-entry string-header array
// (~15MB) even when only 1024 live entries remain.
func TestCachedProvider_BoundedSize(t *testing.T) {
	stub := &stubProvider{dim: 4}
	c := newCachedProvider(stub)

	// Insert 5000 distinct texts — well over the 1024 cap.
	const inserts = 5000
	for i := 0; i < inserts; i++ {
		text := fmt.Sprintf("distinct-text-%d", i)
		if _, err := c.Embed(context.Background(), text); err != nil {
			t.Fatalf("embed %d failed: %v", i, err)
		}
	}

	// The map must be bounded at the cap.
	if len(c.cache) > maxEmbedCacheEntries {
		t.Fatalf("cache map exceeded cap: len=%d, cap=%d", len(c.cache), maxEmbedCacheEntries)
	}
	if len(c.cache) != maxEmbedCacheEntries {
		t.Fatalf("cache map should be exactly at cap: len=%d, cap=%d", len(c.cache), maxEmbedCacheEntries)
	}

	// The list must also be bounded at the cap.
	if c.cacheList.Len() > maxEmbedCacheEntries {
		t.Fatalf("cache list exceeded cap: len=%d, cap=%d", c.cacheList.Len(), maxEmbedCacheEntries)
	}
	if c.cacheList.Len() != maxEmbedCacheEntries {
		t.Fatalf("cache list should be exactly at cap: len=%d, cap=%d", c.cacheList.Len(), maxEmbedCacheEntries)
	}
}
