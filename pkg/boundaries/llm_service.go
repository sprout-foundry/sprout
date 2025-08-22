package boundaries

import (
	"context"
	"io"
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// LLMService defines the interface for LLM operations
// This provides a clean boundary between LLM providers and consumers
type LLMService interface {
	// ExecuteRequest executes a single LLM request
	ExecuteRequest(ctx context.Context, request *LLMRequest) (*LLMResponse, error)

	// ExecuteStreamingRequest executes a streaming LLM request
	ExecuteStreamingRequest(ctx context.Context, request *LLMRequest, writer io.Writer) (*LLMResponse, error)

	// GetAvailableModels returns the list of available models
	GetAvailableModels() ([]ModelInfo, error)

	// GetModelInfo returns information about a specific model
	GetModelInfo(modelName string) (*ModelInfo, error)

	// ValidateRequest validates an LLM request
	ValidateRequest(request *LLMRequest) error

	// EstimateTokens estimates the number of tokens in a text
	EstimateTokens(text string) int

	// GetTokenUsage returns current token usage statistics
	GetTokenUsage() (*TokenUsageStats, error)

	// GetProviderHealth returns health information for LLM providers
	GetProviderHealth() (*ProviderHealth, error)
}

// ModelInfo contains information about an LLM model
type ModelInfo struct {
	Name           string
	Provider       string
	ContextLength  int
	InputPricing   float64 // Cost per 1K input tokens
	OutputPricing  float64 // Cost per 1K output tokens
	Capabilities   []string
	MaxTokens      int
	SupportsStream bool
	SupportsVision bool
}

// TokenUsageStats contains token usage statistics
type TokenUsageStats struct {
	TotalRequests   int64
	TotalTokens     int64
	InputTokens     int64
	OutputTokens    int64
	TotalCost       float64
	AverageLatency  time.Duration
	ErrorRate       float64
	RequestsByModel map[string]int64
	TokensByModel   map[string]int64
}

// ProviderHealth contains health information for LLM providers
type ProviderHealth struct {
	Providers     []ProviderStatus
	OverallHealth string // "healthy", "degraded", "unhealthy"
}

// ProviderStatus contains status information for a single provider
type ProviderStatus struct {
	Name           string
	Health         string // "healthy", "degraded", "unhealthy"
	LastCheck      time.Time
	ResponseTime   time.Duration
	ErrorCount     int
	SuccessRate    float64
	ActiveRequests int
}

// LLMRequest represents a request to the LLM service
type LLMRequest struct {
	Messages       []prompts.Message
	Filename       string
	Model          string
	Timeout        time.Duration
	MaxTokens      int
	Temperature    float64
	Stream         bool
	ImagePaths     []string
	SystemPrompt   string
	UserPrompt     string
	RequestID      string
	Priority       int
	RetryCount     int
	RetryOnFailure bool
}

// LLMResponse represents a response from the LLM service
type LLMResponse struct {
	Content      string
	TokenUsage   *llm.TokenUsage
	Model        string
	FinishReason string
	Error        error
	RequestID    string
	Duration     time.Duration
	RetryAttempt int
	Provider     string
}

// LLMCallback defines callbacks for LLM operations
type LLMCallback interface {
	OnLLMRequest(request *LLMRequest)
	OnLLMResponse(response *LLMResponse)
	OnLLMError(request *LLMRequest, err error)
	OnLLMStreamChunk(chunk string, usage *llm.TokenUsage)
}

// LLMServiceFactory creates LLM service instances
type LLMServiceFactory interface {
	CreateLLMService(config *config.Config) LLMService
	CreateLLMServiceWithLogger(config *config.Config, logger *utils.Logger) LLMService
}

// DefaultLLMService provides a default implementation of LLMService
type DefaultLLMService struct {
	config    *config.Config
	logger    *utils.Logger
	callbacks []LLMCallback
	stats     *TokenUsageStats
}

// NewDefaultLLMService creates a new default LLM service
func NewDefaultLLMService(config *config.Config, logger *utils.Logger) *DefaultLLMService {
	return &DefaultLLMService{
		config:    config,
		logger:    logger,
		callbacks: make([]LLMCallback, 0),
		stats: &TokenUsageStats{
			RequestsByModel: make(map[string]int64),
			TokensByModel:   make(map[string]int64),
		},
	}
}

// ExecuteRequest implements LLMService.ExecuteRequest
func (s *DefaultLLMService) ExecuteRequest(ctx context.Context, request *LLMRequest) (*LLMResponse, error) {
	// Validate request
	if err := s.ValidateRequest(request); err != nil {
		return nil, err
	}

	// Notify callbacks
	s.notifyCallbacks(func(cb LLMCallback) {
		cb.OnLLMRequest(request)
	})

	startTime := time.Now()

	// Create messages if not provided
	messages := request.Messages
	if len(messages) == 0 {
		messages = s.createMessages(request)
	}

	// Determine model
	model := request.Model
	if model == "" {
		model = request.Filename
	}

	// Execute request
	var content string
	var tokenUsage *llm.TokenUsage
	var err error

	if len(request.ImagePaths) > 0 {
		content, tokenUsage, err = llm.GetLLMResponse(model, messages, request.Filename, s.config, request.Timeout, request.ImagePaths...)
	} else {
		content, tokenUsage, err = llm.GetLLMResponse(model, messages, request.Filename, s.config, request.Timeout)
	}

	duration := time.Since(startTime)

	// Update statistics
	s.updateStats(model, tokenUsage, duration, err == nil)

	response := &LLMResponse{
		Content:      content,
		TokenUsage:   tokenUsage,
		Model:        model,
		Error:        err,
		RequestID:    request.RequestID,
		Duration:     duration,
		RetryAttempt: request.RetryCount,
	}

	// Notify callbacks
	if err != nil {
		s.notifyCallbacks(func(cb LLMCallback) {
			cb.OnLLMError(request, err)
		})
	} else {
		s.notifyCallbacks(func(cb LLMCallback) {
			cb.OnLLMResponse(response)
		})
	}

	return response, err
}

// ExecuteStreamingRequest implements LLMService.ExecuteStreamingRequest
func (s *DefaultLLMService) ExecuteStreamingRequest(ctx context.Context, request *LLMRequest, writer io.Writer) (*LLMResponse, error) {
	// Validate request
	if err := s.ValidateRequest(request); err != nil {
		return nil, err
	}

	// Notify callbacks
	s.notifyCallbacks(func(cb LLMCallback) {
		cb.OnLLMRequest(request)
	})

	startTime := time.Now()

	// Create messages if not provided
	messages := request.Messages
	if len(messages) == 0 {
		messages = s.createMessages(request)
	}

	// Determine model
	model := request.Model
	if model == "" {
		model = request.Filename
	}

	// Execute streaming request
	var tokenUsage *llm.TokenUsage
	var err error

	if len(request.ImagePaths) > 0 {
		tokenUsage, err = llm.GetLLMResponseStream(model, messages, request.Filename, s.config, request.Timeout, writer, request.ImagePaths...)
	} else {
		tokenUsage, err = llm.GetLLMResponseStream(model, messages, request.Filename, s.config, request.Timeout, writer)
	}

	duration := time.Since(startTime)

	// Update statistics
	s.updateStats(model, tokenUsage, duration, err == nil)

	response := &LLMResponse{
		Content:      "", // Content is written to stream
		TokenUsage:   tokenUsage,
		Model:        model,
		Error:        err,
		RequestID:    request.RequestID,
		Duration:     duration,
		RetryAttempt: request.RetryCount,
	}

	// Notify callbacks
	if err != nil {
		s.notifyCallbacks(func(cb LLMCallback) {
			cb.OnLLMError(request, err)
		})
	} else {
		s.notifyCallbacks(func(cb LLMCallback) {
			cb.OnLLMResponse(response)
		})
	}

	return response, err
}

// GetAvailableModels implements LLMService.GetAvailableModels
func (s *DefaultLLMService) GetAvailableModels() ([]ModelInfo, error) {
	// This would query the LLM providers for available models
	// For now, return a static list based on configuration
	models := []ModelInfo{
		{
			Name:           "gpt-4",
			Provider:       "openai",
			ContextLength:  8192,
			InputPricing:   0.03,
			OutputPricing:  0.06,
			Capabilities:   []string{"text", "code"},
			MaxTokens:      4096,
			SupportsStream: true,
		},
		{
			Name:           "gpt-3.5-turbo",
			Provider:       "openai",
			ContextLength:  4096,
			InputPricing:   0.0015,
			OutputPricing:  0.002,
			Capabilities:   []string{"text", "code"},
			MaxTokens:      2048,
			SupportsStream: true,
		},
		{
			Name:           "claude-3-sonnet",
			Provider:       "anthropic",
			ContextLength:  200000,
			InputPricing:   0.003,
			OutputPricing:  0.015,
			Capabilities:   []string{"text", "code"},
			MaxTokens:      4096,
			SupportsStream: true,
		},
	}

	return models, nil
}

// GetModelInfo implements LLMService.GetModelInfo
func (s *DefaultLLMService) GetModelInfo(modelName string) (*ModelInfo, error) {
	models, err := s.GetAvailableModels()
	if err != nil {
		return nil, err
	}

	for _, model := range models {
		if model.Name == modelName {
			return &model, nil
		}
	}

	return nil, utils.NewUserError("model not found: "+modelName, nil)
}

// ValidateRequest implements LLMService.ValidateRequest
func (s *DefaultLLMService) ValidateRequest(request *LLMRequest) error {
	if request == nil {
		return utils.NewValidationError("request", "cannot be nil")
	}

	if len(request.Messages) == 0 && request.UserPrompt == "" {
		return utils.NewValidationError("messages", "cannot be empty")
	}

	if request.Timeout < 0 {
		return utils.NewValidationError("timeout", "cannot be negative")
	}

	if request.MaxTokens < 0 {
		return utils.NewValidationError("max_tokens", "cannot be negative")
	}

	if request.Temperature < 0.0 || request.Temperature > 2.0 {
		return utils.NewValidationError("temperature", "must be between 0.0 and 2.0")
	}

	return nil
}

// EstimateTokens implements LLMService.EstimateTokens
func (s *DefaultLLMService) EstimateTokens(text string) int {
	return utils.EstimateTokens(text)
}

// GetTokenUsage implements LLMService.GetTokenUsage
func (s *DefaultLLMService) GetTokenUsage() (*TokenUsageStats, error) {
	return s.stats, nil
}

// GetProviderHealth implements LLMService.GetProviderHealth
func (s *DefaultLLMService) GetProviderHealth() (*ProviderHealth, error) {
	// This would check the health of LLM providers
	// For now, return a simple healthy status
	return &ProviderHealth{
		Providers: []ProviderStatus{
			{
				Name:        "openai",
				Health:      "healthy",
				LastCheck:   time.Now(),
				SuccessRate: 0.99,
			},
			{
				Name:        "anthropic",
				Health:      "healthy",
				LastCheck:   time.Now(),
				SuccessRate: 0.98,
			},
		},
		OverallHealth: "healthy",
	}, nil
}

// AddCallback adds a callback for LLM events
func (s *DefaultLLMService) AddCallback(callback LLMCallback) {
	s.callbacks = append(s.callbacks, callback)
}

// RemoveCallback removes a callback
func (s *DefaultLLMService) RemoveCallback(callback LLMCallback) {
	for i, cb := range s.callbacks {
		if cb == callback {
			s.callbacks = append(s.callbacks[:i], s.callbacks[i+1:]...)
			break
		}
	}
}

// createMessages creates prompt messages from request
func (s *DefaultLLMService) createMessages(request *LLMRequest) []prompts.Message {
	messages := []prompts.Message{}

	if request.SystemPrompt != "" {
		messages = append(messages, prompts.Message{
			Role:    "system",
			Content: request.SystemPrompt,
		})
	}

	if request.UserPrompt != "" {
		messages = append(messages, prompts.Message{
			Role:    "user",
			Content: request.UserPrompt,
		})
	}

	return messages
}

// updateStats updates usage statistics
func (s *DefaultLLMService) updateStats(model string, usage *llm.TokenUsage, duration time.Duration, success bool) {
	s.stats.TotalRequests++

	if usage != nil {
		s.stats.TotalTokens += int64(usage.TotalTokens)
		s.stats.InputTokens += int64(usage.PromptTokens)
		s.stats.OutputTokens += int64(usage.CompletionTokens)

		// Simple cost estimation (would need real pricing data)
		s.stats.TotalCost += float64(usage.TotalTokens) * 0.00002 // $0.02 per 1K tokens
	}

	if success {
		s.stats.RequestsByModel[model]++
		if usage != nil {
			s.stats.TokensByModel[model] += int64(usage.TotalTokens)
		}
	}

	// Update average latency (simple moving average)
	if s.stats.TotalRequests == 1 {
		s.stats.AverageLatency = duration
	} else {
		// Simple moving average
		s.stats.AverageLatency = (s.stats.AverageLatency + duration) / 2
	}
}

// notifyCallbacks notifies all callbacks of an event
func (s *DefaultLLMService) notifyCallbacks(fn func(LLMCallback)) {
	for _, callback := range s.callbacks {
		fn(callback)
	}
}
