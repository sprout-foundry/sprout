package credentials

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const (
	// backendModeFileName is the name of the file that tracks the active storage backend
	backendModeFileName = "backend.mode"
	// keyringProvidersFileName tracks which providers have entries in the keyring
	keyringProvidersFileName = "keyring_providers.json"
)

// Package-level caching for GetStorageBackend() to avoid repeated auto-detection.
// Uses sync.Once to ensure the backend is resolved exactly once per process lifetime.
var (
	backendOnce     sync.Once
	cachedBackend   Backend
	cachedBackendErr error
)

// StorageMode represents the active credential storage backend.
// Valid values: "keyring", "file", "" (unset/auto-detect)
type StorageMode string

const (
	// StorageModeKeyring uses OS-native keyring (GNOME Keyring, macOS Keychain, Windows Credential Manager)
	StorageModeKeyring StorageMode = "keyring"
	// StorageModeFile uses encrypted JSON file storage
	StorageModeFile StorageMode = "file"
	// StorageModeUnset means auto-detect on first use
	StorageModeUnset StorageMode = ""
)

// getBackendModePath returns the path to the backend mode file.
func getBackendModePath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, backendModeFileName), nil
}

// getKeyringProvidersPath returns the path to the keyring providers tracking file.
// This file tracks WHICH providers have entries in the keyring (just names, no secrets).
func getKeyringProvidersPath() (string, error) {
	configDir, err := GetConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get config directory: %w", err)
	}
	return filepath.Join(configDir, keyringProvidersFileName), nil
}

// getTrackedKeyringProviders loads the list of providers tracked in the keyring.
// Returns an empty list (not error) if the file doesn't exist.
func getTrackedKeyringProviders() ([]string, error) {
	path, err := getKeyringProvidersPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil // No providers tracked yet
		}
		return nil, fmt.Errorf("failed to read keyring providers file: %w", err)
	}

	var providers []string
	if err := json.Unmarshal(data, &providers); err != nil {
		return nil, fmt.Errorf("failed to parse keyring providers file: %w", err)
	}

	return providers, nil
}

// saveTrackedKeyringProviders persists the list of providers tracked in the keyring.
func saveTrackedKeyringProviders(providers []string) error {
	path, err := getKeyringProvidersPath()
	if err != nil {
		return err
	}

	data, err := json.MarshalIndent(providers, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal keyring providers: %w", err)
	}

	if err := AtomicWriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("failed to write keyring providers file: %w", err)
	}

	return nil
}

// addTrackedProvider adds a provider to the tracked list.
func addTrackedProvider(provider string) error {
	providers, err := getTrackedKeyringProviders()
	if err != nil {
		return err
	}

	// Check if already tracked
	for _, p := range providers {
		if p == provider {
			return nil // Already tracked
		}
	}

	providers = append(providers, provider)
	return saveTrackedKeyringProviders(providers)
}

// removeTrackedProvider removes a provider from the tracked list.
func removeTrackedProvider(provider string) error {
	providers, err := getTrackedKeyringProviders()
	if err != nil {
		return err
	}

	// Remove the provider
	newProviders := make([]string, 0, len(providers))
	for _, p := range providers {
		if p != provider {
			newProviders = append(newProviders, p)
		}
	}

	if len(newProviders) == len(providers) {
		return nil // Not found
	}

	return saveTrackedKeyringProviders(newProviders)
}

// GetStorageMode returns the persisted storage mode ("keyring", "file", or "").
// Returns empty string if no mode file exists (will be auto-detected on first use).
func GetStorageMode() (string, error) {
	path, err := getBackendModePath()
	if err != nil {
		return "", fmt.Errorf("failed to get backend mode path: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil // No mode file yet - will auto-detect
		}
		return "", fmt.Errorf("failed to read backend mode file: %w", err)
	}

	mode := strings.TrimSpace(string(data))
	if mode == "keyring" || mode == "file" {
		return mode, nil
	}
	return "", nil // Invalid mode treated as unset
}

// SetStorageMode persists the storage mode.
// mode must be "keyring" or "file".
func SetStorageMode(mode string) error {
	if mode != "keyring" && mode != "file" {
		return fmt.Errorf("invalid storage mode: %q (must be 'keyring' or 'file')", mode)
	}

	path, err := getBackendModePath()
	if err != nil {
		return fmt.Errorf("failed to get backend mode path: %w", err)
	}

	if err := AtomicWriteFile(path, []byte(mode+"\n"), 0600); err != nil {
		return fmt.Errorf("failed to write backend mode file: %w", err)
	}

	log.Printf("[credentials] Storage mode set to: %s", mode)
	return nil
}

// GetStorageBackend returns the active backend based on configuration and auto-detection.
// Resolution order:
// 1. If LEDIT_CREDENTIAL_BACKEND=keyring → OSKeyringBackend
// 2. If LEDIT_CREDENTIAL_BACKEND=file → FileBackend
// 3. Auto-detect: try OSKeyringBackend.Get("__ledit_probe__") to check availability
//    - If available → use OSKeyringBackend (persist mode as "keyring")
//    - If unavailable → fallback to FileBackend (persist mode as "file")
//
// The backend is resolved once per process lifetime using sync.Once caching.
// Call ResetStorageBackend() to force re-detection (useful for tests).
func GetStorageBackend() (Backend, error) {
	backendOnce.Do(func() {
		cachedBackend, cachedBackendErr = resolveBackend()
	})
	return cachedBackend, cachedBackendErr
}

// resolveBackend performs the actual backend resolution logic.
// This is extracted from GetStorageBackend() to allow caching via sync.Once.
func resolveBackend() (Backend, error) {
	// Check environment variable first
	envMode := strings.TrimSpace(os.Getenv("LEDIT_CREDENTIAL_BACKEND"))
	if envMode == "keyring" {
		log.Printf("[credentials] Using keyring backend (forced via LEDIT_CREDENTIAL_BACKEND)")
		return NewOSKeyringBackend(), nil
	}
	if envMode == "file" {
		log.Printf("[credentials] Using file backend (forced via LEDIT_CREDENTIAL_BACKEND)")
		return NewFileBackend(), nil
	}

	// Check persisted mode
	persistedMode, err := GetStorageMode()
	if err != nil {
		return nil, fmt.Errorf("failed to get storage mode: %w", err)
	}

	if persistedMode == "keyring" {
		log.Printf("[credentials] Using keyring backend (persisted mode)")
		return NewOSKeyringBackend(), nil
	}
	if persistedMode == "file" {
		log.Printf("[credentials] Using file backend (persisted mode)")
		return NewFileBackend(), nil
	}

	// Auto-detect: try to use keyring, fall back to file
	log.Printf("[credentials] Auto-detecting storage backend...")
	if IsKeyringAvailable() {
		log.Printf("[credentials] OS keyring available, using keyring backend")
		if err := SetStorageMode("keyring"); err != nil {
			log.Printf("[credentials] Warning: failed to persist keyring mode: %v", err)
		}
		return NewOSKeyringBackend(), nil
	}

	log.Printf("[credentials] OS keyring not available, using file backend")
	if err := SetStorageMode("file"); err != nil {
		log.Printf("[credentials] Warning: failed to persist file mode: %v", err)
	}
	return NewFileBackend(), nil
}

// ResetStorageBackend resets the cached backend, forcing re-detection on next call.
// This is primarily useful for tests that need to verify different backend configurations.
func ResetStorageBackend() {
	backendOnce = sync.Once{}
	cachedBackend = nil
	cachedBackendErr = nil
}

// MigrateFileToKeyring migrates all credentials from file store to keyring.
// Returns the list of providers that were migrated.
// If clearFile is true, removes all credentials from the file store after successful migration.
// On failure, rolls back any partially migrated credentials to prevent orphaned entries.
func MigrateFileToKeyring(clearFile bool) ([]string, error) {
	log.Printf("[credentials] Migrating credentials from file store to keyring...")

	// Load all credentials from file store
	store, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials from file store: %w", err)
	}

	if len(store) == 0 {
		log.Printf("[credentials] No credentials to migrate from file store")
		return []string{}, nil
	}

	// Get keyring backend
	keyringBackend := NewOSKeyringBackend()

	// Migrate each credential
	migrated := make([]string, 0, len(store))
	for provider, value := range store {
		if err := keyringBackend.Set(provider, value); err != nil {
			// Rollback: delete any credentials that were already migrated
			for _, p := range migrated {
				_ = keyringBackend.Delete(p) // best-effort rollback
			}
			// Clear tracking to prevent orphaned tracking entries
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to migrate credential for %q to keyring: %w", provider, err)
		}

		// Track this provider in the keyring
		if err := addTrackedProvider(provider); err != nil {
			// Rollback: delete any credentials that were already migrated
			for _, p := range migrated {
				_ = keyringBackend.Delete(p) // best-effort rollback
			}
			// Clear tracking to prevent orphaned tracking entries
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to track provider %q in keyring: %w", provider, err)
		}

		migrated = append(migrated, provider)
	}

	// Optionally clear the file store
	if clearFile {
		log.Printf("[credentials] Clearing file store after migration")
		if err := Save(Store{}); err != nil {
			// Rollback: delete migrated credentials from keyring if file clear fails
			for _, p := range migrated {
				_ = keyringBackend.Delete(p)
			}
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to clear file store: %w", err)
		}
	}

	log.Printf("[credentials] Successfully migrated %d credentials to keyring", len(migrated))
	return migrated, nil
}

// MigrateKeyringToFile migrates all credentials from keyring to file store.
// Returns the list of providers that were migrated.
// If clearKeyring is true, removes all credentials from the keyring after successful migration.
// On failure, rolls back any partially migrated credentials to prevent orphaned entries.
func MigrateKeyringToFile(clearKeyring bool) ([]string, error) {
	log.Printf("[credentials] Migrating credentials from keyring to file store...")

	// Get tracked providers from keyring
	providers, err := getTrackedKeyringProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get tracked keyring providers: %w", err)
	}

	if len(providers) == 0 {
		log.Printf("[credentials] No credentials to migrate from keyring")
		return []string{}, nil
	}

	// Get keyring backend and file backend
	keyringBackend := NewOSKeyringBackend()
	fileBackend := NewFileBackend()

	// Migrate each credential
	migrated := make([]string, 0, len(providers))
	for _, provider := range providers {
		value, err := keyringBackend.Get(provider)
		if err != nil {
			// Rollback: delete any credentials already written to file
			for _, p := range migrated {
				_ = fileBackend.Delete(p) // best-effort rollback
			}
			return nil, fmt.Errorf("failed to get credential for %q from keyring: %w", provider, err)
		}

		if value == "" {
			log.Printf("[credentials] Warning: no credential found for %q in keyring, skipping", provider)
			continue
		}

		if err := fileBackend.Set(provider, value); err != nil {
			// Rollback: delete any credentials already written to file
			for _, p := range migrated {
				_ = fileBackend.Delete(p) // best-effort rollback
			}
			return nil, fmt.Errorf("failed to migrate credential for %q to file store: %w", provider, err)
		}

		migrated = append(migrated, provider)
	}

	// Optionally clear the keyring
	if clearKeyring {
		log.Printf("[credentials] Clearing keyring after migration")
		for _, provider := range providers {
			if err := keyringBackend.Delete(provider); err != nil {
				log.Printf("[credentials] Warning: failed to delete %q from keyring: %v", provider, err)
			}
			if err := removeTrackedProvider(provider); err != nil {
				log.Printf("[credentials] Warning: failed to remove %q from tracking: %v", provider, err)
			}
		}
	}

	log.Printf("[credentials] Successfully migrated %d credentials to file store", len(migrated))
	return migrated, nil
}

// ListKeyringProviders returns the list of providers that have entries in the keyring.
func ListKeyringProviders() ([]string, error) {
	return getTrackedKeyringProviders()
}

// DeleteFromActiveBackend deletes a credential using the active backend.
func DeleteFromActiveBackend(provider string) error {
	backend, err := GetStorageBackend()
	if err != nil {
		return fmt.Errorf("failed to get active backend: %w", err)
	}

	if err := backend.Delete(provider); err != nil {
		return fmt.Errorf("failed to delete credential: %w", err)
	}

	// If using keyring backend, also remove from tracking
	if _, ok := backend.(*OSKeyringBackend); ok {
		if err := removeTrackedProvider(provider); err != nil {
			log.Printf("[credentials] Warning: failed to remove %q from tracking: %v", provider, err)
		}
	}

	return nil
}

// SetToActiveBackend sets a credential using the active backend.
func SetToActiveBackend(provider, value string) error {
	backend, err := GetStorageBackend()
	if err != nil {
		return fmt.Errorf("failed to get active backend: %w", err)
	}

	if err := backend.Set(provider, value); err != nil {
		return fmt.Errorf("failed to set credential: %w", err)
	}

	// If using keyring backend, add to tracking
	if _, ok := backend.(*OSKeyringBackend); ok {
		if err := addTrackedProvider(provider); err != nil {
			log.Printf("[credentials] Warning: failed to track %q: %v", provider, err)
		}
	}

	return nil
}

// GetFromActiveBackend gets a credential using the active backend.
// Returns the value, source ("keyring" or "file"), and error.
func GetFromActiveBackend(provider string) (string, string, error) {
	backend, err := GetStorageBackend()
	if err != nil {
		return "", "", fmt.Errorf("failed to get active backend: %w", err)
	}

	value, err := backend.Get(provider)
	if err != nil {
		return "", "", fmt.Errorf("failed to get credential: %w", err)
	}

	return value, backend.Source(), nil
}
