package credentials

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"filippo.io/age"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptRoundTrip(t *testing.T) {
	// Create a temporary directory for testing
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Create a test store
	store := Store{
		"openai":     "sk-test123",
		"anthropic":  "sk-anthropic456",
		"gemini":     "sk-gemini789",
	}

	// Marshal to JSON
	jsonData, err := json.MarshalIndent(store, "", "  ")
	require.NoError(t, err)

	// Encrypt
	encrypted, err := EncryptStore(jsonData)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)
	require.False(t, IsPlaintextJSON(encrypted))

	// Decrypt
	decrypted, err := DecryptStore(encrypted)
	require.NoError(t, err)

	// Unmarshal and compare
	var decryptedStore Store
	err = json.Unmarshal(decrypted, &decryptedStore)
	require.NoError(t, err)

	assert.Equal(t, store, decryptedStore)
}

func TestPlaintextDetection(t *testing.T) {
	// Test plaintext JSON detection
	plaintext := []byte(`{"key": "value"}`)
	assert.True(t, IsPlaintextJSON(plaintext))

	// Test null JSON
	nullJSON := []byte(`null`)
	assert.True(t, IsPlaintextJSON(nullJSON))

	// Test whitespace + JSON
	whitespaceJSON := []byte("  {\"key\": \"value\"}  ")
	assert.True(t, IsPlaintextJSON(whitespaceJSON))

	// Test encrypted data (should not be detected as plaintext)
	encryptedMagic := []byte("age-encryption.org/v1")
	assert.False(t, IsPlaintextJSON(encryptedMagic))

	// Test random binary data
	binary := []byte{0x00, 0x01, 0x02, 0x03}
	assert.False(t, IsPlaintextJSON(binary))
}

func TestEncryptionStatus(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Test with no files
	status, err := CheckEncryptionStatus()
	require.NoError(t, err)
	assert.False(t, status.Encrypted)
	assert.Equal(t, "", status.Mode)
	assert.False(t, status.MachineKeyExists)

	// Create a plaintext file
	apiKeysPath := filepath.Join(tmpDir, "api_keys.json")
	err = os.WriteFile(apiKeysPath, []byte(`{"test": "value"}`), 0600)
	require.NoError(t, err)

	status, err = CheckEncryptionStatus()
	require.NoError(t, err)
	assert.False(t, status.Encrypted)
	assert.Equal(t, "plaintext", status.Mode)
	assert.False(t, status.MachineKeyExists)

	// Create a machine key
	identity, err := age.GenerateX25519Identity()
	require.NoError(t, err)
	keyPath := filepath.Join(tmpDir, "key.age")
	err = os.WriteFile(keyPath, []byte(identity.String()), 0600)
	require.NoError(t, err)

	status, err = CheckEncryptionStatus()
	require.NoError(t, err)
	assert.False(t, status.Encrypted)
	assert.Equal(t, "plaintext", status.Mode)
	assert.True(t, status.MachineKeyExists)
}

func TestLoadSaveRoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Create and save a store
	store := Store{
		"provider1": "key1",
		"provider2": "key2",
	}

	err := Save(store)
	require.NoError(t, err)

	// Verify file exists and is encrypted
	apiKeysPath := filepath.Join(tmpDir, "api_keys.json")
	data, err := os.ReadFile(apiKeysPath)
	require.NoError(t, err)
	assert.False(t, IsPlaintextJSON(data))

	// Load and verify
	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, store, loaded)
}

func TestLoadNonExistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Load should return empty store, not error
	store, err := Load()
	require.NoError(t, err)
	assert.Empty(t, store)
}

func TestResolveWithEnvVar(t *testing.T) {
	originalEnv := os.Getenv("TEST_API_KEY")
	defer os.Setenv("TEST_API_KEY", originalEnv)

	os.Setenv("TEST_API_KEY", "env-value")

	resolved, err := Resolve("test", "TEST_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "test", resolved.Provider)
	assert.Equal(t, "TEST_API_KEY", resolved.EnvVar)
	assert.Equal(t, "env-value", resolved.Value)
	assert.Equal(t, "environment", resolved.Source)
}

func TestResolveWithStoredKey(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Create a stored key
	store := Store{"test": "stored-value"}
	err := Save(store)
	require.NoError(t, err)

	// Clear env var
	originalEnv := os.Getenv("TEST_API_KEY")
	defer os.Setenv("TEST_API_KEY", originalEnv)
	os.Unsetenv("TEST_API_KEY")

	resolved, err := Resolve("test", "TEST_API_KEY")
	require.NoError(t, err)
	assert.Equal(t, "test", resolved.Provider)
	assert.Equal(t, "TEST_API_KEY", resolved.EnvVar)
	assert.Equal(t, "stored-value", resolved.Value)
	assert.Equal(t, "stored", resolved.Source)
}

func TestMachineKeyGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Generate a new key
	identity, err := LoadOrCreateMachineKey()
	require.NoError(t, err)
	require.NotNil(t, identity)

	// Verify key file exists
	keyPath := filepath.Join(tmpDir, "key.age")
	_, err = os.Stat(keyPath)
	assert.NoError(t, err)

	// Load the same key
	loadedIdentity, err := loadMachineKey()
	require.NoError(t, err)
	assert.Equal(t, identity.String(), loadedIdentity.String())
}

func TestPassphraseEncryption(t *testing.T) {
	passphrase := "test-passphrase-123"
	plaintext := []byte(`{"key": "value"}`)

	// Encrypt with passphrase
	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)
	require.NotEmpty(t, encrypted)

	// Decrypt with correct passphrase
	decrypted, err := DecryptWithPassphrase(encrypted, passphrase)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	// Decrypt with wrong passphrase should fail
	_, err = DecryptWithPassphrase(encrypted, "wrong-passphrase")
	assert.Error(t, err)
}

func TestConfigDirCreation(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", filepath.Join(tmpDir, "nonexistent"))
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	configDir, err := GetConfigDir()
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(tmpDir, "nonexistent"), configDir)

	// Verify directory was created
	_, err = os.Stat(configDir)
	assert.NoError(t, err)
}

// TestCorruptedKeyFileRegeneration verifies that LoadOrCreateMachineKey
// regenerates a new key when the existing key file is corrupted, rather than
// returning an error.
func TestCorruptedKeyFileRegeneration(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Get the key path
	keyPath, err := GetMachineKeyPath()
	require.NoError(t, err)

	// Write corrupted data to the key file
	err = os.WriteFile(keyPath, []byte("this is not a valid age key"), 0600)
	require.NoError(t, err)

	// LoadOrCreateMachineKey should regenerate the key instead of failing
	identity, err := LoadOrCreateMachineKey()
	require.NoError(t, err)
	require.NotNil(t, identity)

	// Verify the key file now contains valid data
	keyData, err := os.ReadFile(keyPath)
	require.NoError(t, err)

	parsed, err := age.ParseX25519Identity(string(keyData))
	require.NoError(t, err)
	assert.Equal(t, identity.String(), parsed.String())
	assert.NotEqual(t, "this is not a valid age key", string(keyData),
		"key file should have been replaced, not left as-is")
}

// TestConcurrentMachineKeyGeneration verifies that only one machine key
// is generated even when multiple goroutines call LoadOrCreateMachineKey()
// simultaneously. This test ensures the file locking mechanism works correctly.
func TestConcurrentMachineKeyGeneration(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Remove any existing key to ensure we're testing generation
	keyPath := filepath.Join(tmpDir, "key.age")
	os.Remove(keyPath)

	type result struct {
		identity string
		err      error
	}

	// Spawn multiple goroutines to generate keys concurrently
	const numGoroutines = 10
	results := make(chan result, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			identity, err := LoadOrCreateMachineKey()
			if err != nil {
				results <- result{err: err}
				return
			}
			results <- result{identity: identity.String()}
		}()
	}

	// Collect all results
	var identities []string
	var errors []error
	for i := 0; i < numGoroutines; i++ {
		r := <-results
		if r.err != nil {
			errors = append(errors, r.err)
		} else {
			identities = append(identities, r.identity)
		}
	}

	// All goroutines should succeed (the lock timeout could cause some to fail)
	if len(errors) > 0 && len(errors) >= numGoroutines {
		t.Fatalf("all goroutines failed: %v", errors[0])
	}

	// At least some goroutines should have gotten an identity
	if len(identities) == 0 {
		t.Fatalf("no goroutines got an identity, errors: %v", errors)
	}

	// All successful goroutines should have the same identity
	first := identities[0]
	for _, id := range identities[1:] {
		if id != first {
			t.Fatalf("different identities generated concurrently: %q vs %q", first, id)
		}
	}

	// Verify only one key file was created
	_, err := os.Stat(keyPath)
	assert.NoError(t, err)

	// Verify the key file contains valid data
	keyData, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.NotEmpty(t, keyData)

	// Verify the key can be parsed
	loadedIdentity, err := age.ParseX25519Identity(string(keyData))
	require.NoError(t, err)
	assert.Equal(t, first, loadedIdentity.String())
}

// TestDecryptStore_WithPassphraseEnvVar verifies that DecryptStore falls back to
// LEDIT_KEY_PASSPHRASE when machine key decryption fails (e.g., data was
// encrypted with passphrase via `ledit keys encrypt --passphrase`).
func TestDecryptStore_WithPassphraseEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	passphrase := "TestPassphrase123"
	plaintext := []byte(`{"provider": "sk-secret-key"}`)

	// Encrypt with passphrase (no machine key will match this)
	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)

	// Set the env var so DecryptStore can fall back to it
	originalPassphrase := os.Getenv("LEDIT_KEY_PASSPHRASE")
	os.Setenv("LEDIT_KEY_PASSPHRASE", passphrase)
	defer os.Setenv("LEDIT_KEY_PASSPHRASE", originalPassphrase)

	// DecryptStore should succeed via passphrase fallback
	decrypted, err := DecryptStore(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestDecryptStore_WrongPassphraseInEnvVar verifies that DecryptStore returns
// a clear error when both machine key and LEDIT_KEY_PASSPHRASE are tried but fail.
func TestDecryptStore_WrongPassphraseInEnvVar(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	passphrase := "CorrectPassphrase123"
	plaintext := []byte(`{"provider": "sk-secret-key"}`)

	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)

	// Set a WRONG passphrase env var
	originalPassphrase := os.Getenv("LEDIT_KEY_PASSPHRASE")
	os.Setenv("LEDIT_KEY_PASSPHRASE", "WrongPassphrase456")
	defer os.Setenv("LEDIT_KEY_PASSPHRASE", originalPassphrase)

	_, err = DecryptStore(encrypted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decrypt API keys")
	assert.Contains(t, err.Error(), "LEDIT_KEY_PASSPHRASE")
}

// TestDecryptStore_SizeLimit verifies that DecryptStore rejects excessively large files.
func TestDecryptStore_SizeLimit(t *testing.T) {
	// Create a large buffer that exceeds the max encrypted size (10MB data + 10MB overhead = 20MB)
	// Build a valid-looking age header followed by garbage bytes
	header := []byte("age-encryption.org/v1\n")
	largeData := make([]byte, 31<<20) // 32 MB
	copy(largeData, header)

	_, err := DecryptStore(largeData)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypted file too large")
}

// TestDecryptStore_MachineKeyPreferredOverPassphrase verifies that when both
// machine key and passphrase env var are available, machine key is tried first.
func TestDecryptStore_MachineKeyPreferredOverPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Ensure a machine key exists
	identity, err := LoadOrCreateMachineKey()
	require.NoError(t, err)
	require.NotNil(t, identity)

	plaintext := []byte(`{"provider": "machine-encrypted-key"}`)

	// Encrypt with machine key
	encrypted, err := EncryptStore(plaintext)
	require.NoError(t, err)

	// Set a wrong passphrase env var — machine key should still work
	originalPassphrase := os.Getenv("LEDIT_KEY_PASSPHRASE")
	os.Setenv("LEDIT_KEY_PASSPHRASE", "WrongPassphrase999")
	defer os.Setenv("LEDIT_KEY_PASSPHRASE", originalPassphrase)

	decrypted, err := DecryptStore(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

// TestGetEncryptionMode_NoModeFile verifies that GetEncryptionMode returns
// an empty string and no error when no mode file exists (legacy or plaintext files).
func TestGetEncryptionMode_NoModeFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	mode, err := GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "", mode)
}

// TestSetAndGetEncryptionMode verifies that SetEncryptionMode correctly persists
// both "machine-key" and "passphrase" modes, and GetEncryptionMode reads them back.
func TestSetAndGetEncryptionMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	// Set machine-key mode
	err := SetEncryptionMode("machine-key")
	require.NoError(t, err)

	mode, err := GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "machine-key", mode)

	// Overwrite with passphrase mode
	err = SetEncryptionMode("passphrase")
	require.NoError(t, err)

	mode, err = GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "passphrase", mode)

	// Overwrite back to machine-key to verify full round-trip
	err = SetEncryptionMode("machine-key")
	require.NoError(t, err)

	mode, err = GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "machine-key", mode)
}

// TestSetEncryptionMode_InvalidMode verifies that SetEncryptionMode rejects
// values other than "machine-key" or "passphrase" with a descriptive error.
func TestSetEncryptionMode_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	err := SetEncryptionMode("invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid encryption mode")
	assert.Contains(t, err.Error(), "invalid")
}

// TestSave_RespectsPassphraseMode verifies that Save() refuses to write when the
// encryption mode is "passphrase" but LEDIT_KEY_PASSPHRASE is not set.
func TestSave_RespectsPassphraseMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	originalPassphrase := os.Getenv("LEDIT_KEY_PASSPHRASE")
	os.Setenv("LEDIT_KEY_PASSPHRASE", "")
	defer os.Setenv("LEDIT_KEY_PASSPHRASE", originalPassphrase)

	// Set mode to passphrase mode
	err := SetEncryptionMode("passphrase")
	require.NoError(t, err)

	store := Store{"provider": "secret-key"}
	err = Save(store)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "LEDIT_KEY_PASSPHRASE")
}

// TestSave_RespectsMachineKeyMode verifies that when the encryption mode is
// "machine-key", Save() encrypts with the machine key and the result can
// be loaded back via Load().
func TestSave_RespectsMachineKeyMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	// Generate a machine key so it exists in the temp dir
	_, err := LoadOrCreateMachineKey()
	require.NoError(t, err)

	// Set mode to machine-key
	err = SetEncryptionMode("machine-key")
	require.NoError(t, err)

	store := Store{"test-provider": "machine-encrypted-value"}

	err = Save(store)
	require.NoError(t, err)

	// Verify the file was written and is encrypted
	apiKeysPath := filepath.Join(tmpDir, "api_keys.json")
	data, err := os.ReadFile(apiKeysPath)
	require.NoError(t, err)
	assert.False(t, IsPlaintextJSON(data))

	// Verify we can load it back
	loaded, err := Load()
	require.NoError(t, err)
	assert.Equal(t, store, loaded)
}

// TestDecryptStore_SetsMachineKeyMode verifies that DecryptStore writes the
// encryption mode file to "machine-key" after successfully decrypting with
// the machine key.
func TestDecryptStore_SetsMachineKeyMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	// Ensure a machine key exists
	_, err := LoadOrCreateMachineKey()
	require.NoError(t, err)

	plaintext := []byte(`{"provider": "machine-secret"}`)
	encrypted, err := EncryptStore(plaintext)
	require.NoError(t, err)

	// Make sure no mode file exists yet
	mode, err := GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "", mode, "mode file should not exist before decrypt")

	// DecryptStore should set the mode file
	decrypted, err := DecryptStore(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	mode, err = GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "machine-key", mode)
}

// TestDecryptStore_SetsPassphraseMode verifies that DecryptStore writes the
// encryption mode file to "passphrase" after successfully decrypting with
// the LEDIT_KEY_PASSPHRASE environment variable.
func TestDecryptStore_SetsPassphraseMode(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	passphrase := "DecryptPassphraseTest99"
	plaintext := []byte(`{"provider": "passphrase-secret"}`)

	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)

	// Set passphrase env var so DecryptStore can use it
	t.Setenv("LEDIT_KEY_PASSPHRASE", passphrase)

	// Make sure no mode file exists yet
	mode, err := GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "", mode, "mode file should not exist before decrypt")

	decrypted, err := DecryptStore(encrypted)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)

	mode, err = GetEncryptionMode()
	require.NoError(t, err)
	assert.Equal(t, "passphrase", mode)
}

// TestCheckEncryptionStatus_UsesModeFile verifies that CheckEncryptionStatus
// reads the mode file when it exists, even in the presence of a machine key.
// This ensures the mode file takes priority over the legacy heuristic.
func TestCheckEncryptionStatus_UsesModeFile(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)

	passphrase := "StatusTestPassphrase123"
	plaintext := []byte(`{"provider": "test-key"}`)

	// Create real encrypted data
	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)

	// Write encrypted data to the api_keys file
	apiKeysPath := filepath.Join(tmpDir, "api_keys.json")
	err = os.WriteFile(apiKeysPath, encrypted, 0600)
	require.NoError(t, err)

	// Set mode file to "passphrase"
	err = SetEncryptionMode("passphrase")
	require.NoError(t, err)

	// Also create a machine key (legacy heuristic would report "machine-key")
	_, err = LoadOrCreateMachineKey()
	require.NoError(t, err)

	status, err := CheckEncryptionStatus()
	require.NoError(t, err)
	assert.True(t, status.Encrypted)
	// Mode file should take priority over the legacy heuristic
	assert.Equal(t, "passphrase", status.Mode)
	assert.True(t, status.MachineKeyExists)
}

// TestDecryptWithPassphrase_SizeLimit verifies that DecryptWithPassphrase rejects
// excessively large input without attempting decryption.
func TestDecryptWithPassphrase_SizeLimit(t *testing.T) {
	// Create a buffer that exceeds MaxEncryptedSize (20 MB)
	header := []byte("age-encryption.org/v1\n")
	largeData := make([]byte, 31<<20) // 32 MB
	copy(largeData, header)

	_, err := DecryptWithPassphrase(largeData, "some-passphrase")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "encrypted file too large")
}

// TestDecryptStore_NoMachineKeyNoPassphrase verifies clear error when neither
// machine key nor passphrase is available.
func TestDecryptStore_NoMachineKeyNoPassphrase(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	passphrase := "TestPass123"
	plaintext := []byte(`{"provider": "sk-key"}`)
	encrypted, err := EncryptWithPassphrase(plaintext, passphrase)
	require.NoError(t, err)

	// Clear passphrase env var
	originalPassphrase := os.Getenv("LEDIT_KEY_PASSPHRASE")
	os.Setenv("LEDIT_KEY_PASSPHRASE", "")
	defer os.Setenv("LEDIT_KEY_PASSPHRASE", originalPassphrase)

	// No machine key exists (no key.age file), no passphrase — should fail with guidance
	_, err = DecryptStore(encrypted)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "machine key not found")
	assert.Contains(t, err.Error(), "LEDIT_KEY_PASSPHRASE")
}
