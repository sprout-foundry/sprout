package credentials

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"filippo.io/age"
	"github.com/gofrs/flock"
)

// IsPlaintextJSON checks if the data is plaintext JSON (legacy unencrypted format).
func IsPlaintextJSON(data []byte) bool {
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

	// Track whether we already warned about corruption on the fast path,
	// so the double-check inside the lock doesn't produce a duplicate warning
	// for the single-process (most common) case.
	warned := false

	// Try to load existing key first (fast path for most cases)
	if data, err := os.ReadFile(keyPath); err == nil {
		identity, err := parseKeyFile(data)
		if err == nil {
			return identity, nil
		}
		// Key file is corrupted (possibly partially written by a prior process).
		// Fall through to the lock-and-regenerate path rather than failing.
		log.Printf("[WARN] Machine key file is corrupted and will be regenerated. " +
			"Previously encrypted API keys may no longer be recoverable.")
		warned = true
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

	// Double-check: another process may have created (or corrupted) the key
	// while we were waiting for the lock.
	if data, err := os.ReadFile(keyPath); err == nil {
		identity, err := parseKeyFile(data)
		if err == nil {
			return identity, nil
		}
		// Key file is corrupted (possibly partially written). Since we hold the lock,
		// we are the authoritative writer — regenerate the key below.
		if !warned {
			log.Printf("[WARN] Machine key file is corrupted and will be regenerated. " +
				"Previously encrypted API keys may no longer be recoverable.")
		}
	}

	// Generate new key
	identity, err := age.GenerateX25519Identity()
	if err != nil {
		return nil, fmt.Errorf("failed to generate machine key: %w", err)
	}

	// Write key to disk atomically using AtomicWriteFile.
	// This prevents a partially-written key file on crash/signal/power loss.
	keyData, err := serializeKeyFile(identity)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize machine key: %w", err)
	}

	if err := AtomicWriteFile(keyPath, keyData, 0600); err != nil {
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
// MaxDecryptedSize is the maximum size of decrypted API keys data (10 MB).
// This limit prevents memory exhaustion attacks from crafted encrypted files.
const MaxDecryptedSize = 10 << 20 // 10 MB

// MaxEncryptedSize is the maximum size of encrypted API keys data (20 MB).
// This limit prevents memory exhaustion attacks from crafted encrypted files.
// It accounts for the MaxDecryptedSize plus age encryption overhead (~10 MB).
const MaxEncryptedSize = MaxDecryptedSize + (10 << 20) // 20 MB

func DecryptStore(data []byte) ([]byte, error) {
	// Check if already plaintext JSON (legacy format)
	if IsPlaintextJSON(data) {
		if len(data) > MaxDecryptedSize {
			return nil, fmt.Errorf("API keys file too large (%d bytes, max %d)", len(data), MaxDecryptedSize)
		}
		return data, nil
	}

	// Sanity check: reject excessively large encrypted files before reading
	// age format overhead: ~100 bytes header + per-chunk framing ≈ ~1KB
	if len(data) > MaxEncryptedSize {
		return nil, fmt.Errorf("encrypted file too large (%d bytes, max %d)", len(data), MaxEncryptedSize)
	}

	// Try machine key first
	identity, err := loadMachineKey()
	if err == nil {
		r, err := age.Decrypt(bytes.NewReader(data), identity)
		if err == nil {
			return io.ReadAll(io.LimitReader(r, MaxDecryptedSize))
		}
		// Machine key decryption failed — data may be passphrase-encrypted
		// or the key file may have been regenerated after corruption
	}

	// Fallback: try environment passphrase
	if passphrase := strings.TrimSpace(os.Getenv("LEDIT_KEY_PASSPHRASE")); passphrase != "" {
		decrypted, passErr := DecryptWithPassphrase(data, passphrase)
		if passErr == nil {
			return decrypted, nil
		}
		// Passphrase decryption also failed
		return nil, fmt.Errorf("failed to decrypt API keys (tried machine key and LEDIT_KEY_PASSPHRASE): %w", passErr)
	}

	// Neither worked — provide actionable guidance
	if os.IsNotExist(err) || identity == nil {
		return nil, fmt.Errorf("machine key not found and LEDIT_KEY_PASSPHRASE not set. "+
			"Recovery options:\n"+
			"  1. Run 'ledit keys migrate' to generate a new machine key (existing encrypted keys will be lost)\n"+
			"  2. Set LEDIT_KEY_PASSPHRASE=<your-passphrase> if you previously encrypted with a passphrase: %w", err)
	}
	return nil, fmt.Errorf("failed to decrypt API keys with machine key (file may be corrupted, or key.age was regenerated). "+
		"Set LEDIT_KEY_PASSPHRASE if the file was passphrase-encrypted: %w", err)
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
	// Sanity check: reject excessively large encrypted files before reading
	if len(data) > MaxEncryptedSize {
		return nil, fmt.Errorf("encrypted file too large (%d bytes, max %d)", len(data), MaxEncryptedSize)
	}

	identity, err := age.NewScryptIdentity(passphrase)
	if err != nil {
		return nil, fmt.Errorf("failed to create scrypt identity: %w", err)
	}

	identity.SetMaxWorkFactor(15) // Don't accept very high work factors

	r, err := age.Decrypt(bytes.NewReader(data), identity)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt with passphrase: %w", err)
	}

	return io.ReadAll(io.LimitReader(r, MaxDecryptedSize))
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
	locked, err := fileLock.TryRLockContext(context.Background(), 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("failed to acquire lock for load: %w", err)
	}
	if !locked {
		return nil, fmt.Errorf("timed out waiting for load lock - another process may be saving")
	}
	defer fileLock.Unlock()

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
// This function marshals the Store to JSON, encrypts it using the appropriate
// encryption mode (machine-key or passphrase), and writes the encrypted data
// to the configured API keys file.
//
// The file is created with permissions 0600 (read/write for owner only) to ensure
// API keys are stored securely on disk. The write is atomic (using a temp file + rename)
// to prevent data corruption if the process crashes during the write.
//
// If the API keys are passphrase-encrypted, this function requires the
// LEDIT_KEY_PASSPHRASE environment variable to be set. Otherwise, it returns
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
		passphrase := strings.TrimSpace(os.Getenv("LEDIT_KEY_PASSPHRASE"))
		if passphrase == "" {
			return fmt.Errorf("cannot save: API keys are passphrase-encrypted but LEDIT_KEY_PASSPHRASE is not set. "+
				"Set LEDIT_KEY_PASSPHRASE or run 'ledit keys encrypt' to switch to machine-key mode")
		}
		encrypted, err = EncryptWithPassphrase(data, passphrase)
	} else if mode == "" && strings.TrimSpace(os.Getenv("LEDIT_KEY_PASSPHRASE")) != "" {
		// Legacy passphrase-encrypted file with no mode file: the user has
		// LEDIT_KEY_PASSPHRASE set (required to have loaded the file), so
		// preserve their passphrase encryption rather than silently downgrading
		// to machine-key mode.
		encrypted, err = EncryptWithPassphrase(data, strings.TrimSpace(os.Getenv("LEDIT_KEY_PASSPHRASE")))
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
	locked, err := fileLock.TryLockContext(context.Background(), 5*time.Second)
	if err != nil {
		return fmt.Errorf("failed to acquire lock for save: %w", err)
	}
	if !locked {
		return fmt.Errorf("timed out waiting for save lock - another process may be saving")
	}
	defer fileLock.Unlock()

	// Write atomically
	if err := AtomicWriteFile(path, encrypted, 0600); err != nil {
		return err
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

	if IsPlaintextJSON(data) {
		status.Encrypted = false
		status.Mode = "plaintext"
	} else if isEncrypted(data) {
		status.Encrypted = true
		// Use the mode file as the primary signal
		mode, _ := GetEncryptionMode()
		if mode != "" {
			status.Mode = mode
		} else {
			// Legacy fallback: heuristic for files without a mode file
			_, err := loadMachineKey()
			if err == nil {
				status.Mode = "machine-key"
			} else {
				status.Mode = "passphrase"
			}
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
