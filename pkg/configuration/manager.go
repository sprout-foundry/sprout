package configuration

// Package configuration: Manager struct and constructor functions (split from manager.go)

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"
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
		names := KnownProviderNames()
		// Check for environment variables
		for _, name := range names {
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
			for _, name := range names {
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
// Config and optional API key set. The manager will persist saves to the same
// location that config.Save()/Load() would use for the current env (when
// configDir is empty) or to configDir (when non-empty). Pass nil for apiKeys
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
