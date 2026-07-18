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

// Get returns the cached analysis for a command, or false if not found.
func (c *SecurityAnalysisCache) Get(command string) (*SecurityAnalysis, bool) {
	if c == nil {
		return nil, false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	sa, ok := c.items[command]
	return sa, ok
}

// Set stores an analysis in the cache.
func (c *SecurityAnalysisCache) Set(command string, sa *SecurityAnalysis) {
	if c == nil || sa == nil {
		return
	}
	c.mu.Lock()
	c.items[command] = sa
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
