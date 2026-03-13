package configuration

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// Manager provides a unified interface for configuration management
type Manager struct {
	mu        sync.Mutex
	config    *Config
	apiKeys   *APIKeys
	lastSaved *Config // Track last saved state, not initial snapshot
}

// NewManager creates a new configuration manager
func NewManager() (*Manager, error) {
	// Initialize configuration with first-run setup if needed
	config, apiKeys, err := Initialize()
	if err != nil {
		return nil, err
	}

	return &Manager{
		config:    config,
		apiKeys:   apiKeys,
		lastSaved: config, // Track last saved state as the base
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
	m.mu.Lock()
	defer m.mu.Unlock()

	// Save the current manager config directly
	if err := m.config.Save(); err != nil {
		return err
	}

	// Update lastSaved
	m.lastSaved = cloneConfig(m.config)
	return nil
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

	return m.mapStringToClientType(provider)
}

// SetProvider sets the current provider
func (m *Manager) SetProvider(clientType api.ClientType) error {
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
	provider := mapClientTypeToString(clientType)
	m.mu.Lock()
	m.config.SetModelForProvider(provider, model)
	m.mu.Unlock()
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
	return HasProviderCredential(provider, m.apiKeys)
}

// SelectNewProvider allows interactive provider selection
func (m *Manager) SelectNewProvider() (api.ClientType, error) {
	m.mu.Lock()
	currentProvider := m.config.LastUsedProvider
	m.mu.Unlock()
	selected, err := SelectProvider(currentProvider, m.apiKeys)
	if err != nil {
		return "", err
	}

	m.mu.Lock()
	m.config.LastUsedProvider = selected
	m.mu.Unlock()
	if err := m.SaveConfig(); err != nil {
		return "", err
	}

	return m.mapStringToClientType(selected)
}

// GetAvailableProviders returns all providers that can be used
func (m *Manager) GetAvailableProviders() []api.ClientType {
	providers := GetAvailableProviders(m.apiKeys)
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

// GetMCPConfig returns the MCP configuration
func (m *Manager) GetMCPConfig() mcp.MCPConfig {
	return m.config.MCP
}

// SetMCPEnabled enables or disables MCP
func (m *Manager) SetMCPEnabled(enabled bool) error {
	m.mu.Lock()
	m.config.MCP.Enabled = enabled
	m.mu.Unlock()
	return m.SaveConfig()
}

// AddMCPServer adds an MCP server configuration
func (m *Manager) AddMCPServer(name string, server mcp.MCPServerConfig) error {
	m.mu.Lock()
	if m.config.MCP.Servers == nil {
		m.config.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	m.config.MCP.Servers[name] = server
	m.mu.Unlock()
	return m.SaveConfig()
}

func cloneConfig(cfg *Config) *Config {
	if cfg == nil {
		return nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return &out
}

func mergeConfigChanges(base, current, latest *Config) (*Config, error) {
	if current == nil {
		return cloneConfig(latest), nil
	}
	if latest == nil {
		latest = NewConfig()
	}

	baseMap, err := configToMap(base)
	if err != nil {
		return nil, err
	}
	currentMap, err := configToMap(current)
	if err != nil {
		return nil, err
	}
	latestMap, err := configToMap(latest)
	if err != nil {
		return nil, err
	}

	// Apply changes: start from latest, then merge in current changes
	// The current state (manager's in-memory state) should be applied on top of the file
	applyMapDiff(baseMap, currentMap, latestMap)
	return mapToConfig(latestMap)
}

func configToMap(cfg *Config) (map[string]interface{}, error) {
	if cfg == nil {
		return map[string]interface{}{}, nil
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return nil, err
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func mapToConfig(m map[string]interface{}) (*Config, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}

	// Keep canonical zero-value protections that Load() applies.
	if out.ProviderModels == nil {
		out.ProviderModels = make(map[string]string)
	}
	if out.Preferences == nil {
		out.Preferences = make(map[string]interface{})
	}
	if out.MCP.Servers == nil {
		out.MCP.Servers = make(map[string]mcp.MCPServerConfig)
	}
	if out.CustomProviders == nil {
		out.CustomProviders = make(map[string]CustomProviderConfig)
	}
	if out.SubagentTypes == nil {
		out.SubagentTypes = make(map[string]SubagentType)
	}
	if out.Skills == nil {
		out.Skills = make(map[string]Skill)
	}
	return &out, nil
}

func applyMapDiff(base, current, target map[string]interface{}) {
	if current == nil {
		return
	}
	for key := range target {
		if _, ok := current[key]; !ok {
			if _, existed := base[key]; existed {
				// Deletion in current relative to base: apply deletion.
				delete(target, key)
			}
			// Keys not in base are new additions (manual edits) - preserve them
		}
	}

	for key, currentVal := range current {
		baseVal, baseHas := base[key]
		targetVal, targetHas := target[key]
		if !baseHas {
			target[key] = currentVal
			continue
		}
		if reflect.DeepEqual(baseVal, currentVal) {
			continue
		}

		baseMap, baseMapOK := baseVal.(map[string]interface{})
		currentMap, currentMapOK := currentVal.(map[string]interface{})
		targetMap, targetMapOK := targetVal.(map[string]interface{})
		if baseMapOK && currentMapOK {
			if !targetMapOK || !targetHas {
				targetMap = map[string]interface{}{}
			}
			applyMapDiff(baseMap, currentMap, targetMap)
			target[key] = targetMap
			continue
		}

		// Scalars/slices/type changes: overwrite with current value.
		target[key] = currentVal
	}
}

// mapClientTypeToString converts ClientType to string
func mapClientTypeToString(ct api.ClientType) string {
	switch ct {
	case api.ChutesClientType:
		return "chutes"
	case api.OpenAIClientType:
		return "openai"
	case api.ZAIClientType:
		return "zai"
	case api.DeepInfraClientType:
		return "deepinfra"
	case api.DeepSeekClientType:
		return "deepseek"
	case api.OpenRouterClientType:
		return "openrouter"
	case api.OllamaClientType:
		return "ollama"
	case api.OllamaLocalClientType:
		return "ollama-local"
	case api.OllamaTurboClientType:
		return "ollama-turbo"
	case api.LMStudioClientType:
		return "lmstudio"
	case api.MistralClientType:
		return "mistral"
	case api.MinimaxClientType:
		return "minimax"
	case api.TestClientType:
		return "test"
	default:
		// For providers not yet in ClientType constants
		return string(ct)
	}
}

// mapStringToClientType converts string to ClientType
func (m *Manager) mapStringToClientType(s string) (api.ClientType, error) {
	return MapProviderStringToClientType(m.config, s)
}
