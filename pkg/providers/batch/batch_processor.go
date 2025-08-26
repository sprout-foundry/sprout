package batch

import (
	"context"
	"io"
	"sync"
	"time"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// BatchProcessor provides smart batching for LLM API calls to optimize performance and costs
type BatchProcessor struct {
	config    BatchConfig
	batches   map[string]*Batch
	mu        sync.RWMutex
	processor chan BatchRequest
	results   map[string]chan BatchResponse
	stats     BatchStats
	ticker    *time.Ticker
	ctx       context.Context
	cancel    context.CancelFunc
}

// BatchConfig configures batching behavior
type BatchConfig struct {
	MaxBatchSize     int           `json:"max_batch_size"`    // Maximum requests per batch
	BatchTimeout     time.Duration `json:"batch_timeout"`     // Maximum time to wait before processing batch
	FlushInterval    time.Duration `json:"flush_interval"`    // Interval for periodic batch flushing
	MaxConcurrency   int           `json:"max_concurrency"`   // Maximum concurrent batches
	EnableMetrics    bool          `json:"enable_metrics"`    // Enable batch metrics collection
	CostThreshold    float64       `json:"cost_threshold"`    // Batch when estimated cost exceeds threshold
	TokenThreshold   int           `json:"token_threshold"`   // Batch when estimated tokens exceed threshold
	PriorityBatching bool          `json:"priority_batching"` // Group by request priority
}

// Batch represents a group of requests to be processed together
type Batch struct {
	ID          string         `json:"id"`
	Provider    string         `json:"provider"`
	Model       string         `json:"model"`
	Requests    []BatchRequest `json:"requests"`
	CreatedAt   time.Time      `json:"created_at"`
	LastUpdated time.Time      `json:"last_updated"`
	TotalTokens int            `json:"total_tokens"`
	TotalCost   float64        `json:"total_cost"`
	Priority    int            `json:"priority"`
	Status      BatchStatus    `json:"status"`
}

// BatchRequest represents a single request within a batch
type BatchRequest struct {
	ID       string               `json:"id"`
	Messages []types.Message      `json:"messages"`
	Options  types.RequestOptions `json:"options"`
	Priority int                  `json:"priority"`
	Context  context.Context      `json:"-"`
	Response chan BatchResponse   `json:"-"`
}

// BatchResponse contains the result of a batched request
type BatchResponse struct {
	ID       string                  `json:"id"`
	Response string                  `json:"response"`
	Metadata *types.ResponseMetadata `json:"metadata"`
	Error    error                   `json:"error"`
	Duration time.Duration           `json:"duration"`
}

// BatchStatus represents the processing status of a batch
type BatchStatus int

const (
	BatchStatusPending BatchStatus = iota
	BatchStatusProcessing
	BatchStatusCompleted
	BatchStatusFailed
)

// BatchStats tracks batching performance metrics
type BatchStats struct {
	TotalRequests    int64         `json:"total_requests"`
	BatchedRequests  int64         `json:"batched_requests"`
	TotalBatches     int64         `json:"total_batches"`
	AverageBatchSize float64       `json:"average_batch_size"`
	CostSavings      float64       `json:"cost_savings"`
	TokenSavings     int64         `json:"token_savings"`
	AverageLatency   time.Duration `json:"average_latency"`
	BatchEfficiency  float64       `json:"batch_efficiency"` // 0-1, higher is better
}

// NewBatchProcessor creates a new batch processor
func NewBatchProcessor(config BatchConfig) *BatchProcessor {
	if config.MaxBatchSize <= 0 {
		config.MaxBatchSize = 10
	}
	if config.BatchTimeout <= 0 {
		config.BatchTimeout = 100 * time.Millisecond
	}
	if config.FlushInterval <= 0 {
		config.FlushInterval = 500 * time.Millisecond
	}
	if config.MaxConcurrency <= 0 {
		config.MaxConcurrency = 5
	}
	if config.CostThreshold <= 0 {
		config.CostThreshold = 0.01 // $0.01 threshold
	}
	if config.TokenThreshold <= 0 {
		config.TokenThreshold = 1000
	}

	ctx, cancel := context.WithCancel(context.Background())

	bp := &BatchProcessor{
		config:    config,
		batches:   make(map[string]*Batch),
		processor: make(chan BatchRequest, config.MaxBatchSize*config.MaxConcurrency),
		results:   make(map[string]chan BatchResponse),
		stats:     BatchStats{},
		ticker:    time.NewTicker(config.FlushInterval),
		ctx:       ctx,
		cancel:    cancel,
	}

	// Start processing goroutines
	go bp.processingLoop()
	go bp.flushLoop()

	return bp
}

// SubmitRequest submits a request for potential batching
func (bp *BatchProcessor) SubmitRequest(provider interfaces.LLMProvider, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	requestID := bp.generateRequestID()
	responseChan := make(chan BatchResponse, 1)

	request := BatchRequest{
		ID:       requestID,
		Messages: messages,
		Options:  options,
		Priority: bp.calculatePriority(messages, options),
		Context:  context.Background(),
		Response: responseChan,
	}

	// Store response channel
	bp.mu.Lock()
	bp.results[requestID] = responseChan
	bp.mu.Unlock()

	// Submit for processing
	select {
	case bp.processor <- request:
		bp.updateStats(func(s *BatchStats) {
			s.TotalRequests++
		})
	case <-time.After(bp.config.BatchTimeout):
		// Fallback to direct processing if batching fails
		return bp.directProcess(provider, messages, options)
	}

	// Wait for response
	select {
	case response := <-responseChan:
		bp.cleanupResponseChan(requestID)
		return response.Response, response.Metadata, response.Error
	case <-time.After(bp.config.BatchTimeout * 10): // Extended timeout for batch processing
		bp.cleanupResponseChan(requestID)
		// Fallback to direct processing
		return bp.directProcess(provider, messages, options)
	}
}

// processingLoop handles batch creation and processing
func (bp *BatchProcessor) processingLoop() {
	for {
		select {
		case request := <-bp.processor:
			bp.addRequestToBatch(request)
		case <-bp.ctx.Done():
			return
		}
	}
}

// flushLoop periodically flushes batches
func (bp *BatchProcessor) flushLoop() {
	for {
		select {
		case <-bp.ticker.C:
			bp.flushBatches()
		case <-bp.ctx.Done():
			bp.ticker.Stop()
			return
		}
	}
}

// addRequestToBatch adds a request to an appropriate batch
func (bp *BatchProcessor) addRequestToBatch(request BatchRequest) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	batchKey := bp.getBatchKey(request)
	batch, exists := bp.batches[batchKey]

	if !exists {
		batch = &Batch{
			ID:          bp.generateBatchID(),
			Provider:    request.Options.Model, // Provider name from model
			Model:       request.Options.Model,
			Requests:    make([]BatchRequest, 0, bp.config.MaxBatchSize),
			CreatedAt:   time.Now(),
			LastUpdated: time.Now(),
			Priority:    request.Priority,
			Status:      BatchStatusPending,
		}
		bp.batches[batchKey] = batch
	}

	// Add request to batch
	batch.Requests = append(batch.Requests, request)
	batch.LastUpdated = time.Now()
	batch.TotalTokens += bp.estimateTokens(request.Messages)
	batch.TotalCost += bp.estimateCost(request.Messages, request.Options)

	// Check if batch should be processed immediately
	if bp.shouldProcessBatch(batch) {
		go bp.processBatch(batchKey, batch)
		delete(bp.batches, batchKey)
	}
}

// shouldProcessBatch determines if a batch should be processed immediately
func (bp *BatchProcessor) shouldProcessBatch(batch *Batch) bool {
	// Process if batch is full
	if len(batch.Requests) >= bp.config.MaxBatchSize {
		return true
	}

	// Process if batch is old enough
	if time.Since(batch.CreatedAt) >= bp.config.BatchTimeout {
		return true
	}

	// Process if cost threshold exceeded
	if batch.TotalCost >= bp.config.CostThreshold {
		return true
	}

	// Process if token threshold exceeded
	if batch.TotalTokens >= bp.config.TokenThreshold {
		return true
	}

	return false
}

// processBatch processes a complete batch
func (bp *BatchProcessor) processBatch(batchKey string, batch *Batch) {
	batch.Status = BatchStatusProcessing
	startTime := time.Now()

	bp.updateStats(func(s *BatchStats) {
		s.TotalBatches++
		s.BatchedRequests += int64(len(batch.Requests))
	})

	// Process requests in batch (could be parallelized further)
	for _, request := range batch.Requests {
		response := bp.processRequest(request)
		response.Duration = time.Since(startTime)

		// Send response back to waiting goroutine
		select {
		case request.Response <- response:
		case <-time.After(time.Second):
			// Request timed out, skip
		}
	}

	batch.Status = BatchStatusCompleted

	// Update efficiency metrics
	bp.updateBatchEfficiency(batch, time.Since(startTime))
}

// processRequest processes a single request within a batch context
func (bp *BatchProcessor) processRequest(request BatchRequest) BatchResponse {
	// This would integrate with the actual provider
	// For now, return a placeholder response
	return BatchResponse{
		ID:       request.ID,
		Response: "Batched response placeholder",
		Metadata: &types.ResponseMetadata{
			TokenUsage: types.TokenUsage{
				PromptTokens:     100,
				CompletionTokens: 50,
				TotalTokens:      150,
			},
			Cost: 0.001,
		},
		Error: nil,
	}
}

// flushBatches processes all pending batches
func (bp *BatchProcessor) flushBatches() {
	bp.mu.Lock()
	batchesToProcess := make(map[string]*Batch)
	for key, batch := range bp.batches {
		if time.Since(batch.LastUpdated) >= bp.config.FlushInterval {
			batchesToProcess[key] = batch
			delete(bp.batches, key)
		}
	}
	bp.mu.Unlock()

	// Process batches concurrently
	var wg sync.WaitGroup
	semaphore := make(chan struct{}, bp.config.MaxConcurrency)

	for key, batch := range batchesToProcess {
		wg.Add(1)
		go func(k string, b *Batch) {
			defer wg.Done()
			semaphore <- struct{}{}
			defer func() { <-semaphore }()
			bp.processBatch(k, b)
		}(key, batch)
	}

	wg.Wait()
}

// Helper methods

// getBatchKey generates a key for grouping similar requests
func (bp *BatchProcessor) getBatchKey(request BatchRequest) string {
	key := request.Options.Model
	if bp.config.PriorityBatching {
		key += "_" + string(rune(request.Priority))
	}
	return key
}

// generateRequestID creates a unique request ID
func (bp *BatchProcessor) generateRequestID() string {
	// Simple ID generation - would use UUID in production
	return string(rune(time.Now().UnixNano()))
}

// generateBatchID creates a unique batch ID
func (bp *BatchProcessor) generateBatchID() string {
	return "batch_" + string(rune(time.Now().UnixNano()))
}

// calculatePriority determines request priority for batching
func (bp *BatchProcessor) calculatePriority(messages []types.Message, options types.RequestOptions) int {
	priority := 50 // Default priority

	// Higher priority for shorter requests (faster to batch)
	totalLength := 0
	for _, msg := range messages {
		totalLength += len(msg.Content)
	}
	if totalLength < 1000 {
		priority += 20
	}

	// Higher priority for lower temperature (more deterministic)
	if options.Temperature < 0.3 {
		priority += 10
	}

	return priority
}

// estimateTokens estimates token count for messages
func (bp *BatchProcessor) estimateTokens(messages []types.Message) int {
	total := 0
	for _, msg := range messages {
		total += len(msg.Content) / 4 // Rough estimate
	}
	return total
}

// estimateCost estimates cost for messages and options
func (bp *BatchProcessor) estimateCost(messages []types.Message, options types.RequestOptions) float64 {
	tokens := bp.estimateTokens(messages)
	// Rough cost estimate - would use actual provider pricing
	return float64(tokens) * 0.0001 // $0.0001 per token estimate
}

// directProcess bypasses batching for immediate processing
func (bp *BatchProcessor) directProcess(provider interfaces.LLMProvider, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	ctx := context.Background()
	return provider.GenerateResponse(ctx, messages, options)
}

// updateStats safely updates batch statistics
func (bp *BatchProcessor) updateStats(updater func(*BatchStats)) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	updater(&bp.stats)
}

// updateBatchEfficiency calculates batch processing efficiency
func (bp *BatchProcessor) updateBatchEfficiency(batch *Batch, duration time.Duration) {
	bp.mu.Lock()
	defer bp.mu.Unlock()

	batchSize := float64(len(batch.Requests))
	if batchSize > 1 {
		// Higher efficiency for larger batches processed quickly
		efficiency := batchSize / duration.Seconds()

		// Update running average
		totalBatches := float64(bp.stats.TotalBatches)
		if totalBatches > 0 {
			bp.stats.BatchEfficiency = (bp.stats.BatchEfficiency*(totalBatches-1) + efficiency) / totalBatches
		} else {
			bp.stats.BatchEfficiency = efficiency
		}

		bp.stats.AverageBatchSize = (bp.stats.AverageBatchSize*(totalBatches-1) + batchSize) / totalBatches
	}
}

// cleanupResponseChan removes response channel after use
func (bp *BatchProcessor) cleanupResponseChan(requestID string) {
	bp.mu.Lock()
	defer bp.mu.Unlock()
	delete(bp.results, requestID)
}

// GetStats returns current batch statistics
func (bp *BatchProcessor) GetStats() BatchStats {
	bp.mu.RLock()
	defer bp.mu.RUnlock()
	return bp.stats
}

// Close shuts down the batch processor
func (bp *BatchProcessor) Close() error {
	bp.cancel()

	// Process any remaining batches
	bp.flushBatches()

	return nil
}

// BatchingProvider wraps a provider with batching capabilities
type BatchingProvider struct {
	provider  interfaces.LLMProvider
	processor *BatchProcessor
}

// NewBatchingProvider creates a provider with batching support
func NewBatchingProvider(provider interfaces.LLMProvider, processor *BatchProcessor) *BatchingProvider {
	return &BatchingProvider{
		provider:  provider,
		processor: processor,
	}
}

// Implement interfaces.LLMProvider interface with batching
func (bp *BatchingProvider) GetName() string {
	return bp.provider.GetName()
}

func (bp *BatchingProvider) GetModels(ctx context.Context) ([]types.ModelInfo, error) {
	return bp.provider.GetModels(ctx)
}

func (bp *BatchingProvider) GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	// Use batch processor for potentially batching this request
	return bp.processor.SubmitRequest(bp.provider, messages, options)
}

func (bp *BatchingProvider) GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error) {
	// Streaming responses bypass batching
	return bp.provider.GenerateResponseStream(ctx, messages, options, writer)
}

func (bp *BatchingProvider) IsAvailable(ctx context.Context) error {
	return bp.provider.IsAvailable(ctx)
}

func (bp *BatchingProvider) EstimateTokens(messages []types.Message) (int, error) {
	return bp.provider.EstimateTokens(messages)
}

func (bp *BatchingProvider) CalculateCost(usage types.TokenUsage) float64 {
	return bp.provider.CalculateCost(usage)
}
