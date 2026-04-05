package credentials

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	configDirName   = ".ledit"
	apiKeysFileName = "api_keys.json"
)

type Store map[string]string

type Resolved struct {
	Provider string
	EnvVar   string
	Value    string
	Source   string
}

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

func GetAPIKeysPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, apiKeysFileName), nil
}

func Load() (Store, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return nil, fmt.Errorf("get API keys directory: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read API keys file: %w", err)
	}
	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("failed to parse API keys file: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	return store, nil
}

func Save(store Store) error {
	path, err := GetAPIKeysPath()
	if err != nil {
		return fmt.Errorf("get API keys path: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}
	return os.WriteFile(path, data, 0600)
}

func Resolve(provider, envVar string) (Resolved, error) {
	resolved := Resolved{
		Provider: strings.TrimSpace(provider),
		EnvVar:   strings.TrimSpace(envVar),
	}
	if resolved.EnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(resolved.EnvVar)); value != "" {
			resolved.Value = value
			resolved.Source = "environment"
			return resolved, nil
		}
	}
	store, err := Load()
	if err != nil {
		return Resolved{}, err
	}
	if value := strings.TrimSpace(store[resolved.Provider]); value != "" {
		resolved.Value = value
		resolved.Source = "stored"
	}
	return resolved, nil
}
