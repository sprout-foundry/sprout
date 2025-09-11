package config

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// Manager handles configuration operations with intelligent fallbacks
type Manager struct {
	config *Config
}

// NewManager creates a new configuration manager
func NewManager() (*Manager, error) {
	config, err := Load()
	if err != nil {
		return nil, err
	}
	
	return &Manager{
		config: config,
	}, nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *Config {
	return m.config
}

// GetBestProvider determines the best provider to use, considering:
// 1. Last used provider (if still available)
// 2. Environment variables
// 3. Availability checks
// 4. User preferences
func (m *Manager) GetBestProvider() (api.ClientType, string, error) {
	// Try last used provider first if it's available
	lastProvider := m.config.GetLastUsedProvider()
	if lastProvider != "" {
		if m.isProviderAvailable(lastProvider) {
			model := m.config.GetModelForProvider(lastProvider)
			return lastProvider, model, nil
		}
	}
	
	// Fall back to environment-based detection with availability check
	envProvider := api.GetClientTypeFromEnv()
	if m.isProviderAvailable(envProvider) {
		model := m.config.GetModelForProvider(envProvider)
		return envProvider, model, nil
	}
	
	// Try providers in priority order
	for _, providerName := range m.config.ProviderPriority {
		provider, err := GetProviderFromConfigName(providerName)
		if err != nil {
			continue
		}
		
		if m.isProviderAvailable(provider) {
			model := m.config.GetModelForProvider(provider)
			return provider, model, nil
		}
	}
	
	return "", "", fmt.Errorf("no available providers found")
}

// SetProviderAndModel sets the provider and model, and persists the choice
func (m *Manager) SetProviderAndModel(provider api.ClientType, model string) error {
	// Validate that the provider is available
	if !m.isProviderAvailable(provider) {
		return fmt.Errorf("provider %s is not available", api.GetProviderName(provider))
	}
	
	// Update configuration
	m.config.SetModelForProvider(provider, model)
	m.config.SetLastUsedProvider(provider)
	
	// Save to disk
	return m.config.Save()
}

// GetModelForProvider returns the configured model for a provider
func (m *Manager) GetModelForProvider(provider api.ClientType) string {
	return m.config.GetModelForProvider(provider)
}

// ListAvailableProviders returns all currently available providers
func (m *Manager) ListAvailableProviders() []api.ClientType {
	var available []api.ClientType
	
	allProviders := []api.ClientType{
		api.DeepInfraClientType,
		api.OllamaClientType,
		api.CerebrasClientType,
		api.OpenRouterClientType,
		api.GroqClientType,
		api.DeepSeekClientType,
	}
	
	for _, provider := range allProviders {
		if m.isProviderAvailable(provider) {
			available = append(available, provider)
		}
	}
	
	return available
}

// isProviderAvailable checks if a provider is currently available
func (m *Manager) isProviderAvailable(provider api.ClientType) bool {
	// For Ollama, check if it's running
	if provider == api.OllamaClientType {
		client, err := api.NewUnifiedClient(api.OllamaClientType)
		if err != nil {
			return false
		}
		return client.CheckConnection() == nil
	}
	
	// For other providers, check if API key is set
	envVar := m.getProviderEnvVar(provider)
	if envVar == "" {
		return false
	}
	
	return os.Getenv(envVar) != ""
}

// getProviderEnvVar returns the environment variable name for a provider
func (m *Manager) getProviderEnvVar(provider api.ClientType) string {
	switch provider {
	case api.DeepInfraClientType:
		return "DEEPINFRA_API_KEY"
	case api.CerebrasClientType:
		return "CEREBRAS_API_KEY"
	case api.OpenRouterClientType:
		return "OPENROUTER_API_KEY"
	case api.GroqClientType:
		return "GROQ_API_KEY"
	case api.DeepSeekClientType:
		return "DEEPSEEK_API_KEY"
	case api.OllamaClientType:
		return "" // Ollama doesn't use an API key
	default:
		return ""
	}
}

// GetProviderStatus returns detailed status information for all providers
func (m *Manager) GetProviderStatus() map[api.ClientType]ProviderStatus {
	status := make(map[api.ClientType]ProviderStatus)
	
	allProviders := []api.ClientType{
		api.DeepInfraClientType,
		api.OllamaClientType,
		api.CerebrasClientType,
		api.OpenRouterClientType,
		api.GroqClientType,
		api.DeepSeekClientType,
	}
	
	for _, provider := range allProviders {
		status[provider] = ProviderStatus{
			Available:     m.isProviderAvailable(provider),
			Name:          api.GetProviderName(provider),
			CurrentModel:  m.config.GetModelForProvider(provider),
			IsLastUsed:    provider == m.config.LastUsedProvider,
			EnvVar:        m.getProviderEnvVar(provider),
		}
	}
	
	return status
}

// ProviderStatus contains status information for a provider
type ProviderStatus struct {
	Available     bool   `json:"available"`
	Name          string `json:"name"`
	CurrentModel  string `json:"current_model"`
	IsLastUsed    bool   `json:"is_last_used"`
	EnvVar        string `json:"env_var"`
}

// UpdateProviderPriority updates the provider priority order
func (m *Manager) UpdateProviderPriority(priority []string) error {
	// Validate that all providers in the priority list are valid
	for _, providerName := range priority {
		if _, err := GetProviderFromConfigName(providerName); err != nil {
			return fmt.Errorf("invalid provider in priority list: %s", providerName)
		}
	}
	
	m.config.ProviderPriority = priority
	return m.config.Save()
}

// Reset resets the configuration to defaults
func (m *Manager) Reset() error {
	m.config = NewConfig()
	return m.config.Save()
}