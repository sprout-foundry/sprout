package providers

import (
	"net/http"
	"os"
	"time"
)

// Provider represents an OpenAI-compatible API provider
type Provider interface {
	// GetName returns the provider name
	GetName() string

	// GetEndpoint returns the API endpoint URL
	GetEndpoint() string

	// GetAPIKey returns the API key
	GetAPIKey() string

	// GetDefaultModel returns the default model for this provider
	GetDefaultModel() string

	// IsAvailable checks if the provider is available (API key set)
	IsAvailable() bool
}

// BaseProvider provides common functionality for all providers
type BaseProvider struct {
	Name         string
	Endpoint     string
	APIKeyEnv    string
	DefaultModel string
	HTTPClient   *http.Client
}

// NewBaseProvider creates a new base provider
func NewBaseProvider(name, endpoint, apiKeyEnv, defaultModel string) *BaseProvider {
	timeout := 120 * time.Second

	return &BaseProvider{
		Name:         name,
		Endpoint:     endpoint,
		APIKeyEnv:    apiKeyEnv,
		DefaultModel: defaultModel,
		HTTPClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// GetName returns the provider name
func (p *BaseProvider) GetName() string {
	return p.Name
}

// GetEndpoint returns the API endpoint
func (p *BaseProvider) GetEndpoint() string {
	return p.Endpoint
}

// GetAPIKey returns the API key from environment
func (p *BaseProvider) GetAPIKey() string {
	return os.Getenv(p.APIKeyEnv)
}

// GetDefaultModel returns the default model
func (p *BaseProvider) GetDefaultModel() string {
	return p.DefaultModel
}

// IsAvailable checks if the provider is available
func (p *BaseProvider) IsAvailable() bool {
	return p.GetAPIKey() != ""
}

// GetHTTPClient returns the HTTP client
func (p *BaseProvider) GetHTTPClient() *http.Client {
	return p.HTTPClient
}
