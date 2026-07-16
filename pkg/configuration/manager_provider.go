package configuration

// Package configuration: provider selection and model management (split from manager.go)

import (
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// GetProvider returns the currently selected provider as ClientType
func (m *Manager) GetProvider() (api.ClientType, error) {
	provider := m.config.LastUsedProvider
	if provider == "" {
		return "", fmt.Errorf("no provider selected")
	}

	return m.mapStringToClientType(provider)
}

// SetProvider sets the current provider
func (m *Manager) SetProvider(clientType api.ClientType) error {
	// Prevent test provider from being persisted - it's for testing only
	if clientType == api.TestClientType {
		return fmt.Errorf("test provider cannot be persisted as the active provider")
	}

	provider := mapClientTypeToString(clientType)
	m.mu.Lock()
	m.config.LastUsedProvider = provider
	m.mu.Unlock()
	return m.SaveConfig()
}

// GetModelForProvider returns the model for the given provider
func (m *Manager) GetModelForProvider(clientType api.ClientType) string {
	provider := mapClientTypeToString(clientType)
	return m.config.GetModelForProvider(provider)
}

// SetModelForProvider sets the model for a provider
func (m *Manager) SetModelForProvider(clientType api.ClientType, model string) error {
	// Prevent test provider models from being persisted
	if clientType == api.TestClientType {
		return fmt.Errorf("test provider cannot be persisted as the active provider")
	}
	provider := mapClientTypeToString(clientType)
	m.mu.Lock()
	m.config.SetModelForProvider(provider, model)
	m.mu.Unlock()
	return m.SaveConfig()
}

// GetAPIKeyForProvider returns the API key for a provider
func (m *Manager) GetAPIKeyForProvider(clientType api.ClientType) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	provider := mapClientTypeToString(clientType)
	return m.apiKeys.GetAPIKey(provider)
}

// EnsureAPIKey ensures a provider has an API key, prompting if needed
func (m *Manager) EnsureAPIKey(clientType api.ClientType) error {
	provider := mapClientTypeToString(clientType)
	m.mu.RLock()
	keys := m.apiKeys
	m.mu.RUnlock()
	return EnsureProviderAPIKey(provider, keys)
}

// HasAPIKey checks if a provider has an API key
func (m *Manager) HasAPIKey(clientType api.ClientType) bool {
	provider := mapClientTypeToString(clientType)
	return HasProviderAuth(provider)
}

// SelectNewProvider allows interactive provider selection
func (m *Manager) SelectNewProvider() (api.ClientType, error) {
	m.mu.RLock()
	currentProvider := m.config.LastUsedProvider
	apiKeys := m.apiKeys
	m.mu.RUnlock()
	selected, err := SelectProvider(currentProvider, apiKeys)
	if err != nil {
		return "", fmt.Errorf("failed to select provider: %w", err)
	}

	// Prevent test provider from being persisted — it should never
	// appear as the active default in config.
	if selected == "test" {
		return "", fmt.Errorf("test provider cannot be persisted as the active provider")
	}

	m.mu.Lock()
	m.config.LastUsedProvider = selected
	m.mu.Unlock()
	if err := m.SaveConfig(); err != nil {
		return "", fmt.Errorf("failed to save config: %w", err)
	}

	return m.mapStringToClientType(selected)
}

// GetAvailableProviders returns all providers that can be used
func (m *Manager) GetAvailableProviders() []api.ClientType {
	providers := GetAvailableProviders()
	result := []api.ClientType{}
	seen := map[api.ClientType]struct{}{}

	for _, p := range providers {
		if ct, err := m.mapStringToClientType(p); err == nil {
			if _, exists := seen[ct]; exists {
				continue
			}
			seen[ct] = struct{}{}
			result = append(result, ct)
		}
	}

	// Add custom providers
	if m.config.CustomProviders != nil {
		for name := range m.config.CustomProviders {
			ct := api.ClientType(name)
			if _, exists := seen[ct]; exists {
				continue
			}
			seen[ct] = struct{}{}
			result = append(result, ct)
		}
	}

	return result
}

// MapStringToClientType converts string to ClientType, handling custom providers
func (m *Manager) MapStringToClientType(s string) (api.ClientType, error) {
	return m.mapStringToClientType(s)
}

// ResolveProviderModel resolves provider+model selection using canonical precedence.
func (m *Manager) ResolveProviderModel(explicitProvider, explicitModel string) (api.ClientType, string, error) {
	return ResolveProviderModel(m.config, explicitProvider, explicitModel)
}
