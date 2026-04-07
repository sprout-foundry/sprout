package credentials

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDirName      = ".ledit"
	apiKeysFileName    = "api_keys.json"
	machineKeyFileName = "key.age"
	encryptedMagic     = "age-encryption.org/v1"
)

// Store holds the encrypted API key store.
type Store map[string]string

// Resolved contains a resolved credential with source information.
type Resolved struct {
	Provider string
	EnvVar   string
	Value    string
	Source   string
}

// GetConfigDir returns the configuration directory path, creating it if it doesn't exist.
func GetConfigDir() (string, error) {
	configDir := strings.TrimSpace(os.Getenv("LEDIT_CONFIG"))
	if configDir == "" {
		xdgConfigHome := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME"))
		if xdgConfigHome != "" {
			configDir = filepath.Join(xdgConfigHome, "ledit")
		} else {
			homeDir, err := os.UserHomeDir()
			if err != nil {
				return "", fmt.Errorf("failed to get home directory: %w", err)
			}
			configDir = filepath.Join(homeDir, configDirName)
		}
	}
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}
	return configDir, nil
}

// GetAPIKeysPath returns the path to the API keys file.
func GetAPIKeysPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, apiKeysFileName), nil
}

// GetMachineKeyPath returns the path to the machine key file.
func GetMachineKeyPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, machineKeyFileName), nil
}
