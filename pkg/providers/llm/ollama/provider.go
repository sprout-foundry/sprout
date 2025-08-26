package ollama

import (
	"context"
	"fmt"
	"io"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// Provider implements the Ollama LLM provider
type Provider struct {
	config *types.ProviderConfig
}

// Factory implements the ProviderFactory interface for Ollama
type Factory struct{}

// GetName returns the provider name
func (f *Factory) GetName() string {
	return "ollama"
}

// Create creates a new Ollama provider instance
func (f *Factory) Create(config *types.ProviderConfig) (interfaces.LLMProvider, error) {
	if err := f.Validate(config); err != nil {
		return nil, err
	}

	return &Provider{
		config: config,
	}, nil
}

// Validate validates the Ollama provider configuration
func (f *Factory) Validate(config *types.ProviderConfig) error {
	if config == nil {
		return fmt.Errorf("configuration is required")
	}

	if config.Model == "" {
		return fmt.Errorf("model is required for Ollama provider")
	}

	// Set defaults
	if config.BaseURL == "" {
		config.BaseURL = "http://localhost:11434"
	}

	if config.Timeout == 0 {
		config.Timeout = 120 // Ollama can be slower
	}

	return nil
}

// GetName returns the provider name
func (p *Provider) GetName() string {
	return "ollama"
}

// GetModels returns available models for Ollama
func (p *Provider) GetModels(ctx context.Context) ([]types.ModelInfo, error) {
	// TODO: Query Ollama API for available models
	return []types.ModelInfo{
		{
			Name:           "llama2",
			Provider:       "ollama",
			MaxTokens:      4096,
			SupportsTools:  false,
			SupportsImages: false,
		},
		{
			Name:           "codellama",
			Provider:       "ollama",
			MaxTokens:      4096,
			SupportsTools:  false,
			SupportsImages: false,
		},
		{
			Name:           "mistral",
			Provider:       "ollama",
			MaxTokens:      8192,
			SupportsTools:  false,
			SupportsImages: false,
		},
	}, nil
}

// GenerateResponse generates a response from Ollama
func (p *Provider) GenerateResponse(ctx context.Context, messages []types.Message, options types.RequestOptions) (string, *types.ResponseMetadata, error) {
	// TODO: Implement actual Ollama API call
	return "", nil, fmt.Errorf("Ollama provider not fully implemented yet")
}

// GenerateResponseStream generates a streaming response from Ollama
func (p *Provider) GenerateResponseStream(ctx context.Context, messages []types.Message, options types.RequestOptions, writer io.Writer) (*types.ResponseMetadata, error) {
	// TODO: Implement actual Ollama streaming API call
	return nil, fmt.Errorf("Ollama streaming not fully implemented yet")
}

// IsAvailable checks if the provider is available
func (p *Provider) IsAvailable(ctx context.Context) error {
	// TODO: Implement actual health check by calling Ollama API
	return fmt.Errorf("Ollama health check not implemented yet")
}

// EstimateTokens provides a rough estimate of token count
func (p *Provider) EstimateTokens(messages []types.Message) (int, error) {
	totalChars := 0
	for _, msg := range messages {
		totalChars += len(msg.Content) + len(msg.Role) + 10
	}
	return totalChars / 4, nil
}

// CalculateCost calculates the cost for given token usage (Ollama is typically free/local)
func (p *Provider) CalculateCost(usage types.TokenUsage) float64 {
	// Ollama is typically run locally and doesn't charge per token
	// Return 0 cost for local/self-hosted models
	return 0.0
}
