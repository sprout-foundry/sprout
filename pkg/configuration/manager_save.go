package configuration

// Package configuration: config persistence, update, and hot-reload (split from manager.go)

import (
	"fmt"
	"log"
	"path/filepath"
)

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
