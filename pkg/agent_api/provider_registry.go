package api

import (
	"fmt"
	"os"
)

// ProviderConfig holds configuration for a provider
type ProviderConfig struct {
	Type        ClientType `json:"type"`
	Name        string     `json:"name"`
	EnvVarName  string     `json:"env_var_name,omitempty"` // Empty for local providers like Ollama
	FactoryFunc func(model string) (ClientInterface, error) `json:"-"` // Function reference, not serialized
}

// ProviderRegistry manages provider configurations in a data-driven way  
type ProviderRegistry struct {
	providers map[ClientType]ProviderConfig
}

var defaultProviderRegistry *ProviderRegistry

// GetProviderRegistry returns the default provider registry
func GetProviderRegistry() *ProviderRegistry {
	if defaultProviderRegistry == nil {
		defaultProviderRegistry = newDefaultProviderRegistry()
	}
	return defaultProviderRegistry
}

// newDefaultProviderRegistry creates the registry with all provider configurations
func newDefaultProviderRegistry() *ProviderRegistry {
	registry := &ProviderRegistry{
		providers: make(map[ClientType]ProviderConfig),
	}
	
	// Register all providers with their configurations
	registry.RegisterProvider(ProviderConfig{
		Type:        OpenAIClientType,
		Name:        "OpenAI",
		EnvVarName:  "OPENAI_API_KEY",
		FactoryFunc: NewOpenAIClientWrapper,
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        DeepInfraClientType,
		Name:        "DeepInfra", 
		EnvVarName:  "DEEPINFRA_API_KEY",
		FactoryFunc: NewDeepInfraClientWrapper,
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        OllamaClientType,
		Name:        "Ollama (Local)",
		EnvVarName:  "", // No API key required for local Ollama
		FactoryFunc: func(model string) (ClientInterface, error) { return NewOllamaClient() },
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        CerebrasClientType,
		Name:        "Cerebras",
		EnvVarName:  "CEREBRAS_API_KEY",
		FactoryFunc: NewCerebrasClientWrapper,
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        OpenRouterClientType,
		Name:        "OpenRouter",
		EnvVarName:  "OPENROUTER_API_KEY", 
		FactoryFunc: NewOpenRouterClientWrapper,
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        GroqClientType,
		Name:        "Groq",
		EnvVarName:  "GROQ_API_KEY",
		FactoryFunc: NewGroqClientWrapper,
	})
	
	registry.RegisterProvider(ProviderConfig{
		Type:        DeepSeekClientType,
		Name:        "DeepSeek", 
		EnvVarName:  "DEEPSEEK_API_KEY",
		FactoryFunc: NewDeepSeekClientWrapper,
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