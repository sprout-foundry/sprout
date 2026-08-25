// Caching layer for embedding providers

package embedding

import (
	"container/list"
	"context"
	"crypto/md5"
	"fmt"
	"sync"
)

const maxEmbedCacheEntries = 1024

// cachedProvider wraps an EmbeddingProvider with a content-hash cache.
// Identical text inputs return the cached vector without re-embedding.
//
// The eviction order is a FIFO backed by container/list (doubly-linked
// list), NOT a slice. A reslicing slice (`s = s[1:]`) leaks its backing
// array: the array's capacity only grows over the cache's lifetime, so a
// long-running daemon would accumulate a ~1M-entry string-header array
// (~15MB) holding only 1024 live entries. The list's nodes are GC'd on
// eviction, keeping the cache's memory footprint strictly bounded.
type cachedProvider struct {
	inner     EmbeddingProvider
	cache     map[string][]float32
	cacheMu   sync.Mutex
	cacheList *list.List // FIFO eviction order; *list.Element is the map value
}

func newCachedProvider(inner EmbeddingProvider) *cachedProvider {
	return &cachedProvider{
		inner:     inner,
		cache:     make(map[string][]float32),
		cacheList: list.New(),
	}
}

func contentHash(text string) string {
	// Non-cryptographic cache key. md5 is used purely for its uniform
	// 128-bit distribution as a map key; collision resistance is irrelevant
	// (worst case is a harmless cache miss on collision).
	return fmt.Sprintf("%x", md5.Sum([]byte(text)))
}

func (c *cachedProvider) evictIfFull() {
	for c.cacheList.Len() >= maxEmbedCacheEntries {
		oldest := c.cacheList.Front()
		if oldest == nil {
			break
		}
		h := oldest.Value.(string)
		c.cacheList.Remove(oldest)
		delete(c.cache, h)
	}
}

func (c *cachedProvider) Embed(ctx context.Context, text string) ([]float32, error) {
	h := contentHash(text)
	c.cacheMu.Lock()
	if v, ok := c.cache[h]; ok {
		// Return a copy so the caller can't mutate the cached vector.
		res := make([]float32, len(v))
		copy(res, v)
		c.cacheMu.Unlock()
		return res, nil
	}
	c.cacheMu.Unlock()

	vec, err := c.inner.Embed(ctx, text)
	if err != nil {
		return nil, err
	}

	c.cacheMu.Lock()
	defer c.cacheMu.Unlock()
	// Double-check: another goroutine may have populated the cache while we
	// were embedding. Return a copy of the cached entry so callers can't
	// mutate the shared vector.
	if v, ok := c.cache[h]; ok {
		res := make([]float32, len(v))
		copy(res, v)
		return res, nil
	}
	c.evictIfFull()
	// Store a private copy in the cache; return the original to the caller.
	// If we cached vec itself, a mutating caller would corrupt the cache.
	cached := make([]float32, len(vec))
	copy(cached, vec)
	c.cache[h] = cached
	c.cacheList.PushBack(h)
	return vec, nil
}

func (c *cachedProvider) EmbedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	// Check cache for each text; collect misses.
	type miss struct {
		index int
		text  string
	}
	hits := make([][]float32, len(texts))
	var misses []miss

	for i, text := range texts {
		h := contentHash(text)
		c.cacheMu.Lock()
		if v, ok := c.cache[h]; ok {
			res := make([]float32, len(v))
			copy(res, v)
			hits[i] = res
			c.cacheMu.Unlock()
			continue
		}
		c.cacheMu.Unlock()
		misses = append(misses, miss{index: i, text: text})
	}

	if len(misses) > 0 {
		batchTexts := make([]string, len(misses))
		for i, m := range misses {
			batchTexts[i] = m.text
		}
		results, err := c.inner.EmbedBatch(ctx, batchTexts)
		if err != nil {
			return nil, err
		}

		c.cacheMu.Lock()
		for i, m := range misses {
			h := contentHash(m.text)
			// Double-check on miss.
			if v, ok := c.cache[h]; ok {
				res := make([]float32, len(v))
				copy(res, v)
				hits[m.index] = res
			} else {
				vec := results[i]
				c.evictIfFull()
				// Cache a private copy; return the original to the caller.
				cached := make([]float32, len(vec))
				copy(cached, vec)
				c.cache[h] = cached
				c.cacheList.PushBack(h)
				hits[m.index] = vec
			}
		}
		c.cacheMu.Unlock()
	}

	return hits, nil
}

func (c *cachedProvider) Dimensions() int {
	return c.inner.Dimensions()
}

func (c *cachedProvider) Name() string {
	return c.inner.Name()
}

func (c *cachedProvider) ModelHash() string {
	return c.inner.ModelHash()
}

// EmbedWithPrefix is intentionally uncached: prefix embedding is used by the
// code-index search path (index.go QuerySimilar), which goes through the
// IndexManager's own provider reference, NOT this cachedProvider. Only the
// conversation store (EmbedAndStoreTurn, rollup embedding, semantic recall,
// proactive context) flows through the cached wrapper, and those paths call
// Embed/EmbedBatch without a prefix. Caching prefixed calls with a composite
// key would add complexity for a path that never re-embeds the same text.
func (c *cachedProvider) EmbedWithPrefix(ctx context.Context, text string, prefix string) ([]float32, error) {
	return c.inner.EmbedWithPrefix(ctx, text, prefix)
}

func (c *cachedProvider) EmbedBatchWithPrefix(ctx context.Context, texts []string, prefix string) ([][]float32, error) {
	return c.inner.EmbedBatchWithPrefix(ctx, texts, prefix)
}

func (c *cachedProvider) Close() error {
	return nil // the inner provider is managed by the EmbeddingManager
}
