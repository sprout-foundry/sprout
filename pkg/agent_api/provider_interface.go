package api

import (
	"context"
	"io"
	"net/http"
	"time"
)

// Provider defines the interface all LLM providers must implement
type Provider interface {
	// Core functionality
	SendChatRequest(ctx context.Context, req *ProviderChatRequest) (*ChatResponse, error)
	CheckConnection(ctx context.Context) error

	// Model management
	GetModel() string
	SetModel(model string) error
	GetAvailableModels(ctx context.Context) ([]ModelDetails, error)
	GetModelContextLimit() (int, error)

	// Provider information
	GetName() string
	GetType() ClientType
	GetEndpoint() string

	// Feature support
	SupportsVision() bool
	SupportsTools() bool
	SupportsStreaming() bool
	SupportsReasoning() bool

	// Configuration
	SetDebug(debug bool)
	IsDebug() bool
}

// ProviderChatRequest represents a unified request structure for providers
// This extends the basic ChatRequest with provider-specific options
type ProviderChatRequest struct {
	Messages []Message
	Tools    []Tool
	Options  *RequestOptions
}

// RequestOptions contains optional parameters for requests
type RequestOptions struct {
	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Stream           bool
	ReasoningEffort  string // For reasoning models
}

// ModelDetails represents detailed information about a model from a provider
type ModelDetails struct {
	ID              string
	Name            string
	ContextLength   int
	InputCostPer1K  float64
	OutputCostPer1K float64
	Features        []string // e.g., "vision", "tools", "reasoning"
	IsDefault       bool
}

// BaseProvider implements common functionality for all providers
type BaseProvider struct {
	name       string
	clientType ClientType
	endpoint   string
	apiKey     string
	model      string
	debug      bool

	// Feature flags
	supportsVision    bool
	supportsTools     bool
	supportsStreaming bool
	supportsReasoning bool

	// HTTP client with reasonable defaults
	httpClient HTTPClient
}

// HTTPClient interface for testing
type HTTPClient interface {
	Do(req *http.Request) (*http.Response, error)
}

// NewBaseProvider creates a base provider with common settings
func NewBaseProvider(name string, clientType ClientType, endpoint string, apiKey string) *BaseProvider {
	return &BaseProvider{
		name:       name,
		clientType: clientType,
		endpoint:   endpoint,
		apiKey:     apiKey,
		httpClient: &http.Client{
			Timeout: 2 * time.Minute,
		},
		supportsTools:     true, // Most providers support tools
		supportsStreaming: true, // Most providers support streaming
	}
}

// GetName returns the provider name
func (p *BaseProvider) GetName() string {
	return p.name
}

// GetType returns the provider type
func (p *BaseProvider) GetType() ClientType {
	return p.clientType
}

// GetEndpoint returns the API endpoint
func (p *BaseProvider) GetEndpoint() string {
	return p.endpoint
}

// GetModel returns the current model
func (p *BaseProvider) GetModel() string {
	return p.model
}

// SetModel sets the current model
func (p *BaseProvider) SetModel(model string) error {
	p.model = model
	return nil
}

// SetDebug enables or disables debug mode
func (p *BaseProvider) SetDebug(debug bool) {
	p.debug = debug
}

// IsDebug returns whether debug mode is enabled
func (p *BaseProvider) IsDebug() bool {
	return p.debug
}

// SupportsVision returns whether the provider supports vision
func (p *BaseProvider) SupportsVision() bool {
	return p.supportsVision
}

// SupportsTools returns whether the provider supports tools
func (p *BaseProvider) SupportsTools() bool {
	return p.supportsTools
}

// SupportsStreaming returns whether the provider supports streaming
func (p *BaseProvider) SupportsStreaming() bool {
	return p.supportsStreaming
}

// SupportsReasoning returns whether the provider supports reasoning
func (p *BaseProvider) SupportsReasoning() bool {
	return p.supportsReasoning
}

// Helper methods for derived providers

// MakeAuthRequest creates an HTTP request with authentication
func (p *BaseProvider) MakeAuthRequest(ctx context.Context, method, url string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, err
	}

	req.Header.Set("Authorization", "Bearer "+p.apiKey)
	req.Header.Set("Content-Type", "application/json")

	return req, nil
}

// EstimateCost calculates the estimated cost for a response
func (p *BaseProvider) EstimateCost(promptTokens, completionTokens int, model string) float64 {
	// This should be overridden by providers with specific pricing
	// Default to a conservative estimate
	inputCost := float64(promptTokens) * 0.001 / 1000      // $0.001 per 1K tokens
	outputCost := float64(completionTokens) * 0.002 / 1000 // $0.002 per 1K tokens
	return inputCost + outputCost
}
