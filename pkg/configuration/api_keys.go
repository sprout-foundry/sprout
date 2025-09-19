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

type ProviderAPIKey struct {
	Name            string `json:"name,omitempty"`
	FormattedName   string `json:"formatted_name,omitempty"`
	RequiresKey     bool   `json:"requires_key,omitempty"`
	Key             string `json:"key,omitempty"`
	EnvVariableName string `json:"env_variable_name,omitempty"`
}

func getSupportedProviders() []ProviderAPIKey {
	return []ProviderAPIKey{
		{
			Name:            "openai",
			FormattedName:   "OpenAI",
			RequiresKey:     true,
			EnvVariableName: "OPENAI_API_KEY",
		},
		{
			Name:            "deepinfra",
			FormattedName:   "DeepInfra",
			RequiresKey:     true,
			EnvVariableName: "DEEPINFRA_API_KEY",
		},
		{
			Name:            "openrouter",
			FormattedName:   "OpenRouter",
			RequiresKey:     true,
			EnvVariableName: "OPENROUTER_API_KEY",
		},
		{
			Name:          "ollama",
			FormattedName: "Ollama (local)",
			RequiresKey:   false,
		},
		{
			Name:            "ollama-turbo",
			FormattedName:   "Ollama (turbo)",
			RequiresKey:     true,
			EnvVariableName: "OLLAMA_API_KEY",
		},
		{
			Name:            "jinaai",
			FormattedName:   "JinaAI",
			RequiresKey:     true,
			EnvVariableName: "JINA_API_KEY",
		},
	}
}

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
	for _, provider := range getSupportedProviders() {
		if provider.RequiresKey && provider.EnvVariableName != "" {
			if envKey := os.Getenv(provider.EnvVariableName); envKey != "" && keys.GetAPIKey(provider.Name) == "" {
				keys.SetAPIKey(provider.Name, envKey)
				updated = true
			}
		}
	}
	if updated {
		// Save updated keys to file
		if err := SaveAPIKeys(keys); err != nil {
			fmt.Printf("Warning: failed to save API keys after populating from environment: %v\n", err)
		}
	}
	return updated
}

// GetAPIKey returns the API key for a provider
func (keys *APIKeys) GetAPIKey(provider string) string {
	if keys == nil {
		return ""
	}
	return (*keys)[provider]
}

// SetAPIKey sets the API key for a provider
func (keys *APIKeys) SetAPIKey(provider, key string) {
	if keys == nil || *keys == nil {
		*keys = make(APIKeys)
	}
	(*keys)[provider] = key
}

// HasAPIKey checks if a provider has an API key set
func (keys *APIKeys) HasAPIKey(provider string) bool {
	// First check stored keys
	if keys.GetAPIKey(provider) != "" {
		return true
	}
	return false
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
	for _, p := range getSupportedProviders() {
		if p.Name == provider {
			return p.FormattedName
		}
	}
	return provider
}

// RequiresAPIKey checks if a provider requires an API key
func RequiresAPIKey(provider string) bool {
	switch provider {
	case "ollama-local":
		return false
	case "ollama-turbo":
		// Ollama turbo requires API key for remote acceleration
		return true
	default:
		return true
	}
}
