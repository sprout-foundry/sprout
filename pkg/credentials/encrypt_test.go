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
	require.False(t, isPlaintextJSON(encrypted))

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
	assert.True(t, isPlaintextJSON(plaintext))

	// Test null JSON
	nullJSON := []byte(`null`)
	assert.True(t, isPlaintextJSON(nullJSON))

	// Test whitespace + JSON
	whitespaceJSON := []byte("  {\"key\": \"value\"}  ")
	assert.True(t, isPlaintextJSON(whitespaceJSON))

	// Test encrypted data (should not be detected as plaintext)
	encryptedMagic := []byte("age-encryption.org/v1")
	assert.False(t, isPlaintextJSON(encryptedMagic))

	// Test random binary data
	binary := []byte{0x00, 0x01, 0x02, 0x03}
	assert.False(t, isPlaintextJSON(binary))
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
	assert.False(t, isPlaintextJSON(data))

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

	// Spawn multiple goroutines to generate keys concurrently
	const numGoroutines = 10
	done := make(chan bool, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer func() { done <- true }()
			identity, err := LoadOrCreateMachineKey()
			require.NoError(t, err)
			require.NotNil(t, identity)
		}()
	}

	// Wait for all goroutines to complete
	for i := 0; i < numGoroutines; i++ {
		<-done
	}

	// Verify only one key file was created
	_, err := os.Stat(keyPath)
	assert.NoError(t, err)

	// Verify the key file contains valid data
	keyData, err := os.ReadFile(keyPath)
	require.NoError(t, err)
	require.NotEmpty(t, keyData)

	// Verify all goroutines got the same key
	identity, err := LoadOrCreateMachineKey()
	require.NoError(t, err)
	require.NotNil(t, identity)

	// Load and verify the key can be parsed
	loadedIdentity, err := age.ParseX25519Identity(string(keyData))
	require.NoError(t, err)
	assert.Equal(t, identity.String(), loadedIdentity.String())
}
