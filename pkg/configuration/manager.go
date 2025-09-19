package configuration

import (
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// Manager provides a unified interface for configuration management
type Manager struct {
	config  *Config
	apiKeys *APIKeys
}

// NewManager creates a new configuration manager
func NewManager() (*Manager, error) {
	// Initialize configuration with first-run setup if needed
	config, apiKeys, err := Initialize()
	if err != nil {
		return nil, err
	}

	return &Manager{
		config:  config,
		apiKeys: apiKeys,
	}, nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *Config {
	return m.config
}

// GetAPIKeys returns the current API keys
func (m *Manager) GetAPIKeys() *APIKeys {
	return m.apiKeys
}

// SaveConfig saves the configuration to disk
func (m *Manager) SaveConfig() error {
	return m.config.Save()
}

// SaveAPIKeys saves the API keys to disk
func (m *Manager) SaveAPIKeys() error {
	return SaveAPIKeys(m.apiKeys)
}

// GetProvider returns the currently selected provider as ClientType
func (m *Manager) GetProvider() (api.ClientType, error) {
	provider := m.config.LastUsedProvider
	if provider == "" {
		return "", fmt.Errorf("no provider selected")
	}

	return mapStringToClientType(provider)
}

// SetProvider sets the current provider
func (m *Manager) SetProvider(clientType api.ClientType) error {
	provider := mapClientTypeToString(clientType)
	m.config.LastUsedProvider = provider
	return m.SaveConfig()
}

// GetModelForProvider returns the model for the given provider
func (m *Manager) GetModelForProvider(clientType api.ClientType) string {
	provider := mapClientTypeToString(clientType)
	return m.config.GetModelForProvider(provider)
}

// SetModelForProvider sets the model for a provider
func (m *Manager) SetModelForProvider(clientType api.ClientType, model string) error {
	provider := mapClientTypeToString(clientType)
	m.config.SetModelForProvider(provider, model)
	return m.SaveConfig()
}

// GetAPIKeyForProvider returns the API key for a provider
func (m *Manager) GetAPIKeyForProvider(clientType api.ClientType) string {
	provider := mapClientTypeToString(clientType)
	return m.apiKeys.GetAPIKey(provider)
}

// EnsureAPIKey ensures a provider has an API key, prompting if needed
func (m *Manager) EnsureAPIKey(clientType api.ClientType) error {
	provider := mapClientTypeToString(clientType)
	return EnsureProviderAPIKey(provider, m.apiKeys)
}

// HasAPIKey checks if a provider has an API key
func (m *Manager) HasAPIKey(clientType api.ClientType) bool {
	provider := mapClientTypeToString(clientType)
	return m.apiKeys.HasAPIKey(provider)
}

// SelectNewProvider allows interactive provider selection
func (m *Manager) SelectNewProvider() (api.ClientType, error) {
	currentProvider := m.config.LastUsedProvider
	selected, err := SelectProvider(currentProvider, m.apiKeys)
	if err != nil {
		return "", err
	}

	m.config.LastUsedProvider = selected
	if err := m.SaveConfig(); err != nil {
		return "", err
	}

	return mapStringToClientType(selected)
}

// GetAvailableProviders returns all providers that can be used
func (m *Manager) GetAvailableProviders() []api.ClientType {
	providers := GetAvailableProviders(m.apiKeys)
	result := []api.ClientType{}

	for _, p := range providers {
		if ct, err := mapStringToClientType(p); err == nil {
			result = append(result, ct)
		}
	}

	return result
}

// GetMCPConfig returns the MCP configuration
func (m *Manager) GetMCPConfig() mcp.MCPConfig {
	return m.config.MCP
}

// SetMCPEnabled enables or disables MCP
func (m *Manager) SetMCPEnabled(enabled bool) error {
	m.config.MCP.Enabled = enabled
	return m.SaveConfig()
}

// AddMCPServer adds an MCP server configuration
func (m *Manager) AddMCPServer(name string, server mcp.MCPServerConfig) error {
	if m.config.MCP.Servers == nil {
		m.config.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	m.config.MCP.Servers[name] = server
	return m.SaveConfig()
}

// mapClientTypeToString converts ClientType to string
func mapClientTypeToString(ct api.ClientType) string {
	switch ct {
	case api.OpenAIClientType:
		return "openai"
	case api.DeepInfraClientType:
		return "deepinfra"
	case api.OpenRouterClientType:
		return "openrouter"
	case api.OllamaClientType:
		return "ollama"
	case api.OllamaLocalClientType:
		return "ollama-local"
	case api.OllamaTurboClientType:
		return "ollama-turbo"
	default:
		// For providers not yet in ClientType constants
		return string(ct)
	}
}

// mapStringToClientType converts string to ClientType
func mapStringToClientType(s string) (api.ClientType, error) {
	switch s {
	case "openai":
		return api.OpenAIClientType, nil
	case "deepinfra":
		return api.DeepInfraClientType, nil
	case "openrouter":
		return api.OpenRouterClientType, nil
	case "ollama":
		return api.OllamaClientType, nil
	case "ollama-local":
		return api.OllamaLocalClientType, nil
	case "ollama-turbo":
		return api.OllamaTurboClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", s)
	}
}
