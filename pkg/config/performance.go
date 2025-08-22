package config

// PerformanceConfig contains all performance tuning and optimization settings
type PerformanceConfig struct {
	// Batch Processing
	FileBatchSize      int `json:"file_batch_size"`      // Batch size for file processing
	EmbeddingBatchSize int `json:"embedding_batch_size"` // Batch size for embedding generation

	// Rate Limiting
	MaxConcurrentRequests int `json:"max_concurrent_requests"` // Max concurrent API requests
	RequestDelayMs        int `json:"request_delay_ms"`        // Delay between requests in milliseconds

	// Timeouts
	ShellTimeoutSecs int `json:"shell_timeout_secs"` // Shell command timeout in seconds
	MaxRunSeconds    int `json:"max_run_seconds"`    // Maximum execution time in seconds

	// Resource Limits
	MaxRunTokens  int     `json:"max_run_tokens"`   // Maximum tokens per run
	MaxRunCostUSD float64 `json:"max_run_cost_usd"` // Maximum cost per run in USD

	// Memory Management
	MemoryLimitMB int `json:"memory_limit_mb"` // Memory limit in megabytes

	// Caching
	EnableCache   bool `json:"enable_cache"`    // Enable caching
	CacheSizeMB   int  `json:"cache_size_mb"`   // Cache size in megabytes
	CacheTTLHours int  `json:"cache_ttl_hours"` // Cache TTL in hours
}

// DefaultPerformanceConfig returns sensible defaults for performance configuration
func DefaultPerformanceConfig() *PerformanceConfig {
	return &PerformanceConfig{
		FileBatchSize:         10,
		EmbeddingBatchSize:    100,
		MaxConcurrentRequests: 5,
		RequestDelayMs:        100,
		ShellTimeoutSecs:      30,
		MaxRunSeconds:         300, // 5 minutes
		MaxRunTokens:          10000,
		MaxRunCostUSD:         1.0,  // $1.00
		MemoryLimitMB:         1024, // 1GB
		EnableCache:           true,
		CacheSizeMB:           100,
		CacheTTLHours:         24,
	}
}

// GetOptimalBatchSize returns the optimal batch size based on available resources
func (c *PerformanceConfig) GetOptimalBatchSize() int {
	if c.FileBatchSize <= 0 {
		return 10 // default
	}
	return c.FileBatchSize
}

// GetRequestDelay returns the delay between API requests in milliseconds
func (c *PerformanceConfig) GetRequestDelay() int {
	if c.RequestDelayMs <= 0 {
		return 100 // default 100ms
	}
	return c.RequestDelayMs
}

// IsWithinLimits checks if current usage is within configured limits
func (c *PerformanceConfig) IsWithinLimits(tokens int, cost float64, durationSeconds int) bool {
	if c.MaxRunTokens > 0 && tokens > c.MaxRunTokens {
		return false
	}
	if c.MaxRunCostUSD > 0 && cost > c.MaxRunCostUSD {
		return false
	}
	if c.MaxRunSeconds > 0 && durationSeconds > c.MaxRunSeconds {
		return false
	}
	return true
}

// ShouldUseCache returns true if caching should be enabled
func (c *PerformanceConfig) ShouldUseCache() bool {
	return c.EnableCache && c.CacheSizeMB > 0
}

// GetCacheTTLSeconds returns the cache TTL in seconds
func (c *PerformanceConfig) GetCacheTTLSeconds() int {
	if c.CacheTTLHours <= 0 {
		return 86400 // 24 hours default
	}
	return c.CacheTTLHours * 3600
}

// IsHighConcurrency returns true if the system is configured for high concurrency
func (c *PerformanceConfig) IsHighConcurrency() bool {
	return c.MaxConcurrentRequests > 10
}
