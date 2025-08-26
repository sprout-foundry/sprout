package llm

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/interfaces"
	"github.com/alantheprice/ledit/pkg/interfaces/types"
)

// Factory provides convenient methods for creating providers
type Factory struct {
	registry *Registry
}

// NewFactory creates a new provider factory
func NewFactory(registry *Registry) *Factory {
	return &Factory{
		registry: registry,
	}
}

// CreateProvider creates a provider instance from configuration
func (f *Factory) CreateProvider(config *types.ProviderConfig) (interfaces.LLMProvider, error) {
	if config == nil {
		return nil, fmt.Errorf("provider configuration is required")
	}

	if config.Name == "" {
		return nil, fmt.Errorf("provider name is required")
	}

	if !config.Enabled {
		return nil, fmt.Errorf("provider '%s' is disabled", config.Name)
	}

	// Normalize provider name
	providerName := strings.ToLower(config.Name)

	return f.registry.GetProvider(providerName, config)
}

// CreateProviderByName creates a provider with minimal configuration
func (f *Factory) CreateProviderByName(name, model, apiKey string) (interfaces.LLMProvider, error) {
	config := &types.ProviderConfig{
		Name:    name,
		Model:   model,
		APIKey:  apiKey,
		Enabled: true,
	}

	return f.CreateProvider(config)
}

// GetAvailableProviders returns a list of available provider names
func (f *Factory) GetAvailableProviders() []string {
	return f.registry.ListProviders()
}

// ValidateProviderConfig validates a provider configuration
func (f *Factory) ValidateProviderConfig(config *types.ProviderConfig) error {
	if config == nil {
		return fmt.Errorf("provider configuration is required")
	}

	if config.Name == "" {
		return fmt.Errorf("provider name is required")
	}

	providerName := strings.ToLower(config.Name)
	return f.registry.ValidateConfig(providerName, config)
}

// AutoDetectProvider attempts to auto-detect the best available provider
func (f *Factory) AutoDetectProvider(configs []*types.ProviderConfig) (*types.ProviderConfig, error) {
	if len(configs) == 0 {
		return nil, fmt.Errorf("no provider configurations provided")
	}

	// Priority order for provider selection
	priorityOrder := []string{"openai", "gemini", "ollama", "groq", "deepinfra"}

	// First, try providers in priority order
	for _, preferredProvider := range priorityOrder {
		for _, config := range configs {
			if strings.ToLower(config.Name) == preferredProvider && config.Enabled {
				if err := f.ValidateProviderConfig(config); err == nil {
					return config, nil
				}
			}
		}
	}

	// If no priority provider works, try any enabled provider
	for _, config := range configs {
		if config.Enabled {
			if err := f.ValidateProviderConfig(config); err == nil {
				return config, nil
			}
		}
	}

	return nil, fmt.Errorf("no valid provider configurations found")
}

// CreateMultipleProviders creates multiple providers from configurations
func (f *Factory) CreateMultipleProviders(configs []*types.ProviderConfig) (map[string]interfaces.LLMProvider, error) {
	providers := make(map[string]interfaces.LLMProvider)
	errors := make([]string, 0)

	for _, config := range configs {
		if !config.Enabled {
			continue
		}

		provider, err := f.CreateProvider(config)
		if err != nil {
			errors = append(errors, fmt.Sprintf("failed to create provider '%s': %v", config.Name, err))
			continue
		}

		providers[config.Name] = provider
	}

	if len(providers) == 0 {
		if len(errors) > 0 {
			return nil, fmt.Errorf("failed to create any providers: %s", strings.Join(errors, "; "))
		}
		return nil, fmt.Errorf("no enabled providers found")
	}

	return providers, nil
}

// GetProviderCapabilities returns the capabilities of a provider
func (f *Factory) GetProviderCapabilities(providerName string) (*ProviderCapabilities, error) {
	// This would typically be loaded from configuration or metadata
	capabilities := getDefaultCapabilities(providerName)
	if capabilities == nil {
		return nil, fmt.Errorf("unknown provider: %s", providerName)
	}

	return capabilities, nil
}

// ProviderCapabilities describes what a provider supports
type ProviderCapabilities struct {
	Name            string   `json:"name"`
	SupportsTools   bool     `json:"supports_tools"`
	SupportsImages  bool     `json:"supports_images"`
	SupportsStream  bool     `json:"supports_stream"`
	MaxTokens       int      `json:"max_tokens"`
	SupportedModels []string `json:"supported_models"`
}

// getDefaultCapabilities returns default capabilities for known providers
func getDefaultCapabilities(providerName string) *ProviderCapabilities {
	switch strings.ToLower(providerName) {
	case "openai":
		return &ProviderCapabilities{
			Name:            "openai",
			SupportsTools:   true,
			SupportsImages:  true,
			SupportsStream:  true,
			MaxTokens:       128000,
			SupportedModels: []string{"gpt-4", "gpt-4-turbo", "gpt-3.5-turbo"},
		}
	case "gemini":
		return &ProviderCapabilities{
			Name:            "gemini",
			SupportsTools:   true,
			SupportsImages:  true,
			SupportsStream:  true,
			MaxTokens:       32768,
			SupportedModels: []string{"gemini-pro", "gemini-pro-vision"},
		}
	case "ollama":
		return &ProviderCapabilities{
			Name:            "ollama",
			SupportsTools:   false,
			SupportsImages:  false,
			SupportsStream:  true,
			MaxTokens:       4096,
			SupportedModels: []string{"llama2", "codellama", "mistral"},
		}
	case "groq":
		return &ProviderCapabilities{
			Name:            "groq",
			SupportsTools:   true,
			SupportsImages:  false,
			SupportsStream:  true,
			MaxTokens:       32768,
			SupportedModels: []string{"llama-3.1-70b", "mixtral-8x7b"},
		}
	default:
		return nil
	}
}

// NewGlobalFactory creates a factory using the global registry
func NewGlobalFactory() *Factory {
	return NewFactory(GetGlobalRegistry())
}
