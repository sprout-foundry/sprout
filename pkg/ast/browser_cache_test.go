package ast

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/odvcencio/gotreesitter/grammars"
)

// ---------------------------------------------------------------------------
// BrowserCache stub (non-WASM build) tests
// ---------------------------------------------------------------------------

// TestInitBrowserCacheStub verifies that InitBrowserCache is a no-op on
// non-WASM builds and does not change the default cache or panic.
func TestInitBrowserCacheStub(t *testing.T) {
	orig := DefaultCache()
	InitBrowserCache()
	after := DefaultCache()

	if after != orig {
		t.Error("InitBrowserCache should be a no-op on non-WASM builds")
	}
}

// TestCachedGrammarNamesStub verifies that CachedGrammarNames returns nil
// on non-WASM builds.
func TestCachedGrammarNamesStub(t *testing.T) {
	names := CachedGrammarNames()
	if names != nil {
		t.Errorf("CachedGrammarNames() = %v, want nil on non-WASM", names)
	}
}

// TestBrowserCacheInterface verifies that MemoryCache satisfies the
// GrammarCache interface at compile time.  BrowserCache is verified in
// browser_cache.go via a compile-time check (var _ GrammarCache = ...).
func TestBrowserCacheInterface(t *testing.T) {
	var _ GrammarCache = (*MemoryCache)(nil)
}

// ---------------------------------------------------------------------------
// GrammarCache contract tests
// ---------------------------------------------------------------------------

// TestGrammarCacheContract runs a standardised set of operations against a
// GrammarCache implementation to verify the interface contract.  This test
// uses MemoryCache as the test subject, but the contract applies equally to
// BrowserCache in WASM builds.
func TestGrammarCacheContract(t *testing.T) {
	cache := NewMemoryCache()

	// Empty cache should have zero stats.
	stats := cache.Stats()
	if stats.Size != 0 || stats.Hits != 0 || stats.Misses != 0 || stats.Evictions != 0 {
		t.Fatalf("empty cache: Stats() = %+v", stats)
	}

	// Put a blob.
	blob := &GrammarBlob{
		Name:     "go",
		Size:     50000,
		LoadedAt: time.Now(),
	}
	cache.Put(blob)

	// Get should hit.
	got, ok := cache.Get("go")
	if !ok {
		t.Fatal("Get(go) should hit after Put")
	}
	if got.Name != "go" {
		t.Errorf("got.Name = %q, want %q", got.Name, "go")
	}
	if got.Size != 50000 {
		t.Errorf("got.Size = %d, want %d", got.Size, 50000)
	}

	// Miss should work.
	_, ok = cache.Get("missing")
	if ok {
		t.Fatal("Get(missing) should miss")
	}

	// Names should be sorted.
	cache.Put(&GrammarBlob{Name: "python", Size: 30000, LoadedAt: time.Now()})
	cache.Put(&GrammarBlob{Name: "typescript", Size: 40000, LoadedAt: time.Now()})
	names := cache.Names()
	if len(names) != 3 {
		t.Fatalf("Names() = %d entries, want 3", len(names))
	}
	if names[0] != "go" || names[1] != "python" || names[2] != "typescript" {
		t.Errorf("Names() not sorted: %v", names)
	}

	// Invalidate should remove.
	cache.Invalidate("go")
	_, ok = cache.Get("go")
	if ok {
		t.Fatal("Get(go) should miss after Invalidate")
	}

	// InvalidateAll should clear everything.
	cache.Put(&GrammarBlob{Name: "go", Size: 50000, LoadedAt: time.Now()})
	cache.InvalidateAll()
	stats = cache.Stats()
	if stats.Size != 0 {
		t.Errorf("after InvalidateAll: Size = %d, want 0", stats.Size)
	}
}

// ---------------------------------------------------------------------------
// BrowserCache metadata serialization tests
// ---------------------------------------------------------------------------

// testLocalEntry mirrors the private localStorageEntry in browser_cache.go
// for testing the JSON serialization format.
type testLocalEntry struct {
	Name     string `json:"name"`
	Size     int    `json:"size"`
	LoadedAt string `json:"loadedAt"`
}

// TestBrowserCacheMetadataRoundTrip verifies that GrammarBlob metadata
// (name, size, loadedAt) survives JSON round-tripping as the BrowserCache
// does with localStorage.  This test doesn't use syscall/js.
func TestBrowserCacheMetadataRoundTrip(t *testing.T) {
	blob := &GrammarBlob{
		Name:     "go",
		Size:     123456,
		LoadedAt: time.Date(2026, 5, 16, 12, 0, 0, 0, time.UTC),
	}

	entry := testLocalEntry{
		Name:     blob.Name,
		Size:     blob.Size,
		LoadedAt: blob.LoadedAt.Format(time.RFC3339),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var parsed testLocalEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if parsed.Name != "go" {
		t.Errorf("Name = %q, want %q", parsed.Name, "go")
	}
	if parsed.Size != 123456 {
		t.Errorf("Size = %d, want %d", parsed.Size, 123456)
	}
	if parsed.LoadedAt != "2026-05-16T12:00:00Z" {
		t.Errorf("LoadedAt = %q, want %q", parsed.LoadedAt, "2026-05-16T12:00:00Z")
	}

	parsedTime, err := time.Parse(time.RFC3339, parsed.LoadedAt)
	if err != nil {
		t.Fatalf("Parse time: %v", err)
	}
	if !parsedTime.Equal(blob.LoadedAt) {
		t.Errorf("parsed time = %v, want %v", parsedTime, blob.LoadedAt)
	}
}

// TestBrowserCacheMetadataRoundTripZeroTime verifies that a zero-time
// LoadedAt survives serialization round-trip without errors.
func TestBrowserCacheMetadataRoundTripZeroTime(t *testing.T) {
	entry := testLocalEntry{
		Name:     "python",
		Size:     0,
		LoadedAt: time.Time{}.Format(time.RFC3339),
	}

	data, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}

	var parsed testLocalEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	if parsed.Name != "python" {
		t.Errorf("Name = %q, want %q", parsed.Name, "python")
	}

	parsedTime, err := time.Parse(time.RFC3339, parsed.LoadedAt)
	if err != nil {
		t.Fatalf("Parse zero time: %v", err)
	}
	if !parsedTime.IsZero() {
		t.Errorf("expected zero time, got %v", parsedTime)
	}
}

// TestBrowserCacheMetadataRoundTripAllLanguages verifies the serialization
// format works for all supported languages with real grammar sizes.
func TestBrowserCacheMetadataRoundTripAllLanguages(t *testing.T) {
	for lang := range SupportedLanguages {
		t.Run(lang, func(t *testing.T) {
			entry := grammars.DetectLanguageByName(lang)
			if entry == nil || entry.Language == nil {
				t.Skip("grammar not available")
			}
			g := entry.Language()
			if g == nil {
				t.Skip("language loader returned nil")
			}

			now := time.Now()
			blob := &GrammarBlob{
				Language: g,
				Name:     lang,
				LoadedAt: now,
				Size:     EstimateLanguageSize(g),
			}

			ser := testLocalEntry{
				Name:     blob.Name,
				Size:     blob.Size,
				LoadedAt: blob.LoadedAt.Format(time.RFC3339),
			}

			data, err := json.Marshal(ser)
			if err != nil {
				t.Fatalf("Marshal(%s): %v", lang, err)
			}

			var parsed testLocalEntry
			if err := json.Unmarshal(data, &parsed); err != nil {
				t.Fatalf("Unmarshal(%s): %v", lang, err)
			}

			if parsed.Name != lang {
				t.Errorf("%s: Name = %q, want %q", lang, parsed.Name, lang)
			}
			if parsed.Size != blob.Size {
				t.Errorf("%s: Size mismatch: got %d, want %d", lang, parsed.Size, blob.Size)
			}
			if parsed.Size <= 0 {
				t.Errorf("%s: Size = %d, want > 0", lang, parsed.Size)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// Blob creation with real grammars
// ---------------------------------------------------------------------------

// TestBrowserCacheMetadataBlobFields verifies that GrammarBlob fields are
// populated correctly when created from a real grammar.
func TestBrowserCacheMetadataBlobFields(t *testing.T) {
	entry := grammars.DetectLanguageByName("go")
	if entry == nil || entry.Language == nil {
		t.Skip("go grammar not available")
	}
	lang := entry.Language()
	if lang == nil {
		t.Skip("go language loader returned nil")
	}

	blob := &GrammarBlob{
		Language: lang,
		Name:     "go",
		LoadedAt: time.Now(),
		Size:     EstimateLanguageSize(lang),
	}

	if blob.Name != "go" {
		t.Errorf("Name = %q", blob.Name)
	}
	if blob.Language == nil {
		t.Error("Language should not be nil")
	}
	if blob.Size <= 0 {
		t.Errorf("Size = %d, want > 0", blob.Size)
	}
	if blob.LoadedAt.IsZero() {
		t.Error("LoadedAt should not be zero")
	}
}

// TestBrowserCacheNewReturnsEmpty verifies that BrowserCache (or MemoryCache
// on non-WASM) starts empty.  On non-WASM builds, NewMemoryCache is the
// equivalent test.
func TestBrowserCacheNewReturnsEmpty(t *testing.T) {
	cache := NewMemoryCache()
	stats := cache.Stats()
	if stats.Size != 0 {
		t.Errorf("new cache should be empty, got Size = %d", stats.Size)
	}
	if len(cache.Names()) != 0 {
		t.Errorf("new cache should have no names, got %v", cache.Names())
	}
}
