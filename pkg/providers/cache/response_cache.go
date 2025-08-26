package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// ResponseCache provides caching for LLM provider responses
type ResponseCache struct {
	mu       sync.RWMutex
	cache    map[string]*CacheEntry
	config   CacheConfig
	stats    CacheStats
	evictors []EvictionPolicy
}

// CacheEntry represents a cached response
type CacheEntry struct {
	Key          string                  `json:"key"`
	Response     string                  `json:"response"`
	Metadata     *types.ResponseMetadata `json:"metadata"`
	CreatedAt    time.Time               `json:"created_at"`
	LastAccessed time.Time               `json:"last_accessed"`
	AccessCount  int64                   `json:"access_count"`
	Provider     string                  `json:"provider"`
	Model        string                  `json:"model"`
	TokenCount   int                     `json:"token_count"`
	Cost         float64                 `json:"cost"`
}

// CacheConfig configures the response cache
type CacheConfig struct {
	MaxSize       int           `json:"max_size"`        // Maximum number of cached responses
	TTL           time.Duration `json:"ttl"`             // Time to live for cache entries
	MaxMemoryMB   int           `json:"max_memory_mb"`   // Maximum memory usage in MB
	EnableMetrics bool          `json:"enable_metrics"`  // Enable cache metrics collection
	PersistToDisk bool          `json:"persist_to_disk"` // Persist cache to disk
	CacheDir      string        `json:"cache_dir"`       // Directory for disk persistence
}

// CacheStats tracks cache performance metrics
type CacheStats struct {
	Hits          int64   `json:"hits"`
	Misses        int64   `json:"misses"`
	Evictions     int64   `json:"evictions"`
	Size          int     `json:"size"`
	MemoryUsageMB float64 `json:"memory_usage_mb"`
	HitRate       float64 `json:"hit_rate"`
}

// EvictionPolicy defines cache eviction strategies
type EvictionPolicy interface {
	ShouldEvict(entry *CacheEntry, config CacheConfig) bool
	Priority(entry *CacheEntry) float64 // Higher priority = more likely to evict
}

// NewResponseCache creates a new response cache
func NewResponseCache(config CacheConfig) *ResponseCache {
	if config.MaxSize <= 0 {
		config.MaxSize = 1000
	}
	if config.TTL <= 0 {
		config.TTL = 30 * time.Minute
	}
	if config.MaxMemoryMB <= 0 {
		config.MaxMemoryMB = 100
	}

	cache := &ResponseCache{
		cache:  make(map[string]*CacheEntry),
		config: config,
		stats:  CacheStats{},
		evictors: []EvictionPolicy{
			&TTLEvictionPolicy{},
			&LRUEvictionPolicy{},
			&SizeEvictionPolicy{},
		},
	}

	// Start cleanup goroutine
	go cache.cleanupLoop()

	return cache
}

// Get retrieves a cached response
func (c *ResponseCache) Get(ctx context.Context, messages []types.Message, options types.RequestOptions, providerName string) (*CacheEntry, bool) {
	key := c.generateKey(messages, options, providerName)

	c.mu.RLock()
	entry, exists := c.cache[key]
	c.mu.RUnlock()

	if !exists {
		c.recordMiss()
		return nil, false
	}

	// Check if entry is still valid
	if c.isExpired(entry) {
		c.mu.Lock()
		delete(c.cache, key)
		c.mu.Unlock()
		c.recordMiss()
		return nil, false
	}

	// Update access statistics
	c.mu.Lock()
	entry.LastAccessed = time.Now()
	entry.AccessCount++
	c.mu.Unlock()

	c.recordHit()
	return entry, true
}

// Set stores a response in the cache
func (c *ResponseCache) Set(ctx context.Context, messages []types.Message, options types.RequestOptions, providerName string, response string, metadata *types.ResponseMetadata) error {
	key := c.generateKey(messages, options, providerName)

	now := time.Now()
	entry := &CacheEntry{
		Key:          key,
		Response:     response,
		Metadata:     metadata,
		CreatedAt:    now,
		LastAccessed: now,
		AccessCount:  0,
		Provider:     providerName,
		Model:        options.Model,
		TokenCount:   c.estimateTokenCount(response),
		Cost:         c.estimateCost(metadata),
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	// Check size limits before adding
	if c.shouldEvictBeforeAdd() {
		c.evictEntries()
	}

	c.cache[key] = entry
	c.updateStats()

	return nil
}

// generateKey creates a unique key for caching
func (c *ResponseCache) generateKey(messages []types.Message, options types.RequestOptions, providerName string) string {
	hasher := sha256.New()

	// Hash provider name
	hasher.Write([]byte(providerName))

	// Hash model
	hasher.Write([]byte(options.Model))

	// Hash temperature (rounded to avoid cache misses on tiny differences)
	tempStr := fmt.Sprintf("%.2f", options.Temperature)
	hasher.Write([]byte(tempStr))

	// Hash max tokens
	hasher.Write([]byte(fmt.Sprintf("%d", options.MaxTokens)))

	// Hash messages
	for _, msg := range messages {
		hasher.Write([]byte(msg.Role))
		hasher.Write([]byte(msg.Content))
	}

	return hex.EncodeToString(hasher.Sum(nil))
}

// isExpired checks if a cache entry has expired
func (c *ResponseCache) isExpired(entry *CacheEntry) bool {
	return time.Since(entry.CreatedAt) > c.config.TTL
}

// shouldEvictBeforeAdd checks if we need to evict before adding new entry
func (c *ResponseCache) shouldEvictBeforeAdd() bool {
	// Check size limit
	if len(c.cache) >= c.config.MaxSize {
		return true
	}

	// Check memory limit
	memUsage := c.estimateMemoryUsage()
	if memUsage > float64(c.config.MaxMemoryMB) {
		return true
	}

	return false
}

// evictEntries removes entries based on eviction policies
func (c *ResponseCache) evictEntries() {
	// Calculate eviction priorities for all entries
	type entryPriority struct {
		key      string
		entry    *CacheEntry
		priority float64
	}

	var priorities []entryPriority
	for key, entry := range c.cache {
		totalPriority := 0.0
		for _, evictor := range c.evictors {
			if evictor.ShouldEvict(entry, c.config) {
				totalPriority += evictor.Priority(entry)
			}
		}
		if totalPriority > 0 {
			priorities = append(priorities, entryPriority{
				key:      key,
				entry:    entry,
				priority: totalPriority,
			})
		}
	}

	// Sort by priority (highest first = most likely to evict)
	for i := 0; i < len(priorities)-1; i++ {
		for j := i + 1; j < len(priorities); j++ {
			if priorities[i].priority < priorities[j].priority {
				priorities[i], priorities[j] = priorities[j], priorities[i]
			}
		}
	}

	// Evict entries until we're under limits
	evicted := 0
	for _, p := range priorities {
		if !c.shouldEvictBeforeAdd() {
			break
		}

		delete(c.cache, p.key)
		evicted++
		c.stats.Evictions++

		// Don't evict too many at once
		if evicted >= 100 {
			break
		}
	}
}

// estimateMemoryUsage estimates current memory usage in MB
func (c *ResponseCache) estimateMemoryUsage() float64 {
	totalBytes := 0
	for _, entry := range c.cache {
		// Rough estimation: response + metadata + overhead
		entrySize := len(entry.Response) + len(entry.Key) + 200 // overhead
		if entry.Metadata != nil {
			entrySize += 100 // rough metadata size
		}
		totalBytes += entrySize
	}
	return float64(totalBytes) / (1024 * 1024) // Convert to MB
}

// estimateTokenCount estimates token count for a response
func (c *ResponseCache) estimateTokenCount(response string) int {
	// Rough estimate: 4 characters per token
	return len(response) / 4
}

// estimateCost estimates the cost of a response
func (c *ResponseCache) estimateCost(metadata *types.ResponseMetadata) float64 {
	if metadata != nil && metadata.Cost > 0 {
		return metadata.Cost
	}
	return 0.0
}

// recordHit updates hit statistics
func (c *ResponseCache) recordHit() {
	if !c.config.EnableMetrics {
		return
	}
	c.mu.Lock()
	c.stats.Hits++
	c.updateHitRate()
	c.mu.Unlock()
}

// recordMiss updates miss statistics
func (c *ResponseCache) recordMiss() {
	if !c.config.EnableMetrics {
		return
	}
	c.mu.Lock()
	c.stats.Misses++
	c.updateHitRate()
	c.mu.Unlock()
}

// updateStats updates cache statistics
func (c *ResponseCache) updateStats() {
	if !c.config.EnableMetrics {
		return
	}
	c.stats.Size = len(c.cache)
	c.stats.MemoryUsageMB = c.estimateMemoryUsage()
}

// updateHitRate updates the cache hit rate
func (c *ResponseCache) updateHitRate() {
	total := c.stats.Hits + c.stats.Misses
	if total > 0 {
		c.stats.HitRate = float64(c.stats.Hits) / float64(total)
	}
}

// cleanupLoop periodically cleans up expired entries
func (c *ResponseCache) cleanupLoop() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		c.cleanup()
	}
}

// cleanup removes expired entries
func (c *ResponseCache) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()

	toRemove := make([]string, 0)
	for key, entry := range c.cache {
		if c.isExpired(entry) {
			toRemove = append(toRemove, key)
		}
	}

	for _, key := range toRemove {
		delete(c.cache, key)
		c.stats.Evictions++
	}

	c.updateStats()
}

// GetStats returns current cache statistics
func (c *ResponseCache) GetStats() CacheStats {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Update real-time stats
	stats := c.stats
	stats.Size = len(c.cache)
	stats.MemoryUsageMB = c.estimateMemoryUsage()

	return stats
}

// Clear empties the cache
func (c *ResponseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cache = make(map[string]*CacheEntry)
	c.stats = CacheStats{}
}

// Close shuts down the cache
func (c *ResponseCache) Close() error {
	c.Clear()
	return nil
}

// CachingProvider wraps a provider with caching capabilities
type CachingProvider struct {
	provider interfaces.LLMProvider
	cache    *ResponseCache
}

// NewCachingProvider creates a new caching provider wrapper
func NewCachingProvider(provider interfaces.LLMProvider, cache *ResponseCache) *CachingProvider {
	return &CachingProvider{
		provider: provider,
		cache:    cache,
	}
}

// Implement interfaces.LLMProvider interface with caching
func (cp *CachingProvider) GetName() string {
	return cp.provider.GetName()
}

func (cp *CachingProvider) GetModels(ctx context.Context) ([]types.ModelInfo, error) {
	return cp.provider.GetModels(ctx)
}

func (cp *CachingProvider) GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	// Try to get from cache first
	if entry, found := cp.cache.Get(ctx, messages, options, cp.provider.GetName()); found {
		return entry.Response, entry.Metadata, nil
	}

	// Generate response from provider
	response, metadata, err := cp.provider.GenerateResponse(ctx, messages, options)
	if err != nil {
		return "", nil, err
	}

	// Cache the response
	cp.cache.Set(ctx, messages, options, cp.provider.GetName(), response, metadata)

	return response, metadata, nil
}

func (cp *CachingProvider) GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error) {
	// Note: Streaming responses are not cached for now
	return cp.provider.GenerateResponseStream(ctx, messages, options, writer)
}

func (cp *CachingProvider) IsAvailable(ctx context.Context) error {
	return cp.provider.IsAvailable(ctx)
}

func (cp *CachingProvider) EstimateTokens(messages []types.Message) (int, error) {
	return cp.provider.EstimateTokens(messages)
}

func (cp *CachingProvider) CalculateCost(usage types.TokenUsage) float64 {
	return cp.provider.CalculateCost(usage)
}

// Eviction Policies

// TTLEvictionPolicy evicts entries based on time-to-live
type TTLEvictionPolicy struct{}

func (p *TTLEvictionPolicy) ShouldEvict(entry *CacheEntry, config CacheConfig) bool {
	return time.Since(entry.CreatedAt) > config.TTL
}

func (p *TTLEvictionPolicy) Priority(entry *CacheEntry) float64 {
	age := time.Since(entry.CreatedAt)
	return age.Seconds() // Older = higher priority for eviction
}

// LRUEvictionPolicy evicts least recently used entries
type LRUEvictionPolicy struct{}

func (p *LRUEvictionPolicy) ShouldEvict(entry *CacheEntry, config CacheConfig) bool {
	return time.Since(entry.LastAccessed) > config.TTL/2 // Half TTL for LRU
}

func (p *LRUEvictionPolicy) Priority(entry *CacheEntry) float64 {
	timeSinceAccess := time.Since(entry.LastAccessed)
	return timeSinceAccess.Seconds() // Longer since last access = higher priority
}

// SizeEvictionPolicy evicts entries when size limits are reached
type SizeEvictionPolicy struct{}

func (p *SizeEvictionPolicy) ShouldEvict(entry *CacheEntry, config CacheConfig) bool {
	return true // Size eviction can apply to any entry
}

func (p *SizeEvictionPolicy) Priority(entry *CacheEntry) float64 {
	// Lower access count = higher priority for eviction
	if entry.AccessCount == 0 {
		return 1000.0 // Never accessed entries have highest priority
	}
	return 100.0 / float64(entry.AccessCount)
}
