package credentials

// backend_migrate.go — the two-way credential storage migration between the
// on-disk file store and the OS keyring: MigrateFileToKeyring and
// MigrateKeyringToFile. Split out of backend.go.
import (
	"encoding/json"
	"fmt"
)

// MigrateFileToKeyring migrates all credentials from file store to keyring.
// Returns the list of providers that were migrated.
// If clearFile is true, removes all credentials from the file store after successful migration.
// On failure, rolls back any partially migrated credentials to prevent orphaned entries.
func MigrateFileToKeyring(clearFile bool) ([]string, error) {
	debugLogf("[credentials] Migrating credentials from file store to keyring...")

	// Load all credentials from file store
	store, err := Load()
	if err != nil {
		return nil, fmt.Errorf("failed to load credentials from file store: %w", err)
	}

	if len(store) == 0 {
		debugLogf("[credentials] No credentials to migrate from file store")
		return []string{}, nil
	}

	// Get keyring backend
	keyringBackend := NewOSKeyringBackend()

	// Migrate each credential
	migrated := make([]string, 0, len(store))
	for provider, value := range store {
		// Parse the value to detect multi-key JSON arrays.
		// File backend stores multiple keys as '["key1","key2"]'.
		// Keyring backend stores each key separately (provider, provider__pool_1, …).
		// We must convert between these formats during migration.
		pool, parseErr := parseKeyArray(value)
		if parseErr != nil {
			// Parse error is unexpected (parseKeyArray falls back to plain string
			// on invalid JSON arrays), but handle it defensively.
			pool = &KeyPool{Keys: []string{value}}
		}

		if len(pool.Keys) == 0 {
			continue
		}

		// Store primary key
		if err := keyringBackend.Set(provider, pool.Keys[0]); err != nil {
			// Rollback: delete any credentials that were already migrated
			for _, p := range migrated {
				_ = keyringBackend.Delete(p)
				for j := 1; j < MaxPoolEntries; j++ {
					_ = keyringBackend.Delete(fmt.Sprintf("%s__pool_%d", p, j))
				}
			}
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to migrate credential for %q to keyring: %w", provider, err)
		}

		// Store additional keys in keyring pool format
		for i := 1; i < len(pool.Keys); i++ {
			poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
			if err := keyringBackend.Set(poolKey, pool.Keys[i]); err != nil {
				// Rollback
				_ = keyringBackend.Delete(provider)
				for _, p := range migrated {
					_ = keyringBackend.Delete(p)
					for j := 1; j < MaxPoolEntries; j++ {
						_ = keyringBackend.Delete(fmt.Sprintf("%s__pool_%d", p, j))
					}
				}
				_ = saveTrackedKeyringProviders([]string{})
				return nil, fmt.Errorf("failed to migrate pool key %d for %q to keyring: %w", i, provider, err)
			}
		}

		// Track this provider in the keyring
		if err := addTrackedProvider(provider); err != nil {
			// Rollback: delete the keys we just stored plus any previously migrated
			_ = keyringBackend.Delete(provider)
			for j := 1; j < len(pool.Keys); j++ {
				_ = keyringBackend.Delete(fmt.Sprintf("%s__pool_%d", provider, j))
			}
			for _, p := range migrated {
				_ = keyringBackend.Delete(p)
				for j := 1; j < MaxPoolEntries; j++ {
					_ = keyringBackend.Delete(fmt.Sprintf("%s__pool_%d", p, j))
				}
			}
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to track provider %q in keyring: %w", provider, err)
		}

		migrated = append(migrated, provider)
	}

	// Optionally clear the file store
	if clearFile {
		debugLogf("[credentials] Clearing file store after migration")
		if err := Save(Store{}); err != nil {
			// Rollback: delete migrated credentials from keyring if file clear fails
			for _, p := range migrated {
				_ = keyringBackend.Delete(p)
				for j := 1; j < MaxPoolEntries; j++ {
					_ = keyringBackend.Delete(fmt.Sprintf("%s__pool_%d", p, j))
				}
			}
			_ = saveTrackedKeyringProviders([]string{})
			return nil, fmt.Errorf("failed to clear file store: %w", err)
		}
	}

	debugLogf("[credentials] Successfully migrated %d credentials to keyring", len(migrated))
	return migrated, nil
}

// MigrateKeyringToFile migrates all credentials from keyring to file store.
// Returns the list of providers that were migrated.
// If clearKeyring is true, removes all credentials from the keyring after successful migration.
// On failure, rolls back any partially migrated credentials to prevent orphaned entries.
func MigrateKeyringToFile(clearKeyring bool) ([]string, error) {
	debugLogf("[credentials] Migrating credentials from keyring to file store...")

	// Get tracked providers from keyring
	providers, err := getTrackedKeyringProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to get tracked keyring providers: %w", err)
	}

	if len(providers) == 0 {
		debugLogf("[credentials] No credentials to migrate from keyring")
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
				_ = fileBackend.Delete(p)
			}
			return nil, fmt.Errorf("failed to get credential for %q from keyring: %w", provider, err)
		}

		if value == "" {
			debugLogf("[credentials] Warning: no credential found for %q in keyring, skipping", provider)
			continue
		}

		// Reassemble multi-key pool from keyring's __pool_N entries.
		// Keyring stores: provider -> key0, provider__pool_1 -> key1, …
		// File stores: provider -> '["key0","key1",…]' JSON array.
		keys := []string{value}
		for i := 1; i < MaxPoolEntries; i++ {
			poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
			poolValue, err := keyringBackend.Get(poolKey)
			if err != nil || poolValue == "" {
				break
			}
			keys = append(keys, poolValue)
		}

		// Store in file backend format (plain string for single key, JSON array for multiple)
		var fileValue string
		if len(keys) == 1 {
			fileValue = keys[0]
		} else {
			jsonBytes, jsonErr := json.Marshal(keys)
			if jsonErr != nil {
				for _, p := range migrated {
					_ = fileBackend.Delete(p)
				}
				return nil, fmt.Errorf("failed to serialize key pool for %q: %w", provider, jsonErr)
			}
			fileValue = string(jsonBytes)
		}

		if err := fileBackend.Set(provider, fileValue); err != nil {
			// Rollback: delete any credentials already written to file
			for _, p := range migrated {
				_ = fileBackend.Delete(p)
			}
			return nil, fmt.Errorf("failed to migrate credential for %q to file store: %w", provider, err)
		}

		migrated = append(migrated, provider)
	}

	// Optionally clear the keyring
	if clearKeyring {
		debugLogf("[credentials] Clearing keyring after migration")
		for _, provider := range providers {
			if err := keyringBackend.Delete(provider); err != nil {
				debugLogf("[credentials] Warning: failed to delete %q from keyring: %v", provider, err)
			}
			// Also clean up pool entries
			for i := 1; i < MaxPoolEntries; i++ {
				poolKey := fmt.Sprintf("%s__pool_%d", provider, i)
				if err := keyringBackend.Delete(poolKey); err != nil {
					debugLogf("[credentials] Warning: failed to delete %q from keyring: %v", poolKey, err)
					continue // Keep trying; entries may not be contiguous after add/remove
				}
			}
			if err := removeTrackedProvider(provider); err != nil {
				debugLogf("[credentials] Warning: failed to remove %q from tracking: %v", provider, err)
			}
		}
	}

	debugLogf("[credentials] Successfully migrated %d credentials to file store", len(migrated))
	return migrated, nil
}
