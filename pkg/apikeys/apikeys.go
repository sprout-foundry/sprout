package apikeys

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/alantheprice/ledit/pkg/config"
)

const apiKeysFile = ".ledit/api_keys.json"

var (
	apiKeys     map[string]string
	apiKeysOnce sync.Once
	apiKeysMu   sync.Mutex
)

// GetAPIKey retrieves the API key for the specified provider.
// It first checks in-memory cache, then the config file, then environment variables,
// and finally prompts the user if not found and interactive mode is enabled.
func GetAPIKey(provider string) (string, error) {
	apiKeysOnce.Do(func() {
		apiKeys = make(map[string]string)
		// Attempt to load from file on first access
		loadedKeys, err := loadAPIKeys()
		if err == nil {
			apiKeysMu.Lock()
			for k, v := range loadedKeys {
				apiKeys[k] = v
			}
			apiKeysMu.Unlock()
		} else {
			fmt.Printf("Warning: Could not load API keys from file: %v\n", err)
		}
	})

	apiKeysMu.Lock()
	key, ok := apiKeys[provider]
	apiKeysMu.Unlock()

	if ok && key != "" {
		return key, nil
	}

	// Check environment variable
	envVar := strings.ToUpper(provider) + "_API_KEY"
	key = os.Getenv(envVar)
	if key != "" {
		apiKeysMu.Lock()
		apiKeys[provider] = key
		apiKeysMu.Unlock()
		saveAPIKeys(apiKeys) // Save to file for future use
		return key, nil
	}

	// If not found, prompt the user
	key = promptForAPIKey(provider)
	if key != "" {
		apiKeysMu.Lock()
		apiKeys[provider] = key
		apiKeysMu.Unlock()
		saveAPIKeys(apiKeys) // Save to file for future use
		return key, nil
	}

	return "", fmt.Errorf("API key for %s not found and not provided", provider)
}

// loadAPIKeys loads the API keys from a file.
func loadAPIKeys() (map[string]string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, fmt.Errorf("could not get user home directory: %w", err)
	}
	filePath := filepath.Join(homeDir, apiKeysFile)

	data, err := os.ReadFile(filePath)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]string), nil // File doesn't exist, return empty map
		}
		return nil, fmt.Errorf("could not read API keys file: %w", err)
	}

	var keys map[string]string
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("could not unmarshal API keys: %w", err)
	}
	return keys, nil
}

// saveAPIKeys saves the API keys to a file.
func saveAPIKeys(keys map[string]string) error {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return fmt.Errorf("could not get user home directory: %w", err)
	}
	dirPath := filepath.Join(homeDir, ".ledit")
	filePath := filepath.Join(dirPath, "api_keys.json")

	if err := os.MkdirAll(dirPath, 0755); err != nil {
		return fmt.Errorf("could not create .ledit directory: %w", err)
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("could not marshal API keys: %w", err)
	}

	if err := os.WriteFile(filePath, data, 0600); err != nil {
		return fmt.Errorf("could not write API keys file: %w", err)
	}
	return nil
}

// promptForAPIKey prompts the user for an API key if not already provided.
func promptForAPIKey(provider string) string {
	cfg, err := config.LoadOrInitConfig(false) // Load config to check interactive mode
	if err != nil || !cfg.Interactive {
		fmt.Printf("API key for %s not found. Please set %s_API_KEY environment variable.\n", provider, strings.ToUpper(provider))
		return ""
	}

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("Enter your %s API Key (or set %s_API_KEY environment variable): ", provider, strings.ToUpper(provider))
	input, _ := reader.ReadString('\n')
	return strings.TrimSpace(input)
}
