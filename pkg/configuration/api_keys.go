package configuration

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// GetAPIKeysPath returns the full path to the API keys file
func GetAPIKeysPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, APIKeysFileName), nil
}

// LoadAPIKeys loads API keys from the file
func LoadAPIKeys() (*APIKeys, error) {
	apiKeysPath, err := GetAPIKeysPath()
	if err != nil {
		return nil, err
	}

	// If API keys file doesn't exist, create empty
	if _, err := os.Stat(apiKeysPath); os.IsNotExist(err) {
		return &APIKeys{}, nil
	}

	data, err := os.ReadFile(apiKeysPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read API keys file: %w", err)
	}

	var keys APIKeys
	if err := json.Unmarshal(data, &keys); err != nil {
		return nil, fmt.Errorf("failed to parse API keys file: %w", err)
	}

	return &keys, nil
}

// SaveAPIKeys saves API keys to file
func SaveAPIKeys(keys *APIKeys) error {
	apiKeysPath, err := GetAPIKeysPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(keys, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}

	return os.WriteFile(apiKeysPath, data, 0600)
}

// PopulateFromEnvironment populates API keys from environment variables
// This is called on startup to capture any keys set via environment
func (keys *APIKeys) PopulateFromEnvironment() bool {
	updated := false

	if envKey := os.Getenv("OPENAI_API_KEY"); envKey != "" && keys.OpenAI == "" {
		keys.OpenAI = envKey
		updated = true
	}

	if envKey := os.Getenv("DEEPINFRA_API_KEY"); envKey != "" && keys.DeepInfra == "" {
		keys.DeepInfra = envKey
		updated = true
	}

	if envKey := os.Getenv("OPENROUTER_API_KEY"); envKey != "" && keys.OpenRouter == "" {
		keys.OpenRouter = envKey
		updated = true
	}

	return updated
}

// GetAPIKey returns the API key for a provider
func (keys *APIKeys) GetAPIKey(provider string) string {
	switch provider {
	case "openai":
		return keys.OpenAI
	case "deepinfra":
		return keys.DeepInfra
	case "openrouter":
		return keys.OpenRouter
	default:
		return ""
	}
}

// SetAPIKey sets the API key for a provider
func (keys *APIKeys) SetAPIKey(provider, key string) {
	switch provider {
	case "openai":
		keys.OpenAI = key
	case "deepinfra":
		keys.DeepInfra = key
	case "openrouter":
		keys.OpenRouter = key
	}
}

// HasAPIKey checks if a provider has an API key set
func (keys *APIKeys) HasAPIKey(provider string) bool {
	return keys.GetAPIKey(provider) != ""
}

// PromptForAPIKey prompts the user for an API key
func PromptForAPIKey(provider string) (string, error) {
	providerName := getProviderDisplayName(provider)
	fmt.Printf("🔑 API key required for %s\n", providerName)
	fmt.Printf("Please enter your %s API key: ", providerName)

	// Read API key securely (hidden input)
	byteKey, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		// Fall back to regular input if term doesn't work
		fmt.Println() // New line after the prompt
		reader := bufio.NewReader(os.Stdin)
		key, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read API key: %w", err)
		}
		byteKey = []byte(strings.TrimSpace(key))
	} else {
		fmt.Println() // New line after hidden input
	}

	apiKey := strings.TrimSpace(string(byteKey))
	if apiKey == "" {
		return "", fmt.Errorf("no API key provided")
	}

	return apiKey, nil
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider string) string {
	switch provider {
	case "openai":
		return "OpenAI"
	case "deepinfra":
		return "DeepInfra"
	case "openrouter":
		return "OpenRouter"
	case "ollama":
		return "Ollama"
	case "ollama-local":
		return "Ollama (local)"
	case "ollama-turbo":
		return "Ollama (turbo)"
	default:
		return provider
	}
}

// RequiresAPIKey checks if a provider requires an API key
func RequiresAPIKey(provider string) bool {
	switch provider {
	case "ollama-local":
		return false
	case "ollama-turbo":
		// Ollama turbo requires API key for remote acceleration
		return true
	case "ollama":
		// Regular ollama maps to local, doesn't need API key
		return false
	default:
		return true
	}
}
