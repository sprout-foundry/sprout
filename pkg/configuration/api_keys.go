package configuration

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"strings"
	"syscall"

	"github.com/alantheprice/ledit/pkg/credentials"
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
			Name:            "chutes",
			FormattedName:   "Chutes",
			RequiresKey:     true,
			EnvVariableName: "CHUTES_API_KEY",
		},
		{
			Name:            "openrouter",
			FormattedName:   "OpenRouter (Recommended)",
			RequiresKey:     true,
			EnvVariableName: "OPENROUTER_API_KEY",
		},
		{
			Name:            "zai",
			FormattedName:   "Z.AI Coding Plan",
			RequiresKey:     true,
			EnvVariableName: "ZAI_API_KEY",
		},
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
			Name:            "lmstudio",
			FormattedName:   "LM Studio",
			RequiresKey:     false,
			EnvVariableName: "LMSTUDIO_API_KEY",
		},
		{
			Name:            "mistral",
			FormattedName:   "Mistral",
			RequiresKey:     true,
			EnvVariableName: "MISTRAL_API_KEY",
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
	return credentials.GetAPIKeysPath()
}

// LoadAPIKeys loads API keys from the active backend.
// When keyring is active, it loads from both keyring (tracked providers) and the
// file store (for backward compatibility with keys stored before keyring was enabled).
// When file is active, it uses the existing file-based Load behavior.
//
// Uses GetStorageBackend() (not GetStorageMode()) to ensure consistent resolution
// on first run — the auto-detection logic runs exactly once and persists the mode,
// so subsequent Load/Save calls see the same backend.
func LoadAPIKeys() (*APIKeys, error) {
	backend, err := credentials.GetStorageBackend()
	if err != nil {
		return nil, fmt.Errorf("load API keys: %w", err)
	}

	if _, isKeyring := backend.(*credentials.OSKeyringBackend); isKeyring {
		// Load tracked providers from the keyring
		keyringProviders, err := credentials.ListKeyringProviders()
		if err != nil {
			return nil, fmt.Errorf("load keyring providers: %w", err)
		}

		keys := make(APIKeys)
		keyringSet := make(map[string]bool)

		for _, provider := range keyringProviders {
			value, _, err := credentials.GetFromActiveBackend(provider)
			if err != nil {
				log.Printf("[config] Warning: failed to get key for %q from keyring: %v", provider, err)
				continue
			}
			if value != "" {
				keys[provider] = value
				keyringSet[provider] = true
			}
		}

		// Also load from file store for keys not yet in the keyring (backward compat)
		fileStore, err := credentials.Load()
		if err == nil {
			for provider, value := range fileStore {
				if !keyringSet[provider] && value != "" {
					keys[provider] = value
				}
			}
		}

		return &keys, nil
	}

	// File backend or unset: use existing behavior
	store, err := credentials.Load()
	if err != nil {
		return nil, fmt.Errorf("load API keys: %w", err)
	}
	keys := APIKeys(store)
	return &keys, nil
}

// SaveAPIKeys saves API keys to the active backend.
// When keyring is active, each key is stored via SetToActiveBackend, keys that
// are no longer in the map are deleted from the keyring, and keys that are now
// in the keyring are cleaned from the encrypted file store.
// When file is active, it uses the existing file-based Save behavior.
//
// Uses GetStorageBackend() (not GetStorageMode()) for consistent resolution.
func SaveAPIKeys(keys *APIKeys) error {
	backend, err := credentials.GetStorageBackend()
	if err != nil {
		return fmt.Errorf("save API keys: %w", err)
	}

	if _, isKeyring := backend.(*credentials.OSKeyringBackend); isKeyring {
		// Build set of providers the caller wants to keep
		keepSet := make(map[string]bool)
		if keys != nil {
			for provider, value := range *keys {
				if value != "" {
					if err := credentials.SetToActiveBackend(provider, value); err != nil {
						return fmt.Errorf("save API key for %q: %w", provider, err)
					}
					keepSet[provider] = true
				}
			}
		}

		// Delete providers that were in the keyring but are no longer in the map
		keyringProviders, err := credentials.ListKeyringProviders()
		if err != nil {
			return fmt.Errorf("list keyring providers for cleanup: %w", err)
		}
		for _, p := range keyringProviders {
			if !keepSet[p] {
				if err := credentials.DeleteFromActiveBackend(p); err != nil {
					log.Printf("[config] Warning: failed to delete key for %q from keyring: %v", p, err)
				}
			}
		}

		// Clean file store: remove keys that are now tracked in the keyring
		// Re-read the (possibly updated) provider list after deletions above
		keyringProviders, err = credentials.ListKeyringProviders()
		if err != nil {
			log.Printf("[config] Warning: could not list keyring providers for file cleanup: %v", err)
			return nil
		}

		fileStore, err := credentials.Load()
		if err != nil {
			// Can't clean file store; not fatal since keys are in keyring
			log.Printf("[config] Warning: could not load file store for cleanup: %v", err)
			return nil
		}

		keyringSet := make(map[string]bool, len(keyringProviders))
		for _, p := range keyringProviders {
			keyringSet[p] = true
		}

		cleaned := make(credentials.Store)
		for provider, value := range fileStore {
			if !keyringSet[provider] {
				cleaned[provider] = value
			}
		}
		if len(cleaned) != len(fileStore) {
			if err := credentials.Save(cleaned); err != nil {
				log.Printf("[config] Warning: failed to clean migrated keys from file store: %v", err)
			}
		}

		return nil
	}

	// File backend or unset: use existing behavior
	if keys == nil {
		empty := credentials.Store{}
		return credentials.Save(empty)
	}
	return credentials.Save(credentials.Store(*keys))
}

// PopulateFromEnvironment populates API keys from environment variables
// This is called on startup only to detect whether environment credentials are available.
func (keys *APIKeys) PopulateFromEnvironment() bool {
	for _, provider := range getSupportedProviders() {
		if provider.RequiresKey && provider.EnvVariableName != "" {
			if envKey := strings.TrimSpace(os.Getenv(provider.EnvVariableName)); envKey != "" {
				return true
			}
		}
	}
	return false
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

// HasAPIKey checks if a provider has an API key set.
// Checks the in-memory map first, then falls back to the active backend
// (keyring or file store) for credentials not in the map.
func (keys *APIKeys) HasAPIKey(provider string) bool {
	// First check stored keys
	if keys.GetAPIKey(provider) != "" {
		return true
	}
	// Check active backend (keyring or file store) as fallback
	value, _, err := credentials.GetFromActiveBackend(provider)
	if err == nil && value != "" {
		return true
	}
	return false
}

// PromptForAPIKey prompts the user for an API key with helpful guidance
func PromptForAPIKey(provider string) (string, error) {
	providerName := getProviderDisplayName(provider)

	// Provide specific guidance for getting API keys
	fmt.Printf("[key] Enter your %s API key\n", providerName)
	fmt.Printf("   (The key will be hidden as you type for security)\n")
	fmt.Printf("   API key: ")

	// Read API key securely (hidden input)
	byteKey, err := term.ReadPassword(int(syscall.Stdin))
	if err != nil {
		// Fall back to regular input if term doesn't work
		fmt.Println() // New line after the prompt
		fmt.Printf("   (Hidden input not available, key will be visible)\n")
		fmt.Printf("   API key: ")
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

	// Basic validation
	if len(apiKey) < 10 {
		return "", fmt.Errorf("API key seems too short (expected at least 10 characters, got %d)", len(apiKey))
	}

	// Provider-specific validation patterns
	switch provider {
	case "openai":
		if !strings.HasPrefix(apiKey, "sk-") {
			fmt.Println("[WARN] Warning: OpenAI API keys typically start with 'sk-'")
		}
	case "openrouter":
		if !strings.HasPrefix(apiKey, "sk-or-") {
			fmt.Println("[WARN] Warning: OpenRouter API keys typically start with 'sk-or-'")
		}
	}

	fmt.Printf("[OK] API key accepted (%d characters)\n", len(apiKey))
	return apiKey, nil
}

// getProviderDisplayName returns a user-friendly name for the provider
func getProviderDisplayName(provider string) string {
	for _, p := range getSupportedProviders() {
		if p.Name == provider {
			return p.FormattedName
		}
	}
	if cfg, err := Load(); err == nil {
		if custom, exists := cfg.CustomProviders[provider]; exists {
			if custom.Name != "" {
				return custom.Name
			}
		}
	}
	return provider
}

// RequiresAPIKey checks if a provider requires an API key
func RequiresAPIKey(provider string) bool {
	// First check hardcoded providers
	switch provider {
	case "ollama-local":
		return false
	case "test":
		// Test provider is for CI/testing and doesn't require API key
		return false
	case "lmstudio":
		// LM Studio is a local provider and doesn't require API key
		return false
	case "ollama-turbo":
		// Ollama turbo requires API key for remote acceleration
		return true
	}

	// Check if it's a custom provider
	config, err := Load()
	if err == nil && config.CustomProviders != nil {
		if customProvider, exists := config.CustomProviders[provider]; exists {
			return customProvider.RequiresAPIKey
		}
	}

	// Default to true for unknown providers
	return true
}
