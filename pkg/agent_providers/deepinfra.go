package providers

import (
	"fmt"
	"net/http"
	"os"
	"time"
)

// DeepInfraProvider implements the OpenAI-compatible DeepInfra API
type DeepInfraProvider struct {
	httpClient *http.Client
	apiToken   string
	debug      bool
	model      string
}

// NewDeepInfraProvider creates a new DeepInfra provider instance
func NewDeepInfraProvider() (*DeepInfraProvider, error) {
	token := os.Getenv("DEEPINFRA_API_KEY")
	if token == "" {
		return nil, fmt.Errorf("DEEPINFRA_API_KEY environment variable not set")
	}

	return &DeepInfraProvider{
		httpClient: &http.Client{
			Timeout: 300 * time.Second,
		},
		apiToken: token,
		debug:    false,
		model:    "deepseek-ai/DeepSeek-V3.1", // Default DeepInfra model
	}, nil
}

// NewDeepInfraProviderWithModel creates a DeepInfra provider with a specific model
func NewDeepInfraProviderWithModel(model string) (*DeepInfraProvider, error) {
	provider, err := NewDeepInfraProvider()
	if err != nil {
		return nil, err
	}
	provider.model = model
	return provider, nil
}

// GetEndpoint returns the DeepInfra API endpoint
func (p *DeepInfraProvider) GetEndpoint() string {
	return "https://api.deepinfra.com/v1/openai/chat/completions"
}

// GetAPIKey returns the DeepInfra API key
func (p *DeepInfraProvider) GetAPIKey() string {
	return p.apiToken
}

// GetModel returns the current model
func (p *DeepInfraProvider) GetModel() string {
	return p.model
}

// SetModel sets the model to use
func (p *DeepInfraProvider) SetModel(model string) {
	p.model = model
}

// SetDebug enables or disables debug mode
func (p *DeepInfraProvider) SetDebug(debug bool) {
	p.debug = debug
}

// GetHTTPClient returns the HTTP client
func (p *DeepInfraProvider) GetHTTPClient() *http.Client {
	return p.httpClient
}

// IsDebug returns whether debug mode is enabled
func (p *DeepInfraProvider) IsDebug() bool {
	return p.debug
}

// GetProviderName returns the provider name
func (p *DeepInfraProvider) GetProviderName() string {
	return "deepinfra"
}


// SupportsVision checks if DeepInfra supports vision
func (p *DeepInfraProvider) SupportsVision() bool {
	// Check if we have a vision model available
	visionModel := p.GetVisionModel()
	return visionModel != ""
}

// GetVisionModel returns the vision model for DeepInfra
func (p *DeepInfraProvider) GetVisionModel() string {
	return "google/gemma-3-27b-it" // DeepInfra's vision-capable model
}
