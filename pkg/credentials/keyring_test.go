package credentials

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zalando/go-keyring"
)

// TestOSKeyringBackend_SetGetDelete tests full round-trip using MockInit
func TestOSKeyringBackend_SetGetDelete(t *testing.T) {
	// Initialize mock keyring (in-memory store)
	keyring.MockInit()
	backend := NewOSKeyringBackend()

	// Test Set
	err := backend.Set("test-provider", "test-secret-value")
	require.NoError(t, err)

	// Test Get
	value, err := backend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-secret-value", value)

	// Test Delete
	err = backend.Delete("test-provider")
	require.NoError(t, err)

	// Verify deleted
	value, err = backend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

// TestOSKeyringBackend_GetNotFound returns empty, no error
func TestOSKeyringBackend_GetNotFound(t *testing.T) {
	keyring.MockInit()
	

	backend := NewOSKeyringBackend()

	value, err := backend.Get("non-existent-provider")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

// TestOSKeyringBackend_DeleteNotFound should not error
func TestOSKeyringBackend_DeleteNotFound(t *testing.T) {
	keyring.MockInit()
	

	backend := NewOSKeyringBackend()

	err := backend.Delete("non-existent-provider")
	require.NoError(t, err)
}

// TestOSKeyringBackend_EmptyProvider returns error
func TestOSKeyringBackend_EmptyProvider(t *testing.T) {
	keyring.MockInit()
	

	backend := NewOSKeyringBackend()

	// Empty provider should error
	err := backend.Set("", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")

	_, err = backend.Get("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")

	err = backend.Delete("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")
}

// TestFileBackend_SetGetDelete tests full round-trip using temp dir
func TestFileBackend_SetGetDelete(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	backend := NewFileBackend()

	// Test Set
	err := backend.Set("test-provider", "test-secret-value")
	require.NoError(t, err)

	// Test Get
	value, err := backend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-secret-value", value)

	// Test Delete
	err = backend.Delete("test-provider")
	require.NoError(t, err)

	// Verify deleted
	value, err = backend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

// TestFileBackend_GetNotFound returns empty, no error
func TestFileBackend_GetNotFound(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	backend := NewFileBackend()

	value, err := backend.Get("non-existent-provider")
	require.NoError(t, err)
	assert.Equal(t, "", value)
}

// TestFileBackend_EmptyProvider returns error
func TestFileBackend_EmptyProvider(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	backend := NewFileBackend()

	// Empty provider should error
	err := backend.Set("", "value")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")

	_, err = backend.Get("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")

	err = backend.Delete("")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "provider name cannot be empty")
}

// TestGetStorageBackend_AutoDetect with mocked keyring should auto-detect keyring
func TestGetStorageBackend_AutoDetect(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear any persisted mode
	os.Remove(filepath.Join(tmpDir, "backend.mode"))

	// Mock keyring is available
	keyring.MockInit()
	

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	backend, err := GetStorageBackend()
	require.NoError(t, err)

	// Should auto-detect and use keyring
	_, ok := backend.(*OSKeyringBackend)
	assert.True(t, ok, "expected OSKeyringBackend after auto-detect")

	// Should have persisted the mode
	mode, err := GetStorageMode()
	require.NoError(t, err)
	assert.Equal(t, "keyring", mode)
}

// TestGetStorageBackend_ForceFile LEDIT_CREDENTIAL_BACKEND=file should return FileBackend
func TestGetStorageBackend_ForceFile(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Force file backend via env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	backend, err := GetStorageBackend()
	require.NoError(t, err)

	_, ok := backend.(*FileBackend)
	assert.True(t, ok, "expected FileBackend when forced via env var")
}

// TestGetStorageBackend_ForceKeyring LEDIT_CREDENTIAL_BACKEND=keyring should return OSKeyringBackend
func TestGetStorageBackend_ForceKeyring(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Force keyring backend via env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "keyring")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	backend, err := GetStorageBackend()
	require.NoError(t, err)

	_, ok := backend.(*OSKeyringBackend)
	assert.True(t, ok, "expected OSKeyringBackend when forced via env var")
}

// TestGetStorageBackend_PersistedMode uses persisted mode
func TestGetStorageBackend_PersistedMode(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Set persisted mode to file
	err := SetStorageMode("file")
	require.NoError(t, err)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	backend, err := GetStorageBackend()
	require.NoError(t, err)

	_, ok := backend.(*FileBackend)
	assert.True(t, ok, "expected FileBackend from persisted mode")
}

// TestMigrateFileToKeyring set up file with keys, migrate to keyring, verify keyring has them
func TestMigrateFileToKeyring(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Set up file backend with some keys
	fileBackend := NewFileBackend()
	err := fileBackend.Set("openai", "sk-openai-123")
	require.NoError(t, err)
	err = fileBackend.Set("anthropic", "sk-anthropic-456")
	require.NoError(t, err)

	// Initialize mock keyring
	keyring.MockInit()
	

	// Migrate to keyring
	migrated, err := MigrateFileToKeyring(false) // Don't clear file
	require.NoError(t, err)
	assert.Len(t, migrated, 2)
	assert.Contains(t, migrated, "openai")
	assert.Contains(t, migrated, "anthropic")

	// Verify keyring has the keys
	keyringBackend := NewOSKeyringBackend()
	value, err := keyringBackend.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-123", value)

	value, err = keyringBackend.Get("anthropic")
	require.NoError(t, err)
	assert.Equal(t, "sk-anthropic-456", value)

	// Verify tracking file exists
	providers, err := getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Contains(t, providers, "openai")
	assert.Contains(t, providers, "anthropic")
}

// TestMigrateKeyringToFile reverse migration
func TestMigrateKeyringToFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Initialize mock keyring and set some keys
	keyring.MockInit()
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("openai", "sk-openai-123")
	require.NoError(t, err)
	err = keyringBackend.Set("anthropic", "sk-anthropic-456")
	require.NoError(t, err)

	// Manually add to tracking (since mock keyring doesn't auto-track)
	err = addTrackedProvider("openai")
	require.NoError(t, err)
	err = addTrackedProvider("anthropic")
	require.NoError(t, err)

	// Migrate to file
	migrated, err := MigrateKeyringToFile(false) // Don't clear keyring
	require.NoError(t, err)
	assert.Len(t, migrated, 2)
	assert.Contains(t, migrated, "openai")
	assert.Contains(t, migrated, "anthropic")

	// Verify file has the keys
	fileBackend := NewFileBackend()
	value, err := fileBackend.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-123", value)

	value, err = fileBackend.Get("anthropic")
	require.NoError(t, err)
	assert.Equal(t, "sk-anthropic-456", value)
}

// TestKeyringProviderTracking add/remove/list tracked providers
func TestKeyringProviderTracking(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	keyring.MockInit()
	

	// Initially empty
	providers, err := getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Empty(t, providers)

	// Add providers
	err = addTrackedProvider("openai")
	require.NoError(t, err)
	err = addTrackedProvider("anthropic")
	require.NoError(t, err)

	providers, err = getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 2)
	assert.Contains(t, providers, "openai")
	assert.Contains(t, providers, "anthropic")

	// Add duplicate (should not duplicate)
	err = addTrackedProvider("openai")
	require.NoError(t, err)

	providers, err = getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 2)

	// Remove one provider
	err = removeTrackedProvider("anthropic")
	require.NoError(t, err)

	providers, err = getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 1)
	assert.Contains(t, providers, "openai")
	assert.NotContains(t, providers, "anthropic")

	// Remove non-existent (should not error)
	err = removeTrackedProvider("non-existent")
	require.NoError(t, err)

	providers, err = getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Len(t, providers, 1)
}

// TestResolve_WithKeyringBackend when keyring backend is active, Resolve should check keyring
func TestResolve_WithKeyringBackend(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set key in keyring
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "keyring-value")
	require.NoError(t, err)

	// Clear file store
	err = Save(Store{})
	require.NoError(t, err)

	// Resolve should find it in keyring
	resolved, err := Resolve("test-provider", "")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", resolved.Provider)
	assert.Equal(t, "keyring-value", resolved.Value)
	assert.Equal(t, "keyring", resolved.Source)
}

// TestResolve_EnvironmentPriorityOverKeyring env var should still take priority over keyring
func TestResolve_EnvironmentPriorityOverKeyring(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	keyring.MockInit()
	

	// Set key in keyring
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "keyring-value")
	require.NoError(t, err)

	// Set env var with different value
	originalEnv := os.Getenv("TEST_PROVIDER_KEY")
	os.Setenv("TEST_PROVIDER_KEY", "env-value")
	defer os.Setenv("TEST_PROVIDER_KEY", originalEnv)

	// Resolve should prefer env var
	resolved, err := Resolve("test-provider", "TEST_PROVIDER_KEY")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", resolved.Provider)
	assert.Equal(t, "env-value", resolved.Value)
	assert.Equal(t, "environment", resolved.Source)
}

// TestResolve_KeyringPriorityOverFile keyring should be checked before file store
func TestResolve_KeyringPriorityOverFile(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set different values in keyring and file
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "keyring-value")
	require.NoError(t, err)

	fileBackend := NewFileBackend()
	err = fileBackend.Set("test-provider", "file-value")
	require.NoError(t, err)

	// Resolve should prefer keyring (active backend)
	resolved, err := Resolve("test-provider", "")
	require.NoError(t, err)
	assert.Equal(t, "keyring-value", resolved.Value)
	assert.Equal(t, "keyring", resolved.Source)
}

// TestResolve_NoCredential returns empty value
func TestResolve_NoCredential(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// No credentials anywhere
	resolved, err := Resolve("non-existent-provider", "")
	require.NoError(t, err)
	assert.Equal(t, "non-existent-provider", resolved.Provider)
	assert.Equal(t, "", resolved.Value)
	assert.Equal(t, "", resolved.Source)
}

// TestGetFromActiveBackend gets credential using active backend
func TestGetFromActiveBackend(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set credential in keyring
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "test-value")
	require.NoError(t, err)

	// Get from active backend
	value, source, err := GetFromActiveBackend("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-value", value)
	assert.Equal(t, "keyring", source)
}

// TestSetToActiveBackend sets credential using active backend
func TestSetToActiveBackend(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set credential using active backend
	err := SetToActiveBackend("test-provider", "test-value")
	require.NoError(t, err)

	// Verify it was set in keyring
	keyringBackend := NewOSKeyringBackend()
	value, err := keyringBackend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "test-value", value)

	// Verify tracking
	providers, err := getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.Contains(t, providers, "test-provider")
}

// TestDeleteFromActiveBackend deletes credential using active backend
func TestDeleteFromActiveBackend(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set credential in keyring
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "test-value")
	require.NoError(t, err)

	// Delete using active backend
	err = DeleteFromActiveBackend("test-provider")
	require.NoError(t, err)

	// Verify deleted
	value, err := keyringBackend.Get("test-provider")
	require.NoError(t, err)
	assert.Equal(t, "", value)

	// Verify removed from tracking
	providers, err := getTrackedKeyringProviders()
	require.NoError(t, err)
	assert.NotContains(t, providers, "test-provider")
}

// TestIsKeyringAvailable with mock returns true
func TestIsKeyringAvailable(t *testing.T) {
	keyring.MockInit()
	

	// Keyring should be available with mock
	assert.True(t, IsKeyringAvailable())
}

// TestMigrateFileToKeyring_ClearFile clears file after migration
func TestMigrateFileToKeyring_ClearFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Set up file backend with a key
	fileBackend := NewFileBackend()
	err := fileBackend.Set("openai", "sk-openai-123")
	require.NoError(t, err)

	// Initialize mock keyring
	keyring.MockInit()
	

	// Migrate and clear file
	migrated, err := MigrateFileToKeyring(true) // Clear file
	require.NoError(t, err)
	assert.Len(t, migrated, 1)

	// Verify file is now empty
	store, err := Load()
	require.NoError(t, err)
	assert.Empty(t, store)

	// Verify keyring has the key
	keyringBackend := NewOSKeyringBackend()
	value, err := keyringBackend.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-123", value)
}

// TestMigrateKeyringToFile_ClearKeyring clears keyring after migration
func TestMigrateKeyringToFile_ClearKeyring(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Initialize mock keyring and set a key
	keyring.MockInit()
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("openai", "sk-openai-123")
	require.NoError(t, err)

	// Manually add to tracking (since mock keyring doesn't auto-track)
	err = addTrackedProvider("openai")
	require.NoError(t, err)

	// Migrate and clear keyring
	migrated, err := MigrateKeyringToFile(true) // Clear keyring
	require.NoError(t, err)
	assert.Len(t, migrated, 1)

	// Verify keyring is now empty
	value, err := keyringBackend.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, "", value)

	// Verify file has the key
	fileBackend := NewFileBackend()
	value, err = fileBackend.Get("openai")
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-123", value)
}

// TestSetStorageMode_InvalidMode returns error
func TestSetStorageMode_InvalidMode(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	err := SetStorageMode("invalid")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid storage mode")
}

// TestGetStorageMode_NoModeFile returns empty
func TestGetStorageMode_NoModeFile(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	mode, err := GetStorageMode()
	require.NoError(t, err)
	assert.Equal(t, "", mode)
}

// TestListKeyringProviders returns tracked providers
func TestListKeyringProviders(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	keyring.MockInit()
	

	// Add some providers
	err := addTrackedProvider("openai")
	require.NoError(t, err)
	err = addTrackedProvider("anthropic")
	require.NoError(t, err)

	providers, err := ListKeyringProviders()
	require.NoError(t, err)
	assert.Contains(t, providers, "openai")
	assert.Contains(t, providers, "anthropic")
}

// TestResolve_WhitespaceTrimmedProvider trims whitespace from provider name
func TestResolve_WhitespaceTrimmedProvider(t *testing.T) {
	ResetStorageBackend() // Reset backend cache for this test

	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	// Clear env var
	originalEnv := os.Getenv("LEDIT_CREDENTIAL_BACKEND")
	os.Setenv("LEDIT_CREDENTIAL_BACKEND", "")
	defer os.Setenv("LEDIT_CREDENTIAL_BACKEND", originalEnv)

	keyring.MockInit()
	

	// Set key with trimmed name
	keyringBackend := NewOSKeyringBackend()
	err := keyringBackend.Set("test-provider", "test-value")
	require.NoError(t, err)

	// Resolve with whitespace
	resolved, err := Resolve("  test-provider  ", "")
	require.NoError(t, err)
	assert.Equal(t, "test-provider", resolved.Provider)
	assert.Equal(t, "test-value", resolved.Value)
}

// TestFileBackend_EmptyValue returns error
func TestFileBackend_EmptyValue(t *testing.T) {
	tmpDir := t.TempDir()
	originalConfig := os.Getenv("LEDIT_CONFIG")
	os.Setenv("LEDIT_CONFIG", tmpDir)
	defer os.Setenv("LEDIT_CONFIG", originalConfig)

	backend := NewFileBackend()

	err := backend.Set("test-provider", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credential value cannot be empty")
}
