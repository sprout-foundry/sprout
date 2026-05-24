package configuration

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// Manager manages configuration state with safe concurrent access.
type Manager struct {
	mu        sync.RWMutex
	config    *Config
	apiKeys   *APIKeys
	lastSaved *Config // Track last saved state, not initial snapshot
	loaded    bool    // Track if config has been loaded
	configDir string  // Explicit config directory for saves (empty = use env/default)
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

	// Populate from SPROUT_API_KEYS_JSON env var (bulk injection for SaaS/container environments)
	apiKeys.PopulateFromJSONEnv()

	// Populate from individual environment variables — these take priority over JSON blob
	if !apiKeys.PopulateFromEnvironment() {
		log.Printf("[debug] no API keys found in environment variables")
	}

	// Check if we need to set a default provider
	if config.LastUsedProvider == "" {
		// Check for environment variables
		for _, name := range knownProviderNames {
			metadata, err := GetProviderAuthMetadata(name)
			if err != nil {
				continue
			}
			if metadata.RequiresAPIKey && metadata.EnvVar != "" {
				if envKey := os.Getenv(metadata.EnvVar); envKey != "" {
					config.LastUsedProvider = name
					break
				}
			}
		}

		// If no env provider, check for saved API keys (cloud providers only)
		if config.LastUsedProvider == "" {
			for _, name := range knownProviderNames {
				if RequiresAPIKey(name) && HasProviderAuth(name) {
					config.LastUsedProvider = name
					break
				}
			}
		}

		// If still no provider found, leave LastUsedProvider empty.
		// Do NOT default to "test" — that's only for testing and should never
		// be persisted to the user's real config file. Callers (agent startup,
		// provider resolution) will handle the empty case by falling through to
		// auto-detection or provider selection.
		if config.LastUsedProvider == "" {
			// Only save if we resolved a provider above (env vars or saved keys).
			// No need to save when nothing changed.
			return config, apiKeys, nil
		}

		// A default was picked from env vars or saved keys — persist it.
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
// location that config.Save()/Load() would use for the current env (when
// configDir is empty) or to configDir (when non-empty).  Pass nil for apiKeys
// to skip key loading.
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
// environment without touching the caller's real ~/.config/sprout.
func NewManagerWithDir(configDir string) (*Manager, error) {
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return nil, fmt.Errorf("failed to create config directory %q: %w", configDir, err)
	}

	// Ensure a config file exists so we can load from it.
	//
	// Note: do NOT preload LastUsedProvider with "test" here, even
	// though this is a test-oriented helper. Older versions did, and
	// the literal string used to leak into the user's real config
	// whenever a misbehaving test bypassed isolation. The sentinel is
	// for in-memory test fixtures only; tests that need a specific
	// provider should set it explicitly on the returned Manager via
	// the type-safe SetProvider API (which rejects api.TestClientType).
	configPath := filepath.Join(configDir, ConfigFileName)
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		cfg := NewConfig()
		if err := cfg.SaveToDir(configDir); err != nil {
			return nil, fmt.Errorf("failed to write default config to %q: %w", configDir, err)
		}
	}

	// Load config from explicit directory without mutating env vars
	config, err := LoadConfigWithLayers(configPath, "", "", configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load config from %q: %w", configDir, err)
	}

	// Load API keys from explicit directory without mutating env vars
	apiKeys, err := LoadAPIKeysFromDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to load API keys from %q: %w", configDir, err)
	}

	mgr := NewManagerWithConfig(config, apiKeys)
	mgr.configDir = configDir // Store explicit dir for saves
	return mgr, nil
}

// NewManagerWithLayers creates a configuration manager using layered config.
// globalDir is the directory containing global config (~/.config/sprout/).
// workspaceDir is the directory containing workspace config ({workspace}/.sprout/).
// Each layer is optional - missing layers are skipped.
// Settings writes go to the workspace dir if provided, otherwise global.
func NewManagerWithLayers(globalDir, workspaceDir string) (*Manager, error) {
	// Determine which directory should receive save writes
	saveDir := globalDir
	if workspaceDir != "" {
		saveDir = workspaceDir
	}
	if saveDir != "" {
		if err := os.MkdirAll(saveDir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create config directory %q: %w", saveDir, err)
		}
	}

	// Compute file paths
	var globalPath, workspacePath string
	if globalDir != "" {
		globalPath = filepath.Join(globalDir, ConfigFileName)
	}
	if workspaceDir != "" {
		workspacePath = filepath.Join(workspaceDir, ConfigFileName)
	}

	// Load merged config (global + workspace, no session layer)
	config, err := LoadConfigWithLayers(globalPath, workspacePath, "", globalDir)
	if err != nil {
		return nil, fmt.Errorf("load layered config: %w", err)
	}

	// Load API keys (always from global location, without mutating env vars)
	var apiKeys *APIKeys
	if globalDir != "" {
		apiKeys, err = LoadAPIKeysFromDir(globalDir)
	} else {
		apiKeys, err = LoadAPIKeys()
	}
	if err != nil {
		return nil, fmt.Errorf("load API keys: %w", err)
	}

	// Populate from environment (always do this for any manager)
	apiKeys.PopulateFromJSONEnv()
	if !apiKeys.PopulateFromEnvironment() {
		log.Printf("[debug] no API keys found in environment variables")
	}

	return &Manager{
		config:    config,
		apiKeys:   apiKeys,
		lastSaved: cloneConfig(config),
		loaded:    true,
		configDir: saveDir, // Store explicit dir for saves
	}, nil
}

// LoadConfigWithLayers loads configuration from three layers:
// globalPath -> workspacePath -> sessionPath (each overrides previous)
// Each layer is optional; missing layers are skipped.
// globalDir is the directory for global providers (used when globalPath is empty but custom providers need loading).
func LoadConfigWithLayers(globalPath, workspacePath, sessionPath, globalDir string) (*Config, error) {
	var result *Config

	// 1. Load global config (base)
	if globalPath != "" {
		if data, err := os.ReadFile(globalPath); err == nil {
			var cfg Config
			if err := json.Unmarshal(data, &cfg); err != nil {
				log.Printf("[config] warning: failed to parse global config %s: %v", globalPath, err)
			} else {
				result = &cfg
			}
		}
	}

	if result == nil {
		result = NewConfig()
	}

	// 2. Merge workspace config if exists
	if workspacePath != "" {
		if data, err := os.ReadFile(workspacePath); err == nil {
			var workspaceCfg Config
			if err := json.Unmarshal(data, &workspaceCfg); err != nil {
				log.Printf("[config] warning: failed to parse workspace config %s: %v", workspacePath, err)
			} else {
				result = MergeConfig(result, &workspaceCfg)
			}
		}
	}

	// 3. Merge session config if exists (highest priority)
	if sessionPath != "" {
		if data, err := os.ReadFile(sessionPath); err == nil {
			var sessionCfg Config
			if err := json.Unmarshal(data, &sessionCfg); err != nil {
				log.Printf("[config] warning: failed to parse session config %s: %v", sessionPath, err)
			} else {
				result = MergeConfig(result, &sessionCfg)
			}
		}
	}

	// 4. Load custom providers from individual files.
	// Custom providers are never persisted to config.json (CustomProviders is set
	// to nil before saving), so they must be loaded from the provider directory.
	// Always load from the global config directory, not from the (possibly
	// overridden) SPROUT_CONFIG env var — custom providers are a global resource.
	if result.CustomProviders == nil {
		result.CustomProviders = make(map[string]CustomProviderConfig)
	}
	var globalProvidersDir string
	if globalPath != "" {
		globalProvidersDir = filepath.Join(filepath.Dir(globalPath), ProvidersDirName)
	} else if globalDir != "" {
		// Use explicit globalDir if available (for hermetic environments)
		globalProvidersDir = filepath.Join(globalDir, ProvidersDirName)
	}
	if globalProvidersDir != "" {
		fileProviders, err := LoadCustomProvidersFromDir(globalProvidersDir)
		if err != nil {
			log.Printf("[config] warning: failed to load custom provider files from %s: %v", globalProvidersDir, err)
		} else {
			for name, provider := range fileProviders {
				result.CustomProviders[name] = provider
			}
		}
	}

	// Same self-heal as Load() — a stale "test" sentinel on disk
	// shouldn't drive the real CLI.
	sanitizeTestProvider(result)

	return result, nil
}

// GetConfig returns the current configuration
func (m *Manager) GetConfig() *Config {
	m.mu.Lock()
	defer m.mu.Unlock()
	return cloneConfig(m.config)
}

// GetAPIKeys returns the current API keys
func (m *Manager) GetAPIKeys() *APIKeys {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneAPIKeys(m.apiKeys)
}

// GetConfigDir returns the stored config directory for this manager.
// Returns empty string if the manager uses the default (env-based) location.
func (m *Manager) GetConfigDir() string {
	return m.configDir
}

// saveConfigLocked persists the in-memory config to disk.
// If m.configDir is set, it uses SaveToDir (bypassing env vars).
// Otherwise it falls back to Config.Save() (which reads env vars).
//
// On ConfigConflictError (another process modified the file since we loaded),
// it reloads the on-disk config, merges pending in-memory changes on top,
// and retries once. Caller must hold m.mu.
func (m *Manager) saveConfigLocked() error {
	err := m.saveConfigDirectLocked()
	if err == nil || !IsConfigConflict(err) {
		return err
	}

	// Conflict detected: reload from disk and merge our pending changes.
	log.Printf("[config] conflict detected, reloading from disk and retrying: %v", err)
	if mergeErr := m.reloadAndMergeLocked(); mergeErr != nil {
		return fmt.Errorf("config conflict, reload-merge failed: %w (original: %v)", mergeErr, err)
	}

	// Retry save with the merged config.
	return m.saveConfigDirectLocked()
}

// saveConfigDirectLocked performs the actual config write without retry logic.
func (m *Manager) saveConfigDirectLocked() error {
	if m.configDir != "" {
		if err := m.config.SaveToDir(m.configDir); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	} else {
		if err := m.config.Save(); err != nil {
			return fmt.Errorf("save config: %w", err)
		}
	}
	return nil
}

// reloadAndMergeLocked reloads the config from disk, then overlays the
// in-memory changes that haven't been persisted yet (diff of m.config vs
// m.lastSaved). Caller must hold m.mu.
func (m *Manager) reloadAndMergeLocked() error {
	// Capture pending changes (what m.config has that m.lastSaved doesn't).
	pending := m.pendingChangesLocked()

	// Reload from disk.
	var reloaded *Config
	var err error
	if m.configDir != "" {
		configPath := filepath.Join(m.configDir, ConfigFileName)
		reloaded, err = LoadConfigWithLayers(configPath, "", "", m.configDir)
	} else {
		reloaded, err = Load()
	}
	if err != nil {
		return fmt.Errorf("reload: %w", err)
	}

	// Apply pending changes on top of the reloaded config.
	pending.applyTo(reloaded)

	// Swap in the merged config.
	m.config = reloaded
	return nil
}

// pendingConfigChanges captures the full in-memory config state that
// differs from the last-saved snapshot, so it can be re-applied after
// a reload from disk.
type pendingConfigChanges struct {
	// The entire in-memory config at the time of the conflict. We'll
	// do a JSON-level merge with the reloaded config to preserve
	// external changes while re-applying our pending mutations.
	current *Config
	last    *Config
}

// pendingChangesLocked captures the current and last-saved config state.
// Caller must hold m.mu.
func (m *Manager) pendingChangesLocked() pendingConfigChanges {
	return pendingConfigChanges{
		current: m.config,
		last:    m.lastSaved,
	}
}

// applyTo overlays the pending changes onto a target config using a
// JSON-level merge. Fields that changed between lastSaved and current
// are applied to target; fields that didn't change are left at the
// target's (reloaded) value.
func (ch *pendingConfigChanges) applyTo(target *Config) {
	if ch.current == nil || ch.last == nil {
		return
	}
	merged, err := mergeConfigChanges(ch.last, ch.current, target)
	if err != nil {
		log.Printf("[config] merge failed during conflict retry: %v", err)
		return
	}
	// Preserve loadedModTime/loadedSize from target (which was freshly loaded).
	merged.loadedModTime = target.loadedModTime
	merged.loadedSize = target.loadedSize
	*target = *merged
}

// SaveConfig saves the configuration to disk
func (m *Manager) SaveConfig() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Save the current manager config directly
	if err := m.saveConfigLocked(); err != nil {
		return err
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
	if err := m.saveConfigLocked(); err != nil {
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

// SaveAPIKeys saves the API keys to disk.
//
// Deprecated: This performs a blind write with no validation.
// Use ValidateAndSaveAPIKey instead, which validates the key before saving
// and preserves the old key on validation failure.
// This method is retained for backward compatibility only.
func (m *Manager) SaveAPIKeys() error {
	m.mu.RLock()
	keys := m.apiKeys
	configDir := m.configDir
	m.mu.RUnlock()
	if configDir != "" {
		return SaveAPIKeysToDir(keys, configDir)
	}
	return SaveAPIKeys(keys)
}

// RefreshAPIKeys reloads API keys from the backend into the in-memory cache.
// This must be called after any external mutation of the credential backend
// (e.g., ValidateAndSaveAPIKey) to keep the Manager's in-memory map in sync.
func (m *Manager) RefreshAPIKeys() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var keys *APIKeys
	var err error
	if m.configDir != "" {
		keys, err = LoadAPIKeysFromDir(m.configDir)
		if err != nil {
			return fmt.Errorf("refresh API keys: %w", err)
		}
	} else {
		keys, err = LoadAPIKeys()
		if err != nil {
			return fmt.Errorf("refresh API keys: %w", err)
		}
	}
	m.apiKeys = keys
	return nil
}

// Reload re-reads the on-disk configuration and API keys into the in-memory
// cache.  It is intended to be called from a SIGHUP handler so that config
// changes made externally (e.g., editing config.yaml) take effect without
// restarting the daemon.  Running agents and tools are NOT affected — only
// subsequent queries will see the new configuration.
func (m *Manager) Reload() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var cfg *Config
	var err error
	if m.configDir != "" {
		configPath := filepath.Join(m.configDir, ConfigFileName)
		cfg, err = LoadConfigWithLayers(configPath, "", "", m.configDir)
	} else {
		cfg, err = Load()
	}
	if err != nil {
		return fmt.Errorf("reload config: %w", err)
	}
	m.config = cfg

	var keys *APIKeys
	if m.configDir != "" {
		keys, err = LoadAPIKeysFromDir(m.configDir)
	} else {
		keys, err = LoadAPIKeys()
	}
	if err != nil {
		return fmt.Errorf("reload API keys: %w", err)
	}
	keys.PopulateFromJSONEnv()
	keys.PopulateFromEnvironment()
	m.apiKeys = keys

	return nil
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
