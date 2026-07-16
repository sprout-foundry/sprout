package configuration

// Package configuration: config loading, reload, and save-locking (split from manager.go)

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
)

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

	// Personas are catalog-fixed and never read from disk; hydrate from the
	// embedded catalog so every layered-load path matches the regular Load().
	result.SubagentTypes = defaultSubagentTypes()

	// Merge missing default (built-in) skills so that a hot-reload via
	// Manager.Reload() picks up new builtins added to the embedded library
	// without requiring a process restart. Mirrors what Load() does.
	if result.Skills == nil {
		result.Skills = make(map[string]Skill)
	}
	mergeMissingDefaultSkills(result)

	// Discover user-level and project-specific skills so that hot-reload
	// paths (Manager.Reload -> LoadConfigWithLayers) pick up new SKILL.md
	// files without requiring a process restart.
	if discovered := result.discoverSkills(); len(discovered) > 0 {
		log.Printf("[skills] Discovered %d skill(s): %s",
			len(discovered), strings.Join(discovered, ", "))
	}

	// Migrate legacy approved_shell_commands to unified command_policies
	MigrateCommandPolicies(result)

	return result, nil
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

	// Config changed on disk (likely another process); reload and merge our pending changes.
	log.Printf("[config] merged external config change: %v", err)
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
