// Encryption and decryption operations
package credentials

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"

	"filippo.io/age"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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

// MaxDecryptedSize is the maximum size of decrypted API keys data (10 MB).
// This limit prevents memory exhaustion attacks from crafted encrypted files.
const MaxDecryptedSize = 10 << 20 // 10 MB

// MaxEncryptedSize is the maximum size of encrypted API keys data (20 MB).
// This limit prevents memory exhaustion attacks from crafted encrypted files.
// It accounts for the MaxDecryptedSize plus age encryption overhead (~10 MB).
const MaxEncryptedSize = MaxDecryptedSize + (10 << 20) // 20 MB

// EncryptStore encrypts plaintext data using the machine-specific X25519 key.
//
// This function ensures the machine key exists (generating it if necessary),
// then encrypts the provided plaintext using age encryption. The encrypted
// output is returned as a byte slice.
//
// Use this function when you want to encrypt data with the machine-specific
// key that is stored in ~/.config/sprout/key.age.
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

	// Detect encrypted files early to provide a clear message if decryption
	// ultimately fails (instead of falling through and producing a confusing
	// JSON parse error about "invalid character 'a'" from the age header).
	encrypted := isEncrypted(data)

	// Try machine key first
	identity, err := loadMachineKey()
	if err == nil {
		r, err := age.Decrypt(bytes.NewReader(data), identity)
		if err == nil {
			decrypted, readErr := io.ReadAll(io.LimitReader(r, MaxDecryptedSize))
			if readErr != nil {
				return nil, fmt.Errorf("failed to read decrypted data: %w", readErr)
			}
			if !IsPlaintextJSON(decrypted) {
				return nil, fmt.Errorf("decrypted data is not valid JSON — the machine key may be wrong (was key.age regenerated?). " +
					"Run 'sprout keys migrate' to re-encrypt with the current machine key")
			}
			return decrypted, nil
		}
		// Machine key decryption failed — data may be passphrase-encrypted
		// or the key file may have been regenerated after corruption
	}

	// Fallback: try environment passphrase
	if passphrase := strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")); passphrase != "" {
		decrypted, passErr := DecryptWithPassphrase(data, passphrase)
		if passErr == nil {
			if !IsPlaintextJSON(decrypted) {
				return nil, fmt.Errorf("decrypted data is not valid JSON — the passphrase may be incorrect")
			}
			return decrypted, nil
		}
		// Passphrase decryption also failed
		return nil, fmt.Errorf("failed to decrypt API keys (tried machine key and SPROUT_KEY_PASSPHRASE): %w", passErr)
	}

	// Neither worked — provide actionable guidance
	if !encrypted {
		return nil, fmt.Errorf("API keys file is not valid JSON and not encrypted — file may be corrupted. "+
			"Restore from backup or delete %s to start fresh", "api_keys.json")
	}

	if os.IsNotExist(err) || identity == nil {
		return nil, fmt.Errorf("API keys file is encrypted (age format) but no decryption key is available.\n"+
			"This usually means you need to update your sprout binary to a version that supports encryption,\n"+
			"or the machine key file (key.age) was deleted.\n\n"+
			"Recovery options:\n"+
			"  1. Update sprout to the latest version if you're running an older build\n"+
			"  2. Set SPROUT_KEY_PASSPHRASE=<your-passphrase> if you previously used passphrase encryption\n"+
			"  3. Run 'sprout keys migrate' to generate a new machine key (existing encrypted keys will be lost): %w", err)
	}
	return nil, fmt.Errorf("API keys file is encrypted but decryption with the machine key failed.\n"+
		"The machine key (key.age) may have been regenerated, making the old encrypted data unreadable.\n\n"+
		"Recovery options:\n"+
		"  1. If you have a backup of api_keys.json from before encryption, restore it\n"+
		"  2. Set SPROUT_KEY_PASSPHRASE if the file was passphrase-encrypted\n"+
		"  3. Delete api_keys.json and re-enter your API keys: %w", err)
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
