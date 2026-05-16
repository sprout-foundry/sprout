package ast

import (
	"sync"
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter/grammars"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeGoBlob(t *testing.T, name string) *GrammarBlob {
	t.Helper()
	entry := grammars.DetectLanguageByName("go")
	if entry == nil || entry.Language == nil {
		t.Skip("go grammar not available")
	}
	lang := entry.Language()
	if lang == nil {
		t.Skip("go language loader returned nil")
	}
	return &GrammarBlob{
		Language: lang,
		Name:     name,
		LoadedAt: time.Now(),
		Size:     EstimateLanguageSize(lang),
	}
}

// ---------------------------------------------------------------------------
// TestNewMemoryCache
// ---------------------------------------------------------------------------

func TestNewMemoryCache(t *testing.T) {
	cache := NewMemoryCache()

	// Should be empty.
	stats := cache.Stats()
	if stats.Size != 0 {
		t.Errorf("new cache should have 0 entries, got %d", stats.Size)
	}
	if stats.Hits != 0 || stats.Misses != 0 || stats.Evictions != 0 {
		t.Errorf("new cache stats should be zero: %+v", stats)
	}

	names := cache.Names()
	if len(names) != 0 {
		t.Errorf("new cache should have no names, got %v", names)
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCachePutGet
// ---------------------------------------------------------------------------

func TestMemoryCachePutGet(t *testing.T) {
	cache := NewMemoryCache()

	blob := makeGoBlob(t, "go")
	cache.Put(blob)

	got, ok := cache.Get("go")
	if !ok {
		t.Fatal("Get after Put should succeed")
	}
	if got != blob {
		t.Errorf("Get returned wrong blob pointer")
	}
	if got.Name != "go" {
		t.Errorf("blob Name = %q, want %q", got.Name, "go")
	}
	if got.Language == nil {
		t.Error("blob.Language should not be nil")
	}
	if got.Size <= 0 {
		t.Errorf("blob.Size = %d, want > 0", got.Size)
	}
	if got.LoadedAt.IsZero() {
		t.Error("blob.LoadedAt should not be zero")
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheMiss
// ---------------------------------------------------------------------------

func TestMemoryCacheMiss(t *testing.T) {
	cache := NewMemoryCache()

	_, ok := cache.Get("nonexistent")
	if ok {
		t.Fatal("Get should miss on unknown key")
	}

	stats := cache.Stats()
	if stats.Misses != 1 {
		t.Errorf("misses = %d, want 1", stats.Misses)
	}
	if stats.Hits != 0 {
		t.Errorf("hits = %d, want 0", stats.Hits)
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheInvalidate
// ---------------------------------------------------------------------------

func TestMemoryCacheInvalidate(t *testing.T) {
	cache := NewMemoryCache()

	blob := makeGoBlob(t, "go")
	cache.Put(blob)

	cache.Invalidate("go")

	_, ok := cache.Get("go")
	if ok {
		t.Fatal("Get after Invalidate should miss")
	}

	stats := cache.Stats()
	if stats.Size != 0 {
		t.Errorf("after invalidate, size = %d, want 0", stats.Size)
	}
}

func TestMemoryCacheInvalidateMissingKey(t *testing.T) {
	cache := NewMemoryCache()
	// Should not panic or error on missing key.
	cache.Invalidate("no-such-key")
}

// ---------------------------------------------------------------------------
// TestMemoryCacheInvalidateAll
// ---------------------------------------------------------------------------

func TestMemoryCacheInvalidateAll(t *testing.T) {
	cache := NewMemoryCache()

	cache.Put(makeGoBlob(t, "go"))
	cache.Put(makeGoBlob(t, "python"))
	cache.Put(makeGoBlob(t, "typescript"))

	cache.InvalidateAll()

	stats := cache.Stats()
	if stats.Size != 0 {
		t.Errorf("after InvalidateAll, size = %d, want 0", stats.Size)
	}

	_, ok := cache.Get("go")
	if ok {
		t.Error("go should be gone after InvalidateAll")
	}
	_, ok = cache.Get("python")
	if ok {
		t.Error("python should be gone after InvalidateAll")
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheReplace
// ---------------------------------------------------------------------------

func TestMemoryCacheReplace(t *testing.T) {
	cache := NewMemoryCache()

	blob1 := makeGoBlob(t, "go")
	t1 := blob1.LoadedAt

	time.Sleep(1 * time.Millisecond)

	blob2 := makeGoBlob(t, "go")
	t2 := blob2.LoadedAt

	cache.Put(blob1)
	cache.Put(blob2)

	// Verify eviction counter.
	stats := cache.Stats()
	if stats.Evictions != 1 {
		t.Errorf("evictions = %d, want 1", stats.Evictions)
	}
	if stats.Size != 1 {
		t.Errorf("size = %d, want 1 (replaced, not added)", stats.Size)
	}

	// Verify latest value.
	got, ok := cache.Get("go")
	if !ok {
		t.Fatal("Get should find replaced blob")
	}
	if got != blob2 {
		t.Error("should return the latest blob (blob2)")
	}
	if !got.LoadedAt.After(t1) {
		t.Errorf("LoadedAt should be blob2's time (%v), not blob1's (%v)", t2, t1)
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheStats
// ---------------------------------------------------------------------------

func TestMemoryCacheStats(t *testing.T) {
	cache := NewMemoryCache()

	blob := makeGoBlob(t, "go")
	blob2 := makeGoBlob(t, "python")

	cache.Put(blob)
	cache.Put(blob2)

	// Miss
	cache.Get("missing")

	// Hit
	cache.Get("go")

	// Miss again
	cache.Get("another")

	// Hit again
	cache.Get("python")

	stats := cache.Stats()
	if stats.Hits != 2 {
		t.Errorf("hits = %d, want 2", stats.Hits)
	}
	if stats.Misses != 2 {
		t.Errorf("misses = %d, want 2", stats.Misses)
	}
	if stats.Evictions != 0 {
		t.Errorf("evictions = %d, want 0", stats.Evictions)
	}
	if stats.Size != 2 {
		t.Errorf("size = %d, want 2", stats.Size)
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheNames
// ---------------------------------------------------------------------------

func TestMemoryCacheNames(t *testing.T) {
	cache := NewMemoryCache()

	cache.Put(makeGoBlob(t, "typescript"))
	cache.Put(makeGoBlob(t, "go"))
	cache.Put(makeGoBlob(t, "python"))

	names := cache.Names()
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d: %v", len(names), names)
	}

	// Verify sorted.
	if names[0] != "go" || names[1] != "python" || names[2] != "typescript" {
		t.Errorf("names not sorted: %v", names)
	}

	// Verify it's a copy (mutating shouldn't affect cache).
	names[0] = "corrupted"
	names2 := cache.Names()
	if names2[0] != "go" {
		t.Error("Names() should return a copy, not internal slice")
	}
}

// ---------------------------------------------------------------------------
// TestMemoryCacheConcurrent
// ---------------------------------------------------------------------------

func TestMemoryCacheConcurrent(t *testing.T) {
	cache := NewMemoryCache()

	const goroutines = 50
	const opsPerGoroutine = 100

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			defer wg.Done()
			for j := 0; j < opsPerGoroutine; j++ {
				name := "lang" + string(rune('a'+j%5))
				switch j % 3 {
				case 0:
					blob := makeGoBlob(t, name)
					cache.Put(blob)
				case 1:
					cache.Get(name)
				case 2:
					cache.Invalidate(name)
				}
			}
		}(i)
	}

	wg.Wait()

	// Should not have panicked or data-raced.
	stats := cache.Stats()
	if stats.Hits+stats.Misses == 0 {
		t.Error("no operations were performed")
	}
	if stats.Size < 0 || stats.Size > 5 {
		t.Errorf("size = %d, expected 0-5", stats.Size)
	}

	names := cache.Names()
	// Verify names are sorted even after concurrent ops.
	for i := 1; i < len(names); i++ {
		if names[i] < names[i-1] {
			t.Errorf("names not sorted at index %d: %q < %q", i, names[i], names[i-1])
		}
	}
}

// ---------------------------------------------------------------------------
// TestSetDefaultCachePanicsOnNil
// ---------------------------------------------------------------------------

func TestSetDefaultCachePanicsOnNil(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("SetDefaultCache(nil) should panic")
		}
	}()
	SetDefaultCache(nil)
}

// ---------------------------------------------------------------------------
// TestSetDefaultCache
// ---------------------------------------------------------------------------

func TestSetDefaultCache(t *testing.T) {
	// Save and restore original.
	orig := DefaultCache()
	defer SetDefaultCache(orig)

	custom := NewMemoryCache()
	SetDefaultCache(custom)

	if DefaultCache() != custom {
		t.Error("DefaultCache should return the custom cache after SetDefaultCache")
	}

	// Verify the custom cache works.
	blob := makeGoBlob(t, "go")
	custom.Put(blob)

	got, ok := custom.Get("go")
	if !ok || got != blob {
		t.Error("custom cache should work normally")
	}
}

// ---------------------------------------------------------------------------
// TestEstimateLanguageSize
// ---------------------------------------------------------------------------

func TestEstimateLanguageSize(t *testing.T) {
	// nil language should return 0.
	if EstimateLanguageSize(nil) != 0 {
		t.Error("EstimateLanguageSize(nil) should return 0")
	}

	// Load go language and estimate.
	entry := grammars.DetectLanguageByName("go")
	if entry == nil || entry.Language == nil {
		t.Skip("go grammar not available")
	}
	lang := entry.Language()
	if lang == nil {
		t.Skip("go language loader returned nil")
	}

	size := EstimateLanguageSize(lang)
	if size <= 0 {
		t.Errorf("EstimateLanguageSize(go) = %d, want > 0", size)
	}
	t.Logf("go language estimated size: %d bytes (%d KB)", size, size/1024)

	// Also test python and typescript to verify non-zero sizes.
	for _, langName := range []string{"python", "typescript", "javascript"} {
		entry := grammars.DetectLanguageByName(langName)
		if entry == nil || entry.Language == nil {
			continue
		}
		l := entry.Language()
		if l == nil {
			continue
		}
		s := EstimateLanguageSize(l)
		if s <= 0 {
			t.Errorf("EstimateLanguageSize(%s) = %d, want > 0", langName, s)
		}
	}
}

// ---------------------------------------------------------------------------
// TestEstimateLanguageSizeMonotonic
// ---------------------------------------------------------------------------

func TestEstimateLanguageSizeMonotonic(t *testing.T) {
	// Estimate a few languages and verify they all produce positive,
	// reasonable values.  We don't assert ordering (different languages
	// have different grammar sizes) but we do sanity-check the range.
	for _, name := range []string{"go", "python", "typescript"} {
		entry := grammars.DetectLanguageByName(name)
		if entry == nil || entry.Language == nil {
			continue
		}
		lang := entry.Language()
		size := EstimateLanguageSize(lang)
		// Grammar blobs are typically 50KB–20MB. Flag anything unreasonable.
		if size < 1000 {
			t.Errorf("%s: size = %d, seems too small", name, size)
		}
		if size > 50_000_000 {
			t.Errorf("%s: size = %d, seems too large", name, size)
		}
	}
}

// ---------------------------------------------------------------------------
// TestPreloadCache
// ---------------------------------------------------------------------------

func TestPreloadCache(t *testing.T) {
	// Use a fresh cache so we know exactly what's loaded.
	orig := DefaultCache()
	newCache := NewMemoryCache()
	SetDefaultCache(newCache)
	defer SetDefaultCache(orig)

	loaded := PreloadCache()

	stats := newCache.Stats()
	if loaded != len(SupportedLanguages) {
		t.Errorf("PreloadCache loaded = %d, want %d (SupportedLanguages count)",
			loaded, len(SupportedLanguages))
	}
	if stats.Size != len(SupportedLanguages) {
		t.Errorf("PreloadCache size = %d, want %d (SupportedLanguages count)",
			stats.Size, len(SupportedLanguages))
	}

	names := newCache.Names()
	if len(names) != len(SupportedLanguages) {
		t.Errorf("names count = %d, want %d", len(names), len(SupportedLanguages))
	}

	// Verify all supported languages are present.
	for lang := range SupportedLanguages {
		blob, ok := newCache.Get(lang)
		if !ok {
			t.Errorf("PreloadCache should have loaded %q", lang)
			continue
		}
		if blob.Language == nil {
			t.Errorf("blob for %q has nil Language", lang)
		}
		if blob.Size <= 0 {
			t.Errorf("blob for %q has Size <= 0", lang)
		}
		if blob.LoadedAt.IsZero() {
			t.Errorf("blob for %q has zero LoadedAt", lang)
		}
	}
}

// ---------------------------------------------------------------------------
// TestDefaultCacheConcurrent
// ---------------------------------------------------------------------------

func TestDefaultCacheConcurrent(t *testing.T) {
	orig := DefaultCache()
	defer SetDefaultCache(orig)

	var wg sync.WaitGroup
	const goroutines = 20

	// Writers: swap default cache.
	wg.Add(goroutines)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			SetDefaultCache(NewMemoryCache())
		}()
	}
	wg.Wait()

	// Readers: read while writers swap (but writers are done now, so do interleaved).
	wg.Add(goroutines * 2)
	for i := 0; i < goroutines; i++ {
		go func() {
			defer wg.Done()
			_ = DefaultCache()
		}()
		go func() {
			defer wg.Done()
			SetDefaultCache(NewMemoryCache())
		}()
	}
	wg.Wait()

	// Final should be a valid GrammarCache, never nil.
	c := DefaultCache()
	if c == nil {
		t.Fatal("DefaultCache() should never return nil")
	}
}
