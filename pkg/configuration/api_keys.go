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

// knownProviderNames is the canonical list of built-in provider names.
// This is the single source of truth for provider ordering in CLI/UI.
var knownProviderNames = []string{
	"chutes",
	"openrouter",
	"zai",
	"openai",
	"deepinfra",
	"deepseek",
	"minimax",
	"ollama",
	"ollama-local",
	"ollama-turbo",
	"lmstudio",
	"mistral",
	"jinaai",
}

// knownProviderDisplayNames maps provider names to their display names.
// This is the single source of truth for provider display names in CLI/UI.
// getProviderDisplayName() consults this map to avoid a circular dependency
// with GetProviderAuthMetadata() (which calls getProviderDisplayName for display names).
var knownProviderDisplayNames = map[string]string{
	"chutes":       "Chutes",
	"openrouter":   "OpenRouter (Recommended)",
	"zai":          "Z.AI Coding Plan",
	"openai":       "OpenAI",
	"deepinfra":    "DeepInfra",
	"deepseek":     "DeepSeek",
	"minimax":      "MiniMax",
	"ollama":       "Ollama (local)",
	"ollama-local": "Ollama (Local)",
	"ollama-turbo": "Ollama (turbo)",
	"lmstudio":     "LM Studio",
	"mistral":      "Mistral",
	"jinaai":       "JinaAI",
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
	for _, name := range knownProviderNames {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}
		if metadata.RequiresAPIKey && metadata.EnvVar != "" {
			if envKey := strings.TrimSpace(os.Getenv(metadata.EnvVar)); envKey != "" {
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

// getProviderDisplayName returns a user-friendly name for the provider.
// For known built-in providers, uses the static display name map.
// For custom providers, falls back to the custom provider config.
func getProviderDisplayName(provider string) string {
	// Check static display names for built-in providers first
	if displayName, ok := knownProviderDisplayNames[provider]; ok {
		return displayName
	}

	// Fall back to custom providers
	if cfg, err := Load(); err == nil {
		if custom, exists := cfg.CustomProviders[provider]; exists {
			if custom.Name != "" {
				return custom.Name
			}
		}
	}

	return provider
}

// RequiresAPIKey checks if a provider requires an API key.
func RequiresAPIKey(provider string) bool {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return true // default to requiring key for unknown providers
	}
	return metadata.RequiresAPIKey
}
