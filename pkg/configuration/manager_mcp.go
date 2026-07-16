package configuration

// Package configuration: MCP config, helper utilities, and type-mappers (split from manager.go)

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// GetMCPConfig returns the MCP configuration
func (m *Manager) GetMCPConfig() mcp.MCPConfig {
	return m.config.MCP
}

// EnrichCustomProviders loads custom provider files from the global providers
// directory into the config. This is needed before provider name lookups
// because config.json never stores custom providers directly.
func (m *Manager) EnrichCustomProviders() {
	if m.config.CustomProviders == nil {
		m.config.CustomProviders = make(map[string]CustomProviderConfig)
	}
	// Use manager's explicit configDir if set, otherwise fall back to env-based resolution.
	configDir := m.configDir
	if configDir == "" {
		var err error
		configDir, err = GetConfigDir()
		if err != nil {
			return
		}
	}
	providersDir := filepath.Join(configDir, ProvidersDirName)
	fileProviders, err := LoadCustomProvidersFromDir(providersDir)
	if err != nil {
		return
	}
	for name, provider := range fileProviders {
		m.config.CustomProviders[name] = provider
	}
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

func cloneAPIKeys(keys *APIKeys) *APIKeys {
	if keys == nil {
		return nil
	}
	clone := make(APIKeys, len(*keys))
	for k, v := range *keys {
		clone[k] = v
	}
	return &clone
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
		return nil, fmt.Errorf("convert base config to map: %w", err)
	}
	currentMap, err := configToMap(current)
	if err != nil {
		return nil, fmt.Errorf("convert current config to map: %w", err)
	}
	latestMap, err := configToMap(latest)
	if err != nil {
		return nil, fmt.Errorf("convert latest config to map: %w", err)
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
		return nil, fmt.Errorf("marshal config to JSON: %w", err)
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal JSON to config map: %w", err)
	}
	return out, nil
}

func mapToConfig(m map[string]interface{}) (*Config, error) {
	data, err := json.Marshal(m)
	if err != nil {
		return nil, fmt.Errorf("marshal config map to JSON: %w", err)
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("unmarshal JSON to config: %w", err)
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
	case api.OllamaCloudClientType:
		return "ollama-cloud"
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
