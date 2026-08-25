// Machine key management and encryption status
package credentials

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"filippo.io/age"
	"github.com/gofrs/flock"
)

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
