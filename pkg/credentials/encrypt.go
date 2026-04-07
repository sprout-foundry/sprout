package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"filippo.io/age"
)

// isPlaintextJSON checks if the data is plaintext JSON (legacy format).
func isPlaintextJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	return bytes.HasPrefix(trimmed, []byte("{")) || bytes.HasPrefix(trimmed, []byte("null"))
}

// isEncrypted checks if the data is encrypted with age.
func isEncrypted(data []byte) bool {
	return bytes.HasPrefix(data, []byte(encryptedMagic))
}

// LoadOrCreateMachineKey loads the machine key from disk or generates a new one.
// Uses file locking to prevent race conditions when multiple processes try to generate the key concurrently.
func LoadOrCreateMachineKey() (*age.X25519Identity, error) {
	keyPath, err := GetMachineKeyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine key path: %w", err)
	}

	// Try to load existing key first (fast path for most cases)
	if data, err := os.ReadFile(keyPath); err == nil {
		identity, err := parseKeyFile(data)
		if err == nil {
			return identity, nil
		}
		// If parsing fails, we need to generate a new key
	}

	// Use a lock file to prevent race conditions when generating new keys.
	// The lock file path is the same as the key path with ".lock" appended.
	lockPath := keyPath + ".lock"

	// Try to acquire the lock using O_EXCL (fails if file already exists)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		// Lock acquisition failed, another process may be generating the key.
		// Try reading the key file again - the other process may have finished.
		if data, err := os.ReadFile(keyPath); err == nil {
			identity, err := parseKeyFile(data)
			if err == nil {
				return identity, nil
			}
		}
		// If we still can't read the key, wait and try to acquire the lock again
		// with retries to handle the race condition
		for i := 0; i < 10; i++ {
			time.Sleep(10 * time.Millisecond)
			lockFile, err = os.OpenFile(lockPath, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
			if err == nil {
				break
			}
		}
		if err != nil {
			return nil, fmt.Errorf("failed to acquire lock for key generation after retries: %w", err)
		}
	}
	defer lockFile.Close()
	defer os.Remove(lockPath) // Clean up lock file on exit

	// Double-check: another process may have created the key while we were waiting for the lock
	if data, err := os.ReadFile(keyPath); err == nil {
		identity, err := parseKeyFile(data)
		if err == nil {
			return identity, nil
		}
	}

	// Generate new key
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("failed to generate machine key: %w", err)
	}

	// Write key to disk with secure permissions
	keyData, err := serializeKeyFile(identity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize machine key: %w", err)
	}

	if err := os.WriteFile(keyPath, keyData, 0600); err != nil {
		return nil, fmt.Errorf("failed to write machine key: %w", err)
	}

	return identity, nil
}

// loadMachineKey loads the machine key from disk (returns error if not found).
func loadMachineKey() (*age.X25519Identity, error) {
	keyPath, err := GetMachineKeyPath()
	if err != nil {
		return nil, fmt.Errorf("failed to get machine key path: %w", err)
	}

	data, err := os.ReadFile(keyPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read machine key: %w", err)
	}

	return parseKeyFile(data)
}

// parseKeyFile parses an age key file and returns the identity.
func parseKeyFile(data []byte) (*age.X25519Identity, error) {
	// Try unarmored format first
	identity, err := age.ParseX25519Identity(string(data))
	if err == nil {
		return identity, nil
	}

	return nil, fmt.Errorf("failed to parse key file: %w", err)
}

// serializeKeyFile serializes an identity to an unarmored key file.
func serializeKeyFile(identity *age.X25519Identity) ([]byte, error) {
	return []byte(identity.String()), nil
}

// EncryptStore encrypts plaintext data using the machine-specific X25519 key.
//
// This function ensures the machine key exists (generating it if necessary),
// then encrypts the provided plaintext using age encryption. The encrypted
// output is returned as a byte slice.
//
// Use this function when you want to encrypt data with the machine-specific
// key that is stored in ~/.ledit/key.age.
func EncryptStore(plaintext []byte) ([]byte, error) {
	identity, err := LoadOrCreateMachineKey()
	if err != nil {
		return nil, err
	}

	buf := &bytes.Buffer{}
	w, err := age.Encrypt(buf, identity.Recipient())
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close encryptor: %w", err)
	}

	return buf.Bytes(), nil
}

// DecryptStore decrypts age-encrypted data, or returns raw bytes if plaintext.
//
// This function first checks if the data is plaintext JSON (for backward
// compatibility with legacy unencrypted files). If the data is encrypted,
// it attempts to decrypt it using the machine-specific key.
//
// Returns the decrypted data as a byte slice. If decryption fails due to
// a missing machine key, an error is returned with guidance on how to
// resolve the issue.
func DecryptStore(data []byte) ([]byte, error) {
	// Check if already plaintext JSON (legacy format)
	if isPlaintextJSON(data) {
		return data, nil
	}

	// Must be encrypted, try with machine key
	identity, err := loadMachineKey()
	if err != nil {
		// Provide actionable guidance when machine key is missing
		if os.IsNotExist(err) || strings.Contains(err.Error(), "failed to read") {
			return nil, fmt.Errorf("machine key not found. Run 'ledit keys migrate' to generate a new machine key, "+
				"or set LEDIT_PASSPHRASE environment variable to decrypt with passphrase: %w", err)
		}
		return nil, fmt.Errorf("failed to load machine key: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt with machine key: %w", err)
	}

	return io.ReadAll(r)
}

// EncryptWithPassphrase encrypts plaintext data using a passphrase-derived key.
//
// This function uses the age library's Scrypt algorithm to derive an encryption
// key from the provided passphrase. It uses a work factor of 12, which provides
// a good balance between security and performance (~1 second on modern hardware).
//
// The encrypted output can be decrypted using DecryptWithPassphrase with the
// same passphrase. This mode is useful for portable encryption where the same
// encrypted data needs to be accessed from multiple machines.
func EncryptWithPassphrase(plaintext []byte, passphrase string) ([]byte, error) {
	recipient, err := age.NewScryptRecipient(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrypt recipient: %w", err)
	}

	recipient.SetWorkFactor(12) // ~1 second on modern hardware

	buf := &bytes.Buffer{}
	w, err := age.Encrypt(buf, recipient)
	if err != nil {
		return nil, fmt.Errorf("failed to create encryptor: %w", err)
	}

	if _, err := w.Write(plaintext); err != nil {
		return nil, fmt.Errorf("failed to write plaintext: %w", err)
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("failed to close encryptor: %w", err)
	}

	return buf.Bytes(), nil
}

// DecryptWithPassphrase decrypts data using a passphrase-derived key.
//
// This function derives the decryption key from the provided passphrase using
// the same Scrypt algorithm used during encryption. It sets a maximum work
// factor of 15 to prevent denial-of-service attacks from maliciously crafted
// encrypted data with extremely high work factors.
//
// Returns the decrypted data as a byte slice. Returns an error if the passphrase
// is incorrect or if the data cannot be decrypted.
func DecryptWithPassphrase(data []byte, passphrase string) ([]byte, error) {
	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrypt identity: %w", err)
	}

	identity.SetMaxWorkFactor(15) // Don't accept very high work factors

	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt with passphrase: %w", err)
	}

	return io.ReadAll(r)
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
func Load() (Store, error) {
	path, err := GetAPIKeysPath()
	if err != nil {
		return nil, fmt.Errorf("get API keys directory: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}
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

// Save saves the API keys store to disk, encrypting it first.
//
// This function marshals the Store to JSON, encrypts it using the machine-specific
// key, and writes the encrypted data to the configured API keys file.
//
// The file is created with permissions 0600 (read/write for owner only) to ensure
// API keys are stored securely on disk.
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

	// Encrypt before writing
	encrypted, err := EncryptStore(data)
	if err != nil {
		return fmt.Errorf("failed to encrypt API keys: %w", err)
	}

	return os.WriteFile(path, encrypted, 0600)
}

// Resolve resolves a credential for a provider.
//
// This function attempts to resolve a credential in the following order:
// 1. Environment variable (if envVar is provided and set)
// 2. Stored credential (from the API keys file)
//
// The returned Resolved struct contains the credential value, the source
// ("environment" or "stored"), and metadata about the provider and env var.
//
// This function is useful for applications that want to support both
// environment variables and stored credentials as fallback.
func Resolve(provider, envVar string) (Resolved, error) {
	resolved := Resolved{
		Provider: strings.TrimSpace(provider),
		EnvVar:   strings.TrimSpace(envVar),
	}
	if resolved.EnvVar != "" {
		if value := strings.TrimSpace(os.Getenv(resolved.EnvVar)); value != "" {
			resolved.Value = value
			resolved.Source = "environment"
			return resolved, nil
		}
	}
	store, err := Load()
	if err != nil {
		return Resolved{}, fmt.Errorf("load credentials store: %w", err)
	}
	if value := strings.TrimSpace(store[resolved.Provider]); value != "" {
		resolved.Value = value
		resolved.Source = "stored"
	}
	return resolved, nil
}

// EncryptionStatus describes the current encryption state of the API keys file.
type EncryptionStatus struct {
	Encrypted        bool
	Mode             string // "machine-key", "passphrase", or "plaintext"
	MachineKeyExists bool
}

// CheckEncryptionStatus returns the current encryption status of the API keys file.
//
// This function analyzes the API keys file to determine:
// - Whether the file is encrypted or in plaintext
// - The encryption mode (machine-key, passphrase, or plaintext)
// - Whether a machine key exists on disk
//
// This is useful for displaying status information to users or for
// making decisions about whether encryption needs to be enabled.
func CheckEncryptionStatus() (EncryptionStatus, error) {
	status := EncryptionStatus{}

	path, err := GetAPIKeysPath()
	if err != nil {
		return status, fmt.Errorf("get API keys path: %w", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return status, nil // No file yet
		}
		return status, fmt.Errorf("failed to read API keys file: %w", err)
	}

	if isPlaintextJSON(data) {
		status.Encrypted = false
		status.Mode = "plaintext"
	} else if isEncrypted(data) {
		status.Encrypted = true
		// Try to determine mode
		_, err := loadMachineKey()
		if err == nil {
			status.Mode = "machine-key"
		} else {
			status.Mode = "passphrase"
		}
	}

	keyPath, err := GetMachineKeyPath()
	if err == nil {
		if _, err := os.Stat(keyPath); err == nil {
			status.MachineKeyExists = true
		}
	}

	return status, nil
}
