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

// LoadStoreFromDir reads and decrypts the file store under an explicit config
// root, decrypting with the machine key stored in that root (not the
// process's ambient one). It is the read-side counterpart of SetForDir and
// LoadFromDir: the ambient Load() resolves the config root from the
// environment, so a process that does not run inside the same scope (for
// example a daemon that a workspace-scoped `sprout login` did not start)
// cannot read another scope's store through it.
//
// A missing store file is not an error: an empty Store is returned, matching
// Load()'s contract.
func LoadStoreFromDir(configDir string) (Store, error) {
	if configDir == "" {
		return Store{}, fmt.Errorf("config directory is required")
	}
	path, err := GetAPIKeysPathFromDir(configDir)
	if err != nil {
		return nil, fmt.Errorf("get API keys path: %w", err)
	}
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return Store{}, nil
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read api keys file: %w", err)
	}

	decrypted, err := decryptStoreFromDir(data, configDir)
	if err != nil {
		return nil, fmt.Errorf("decrypt api keys from %s: %w", configDir, err)
	}

	var store Store
	if err := json.Unmarshal(decrypted, &store); err != nil {
		return nil, fmt.Errorf("parse api keys: %w", err)
	}
	if store == nil {
		store = Store{}
	}
	return store, nil
}

// decryptStoreFromDir decrypts api_keys.json content for the given config
// directory using that directory's machine key (key.age). Plaintext JSON is
// passed through, and a passphrase-encrypted file falls back to
// SPROUT_KEY_PASSPHRASE, mirroring DecryptStore's precedence.
func decryptStoreFromDir(data []byte, configDir string) ([]byte, error) {
	if IsPlaintextJSON(data) {
		if len(data) > MaxDecryptedSize {
			return nil, fmt.Errorf("API keys file too large (%d bytes, max %d)", len(data), MaxDecryptedSize)
		}
		return data, nil
	}
	if len(data) > MaxEncryptedSize {
		return nil, fmt.Errorf("encrypted API keys file too large (%d bytes, max %d)", len(data), MaxEncryptedSize)
	}

	keyPath, err := GetMachineKeyPathFromDir(configDir)
	if err != nil {
		return nil, err
	}
	keyErr := fmt.Errorf("no machine key at %s", keyPath)
	if raw, readErr := os.ReadFile(keyPath); readErr == nil {
		if identity, parseErr := parseKeyFile(raw); parseErr == nil {
			if r, decErr := age.Decrypt(bytes.NewReader(data), identity); decErr == nil {
				decrypted, readAllErr := io.ReadAll(io.LimitReader(r, MaxDecryptedSize))
				if readAllErr != nil {
					return nil, fmt.Errorf("read decrypted data: %w", readAllErr)
				}
				if !IsPlaintextJSON(decrypted) {
					return nil, fmt.Errorf("decrypted data is not valid JSON — the machine key may be wrong (was key.age regenerated?)")
				}
				return decrypted, nil
			} else {
				keyErr = fmt.Errorf("decrypt with machine key: %w", decErr)
			}
		} else {
			keyErr = fmt.Errorf("parse machine key %s: %w", keyPath, parseErr)
		}
	} else if !os.IsNotExist(readErr) {
		return nil, fmt.Errorf("read machine key %s: %w", keyPath, readErr)
	}

	// Machine key unavailable or failed — fall back to the environment
	// passphrase, mirroring DecryptStore's precedence.
	if passphrase := strings.TrimSpace(envutil.GetEnvSimple("KEY_PASSPHRASE")); passphrase != "" {
		decrypted, passErr := DecryptWithPassphrase(data, passphrase)
		if passErr == nil {
			if !IsPlaintextJSON(decrypted) {
				return nil, fmt.Errorf("decrypted data is not valid JSON — the passphrase may be incorrect")
			}
			return decrypted, nil
		}
		return nil, fmt.Errorf("failed to decrypt API keys from %s (tried machine key and SPROUT_KEY_PASSPHRASE): machine key: %v; passphrase: %w",
			configDir, keyErr, passErr)
	}

	if !isEncrypted(data) {
		return nil, fmt.Errorf("API keys file is not valid JSON and is not encrypted — file may be corrupted")
	}
	return nil, fmt.Errorf("API keys file is encrypted but no decryption key is available in %s: %v. "+
		"Set SPROUT_KEY_PASSPHRASE if the file is passphrase-encrypted, or restore key.age", configDir, keyErr)
}
