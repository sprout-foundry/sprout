package tools

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// ---------------------------------------------------------------------------
// VisionCacheStatsSnapshot — plain struct for reporting (non-atomic)
// ---------------------------------------------------------------------------

type VisionCacheStatsSnapshot struct {
	Hits       uint64
	Misses     uint64
	Evictions  uint64
	Size       int64
	Insertions uint64
}

// ---------------------------------------------------------------------------
// VisionCacheStats — atomic counters for cache monitoring
// ---------------------------------------------------------------------------

type VisionCacheStats struct {
	Hits       atomic.Uint64
	Misses     atomic.Uint64
	Evictions  atomic.Uint64
	Size       atomic.Int64
	Insertions atomic.Uint64
}

// ---------------------------------------------------------------------------
// visionCacheEntry — node in the hand-rolled doubly-linked list
// ---------------------------------------------------------------------------

type visionCacheEntry struct {
	key   string
	result string
	usage  *VisionUsageInfo
	prev, next *visionCacheEntry
}

// ---------------------------------------------------------------------------
// VisionLRUCache — mutex-protected, capacity-bounded LRU cache
//
// Uses a hand-rolled doubly-linked list with head/tail sentinels.
// head.next is the most recently used entry; tail.prev is the least.
// ---------------------------------------------------------------------------

type VisionLRUCache struct {
	mu       sync.Mutex
	capacity int
	stats    VisionCacheStats
	entries  map[string]*visionCacheEntry
	head     *visionCacheEntry // sentinel (most-recent side)
	tail     *visionCacheEntry // sentinel (least-recent side)
}

// NewVisionLRUCache creates a new LRU cache with the given capacity.
func NewVisionLRUCache(capacity int) *VisionLRUCache {
	c := &VisionLRUCache{
		capacity: capacity,
		entries:  make(map[string]*visionCacheEntry),
		head:     &visionCacheEntry{},
		tail:     &visionCacheEntry{},
	}
	c.head.next = c.tail
	c.tail.prev = c.head
	return c
}

// Get looks up key. On hit the entry is moved to the front (most recently
// used) and a hit is recorded. On miss a miss is recorded.
func (c *VisionLRUCache) Get(key string) (string, *VisionUsageInfo, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	e, ok := c.entries[key]
	if !ok {
		c.stats.Misses.Add(1)
		return "", nil, false
	}
	// Move to front (most recently used)
	c.detach(e)
	c.pushFront(e)
	c.stats.Hits.Add(1)
	return e.result, e.usage, true
}

// Put inserts or updates key. If the key already exists, the value is updated
// and the entry is moved to the front. If the cache is at capacity, the least
// recently used entry (tail.prev) is evicted first.
func (c *VisionLRUCache) Put(key, result string, usage *VisionUsageInfo) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Update existing entry
	if e, ok := c.entries[key]; ok {
		e.result = result
		e.usage = usage
		c.detach(e)
		c.pushFront(e)
		return
	}

	// Evict if at capacity
	if len(c.entries) >= c.capacity {
		c.evictTail()
	}

	// Insert new entry at front
	e := &visionCacheEntry{
		key:    key,
		result: result,
		usage:  usage,
	}
	c.entries[key] = e
	c.pushFront(e)
	c.stats.Size.Add(1)
	c.stats.Insertions.Add(1)
}

// Stats returns a snapshot of the current cache statistics.
func (c *VisionLRUCache) Stats() VisionCacheStatsSnapshot {
	return VisionCacheStatsSnapshot{
		Hits:       c.stats.Hits.Load(),
		Misses:     c.stats.Misses.Load(),
		Evictions:  c.stats.Evictions.Load(),
		Size:       c.stats.Size.Load(),
		Insertions: c.stats.Insertions.Load(),
	}
}

// Reset clears all entries and resets stats, preserving capacity.
func (c *VisionLRUCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.entries = make(map[string]*visionCacheEntry)
	c.head.next = c.tail
	c.tail.prev = c.head
	// Reset atomic counters
	c.stats.Hits.Store(0)
	c.stats.Misses.Store(0)
	c.stats.Evictions.Store(0)
	c.stats.Size.Store(0)
	c.stats.Insertions.Store(0)
}

// Capacity returns the configured capacity.
func (c *VisionLRUCache) Capacity() int {
	return c.capacity
}

// CurrentSize returns the number of entries currently in the cache.
func (c *VisionLRUCache) CurrentSize() int64 {
	return c.stats.Size.Load()
}

// ---------------------------------------------------------------------------
// Linked-list helpers (caller must hold c.mu)
// ---------------------------------------------------------------------------

// pushFront inserts e right after head (most recently used position).
func (c *VisionLRUCache) pushFront(e *visionCacheEntry) {
	e.prev = c.head
	e.next = c.head.next
	c.head.next.prev = e
	c.head.next = e
}

// detach removes e from the list without freeing it.
func (c *VisionLRUCache) detach(e *visionCacheEntry) {
	e.prev.next = e.next
	e.next.prev = e.prev
}

// evictTail removes the least recently used entry (tail.prev).
func (c *VisionLRUCache) evictTail() {
	lru := c.tail.prev
	if lru == c.head {
		return // empty list guard
	}
	c.detach(lru)
	delete(c.entries, lru.key)
	c.stats.Size.Add(-1)
	c.stats.Evictions.Add(1)
}

// ---------------------------------------------------------------------------
// Cache key generation
// ---------------------------------------------------------------------------

// visionCacheKey generates a sha256-based cache key from the image path,
// modification time, analysis mode, and prompt.
//
// For local files, mtime_ns comes from os.Stat. For remote URLs, the URL
// length is used as a stable proxy.
func visionCacheKey(imagePath, analysisMode, analysisPrompt string) string {
	mtime := int64(0)
	lowerPath := strings.ToLower(imagePath)
	if !strings.HasPrefix(lowerPath, "http://") && !strings.HasPrefix(lowerPath, "https://") {
		if fi, err := os.Stat(imagePath); err == nil {
			mtime = fi.ModTime().UnixNano()
		}
	} else {
		// For URLs, fall back to URL length as a stable proxy.
		mtime = int64(len(imagePath))
	}

	h := sha256.New()
	h.Write([]byte(imagePath))
	binary.Write(h, binary.LittleEndian, mtime)
	h.Write([]byte{0}) // separator
	h.Write([]byte(analysisMode))
	h.Write([]byte{0}) // separator
	h.Write([]byte(analysisPrompt))
	return hex.EncodeToString(h.Sum(nil))
}

// ---------------------------------------------------------------------------
// Package-level LRU instance
// ---------------------------------------------------------------------------

var visionLRU = newDefaultVisionLRU()

func newDefaultVisionLRU() *VisionLRUCache {
	cap := 256
	if raw := configuration.GetEnvSimple("VISION_CACHE_SIZE"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			cap = v
		}
	}
	return NewVisionLRUCache(cap)
}

// ---------------------------------------------------------------------------
// Backward-compat helpers for tests that directly access the old globals
// ---------------------------------------------------------------------------

// visionCacheSnapshot returns a copy of the current cache contents as maps
// matching the old visionCache / visionCacheUsage signatures.
func visionCacheSnapshot() (map[string]string, map[string]*VisionUsageInfo) {
	visionLRU.mu.Lock()
	defer visionLRU.mu.Unlock()
	m := make(map[string]string, len(visionLRU.entries))
	u := make(map[string]*VisionUsageInfo, len(visionLRU.entries))
	for k, e := range visionLRU.entries {
		m[k] = e.result
		u[k] = e.usage
	}
	return m, u
}

// resetVisionCache clears all cache entries and resets stats.
func resetVisionCache() {
	visionLRU.Reset()
}
