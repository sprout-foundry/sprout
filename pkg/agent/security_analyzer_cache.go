// Package agent: session-scoped cache for LLM security analyses.
package agent

import "sync"

// SecurityAnalysisCache caches LLM security analyses keyed by command string.
type SecurityAnalysisCache struct {
	mu    sync.RWMutex
	items map[string]*SecurityAnalysis
}

// NewSecurityAnalysisCache creates an empty cache.
func NewSecurityAnalysisCache() *SecurityAnalysisCache {
	return &SecurityAnalysisCache{items: make(map[string]*SecurityAnalysis)}
}

// Get returns the cached analysis for a normalized key, or false if not found.
func (c *SecurityAnalysisCache) Get(normalizedKey string) (*SecurityAnalysis, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sa, ok := c.items[normalizedKey]
	return sa, ok
}

// Set stores an analysis under a normalized key.
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
