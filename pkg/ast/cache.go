// Package ast (continued) — grammar blob caching layer.
//
// This package provides a pluggable caching abstraction for compiled
// gotreesitter.Language grammars.  The default implementation is an
// in-memory map, but the GrammarCache interface can be replaced for
// WASM builds (e.g. IndexedDB-backed) or other storage backends.
package ast

import (
	"slices"
	"sync"
	"sync/atomic"
	"time"

	gotreesitter "github.com/odvcencio/gotreesitter"
	"github.com/odvcencio/gotreesitter/grammars"
)

// GrammarBlob wraps a compiled Language with cache metadata.
//
// A GrammarBlob is immutable after creation (except for internal lazily-built
// maps inside the Language itself).  Create one manually after calling
// EstimateLanguageSize, or use PreloadCache to populate the default cache.
type GrammarBlob struct {
	Language *gotreesitter.Language
	Name     string
	LoadedAt time.Time
	Size     int
}

// CacheStats holds hit/miss/eviction statistics for a GrammarCache.
type CacheStats struct {
	Hits      int64
	Misses    int64
	Evictions int64
	Size      int // number of entries in the cache
}

// GrammarCache is a pluggable abstraction for caching compiled grammar blobs.
//
// All methods are safe for concurrent use by multiple goroutines.
//
// The default implementation is MemoryCache.  For WASM builds, callers can
// replace it via SetDefaultCache with an IndexedDB-backed implementation.
type GrammarCache interface {
	// Get returns the cached GrammarBlob for name, or (nil, false) if
	// not found.  Increments the hit counter on found, miss counter
	// on not-found.
	Get(name string) (*GrammarBlob, bool)

	// Put stores a GrammarBlob.  The key is derived from blob.Name.
	// If a blob with the same name already exists, it is replaced and
	// the eviction counter is incremented.
	Put(blob *GrammarBlob)

	// Invalidate removes the cache entry for name.  If no entry exists,
	// this is a no-op.
	Invalidate(name string)

	// InvalidateAll removes all entries from the cache.
	InvalidateAll()

	// Stats returns current cache statistics.
	Stats() CacheStats

	// Names returns a sorted copy of all cached language names.
	Names() []string
}

// MemoryCache is the default in-memory implementation of GrammarCache.
type MemoryCache struct {
	mu        sync.RWMutex
	data      map[string]*GrammarBlob
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
}

// NewMemoryCache creates an empty MemoryCache.
func NewMemoryCache() *MemoryCache {
	return &MemoryCache{
		data: make(map[string]*GrammarBlob),
	}
}

// Get implements GrammarCache.Get for MemoryCache.
func (c *MemoryCache) Get(name string) (*GrammarBlob, bool) {
	c.mu.RLock()
	blob, ok := c.data[name]
	c.mu.RUnlock()
	if ok {
		c.hits.Add(1)
		return blob, true
	}
	c.misses.Add(1)
	return nil, false
}

// Put implements GrammarCache.Put for MemoryCache.
func (c *MemoryCache) Put(blob *GrammarBlob) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.data[blob.Name]; exists {
		c.evictions.Add(1)
	}
	c.data[blob.Name] = blob
}

// Invalidate implements GrammarCache.Invalidate for MemoryCache.
func (c *MemoryCache) Invalidate(name string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, name)
}

// InvalidateAll implements GrammarCache.InvalidateAll for MemoryCache.
func (c *MemoryCache) InvalidateAll() {
	c.mu.Lock()
	defer c.mu.Unlock()
	clear(c.data)
}

// Stats implements GrammarCache.Stats for MemoryCache.
func (c *MemoryCache) Stats() CacheStats {
	c.mu.RLock()
	size := len(c.data)
	c.mu.RUnlock()
	return CacheStats{
		Hits:      c.hits.Load(),
		Misses:    c.misses.Load(),
		Evictions: c.evictions.Load(),
		Size:      size,
	}
}

// Names implements GrammarCache.Names for MemoryCache.
func (c *MemoryCache) Names() []string {
	c.mu.RLock()
	names := make([]string, 0, len(c.data))
	for name := range c.data {
		names = append(names, name)
	}
	c.mu.RUnlock()
	slices.Sort(names)
	return names
}

// EstimateLanguageSize estimates the approximate memory footprint (in bytes)
// of a *gotreesitter.Language struct.
//
// The estimate is based on rough per-element sizes for the major slice fields
// in the Language struct.  It does not include the lazily-built internal maps
// (symbolNameMap, etc.) since those are built on-demand and vary with usage.
//
// This function does not use reflection; it directly inspects the exported
// slice fields.
func EstimateLanguageSize(lang *gotreesitter.Language) int {
	if lang == nil {
		return 0
	}

	size := 0

	// ParseTable: [][]uint16 — outer slice entries + inner slice overhead.
	// Approximate: 200 bytes per state row (outer entry + inner slice data).
	size += len(lang.ParseTable) * 200

	// SmallParseTable: []uint16 — 2 bytes per entry.
	size += len(lang.SmallParseTable) * 2

	// SmallParseTableMap: []uint32 — 4 bytes per entry (included for completeness).
	size += len(lang.SmallParseTableMap) * 4

	// ParseActions: []ParseActionEntry — each entry has a slice of ParseAction.
	// Approximate: 100 bytes per entry (struct + Actions slice).
	size += len(lang.ParseActions) * 100

	// LexModes: []LexMode — 24 bytes each.
	size += len(lang.LexModes) * 24

	// LexStates: []LexState — each has Transitions slice.
	// Approximate: 200 bytes per state (struct + Transitions slice).
	size += len(lang.LexStates) * 200

	// KeywordLexStates: []LexState — same as LexStates.
	size += len(lang.KeywordLexStates) * 200

	// SymbolNames: []string — 64 bytes per string (header + backing data avg).
	size += len(lang.SymbolNames) * 64

	// SymbolMetadata: []SymbolMetadata — 72 bytes each (struct with string field).
	size += len(lang.SymbolMetadata) * 72

	// FieldNames: []string — 64 bytes per string.
	size += len(lang.FieldNames) * 64

	// FieldMapSlices: [][2]uint16 — 4 bytes per entry.
	size += len(lang.FieldMapSlices) * 4

	// FieldMapEntries: []FieldMapEntry — 8 bytes each.
	size += len(lang.FieldMapEntries) * 8

	// AliasSequences: [][]Symbol — approximate 50 bytes per production row.
	size += len(lang.AliasSequences) * 50

	// PrimaryStateIDs: []StateID — 4 bytes each.
	size += len(lang.PrimaryStateIDs) * 4

	// ReservedWords: []Symbol — 2 bytes each.
	size += len(lang.ReservedWords) * 2

	// SupertypeSymbols: []Symbol — 2 bytes each.
	size += len(lang.SupertypeSymbols) * 2

	// SupertypeMapSlices: [][2]uint16 — 4 bytes per entry.
	size += len(lang.SupertypeMapSlices) * 4

	// SupertypeMapEntries: []Symbol — 2 bytes each.
	size += len(lang.SupertypeMapEntries) * 2

	// ExternalSymbols: []Symbol — 2 bytes each.
	size += len(lang.ExternalSymbols) * 2

	// ImmediateTokens: []bool — 1 byte each.
	size += len(lang.ImmediateTokens) * 1

	// ZeroWidthTokens: []bool — 1 byte each.
	size += len(lang.ZeroWidthTokens) * 1

	// ExternalLexStates: [][]bool — approximate 50 bytes per row.
	size += len(lang.ExternalLexStates) * 50

	// Name string (struct field).
	size += len(lang.Name) * 2

	return size
}

// ---------------------------------------------------------------------------
// Package-level default cache (race-safe via atomic.Value)
// ---------------------------------------------------------------------------

// defaultCache holds the package-level GrammarCache.  It uses atomic.Value
// so that DefaultCache() and SetDefaultCache() are safe for concurrent use
// without explicit locking.
var defaultCache atomic.Value

func init() {
	defaultCache.Store(NewMemoryCache())
}

// DefaultCache returns the current default GrammarCache.
func DefaultCache() GrammarCache {
	return defaultCache.Load().(GrammarCache)
}

// SetDefaultCache replaces the default GrammarCache.  Panics if c is nil.
//
// This is intended for WASM builds that want to swap in a persistent
// storage-backed cache, or for tests that need isolation.
func SetDefaultCache(c GrammarCache) {
	if c == nil {
		panic("ast: SetDefaultCache called with nil cache")
	}
	defaultCache.Store(c)
}

// PreloadCache loads all SupportedLanguages grammars into the default cache.
// Unavailable grammars are silently skipped. Returns the number of grammars
// successfully loaded.
func PreloadCache() int {
	cache := DefaultCache()
	loaded := 0
	for lang := range SupportedLanguages {
		entry := grammars.DetectLanguageByName(lang)
		if entry == nil || entry.Language == nil {
			continue
		}
		l := entry.Language()
		if l == nil {
			continue
		}
		cache.Put(&GrammarBlob{
			Language: l,
			Name:     lang,
			LoadedAt: time.Now(),
			Size:     EstimateLanguageSize(l),
		})
		loaded++
	}
	return loaded
}
