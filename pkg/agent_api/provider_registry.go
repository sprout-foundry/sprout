package api

import (
	"fmt"
	"os"
	"sync"
)

// ProviderConfig holds configuration for a provider
type ProviderConfig struct {
	Type               ClientType                                  `json:"type"`
	Name               string                                      `json:"name"`
	EnvVarName         string                                      `json:"env_var_name,omitempty"` // Empty for local providers like Ollama
	DefaultModel       string                                      `json:"default_model"`
	DefaultVisionModel string                                      `json:"default_vision_model,omitempty"` // Empty if no vision support
	FactoryFunc        func(model string) (ClientInterface, error) `json:"-"`                              // Function reference, not serialized
}

// ProviderRegistry manages provider configurations in a data-driven way
type ProviderRegistry struct {
	providers map[ClientType]ProviderConfig
}

var (
	defaultProviderRegistry *ProviderRegistry
	providerRegistryOnce    sync.Once
)

// GetProviderRegistry returns the default provider registry (thread-safe singleton)
func GetProviderRegistry() *ProviderRegistry {
	providerRegistryOnce.Do(func() {
		defaultProviderRegistry = newDefaultProviderRegistry()
	})
	return defaultProviderRegistry
}

// newDefaultProviderRegistry creates the registry with all provider configurations
func newDefaultProviderRegistry() *ProviderRegistry {
	registry := &ProviderRegistry{
		providers: make(map[ClientType]ProviderConfig),
	}

	// Register all providers with their configurations
	registry.RegisterProvider(ProviderConfig{
		Type:               OpenAIClientType,
		Name:               "OpenAI",
		EnvVarName:         "OPENAI_API_KEY",
		DefaultModel:       "gpt-4o-mini",
		DefaultVisionModel: "gpt-4o",
		FactoryFunc:        NewOpenAIClientWrapper,
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               DeepInfraClientType,
		Name:               "DeepInfra",
		EnvVarName:         "DEEPINFRA_API_KEY",
		DefaultModel:       "deepseek-ai/deepseek-v3.1",
		DefaultVisionModel: "",
		FactoryFunc:        NewDeepInfraClientWrapper,
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               OllamaClientType,
		Name:               "Ollama (Local)",
		EnvVarName:         "", // No API key required for local Ollama
		DefaultModel:       "gpt-oss:20b",
		DefaultVisionModel: "",
		FactoryFunc:        func(model string) (ClientInterface, error) { return NewOllamaClient() },
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               CerebrasClientType,
		Name:               "Cerebras",
		EnvVarName:         "CEREBRAS_API_KEY",
		DefaultModel:       "cerebras/btlm-3b-8k-base",
		DefaultVisionModel: "",
		FactoryFunc:        NewCerebrasClientWrapper,
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               OpenRouterClientType,
		Name:               "OpenRouter",
		EnvVarName:         "OPENROUTER_API_KEY",
		DefaultModel:       "deepseek/deepseek-chat-v3.1:free",
		DefaultVisionModel: "gpt-4o",
		FactoryFunc:        NewOpenRouterClientWrapper,
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               GroqClientType,
		Name:               "Groq",
		EnvVarName:         "GROQ_API_KEY",
		DefaultModel:       "llama3-70b-8192",
		DefaultVisionModel: "",
		FactoryFunc:        NewGroqClientWrapper,
	})

	registry.RegisterProvider(ProviderConfig{
		Type:               DeepSeekClientType,
		Name:               "DeepSeek",
		EnvVarName:         "DEEPSEEK_API_KEY",
		DefaultModel:       "deepseek-chat",
		DefaultVisionModel: "",
		FactoryFunc:        NewDeepSeekClientWrapper,
	})

	return registry
}

// RegisterProvider adds a provider to the registry
func (r *ProviderRegistry) RegisterProvider(config ProviderConfig) {
	r.providers[config.Type] = config
}

// CreateClient creates a client for the specified provider type
func (r *ProviderRegistry) CreateClient(clientType ClientType, model string) (ClientInterface, error) {
	provider, exists := r.providers[clientType]
	if !exists {
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}

	// Check API key requirement for non-local providers
	if provider.EnvVarName != "" {
		if err := r.ensureAPIKeyAvailable(provider); err != nil {
			return nil, fmt.Errorf("API key required for %s: %w", provider.Name, err)
		}
	}

	// Use the factory function to create the client
	return provider.FactoryFunc(model)
}

// GetProviderName returns the display name for a provider
func (r *ProviderRegistry) GetProviderName(clientType ClientType) string {
	if provider, exists := r.providers[clientType]; exists {
		return provider.Name
	}
	return string(clientType)
}

// GetProviderEnvVar returns the environment variable name for a provider
func (r *ProviderRegistry) GetProviderEnvVar(clientType ClientType) string {
	if provider, exists := r.providers[clientType]; exists {
		return provider.EnvVarName
	}
	return ""
}

// GetDefaultModel returns the default model for a provider
func (r *ProviderRegistry) GetDefaultModel(clientType ClientType) string {
	if provider, exists := r.providers[clientType]; exists {
		return provider.DefaultModel
	}
	return "gpt-4o-mini" // Fallback default
}

// GetDefaultVisionModel returns the default vision model for a provider
func (r *ProviderRegistry) GetDefaultVisionModel(clientType ClientType) string {
	if provider, exists := r.providers[clientType]; exists {
		return provider.DefaultVisionModel
	}
	return "" // No vision support by default
}

// GetAvailableProviders returns a list of all registered provider types
func (r *ProviderRegistry) GetAvailableProviders() []ClientType {
	types := make([]ClientType, 0, len(r.providers))
	for providerType := range r.providers {
		types = append(types, providerType)
	}
	return types
}

// ensureAPIKeyAvailable checks if the required API key is available
func (r *ProviderRegistry) ensureAPIKeyAvailable(provider ProviderConfig) error {
	if provider.EnvVarName == "" {
		return nil // No API key required
	}

	if os.Getenv(provider.EnvVarName) == "" {
		return fmt.Errorf("environment variable %s is not set", provider.EnvVarName)
	}

	return nil
}
