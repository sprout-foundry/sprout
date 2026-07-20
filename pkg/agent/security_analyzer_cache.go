// Package agent: session-scoped cache for LLM security analyses.
package agent

import "sync"

// SecurityAnalysisCache is a session-scoped cache of LLM security analyses
// keyed by command string. Identical commands within a session reuse the
// cached analysis to avoid N LLM calls for repeated identical prompts
// (e.g., a tight loop calling `make build`).
//
// SP-124: cache lives on the Agent; one per agent instance. Not persisted.
type SecurityAnalysisCache struct {
	mu    sync.RWMutex
	items map[string]*SecurityAnalysis
}

// NewSecurityAnalysisCache creates an empty cache.
func NewSecurityAnalysisCache() *SecurityAnalysisCache {
	return &SecurityAnalysisCache{items: make(map[string]*SecurityAnalysis)}
}

// Get returns the cached analysis for a normalized cache key, or false if not found.
// The caller is responsible for normalizing via ChainCacheKey(cmd) before calling.
// A pre-normalized key is accepted directly so that callers who have already
// normalized (e.g., the broker that calls ChainCacheKey before Set/Get) are
// not double-normalized.
func (c *SecurityAnalysisCache) Get(normalizedKey string) (*SecurityAnalysis, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sa, ok := c.items[normalizedKey]
	return sa, ok
}

// Set stores an analysis under a normalized cache key. The caller is responsible
// for normalizing via ChainCacheKey(cmd) before calling. A pre-normalized key
// is accepted directly so that callers who have already normalized (e.g., the
// broker that calls ChainCacheKey before Set/Get) are not double-normalized.
func (c *SecurityAnalysisCache) Set(normalizedKey string, sa *SecurityAnalysis) {
	if c == nil || sa == nil || normalizedKey == "" {
		return
	}
	c.mu.Lock()
	c.items[normalizedKey] = sa
	c.mu.Unlock()
}

// Clear resets the cache to empty.
func (c *SecurityAnalysisCache) Clear() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.items = make(map[string]*SecurityAnalysis)
	c.mu.Unlock()
}
