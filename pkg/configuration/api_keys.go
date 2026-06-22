package configuration

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"golang.org/x/term"
)

// staticProviderNames is the compile-time canonical list of built-in
// provider names. CLI/UI ordering anchors on this — built-ins keep
// their generated order; runtime-only additions (e.g. providers
// published to GitHub Pages but not yet shipped in the binary) are
// appended in sorted order by KnownProviderNames().
var staticProviderNames = providers.KnownProviders()

// knownProviderDisplayNames maps provider names to their display names.
// This is the single source of truth for provider display names in CLI/UI.
// Generated from provider configs - use providers.ProviderDisplayNames() for the full map.
var knownProviderDisplayNames = providers.ProviderDisplayNames()

// KnownProviderNames returns the union of the compile-time provider
// list and whatever the runtime factory has registered (which includes
// embedded + filesystem + remote configs once pkg/factory.init has
// wired SetProviderNamesLookup). Static entries keep their generated
// order; runtime-only additions are appended in sorted order so the
// result is deterministic across calls.
func KnownProviderNames() []string {
	static := staticProviderNames
	providerNamesLookupMu.RLock()
	lookup := providerNamesLookup
	providerNamesLookupMu.RUnlock()
	if lookup == nil {
		return append([]string(nil), static...)
	}

	seen := make(map[string]struct{}, len(static))
	for _, n := range static {
		seen[n] = struct{}{}
	}

	var extras []string
	for _, n := range lookup() {
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		extras = append(extras, n)
	}

	if len(extras) == 0 {
		return append([]string(nil), static...)
	}
	sort.Strings(extras)
	out := make([]string, 0, len(static)+len(extras))
	out = append(out, static...)
	out = append(out, extras...)
	return out
}

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
	providerName := GetProviderDisplayName(provider)

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

// GetProviderDisplayName returns a user-friendly name for the provider.
// Lookup chain:
//  1. Static display-name map (generated from embedded configs — fastest
//     and the common case for built-ins).
//  2. Runtime factory (covers remote-only providers published to GitHub
//     Pages whose display_name isn't baked into the static map).
//  3. CustomProviders (user-defined local providers in config.json).
//  4. Raw provider ID as a last resort.
func GetProviderDisplayName(provider string) string {
	if displayName, ok := knownProviderDisplayNames[provider]; ok {
		return displayName
	}

	providerDisplayLookupMu.RLock()
	lookup := providerDisplayLookup
	providerDisplayLookupMu.RUnlock()
	if lookup != nil {
		if displayName, ok := lookup(provider); ok && displayName != "" {
			return displayName
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

// RequiresAPIKey checks if a provider requires an API key.
func RequiresAPIKey(provider string) bool {
	metadata, err := GetProviderAuthMetadata(provider)
	if err != nil {
		return true // default to requiring key for unknown providers
	}
	return metadata.RequiresAPIKey
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
