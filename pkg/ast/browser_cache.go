//go:build js && wasm

package ast

import (
	"encoding/json"
	"slices"
	"sync"
	"sync/atomic"
	"syscall/js"
	"time"
)

// BrowserCache implements GrammarCache with localStorage persistence for
// browser/WASM builds.  It keeps live *gotreesitter.Language pointers in
// an in-memory map (since they cannot be serialised), but persists grammar
// metadata (name, size, loadedAt) to localStorage so that subsequent page
// loads can detect which grammars were previously loaded.
//
// On restoration, localStorage metadata is NOT inserted into the live
// in-memory map — only the metadata Size counter is restored for stats.
// Callers must call PreloadCache() to populate the live Language pointers.
// This avoids Get() returning blobs with nil Language.
//
// All methods are safe for concurrent use by multiple goroutines.
type BrowserCache struct {
	mu        sync.RWMutex
	data      map[string]*GrammarBlob
	hits      atomic.Int64
	misses    atomic.Int64
	evictions atomic.Int64
}

const localStoragePrefix = "sprout:ast:grammar:"

// localStorageEntry is the JSON-serialisable metadata persisted to
// localStorage for each cached grammar.  It does NOT include the Language
// pointer because that cannot be serialised.
type localStorageEntry struct {
	Name     string `json:"name"`
	Size     int    `json:"size"`
	LoadedAt string `json:"loadedAt"`
}

// NewBrowserCache creates an empty BrowserCache.  Live Language pointers
// are NOT restored from any previous session; call PreloadCache() to
// populate them.  Use CachedGrammarNames() to discover which grammars
// were persisted to localStorage in a previous session.
func NewBrowserCache() *BrowserCache {
	return &BrowserCache{
		data: make(map[string]*GrammarBlob),
	}
}

// loadMetadata reads localStorage entries for known supported languages and
// returns the set of language names that have cached metadata.
// This allows callers to selectively preload only previously-cached grammars.
func (bc *BrowserCache) loadMetadata() []string {
	global := js.Global()
	localStorage := global.Get("localStorage")
	if !localStorage.Truthy() {
		return nil
	}

	var cached []string
	for lang := range SupportedLanguages {
		key := localStoragePrefix + lang
		val, ok := safeJSGet(localStorage, key)
		if !ok || !val.Truthy() {
			continue
		}
		str := val.String()
		if str == "" {
			continue
		}
		var entry localStorageEntry
		if err := json.Unmarshal([]byte(str), &entry); err != nil {
			continue
		}
		if entry.Name != "" {
			cached = append(cached, lang)
		}
	}
	return cached
}

// saveToLocalStorage persists GrammarBlob metadata to localStorage.
// Panics from JS interop (e.g., quota exceeded, disabled storage) are
// recovered silently — the cache works fine without persistence.
func (bc *BrowserCache) saveToLocalStorage(blob *GrammarBlob) {
	defer recoverJSPanic("saveToLocalStorage")

	global := js.Global()
	localStorage := global.Get("localStorage")
	if !localStorage.Truthy() {
		return
	}

	entry := localStorageEntry{
		Name:     blob.Name,
		Size:     blob.Size,
		LoadedAt: blob.LoadedAt.Format(time.RFC3339),
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return
	}
	key := localStoragePrefix + blob.Name
	localStorage.Call("setItem", key, string(data))
}

// removeFromLocalStorage removes the localStorage entry for a grammar name.
func (bc *BrowserCache) removeFromLocalStorage(name string) {
	defer recoverJSPanic("removeFromLocalStorage")

	global := js.Global()
	localStorage := global.Get("localStorage")
	if !localStorage.Truthy() {
		return
	}
	key := localStoragePrefix + name
	localStorage.Call("removeItem", key)
}

// clearLocalStorage removes all entries with the localStoragePrefix.
// It collects matching keys first, then removes them, to avoid index-shift
// bugs caused by forward iteration with live removal.
func (bc *BrowserCache) clearLocalStorage() {
	defer recoverJSPanic("clearLocalStorage")

	global := js.Global()
	localStorage := global.Get("localStorage")
	if !localStorage.Truthy() {
		return
	}

	lengthVal := localStorage.Get("length")
	if !isJSNumber(lengthVal) {
		return
	}
	length := lengthVal.Int()
	prefixLen := len(localStoragePrefix)

	var keysToRemove []string
	for i := 0; i < length; i++ {
		keyVal := localStorage.Call("key", i)
		if !isJSString(keyVal) {
			continue
		}
		key := keyVal.String()
		if len(key) >= prefixLen && key[:prefixLen] == localStoragePrefix {
			keysToRemove = append(keysToRemove, key)
		}
	}
	for _, key := range keysToRemove {
		localStorage.Call("removeItem", key)
	}
}

// Get returns the cached GrammarBlob for name, or (nil, false) if not found.
// Increments the hit counter on found, miss counter on not-found.
//
// Note: Get only returns blobs with live Language pointers (populated by
// Put).  Metadata restored from localStorage is NOT added to the live map,
// so callers never receive a blob with nil Language.
func (bc *BrowserCache) Get(name string) (*GrammarBlob, bool) {
	bc.mu.RLock()
	blob, ok := bc.data[name]
	bc.mu.RUnlock()
	if ok {
		bc.hits.Add(1)
		return blob, true
	}
	bc.misses.Add(1)
	return nil, false
}

// Put stores a GrammarBlob in memory and persists its metadata to
// localStorage.  If a blob with the same name already exists, it is
// replaced and the eviction counter is incremented.
func (bc *BrowserCache) Put(blob *GrammarBlob) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	if _, exists := bc.data[blob.Name]; exists {
		bc.evictions.Add(1)
	}
	bc.data[blob.Name] = blob
	bc.saveToLocalStorage(blob)
}

// Invalidate removes the cache entry for name from memory and localStorage.
// If no entry exists, this is a no-op.
func (bc *BrowserCache) Invalidate(name string) {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	delete(bc.data, name)
	bc.removeFromLocalStorage(name)
}

// InvalidateAll removes all entries from the in-memory cache and clears
// all localStorage entries with the prefix.
func (bc *BrowserCache) InvalidateAll() {
	bc.mu.Lock()
	defer bc.mu.Unlock()
	clear(bc.data)
	bc.clearLocalStorage()
}

// Stats returns current cache statistics.
func (bc *BrowserCache) Stats() CacheStats {
	bc.mu.RLock()
	size := len(bc.data)
	bc.mu.RUnlock()
	return CacheStats{
		Hits:      bc.hits.Load(),
		Misses:    bc.misses.Load(),
		Evictions: bc.evictions.Load(),
		Size:      size,
	}
}

// Names returns a sorted copy of all cached language names from the
// in-memory map.
func (bc *BrowserCache) Names() []string {
	bc.mu.RLock()
	names := make([]string, 0, len(bc.data))
	for name := range bc.data {
		names = append(names, name)
	}
	bc.mu.RUnlock()
	slices.Sort(names)
	return names
}

// InitBrowserCache replaces the package-level default cache with a new
// BrowserCache.  It is intended to be called once during application
// initialisation in WASM builds (e.g. from cmd/wasm/main.go).
//
// On non-WASM builds this is a no-op; see browser_cache_stub.go.
func InitBrowserCache() {
	SetDefaultCache(NewBrowserCache())
}

// CachedGrammarNames returns the set of language names that have persisted
// metadata in localStorage from a previous session.  This allows callers
// to selectively preload only known grammars instead of all of them.
//
// On non-WASM builds this returns nil; see browser_cache_stub.go.
func CachedGrammarNames() []string {
	bc, ok := DefaultCache().(*BrowserCache)
	if !ok {
		return nil
	}
	return bc.loadMetadata()
}

// ---------------------------------------------------------------------------
// JS interop helpers (with panic recovery)
// ---------------------------------------------------------------------------

// recoverJSPanic recovers from panics in syscall/js calls.  localStorage
// operations can panic if storage is disabled, quota is exceeded, or the
// JS runtime is in an unexpected state.
func recoverJSPanic(context string) {
	_ = recover() // silently swallow JS panics
}

// safeJSGet calls localStorage.getItem and returns (js.Value, false) if the
// value is null/undefined or if the call panics.
func safeJSGet(localStorage js.Value, key string) (js.Value, bool) {
	defer func() { recover() }()
	val := localStorage.Call("getItem", key)
	if val.Type() == js.TypeNull || val.Type() == js.TypeUndefined {
		return js.Value{}, false
	}
	return val, true
}

// isJSNumber returns true if the js.Value is a number type.
func isJSNumber(v js.Value) bool {
	return v.Type() == js.TypeNumber
}

// isJSString returns true if the js.Value is a string type.
func isJSString(v js.Value) bool {
	return v.Type() == js.TypeString
}

// Ensure BrowserCache implements GrammarCache at compile time.
var _ GrammarCache = (*BrowserCache)(nil)
