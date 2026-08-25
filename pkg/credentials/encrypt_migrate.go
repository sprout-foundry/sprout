// Store persistence, loading, saving, and migration
package credentials

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gofrs/flock"
	"github.com/sprout-foundry/sprout/pkg/envutil"
)

// apiKeysLockPath returns the path to the API keys lock file.
func apiKeysLockPath() (string, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return "", err
	}
	return path + ".lock", nil
}

// loadUnlocked reads and decrypts the API keys file without acquiring a lock.
// The caller MUST hold an exclusive lock on the file's lock file.
func loadUnlocked() (Store, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return nil, fmt.Errorf("get API keys path: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}
	return loadFromPath(path)
}

// saveUnlocked encrypts and writes the API keys file without acquiring a lock.
// The caller MUST hold an exclusive lock on the file's lock file.
// This is identical to Save() but skips lock acquisition.
func saveUnlocked(store Store) error {
	path, err := GetAPIKeysPath()
	if err != nil {
		return fmt.Errorf("get API keys path: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}
	mode, err := GetEncryptionMode()
	if err != nil {
		return fmt.Errorf("failed to get encryption mode: %w", err)
	}
	var encrypted []byte
	if mode == "passphrase" {
		passphrase := strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE"))
		if passphrase == "" {
			return fmt.Errorf("cannot save: API keys are passphrase-encrypted but SPROUT_KEY_PASSPHRASE is not set. " +
				"Set SPROUT_KEY_PASSPHRASE or run 'sprout keys encrypt' to switch to machine-key mode")
		}
		encrypted, err = EncryptWithPassphrase(data, passphrase)
	} else if mode == "" && strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")) != "" {
		encrypted, err = EncryptWithPassphrase(data, strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")))
		if err == nil {
			_ = SetEncryptionMode("passphrase")
		}
	} else {
		encrypted, err = EncryptStore(data)
		if err == nil && mode == "" {
			_ = SetEncryptionMode("machine-key")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to encrypt API keys: %w", err)
	}
	if err := AtomicWriteFile(path, encrypted, 0600); err != nil {
		return err
	}
	return nil
}

// saveToPath saves the API keys store to a specific path without acquiring a lock.
// The caller MUST hold an exclusive lock on the file's lock file.
// This is a helper used by SaveToDir() and AtomicModifyForDir() to avoid code duplication.
func saveToPath(path string, configDir string, store Store) error {
	if store == nil {
		store = Store{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}

	// Check encryption mode
	var mode string
	if configDir != "" {
		mode, err = GetEncryptionModeFromDir(configDir)
	} else {
		mode, err = GetEncryptionMode()
	}
	if err != nil {
		return fmt.Errorf("failed to get encryption mode: %w", err)
	}

	var encrypted []byte
	if mode == "passphrase" {
		// Passphrase mode: encrypt with the user's passphrase
		passphrase := strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE"))
		if passphrase == "" {
			return fmt.Errorf("cannot save: API keys are passphrase-encrypted but SPROUT_KEY_PASSPHRASE is not set. " +
				"Set SPROUT_KEY_PASSPHRASE or run 'sprout keys encrypt' to switch to machine-key mode")
		}
		encrypted, err = EncryptWithPassphrase(data, passphrase)
	} else if mode == "" && strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")) != "" {
		// Legacy passphrase-encrypted file with no mode file: the user has
		// SPROUT_KEY_PASSPHRASE set (required to have loaded the file), so
		// preserve their passphrase encryption rather than silently downgrading
		// to machine-key mode.
		encrypted, err = EncryptWithPassphrase(data, strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")))
		if err == nil {
			if configDir != "" {
				_ = SetEncryptionModeForDir(configDir, "passphrase")
			} else {
				_ = SetEncryptionMode("passphrase")
			}
		}
	} else {
		// Machine-key mode or no mode with no passphrase (new file) —
		// always auto-set machine-key mode
		encrypted, err = EncryptStore(data)
		if err == nil && mode == "" {
			if configDir != "" {
				_ = SetEncryptionModeForDir(configDir, "machine-key")
			} else {
				_ = SetEncryptionMode("machine-key")
			}
		}
	}
	if err != nil {
		return fmt.Errorf("failed to encrypt API keys: %w", err)
	}

	if err := AtomicWriteFile(path, encrypted, 0600); err != nil {
		return err
	}
	return nil
}

// AtomicModify acquires an exclusive lock, loads the API keys store,
// calls fn to modify it, and saves it back. The entire load-modify-save
// cycle is protected by the exclusive lock, preventing TOCTOU races.
// Uses a 15-second timeout to account for slow encryption operations.
func AtomicModify(fn func(Store) error) error {
	lockPath, err := apiKeysLockPath()
	if err != nil {
		return err
	}
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return fmt.Errorf("failed to acquire lock for atomic modify: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out waiting for lock during atomic modify — another process may be saving credentials")
	}
	defer fileLock.Unlock()

	store, err := loadUnlocked()
	if err != nil {
		return fmt.Errorf("failed to load credentials for atomic modify: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	if err := fn(store); err != nil {
		return err
	}
	return saveUnlocked(store)
}

// AtomicModifyForDir acquires an exclusive lock, loads the API keys store from a specific
// config directory, calls fn to modify it, and saves it back. The entire load-modify-save
// cycle is protected by the exclusive lock, preventing TOCTOU races.
// Uses a 15-second timeout to account for slow encryption operations.
func AtomicModifyForDir(configDir string, fn func(Store) error) error {
	lockPath, err := GetAPIKeysLockPathFromDir(configDir)
	if err != nil {
		return err
	}
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return fmt.Errorf("failed to acquire lock for atomic modify: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out waiting for lock during atomic modify — another process may be saving credentials")
	}
	defer fileLock.Unlock()

	path, err := GetAPIKeysPathFromDir(configDir)
	if err != nil {
		return fmt.Errorf("failed to get API keys path: %w", err)
	}

	store, err := loadFromPath(path)
	if err != nil {
		return fmt.Errorf("failed to load credentials for atomic modify: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	if err := fn(store); err != nil {
		return err
	}
	return saveToPath(path, configDir, store)
}

// Load loads the API keys store from disk.
//
// This function reads the API keys file from the configured location, decrypts
// it if necessary (supporting both machine-key and passphrase encryption modes),
// and unmarshals the JSON into a Store.
//
// If the file does not exist, an empty Store is returned without error.
// This allows the application to start cleanly even if no API keys have been
// configured yet.
//
// Uses flock-based locking to prevent race conditions when multiple processes
// read the file concurrently.
func Load() (Store, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return nil, fmt.Errorf("get API keys directory: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}

	// Acquire shared lock for reading (allows concurrent reads)
	lockPath := path + ".lock"
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryRLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for load: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("timed out waiting for load lock — another process may be saving (lock timeout is generous to allow for slow encryption)")
	}
	defer fileLock.Unlock()

	return loadFromPath(path)
}

// loadFromPath loads the API keys store from a specific path without acquiring a lock.
// The caller MUST hold a lock on the file if needed.
// This is a helper that both Load() and LoadFromDir() use to avoid code duplication.
func loadFromPath(path string) (Store, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read API keys file: %w", err)
	}

	// Decrypt if encrypted
	data, err = DecryptStore(data)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt API keys: %w", err)
	}

	var store Store
	if err := json.Unmarshal(data, &store); err != nil {
		return nil, fmt.Errorf("failed to parse API keys file: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	return store, nil
}

// LoadFromDir loads the API keys store from a specific config directory.
//
// This function is like Load() but takes an explicit config directory instead
// of reading from environment variables. It's useful for test environments and
// other scenarios where you want to load from a specific location without
// mutating process state.
//
// Uses flock-based locking to prevent race conditions when multiple processes
// read the file concurrently.
func LoadFromDir(configDir string) (Store, error) {
	path, err := GetAPIKeysPathFromDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("get API keys path: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}

	// Acquire shared lock for reading (allows concurrent reads)
	lockPath := path + ".lock"
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryRLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for load: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("timed out waiting for load lock — another process may be saving (lock timeout is generous to allow for slow encryption)")
	}
	defer fileLock.Unlock()

	return loadFromPath(path)
}

// Save saves the API keys store to disk, encrypting it first.
//
// This function marshals the Store to JSON, encrypts it using the appropriate
// encryption mode (machine-key or passphrase), and writes the encrypted data
// to the configured API keys file.
//
// The file is created with permissions 0600 (read/write for owner only) to ensure
// API keys are stored securely on disk. The write is atomic (using a temp file + rename)
// to prevent data corruption if the process crashes during the write.
//
// If the API keys are passphrase-encrypted, this function requires the
// SPROUT_KEY_PASSPHRASE environment variable to be set. Otherwise, it returns
// an error directing the user to set the environment variable or switch to
// machine-key mode.
func Save(store Store) error {
	path, err := GetAPIKeysPath()
	if err != nil {
		return fmt.Errorf("get API keys path: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal API keys: %w", err)
	}

	// Check encryption mode
	mode, err := GetEncryptionMode()
	if err != nil {
		return fmt.Errorf("failed to get encryption mode: %w", err)
	}

	var encrypted []byte
	if mode == "passphrase" {
		// Passphrase mode: encrypt with the user's passphrase
		passphrase := strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE"))
		if passphrase == "" {
			return fmt.Errorf("cannot save: API keys are passphrase-encrypted but SPROUT_KEY_PASSPHRASE is not set. " +
				"Set SPROUT_KEY_PASSPHRASE or run 'sprout keys encrypt' to switch to machine-key mode")
		}
		encrypted, err = EncryptWithPassphrase(data, passphrase)
	} else if mode == "" && strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")) != "" {
		// Legacy passphrase-encrypted file with no mode file: the user has
		// SPROUT_KEY_PASSPHRASE set (required to have loaded the file), so
		// preserve their passphrase encryption rather than silently downgrading
		// to machine-key mode.
		encrypted, err = EncryptWithPassphrase(data, strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")))
		if err == nil {
			_ = SetEncryptionMode("passphrase")
		}
	} else {
		// Machine-key mode or no mode with no passphrase (new file) —
		// always auto-set machine-key mode
		encrypted, err = EncryptStore(data)
		if err == nil && mode == "" {
			_ = SetEncryptionMode("machine-key")
		}
	}
	if err != nil {
		return fmt.Errorf("failed to encrypt API keys: %w", err)
	}

	// Acquire exclusive lock for writing
	lockPath := path + ".lock"
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return fmt.Errorf("failed to acquire lock for save: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out waiting for save lock — another process may be saving (lock timeout is generous to allow for slow encryption)")
	}
	defer fileLock.Unlock()

	// Write atomically
	if err := AtomicWriteFile(path, encrypted, 0600); err != nil {
		return err
	}

	return nil
}

// SaveToDir saves the API keys store to a specific config directory, encrypting it first.
//
// This function is like Save() but takes an explicit config directory instead
// of reading from environment variables. It's useful for test environments and
// other scenarios where you want to save to a specific location without
// mutating process state.
//
// Uses flock-based locking to prevent race conditions when multiple processes
// write to the file concurrently.
func SaveToDir(store Store, configDir string) error {
	path, err := GetAPIKeysPathFromDir(configDir)
	if err != nil {
		return fmt.Errorf("get API keys path: %w", err)
	}
	if store == nil {
		store = Store{}
	}

	// Acquire exclusive lock for writing
	lockPath := path + ".lock"
	fileLock := flock.New(lockPath)
	locked, err := fileLock.TryLockContext(context.Background(), 15*time.Second)
	if err != nil {
		return fmt.Errorf("failed to acquire lock for save: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out waiting for save lock — another process may be saving (lock timeout is generous to allow for slow encryption)")
	}
	defer fileLock.Unlock()

	// Save to path using the helper
	return saveToPath(path, configDir, store)
}

// resolve is the internal credential resolution function.
// Use ResolveProvider() for the public API.
//
// Resolution order:
//  1. Environment variable (if envVar is provided and set, highest priority)
//  2. Key pool with round-robin rotation from stored credentials
//
// For keyring backends, the keyring entries are loaded via the active backend.
// For file backends, Load() is called directly to read the file store
// (avoiding cached backend path issues when SPROUT_CONFIG changes between tests).
func resolve(provider, envVar string) (Resolved, error) {
	resolved := Resolved{
		Provider: strings.TrimSpace(provider),
		EnvVar:   strings.TrimSpace(envVar),
	}

	// 1. Environment variable takes highest priority (single key, no rotation)
	if resolved.EnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(resolved.EnvVar)); value != "" {
			resolved.Value = value
			resolved.Source = "environment"
			return resolved, nil
		}
	}

	// 2. Load provider's key pool from backend and use round-robin rotation.
	// Use the active backend (keyring or file) to load the pool.
	// For keyring, GetFromActiveBackend reads from keyring and returns source="keyring".
	// For file, GetFromActiveBackend reads from encrypted file and returns source="stored".
	result, err := LoadKeyPool(resolved.Provider)
	if err != nil {
		return Resolved{}, fmt.Errorf("load credentials store: %w", err)
	}
	if len(result.Pool.Keys) == 0 {
		return resolved, nil
	}

	key := DefaultRotator.NextKey(resolved.Provider, result.Pool)
	if key == "" {
		return resolved, nil
	}
	resolved.Value = key
	resolved.Source = result.Source

	return resolved, nil
}
