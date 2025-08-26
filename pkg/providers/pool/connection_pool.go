package pool

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// ConnectionPool manages a pool of provider connections for optimal performance
type ConnectionPool struct {
	mu          sync.RWMutex
	providers   map[string]*PooledProvider
	maxIdle     int
	maxActive   int
	idleTimeout time.Duration
	maxLifetime time.Duration
	factory     ProviderFactory
}

// PooledProvider wraps a provider with pooling metadata
type PooledProvider struct {
	provider    interfaces.LLMProvider
	created     time.Time
	lastUsed    time.Time
	inUse       bool
	usageCount  int64
	maxRequests int64
}

// ProviderFactory creates new provider instances
type ProviderFactory interface {
	CreateProvider(config *types.ProviderConfig) (interfaces.LLMProvider, error)
}

// PoolConfig configures the connection pool
type PoolConfig struct {
	MaxIdle     int           `json:"max_idle"`     // Maximum idle connections
	MaxActive   int           `json:"max_active"`   // Maximum active connections
	IdleTimeout time.Duration `json:"idle_timeout"` // Idle connection timeout
	MaxLifetime time.Duration `json:"max_lifetime"` // Maximum connection lifetime
	MaxRequests int64         `json:"max_requests"` // Max requests per connection
}

// NewConnectionPool creates a new connection pool
func NewConnectionPool(config PoolConfig, factory ProviderFactory) *ConnectionPool {
	if config.MaxIdle <= 0 {
		config.MaxIdle = 10
	}
	if config.MaxActive <= 0 {
		config.MaxActive = 50
	}
	if config.IdleTimeout <= 0 {
		config.IdleTimeout = 5 * time.Minute
	}
	if config.MaxLifetime <= 0 {
		config.MaxLifetime = 30 * time.Minute
	}
	if config.MaxRequests <= 0 {
		config.MaxRequests = 1000
	}

	pool := &ConnectionPool{
		providers:   make(map[string]*PooledProvider),
		maxIdle:     config.MaxIdle,
		maxActive:   config.MaxActive,
		idleTimeout: config.IdleTimeout,
		maxLifetime: config.MaxLifetime,
		factory:     factory,
	}

	// Start cleanup goroutine
	go pool.cleanupLoop()

	return pool
}

// GetProvider gets a provider from the pool or creates a new one
func (p *ConnectionPool) GetProvider(providerKey string, config *types.ProviderConfig) (interfaces.LLMProvider, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	// Try to get existing provider
	if pooled, exists := p.providers[providerKey]; exists {
		if p.isValidConnection(pooled) && !pooled.inUse {
			pooled.inUse = true
			pooled.lastUsed = time.Now()
			pooled.usageCount++
			return &PooledProviderWrapper{
				provider: pooled.provider,
				pool:     p,
				key:      providerKey,
				pooled:   pooled,
			}, nil
		}
		// Remove invalid connection
		delete(p.providers, providerKey)
	}

	// Check active connection limit
	activeCount := p.getActiveCount()
	if activeCount >= p.maxActive {
		return nil, &PoolError{
			Type:    "max_active_exceeded",
			Message: "maximum active connections exceeded",
			Details: map[string]interface{}{
				"active_count": activeCount,
				"max_active":   p.maxActive,
			},
		}
	}

	// Create new provider
	provider, err := p.factory.CreateProvider(config)
	if err != nil {
		return nil, err
	}

	pooled := &PooledProvider{
		provider:    provider,
		created:     time.Now(),
		lastUsed:    time.Now(),
		inUse:       true,
		usageCount:  1,
		maxRequests: 1000, // Default max requests per connection
	}

	p.providers[providerKey] = pooled

	return &PooledProviderWrapper{
		provider: provider,
		pool:     p,
		key:      providerKey,
		pooled:   pooled,
	}, nil
}

// ReturnProvider returns a provider to the pool
func (p *ConnectionPool) ReturnProvider(providerKey string) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if pooled, exists := p.providers[providerKey]; exists {
		pooled.inUse = false
		pooled.lastUsed = time.Now()

		// Check if we should keep this connection
		if p.shouldKeepConnection(pooled) {
			return
		}

		// Remove connection that shouldn't be kept
		delete(p.providers, providerKey)
	}
}

// isValidConnection checks if a pooled connection is still valid
func (p *ConnectionPool) isValidConnection(pooled *PooledProvider) bool {
	now := time.Now()

	// Check lifetime
	if now.Sub(pooled.created) > p.maxLifetime {
		return false
	}

	// Check usage count
	if pooled.usageCount >= pooled.maxRequests {
		return false
	}

	return true
}

// shouldKeepConnection determines if a connection should remain in the pool
func (p *ConnectionPool) shouldKeepConnection(pooled *PooledProvider) bool {
	if !p.isValidConnection(pooled) {
		return false
	}

	// Check idle limit
	idleCount := p.getIdleCount()
	if idleCount >= p.maxIdle {
		return false
	}

	return true
}

// getActiveCount returns the number of active connections
func (p *ConnectionPool) getActiveCount() int {
	count := 0
	for _, pooled := range p.providers {
		if pooled.inUse {
			count++
		}
	}
	return count
}

// getIdleCount returns the number of idle connections
func (p *ConnectionPool) getIdleCount() int {
	count := 0
	for _, pooled := range p.providers {
		if !pooled.inUse {
			count++
		}
	}
	return count
}

// cleanupLoop periodically cleans up expired connections
func (p *ConnectionPool) cleanupLoop() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		p.cleanup()
	}
}

// cleanup removes expired idle connections
func (p *ConnectionPool) cleanup() {
	p.mu.Lock()
	defer p.mu.Unlock()

	now := time.Now()
	toRemove := make([]string, 0)

	for key, pooled := range p.providers {
		if pooled.inUse {
			continue
		}

		// Remove if idle too long
		if now.Sub(pooled.lastUsed) > p.idleTimeout {
			toRemove = append(toRemove, key)
			continue
		}

		// Remove if exceeded lifetime
		if now.Sub(pooled.created) > p.maxLifetime {
			toRemove = append(toRemove, key)
			continue
		}
	}

	for _, key := range toRemove {
		delete(p.providers, key)
	}
}

// GetStats returns pool statistics
func (p *ConnectionPool) GetStats() PoolStats {
	p.mu.RLock()
	defer p.mu.RUnlock()

	return PoolStats{
		TotalConnections: len(p.providers),
		ActiveCount:      p.getActiveCount(),
		IdleCount:        p.getIdleCount(),
		MaxIdle:          p.maxIdle,
		MaxActive:        p.maxActive,
	}
}

// Close closes all connections in the pool
func (p *ConnectionPool) Close() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.providers = make(map[string]*PooledProvider)
	return nil
}

// PoolStats provides statistics about the connection pool
type PoolStats struct {
	TotalConnections int `json:"total_connections"`
	ActiveCount      int `json:"active_count"`
	IdleCount        int `json:"idle_count"`
	MaxIdle          int `json:"max_idle"`
	MaxActive        int `json:"max_active"`
}

// PoolError represents pool-specific errors
type PoolError struct {
	Type    string                 `json:"type"`
	Message string                 `json:"message"`
	Details map[string]interface{} `json:"details"`
}

func (e *PoolError) Error() string {
	return e.Message
}

// PooledProviderWrapper wraps a provider to handle pool return
type PooledProviderWrapper struct {
	provider interfaces.LLMProvider
	pool     *ConnectionPool
	key      string
	pooled   *PooledProvider
	returned bool
	mu       sync.Mutex
}

// Implement interfaces.LLMProvider interface
func (w *PooledProviderWrapper) GetName() string {
	return w.provider.GetName()
}

func (w *PooledProviderWrapper) GetModels(ctx context.Context) ([]types.ModelInfo, error) {
	return w.provider.GetModels(ctx)
}

func (w *PooledProviderWrapper) GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	response, metadata, err := w.provider.GenerateResponse(ctx, messages, options)
	w.returnToPool()
	return response, metadata, err
}

func (w *PooledProviderWrapper) GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error) {
	metadata, err := w.provider.GenerateResponseStream(ctx, messages, options, writer)
	w.returnToPool()
	return metadata, err
}

func (w *PooledProviderWrapper) IsAvailable(ctx context.Context) error {
	return w.provider.IsAvailable(ctx)
}

func (w *PooledProviderWrapper) EstimateTokens(messages []types.Message) (int, error) {
	return w.provider.EstimateTokens(messages)
}

func (w *PooledProviderWrapper) CalculateCost(usage types.TokenUsage) float64 {
	return w.provider.CalculateCost(usage)
}

// returnToPool returns the provider to the pool (called automatically after use)
func (w *PooledProviderWrapper) returnToPool() {
	w.mu.Lock()
	defer w.mu.Unlock()

	if !w.returned {
		w.pool.ReturnProvider(w.key)
		w.returned = true
	}
}
