package configuration

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// BootstrapIsolatedConfig initializes an isolated config directory by cloning
// the user's main config on first use.
//
// Behavior:
// - Creates configDir if missing.
// - If configDir/config.json already exists, does nothing.
// - Otherwise clones default config from the main config location (if present).
// - Removes command-history fields from the cloned config.
// - Copies api_keys.json from main location when present and not already copied.
func BootstrapIsolatedConfig(configDir string) error {
	targetDir := strings.TrimSpace(configDir)
	if targetDir == "" {
		return fmt.Errorf("isolated config directory is required")
	}
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return fmt.Errorf("failed to create isolated config directory %q: %w", targetDir, err)
	}

	targetConfigPath := filepath.Join(targetDir, ConfigFileName)
	if _, err := os.Stat(targetConfigPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return fmt.Errorf("failed to stat isolated config file %q: %w", targetConfigPath, err)
	}

	defaultDir, err := getDefaultConfigDir()
	if err != nil {
		return err
	}
	sourceConfigPath := filepath.Join(defaultDir, ConfigFileName)
	sourceAPIKeysPath := filepath.Join(defaultDir, APIKeysFileName)
	targetAPIKeysPath := filepath.Join(targetDir, APIKeysFileName)

	if _, err := os.Stat(sourceConfigPath); err == nil {
		data, err := os.ReadFile(sourceConfigPath)
		if err != nil {
			return fmt.Errorf("failed to read source config file %q: %w", sourceConfigPath, err)
		}
		var cfg Config
		if err := json.Unmarshal(data, &cfg); err != nil {
			return fmt.Errorf("failed to parse source config file %q: %w", sourceConfigPath, err)
		}

		// Keep runtime/provider settings, but avoid copying global history.
		cfg.CommandHistory = nil
		cfg.HistoryIndex = 0
		cfg.CommandHistoryByPath = nil
		cfg.HistoryIndexByPath = nil

		out, err := json.MarshalIndent(&cfg, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to serialize isolated config: %w", err)
		}
		if err := os.WriteFile(targetConfigPath, out, 0600); err != nil {
			return fmt.Errorf("failed to write isolated config file %q: %w", targetConfigPath, err)
		}
	}

	if _, err := os.Stat(targetAPIKeysPath); os.IsNotExist(err) {
		if data, err := os.ReadFile(sourceAPIKeysPath); err == nil {
			if err := os.WriteFile(targetAPIKeysPath, data, 0600); err != nil {
				return fmt.Errorf("failed to write isolated api keys file %q: %w", targetAPIKeysPath, err)
			}
		}
	}

	return nil
}
