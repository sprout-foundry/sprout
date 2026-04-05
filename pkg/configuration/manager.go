package configuration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
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
	loaded    bool    // Track if config has been loaded
}

// loadConfigSilently loads configuration without showing welcome messages
func loadConfigSilently() (*Config, *APIKeys, error) {
	// Ensure config directory exists (ignore errors - we'll handle them later)
	_, _ = GetConfigDir()

	// Load or create config
	config, err := Load()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load config: %w", err)
	}

	// Load API keys
	apiKeys, err := LoadAPIKeys()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load API keys: %w", err)
	}

	// Populate from environment variables FIRST - prioritize env vars over stored keys
	if !apiKeys.PopulateFromEnvironment() {
		log.Printf("[debug] no API keys found in environment variables")
	}

	// Check if we need to set a default provider
	if config.LastUsedProvider == "" {
		// Check for environment variables
		allProviders := getSupportedProviders()
		for _, provider := range allProviders {
			if provider.RequiresKey && provider.EnvVariableName != "" {
				if envKey := os.Getenv(provider.EnvVariableName); envKey != "" {
					config.LastUsedProvider = provider.Name
					break
				}
			}
		}

		// If no env provider, check for saved API keys
		if config.LastUsedProvider == "" {
			for _, provider := range allProviders {
				if provider.RequiresKey && HasProviderCredential(provider.Name, apiKeys) {
					config.LastUsedProvider = provider.Name
					break
				}
			}
		}

		// Default to test provider if nothing found
		if config.LastUsedProvider == "" {
			config.LastUsedProvider = "test"
		}

		if err := config.Save(); err != nil {
			return nil, nil, fmt.Errorf("failed to save config: %w", err)
		}
	}

	return config, apiKeys, nil
}

// NewManager creates a new configuration manager
func NewManager() (*Manager, error) {
	// Initialize configuration with first-run setup if needed
	config, apiKeys, err := Initialize()
	if err != nil {
		return nil, fmt.Errorf("load configuration: %w", err)
	}

	return &Manager{
		config:    config,
		apiKeys:   apiKeys,
		lastSaved: cloneConfig(config), // Track last saved state as the base
		loaded:    true,
	}, nil
}

// NewManagerSilent creates a new configuration manager without showing welcome messages
func NewManagerSilent() (*Manager, error) {
	// Load configuration silently
	config, apiKeys, err := loadConfigSilently()
	if err != nil {
		return nil, fmt.Errorf("initialize API keys: %w", err)
	}

	return &Manager{
		config:    config,
		apiKeys:   apiKeys,
		lastSaved: cloneConfig(config),
		loaded:    true,
	}, nil
}

// NewManagerWithConfig creates a new configuration manager from an explicit
// Config and optional API key set.  The manager will persist saves to the same
// location that config.Save()/Load() would use for the current env.  Pass nil
// for apiKeys to skip key loading.
func NewManagerWithConfig(cfg *Config, apiKeys *APIKeys) *Manager {
	return &Manager{
		config:    cfg,
		apiKeys:   apiKeys,
		lastSaved: cloneConfig(cfg),
		loaded:    true,
	}
}

// NewManagerWithDir creates a configuration Manager fully backed by configDir.
// If no config file exists in configDir a fresh default one is written so that
// subsequent Load/Save calls operate deterministically.
//
// This is intended for tests and tooling that need a hermetic config
// environment without touching the caller's real ~/.ledit.
func NewManagerWithDir(configDir string) (*Manager, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory %q: %w", configDir, err)
	}

	// Temporarily point the configuration layer at configDir.
	prev, ok := os.LookupEnv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", configDir)
	defer func() {
		if ok {
			os.Setenv("LEDIT_CONFIG", prev)
		} else {
			os.Unsetenv("LEDIT_CONFIG")
		}
	}()

	// Ensure a config file exists so Load() doesn't fall through to the real
	// user home directory.
	configPath := filepath.Join(configDir, ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := NewConfig()
		cfg.LastUsedProvider = "test" // predictable default for tests
		if err := cfg.Save(); err != nil {
			return nil, fmt.Errorf("failed to write default config to %q: %w", configDir, err)
		}
	}

	config, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %q: %w", configDir, err)
	}

	apiKeys, err := LoadAPIKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to load API keys from %q: %w", configDir, err)
	}

	return NewManagerWithConfig(config, apiKeys), nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneConfig(m.config)
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
		return fmt.Errorf("save config: %w", err)
	}

	// Update lastSaved
	m.lastSaved = cloneConfig(m.config)
	return nil
}

// UpdateConfig mutates the live config under lock and persists it to disk.
func (m *Manager) UpdateConfig(mutator func(*Config) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return fmt.Errorf("configuration not loaded")
	}
	if mutator != nil {
		if err := mutator(m.config); err != nil {
			return fmt.Errorf("update config mutator: %w", err)
		}
	}
	if err := m.config.Save(); err != nil {
		return fmt.Errorf("update config save: %w", err)
	}
	m.lastSaved = cloneConfig(m.config)
	return nil
}

// UpdateConfigNoSave mutates the live config under lock without persisting it.
func (m *Manager) UpdateConfigNoSave(mutator func(*Config) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.config == nil {
		return fmt.Errorf("configuration not loaded")
	}
	if mutator != nil {
		return mutator(m.config)
	}
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
		return "", fmt.Errorf("failed to select provider: %w", err)
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
		return nil, fmt.Errorf("get model for provider: %w", err)
	}
	currentMap, err := configToMap(current)
	if err != nil {
		return nil, fmt.Errorf("get model for provider: %w", err)
	}
	latestMap, err := configToMap(latest)
	if err != nil {
		return nil, fmt.Errorf("get model for provider: %w", err)
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
		return nil, fmt.Errorf("get system prompt: %w", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("get system prompt: %w", err)
	}
	return out, nil
}

func mapToConfig(m map[string]interface{}) (*Config, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("get system prompt: %w", err)
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("get system prompt: %w", err)
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
