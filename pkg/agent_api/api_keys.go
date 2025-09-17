package api

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

// APIKeys represents the API keys configuration
type APIKeys struct {
	OpenAI     string `json:"openai,omitempty"`
	DeepInfra  string `json:"deepinfra,omitempty"`
	OpenRouter string `json:"openrouter,omitempty"`

	DeepSeek string `json:"deepseek,omitempty"`
	Gemini   string `json:"gemini,omitempty"`
}

const (
	ConfigDirName   = ".ledit"
	APIKeysFileName = "api_keys.json"
)

// GetAPIKeysPath returns the full path to the API keys file
func GetAPIKeysPath() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get home directory: %w", err)
	}

	configDir := filepath.Join(homeDir, ConfigDirName)
	if err := os.MkdirAll(configDir, 0700); err != nil {
		return "", fmt.Errorf("failed to create config directory: %w", err)
	}

	return filepath.Join(configDir, APIKeysFileName), nil
}

// LoadAPIKeys loads API keys from the file and sets environment variables
func LoadAPIKeys() (*APIKeys, error) {
	apiKeysPath, err := GetAPIKeysPath()
	if err != nil {
		return nil, err
	}

	// If API keys file doesn't exist, return empty keys
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

	// Set environment variables from loaded keys
	setEnvVarsFromAPIKeys(&keys)

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

// setEnvVarsFromAPIKeys sets environment variables from API keys
func setEnvVarsFromAPIKeys(keys *APIKeys) {
	if keys.OpenAI != "" {
		os.Setenv("OPENAI_API_KEY", keys.OpenAI)
	}
	if keys.DeepInfra != "" {
		os.Setenv("DEEPINFRA_API_KEY", keys.DeepInfra)
	}
	if keys.OpenRouter != "" {
		os.Setenv("OPENROUTER_API_KEY", keys.OpenRouter)
	}

	if keys.DeepSeek != "" {
		os.Setenv("DEEPSEEK_API_KEY", keys.DeepSeek)
	}
	if keys.Gemini != "" {
		os.Setenv("GOOGLE_API_KEY", keys.Gemini)
	}
}

// GetProviderAPIKeyName returns the environment variable name for a provider
func GetProviderAPIKeyName(provider ClientType) string {
	switch provider {
	case OpenAIClientType:
		return "OPENAI_API_KEY"
	case DeepInfraClientType:
		return "DEEPINFRA_API_KEY"
	case OpenRouterClientType:
		return "OPENROUTER_API_KEY"

	case DeepSeekClientType:
		return "DEEPSEEK_API_KEY"
	default:
		return ""
	}
}

// PromptForAPIKey prompts the user for an API key and saves it
func PromptForAPIKey(provider ClientType) (string, error) {
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

	// Load existing keys
	keys, err := LoadAPIKeys()
	if err != nil {
		keys = &APIKeys{} // Create new if loading fails
	}

	// Set the new key
	setAPIKeyInStruct(keys, provider, apiKey)

	// Save keys
	if err := SaveAPIKeys(keys); err != nil {
		return "", fmt.Errorf("failed to save API key: %w", err)
	}

	// Set environment variable
	envVar := GetProviderAPIKeyName(provider)
	if envVar != "" {
		os.Setenv(envVar, apiKey)
	}

	fmt.Printf("✅ API key saved for %s\n", providerName)
	return apiKey, nil
}

// setAPIKeyInStruct sets the API key in the appropriate field
func setAPIKeyInStruct(keys *APIKeys, provider ClientType, apiKey string) {
	switch provider {
	case OpenAIClientType:
		keys.OpenAI = apiKey
	case DeepInfraClientType:
		keys.DeepInfra = apiKey
	case OpenRouterClientType:
		keys.OpenRouter = apiKey

	case DeepSeekClientType:
		keys.DeepSeek = apiKey
	}
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider ClientType) string {
	switch provider {
	case OpenAIClientType:
		return "OpenAI"
	case DeepInfraClientType:
		return "DeepInfra"
	case OpenRouterClientType:
		return "OpenRouter"

	case DeepSeekClientType:
		return "DeepSeek"
	default:
		return string(provider)
	}
}

// EnsureAPIKeyAvailable ensures an API key is available for a provider, prompting if needed
func EnsureAPIKeyAvailable(provider ClientType) error {
	envVar := GetProviderAPIKeyName(provider)
	if envVar == "" {
		return nil // No API key needed (e.g., Ollama)
	}

	// Always try to load from file first to get the latest keys
	_, err := LoadAPIKeys()
	if err == nil && os.Getenv(envVar) != "" {
		return nil // Successfully loaded from file
	}

	// If still no key available, prompt for it
	_, err = PromptForAPIKey(provider)
	return err
}

// InitializeAPIKeys loads API keys and ensures they're available in environment
func InitializeAPIKeys() error {
	// Load API keys from file if available
	_, err := LoadAPIKeys()
	if err != nil {
		// If we can't load, that's okay - we'll prompt when needed
		return nil
	}
	return nil
}
