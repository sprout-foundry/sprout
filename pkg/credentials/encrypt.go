package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/gofrs/flock"
)

// isPlaintextJSON checks if the data is plaintext JSON (legacy format).
func isPlaintextJSON(data []byte) bool {
	trimmed := bytes.TrimSpace(data)
	if !bytes.HasPrefix(trimmed, []byte("{")) && !bytes.HasPrefix(trimmed, []byte("null")) {
		return false
	}
	var v json.RawMessage
	return json.Unmarshal(trimmed, &v) == nil
}

// isEncrypted checks if the data is encrypted with age.
func isEncrypted(data []byte) bool {
	return bytes.HasPrefix(bytes.TrimSpace(data), []byte(encryptedMagic))
}

// LoadOrCreateMachineKey loads the machine key from disk or generates a new one.
// Uses flock-based locking to prevent race conditions when multiple processes try to generate the key concurrently.
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
		// If parsing fails, the key file is corrupted - return error
		return nil, fmt.Errorf("machine key file exists but is corrupted: %w", err)
	}

	// Use flock for proper file locking that survives process death
	fileLock := flock.New(keyPath + ".lock")
	locked, err := fileLock.TryLockContext(context.Background(), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for key generation: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("timed out waiting for machine key lock - another process may be generating it")
	}
	defer fileLock.Unlock()

	// Double-check: another process may have created the key while we were waiting for the lock
	if data, err := os.ReadFile(keyPath); err == nil {
		identity, err := parseKeyFile(data)
		if err == nil {
			return identity, nil
		}
		return nil, fmt.Errorf("machine key file exists but is corrupted: %w", err)
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
	// Try raw format first
	identity, err := age.ParseX25519Identity(string(data))
	if err == nil {
		return identity, nil
	}

	// Try armored format via generic ParseIdentities
	identities, err := age.ParseIdentities(bytes.NewReader(data))
	if err == nil && len(identities) > 0 {
		if id, ok := identities[0].(*age.X25519Identity); ok {
			return id, nil
		}
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
//
// Maximum decrypted size is limited to 10 MB to prevent memory exhaustion attacks.
const maxDecryptedSize = 10 << 20 // 10 MB

func DecryptStore(data []byte) ([]byte, error) {
	// Check if already plaintext JSON (legacy format)
	if isPlaintextJSON(data) {
		return data, nil
	}

	// Must be encrypted, try with machine key
	identity, err := loadMachineKey()
	if err != nil {
		// Provide actionable guidance when machine key is missing
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("machine key not found. Run 'ledit keys migrate' to generate a new machine key, "+
				"or set LEDIT_PASSPHRASE environment variable to decrypt with passphrase: %w", err)
		}
		return nil, fmt.Errorf("failed to load machine key: %w", err)
	}

	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt with machine key: %w", err)
	}

	return io.ReadAll(io.LimitReader(r, maxDecryptedSize))
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
//
// Maximum decrypted size is limited to 10 MB to prevent memory exhaustion attacks.
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

	return io.ReadAll(io.LimitReader(r, maxDecryptedSize))
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
// API keys are stored securely on disk. The write is atomic (using a temp file + rename)
// to prevent data corruption if the process crashes during the write.
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

	// Write to a unique temp file in the same directory (ensures same filesystem for atomic rename).
	// Using a unique name prevents race conditions when multiple processes call Save() concurrently.
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, ".api_keys-*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp file: %w", err)
	}
	tmpPath := tmpFile.Name()
	// Tighten permissions to 0600 before writing secrets.
	// os.CreateTemp respects umask, so we must chmod explicitly
	// to ensure the file is never world-readable with encrypted data in it.
	if err := os.Chmod(tmpPath, 0600); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to set permissions on temp file: %w", err)
	}
	defer os.Remove(tmpPath) // no-op after successful rename; cleans up on any error path

	if _, err := tmpFile.Write(encrypted); err != nil {
		tmpFile.Close()
		return fmt.Errorf("failed to write temp file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp file: %w", err)
	}

	// Atomically replace the original
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to replace API keys file: %w", err)
	}

	return nil
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
// Note: The Mode field is a best-effort heuristic. It cannot definitively distinguish
// between passphrase-encrypted and foreign-encrypted data without attempting decryption.
// If a machine key exists, it reports "machine-key" as the likely mode, but this may
// be incorrect if the data was encrypted with a different key.
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
		// Try to determine mode - this is a best-effort heuristic
		_, err := loadMachineKey()
		if err == nil {
			status.Mode = "machine-key" // Likely, but not certain
		} else {
			status.Mode = "passphrase" // Likely, but not certain
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
