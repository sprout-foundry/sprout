package configuration

// api_keys_store.go — the persistence + validation layer for the
// provider API-key store: load/save (default path and per-dir variants),
// environment and JSON-env population, LLM-validated save-and-validate, and
// error-message sanitization. Split out of api_keys.go.
import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/credentials"
)

// keyValidationMutex protects ValidateAndSaveAPIKey from concurrent access.
var keyValidationMutex sync.Mutex

// validateAndSaveSkipValidation, when true, skips the network-based
// ListModels validation in ValidateAndSaveAPIKey and stores the key directly.
// Intended for unit tests only.
var validateAndSaveSkipValidation bool

// SetValidateAndSaveAPIKeyValidation enables or disables the test-mode skip.
// Call with true in tests that need to store keys without network validation.
func SetValidateAndSaveAPIKeyValidation(skip bool) {
	validateAndSaveSkipValidation = skip
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

// LoadAPIKeysFromDir loads API keys from a specific config directory.
// This is like LoadAPIKeys() but takes an explicit config directory instead
// of reading from environment variables. It's useful for test environments and
// other scenarios where you want to load from a specific location without
// mutating process state.
//
// When keyring is active, it loads from both keyring (tracked providers) and the
// file store at the specified configDir (for backward compatibility with keys
// stored before keyring was enabled). When file is active, it uses LoadFromDir.
func LoadAPIKeysFromDir(configDir string) (*APIKeys, error) {
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
		fileStore, err := credentials.LoadFromDir(configDir)
		if err == nil {
			for provider, value := range fileStore {
				if !keyringSet[provider] && value != "" {
					keys[provider] = value
				}
			}
		}

		return &keys, nil
	}

	// File backend or unset: use LoadFromDir
	store, err := credentials.LoadFromDir(configDir)
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

		keyringSet := make(map[string]bool, len(keyringProviders))
		for _, p := range keyringProviders {
			keyringSet[p] = true
		}

		// Use AtomicModify to atomically read the file store, remove keys
		// that are now in the keyring, and save — preventing TOCTOU races.
		if err := credentials.AtomicModify(func(store credentials.Store) error {
			for provider := range store {
				if keyringSet[provider] {
					delete(store, provider)
				}
			}
			return nil
		}); err != nil {
			log.Printf("[config] Warning: failed to clean migrated keys from file store: %v", err)
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

// SaveAPIKeysToDir saves API keys to a specific config directory.
// This is like SaveAPIKeys() but routes file-backend saves through
// credentials.SaveToDir() instead of credentials.Save().
func SaveAPIKeysToDir(keys *APIKeys, configDir string) error {
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

		keyringSet := make(map[string]bool, len(keyringProviders))
		for _, p := range keyringProviders {
			keyringSet[p] = true
		}

		// Use AtomicModifyForDir to atomically read the file store from the
		// specific configDir, remove keys that are now in the keyring, and save.
		if err := credentials.AtomicModifyForDir(configDir, func(store credentials.Store) error {
			for provider := range store {
				if keyringSet[provider] {
					delete(store, provider)
				}
			}
			return nil
		}); err != nil {
			log.Printf("[config] Warning: failed to clean migrated keys from file store: %v", err)
		}

		return nil
	}

	// File backend or unset: use dir-aware save
	if keys == nil {
		empty := credentials.Store{}
		return credentials.SaveToDir(empty, configDir)
	}
	return credentials.SaveToDir(credentials.Store(*keys), configDir)
}

// PopulateFromEnvironment populates API keys from environment variables
// This is called on startup only to detect whether environment credentials are available.
func (keys *APIKeys) PopulateFromEnvironment() bool {
	populated := false
	for _, name := range KnownProviderNames() {
		metadata, err := GetProviderAuthMetadata(name)
		if err != nil {
			continue
		}
		if metadata.RequiresAPIKey && metadata.EnvVar != "" {
			if envKey := strings.TrimSpace(os.Getenv(metadata.EnvVar)); envKey != "" {
				// Actually populate the key into the map
				keys.SetAPIKey(name, envKey)
				populated = true
			}
		}
	}
	return populated
}

// PopulateFromJSONEnv populates API keys from the SPROUT_API_KEYS_JSON environment
// variable. The value must be a JSON object mapping provider names to API key strings,
// e.g. {"openrouter":"sk-...","deepinfra":"di-..."}.
// This is designed for containerized/SaaS environments (e.g. Sprout Foundry) where
// keys are injected at runtime rather than stored in config files.
func (keys *APIKeys) PopulateFromJSONEnv() bool {
	raw := strings.TrimSpace(GetEnvSimple("API_KEYS_JSON"))
	if raw == "" {
		return false
	}
	var parsed map[string]string
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		log.Printf("[WARN] SPROUT_API_KEYS_JSON: invalid JSON: %v", err)
		return false
	}
	populated := false
	for provider, key := range parsed {
		provider = strings.TrimSpace(provider)
		key = strings.TrimSpace(key)
		if provider != "" && key != "" {
			keys.SetAPIKey(provider, key)
			populated = true
		}
	}
	return populated
}

// ValidateAndSaveAPIKey validates a new API key before storing it.
// If validation fails, the old key is preserved and an error is returned.
// Returns the number of models available if validation succeeds.
func ValidateAndSaveAPIKey(provider, key string) (int, error) {
	keyValidationMutex.Lock()
	defer keyValidationMutex.Unlock()

	// Parse provider name to ClientType
	clientType, err := api.ParseProviderName(provider)
	if err != nil {
		return 0, fmt.Errorf("unsupported provider: %s", provider)
	}

	// Get the old key for restoration if validation fails
	oldValue, hasOldValue := "", false
	if val, _, err := credentials.GetFromActiveBackend(provider); err == nil && strings.TrimSpace(val) != "" {
		oldValue = val
		hasOldValue = true
	}

	// Set the new key temporarily
	if err := credentials.SetToActiveBackend(provider, key); err != nil {
		// Failed to set key at all - restore old if it existed
		if hasOldValue {
			_ = credentials.SetToActiveBackend(provider, oldValue)
		}
		return 0, fmt.Errorf("failed to store temporary key: %w", err)
	}

	if validateAndSaveSkipValidation {
		// Test mode: skip network validation, key is already stored above
		return 0, nil
	}

	// Validate the new key by calling ListModels
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	models, err := api.GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		// For custom providers, ListModels validation is best-effort — many
		// custom endpoints don't expose a standard /models route. Save the
		// key anyway; the user can test it later via the "Test connection"
		// button. For built-in providers, validation failure means the key
		// is genuinely broken, so restore the old key and reject.
		if isCustomProvider(provider) {
			log.Printf("[config] API key for custom provider %q saved without validation (ListModels failed: %v)", provider, err)
			return 0, nil
		}
		// Validation failed - restore old key if it existed
		if hasOldValue {
			if restoreErr := credentials.SetToActiveBackend(provider, oldValue); restoreErr != nil {
				log.Printf("[config] Warning: failed to restore old key for %q: %v", provider, restoreErr)
			}
		} else {
			// No old key existed, remove the bad key
			_ = credentials.DeleteFromActiveBackend(provider)
		}
		return 0, fmt.Errorf("validation failed: %s", sanitizeValidationError(err))
	}

	// Validation succeeded - key is already stored in backend via SetToActiveBackend above
	log.Printf("[config] API key for %q validated successfully (%d models available)", provider, len(models))
	return len(models), nil
}

// sanitizeValidationError maps internal API errors to user-friendly messages.
func sanitizeValidationError(err error) string {
	errMsg := err.Error()

	// Common error patterns to sanitize
	switch {
	case strings.Contains(errMsg, "401") || strings.Contains(errMsg, "unauthorized") || strings.Contains(errMsg, "invalid api key") || strings.Contains(errMsg, "authentication"):
		return "Invalid API key. Please check your credentials and try again."
	case strings.Contains(errMsg, "403") || strings.Contains(errMsg, "forbidden"):
		return "Access forbidden. Your API key may not have the required permissions."
	case strings.Contains(errMsg, "429") || strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "too many requests"):
		return "Rate limit exceeded. Please wait a moment and try again."
	case strings.Contains(errMsg, "500") || strings.Contains(errMsg, "internal"):
		return "Service temporarily unavailable. Please try again later."
	case strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline"):
		return "Request timed out. Please check your network connection and try again."
	case strings.Contains(errMsg, "network") || strings.Contains(errMsg, "dial"):
		return "Network error. Please check your internet connection and try again."
	default:
		// Don't leak raw error messages - they may contain internal paths or details
		return "Validation failed. Please check your API key and network connection."
	}
}
