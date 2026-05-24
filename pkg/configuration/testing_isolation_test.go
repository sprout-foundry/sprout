package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Verify the defense-in-depth guards prevent the test-provider
// sentinel from ever reaching disk and self-heal a config that was
// poisoned by a past leak.

func TestSanitizeTestProvider_ClearsLastUsedProvider(t *testing.T) {
	c := &Config{LastUsedProvider: "test"}
	sanitizeTestProvider(c)
	assert.Empty(t, c.LastUsedProvider,
		"sanitize should clear 'test' from LastUsedProvider")
}

func TestSanitizeTestProvider_PreservesRealProvider(t *testing.T) {
	c := &Config{LastUsedProvider: "openai"}
	sanitizeTestProvider(c)
	assert.Equal(t, "openai", c.LastUsedProvider,
		"sanitize must not touch real provider names")
}

func TestSanitizeTestProvider_ClearsSubagentProvider(t *testing.T) {
	c := &Config{SubagentProvider: "test"}
	sanitizeTestProvider(c)
	assert.Empty(t, c.SubagentProvider)
}

func TestSanitizeTestProvider_NilSafe(t *testing.T) {
	// Should not panic.
	sanitizeTestProvider(nil)
}

func TestSanitizeTestProvider_DropsTestEntryFromProviderModels(t *testing.T) {
	c := &Config{
		ProviderModels: map[string]string{
			"test":   "mock-model-v1",
			"openai": "gpt-4",
		},
	}
	sanitizeTestProvider(c)
	_, hasTest := c.ProviderModels["test"]
	assert.False(t, hasTest, "test entry should be stripped from ProviderModels")
	assert.Equal(t, "gpt-4", c.ProviderModels["openai"],
		"real entries must be preserved")
}

func TestSave_RefusesTestProvider(t *testing.T) {
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	// Bypass the Manager's type-safe SetProvider and write the bad
	// value directly into the Config struct, then save. This mirrors
	// exactly the leak vector that motivated the guards.
	require.NoError(t, mgr.UpdateConfigNoSave(func(c *Config) error {
		c.LastUsedProvider = "test"
		return nil
	}))
	cfg := mgr.GetConfig()
	require.NoError(t, cfg.SaveToDir(mgr.configDir),
		"save should succeed (sanitizer strips 'test', does not error)")

	// Read the file back from disk — the "test" string must not be
	// present in LastUsedProvider.
	persistedPath := filepath.Join(mgr.configDir, ConfigFileName)
	data, err := os.ReadFile(persistedPath)
	require.NoError(t, err)
	var persisted map[string]any
	require.NoError(t, json.Unmarshal(data, &persisted))
	assert.NotEqual(t, "test", persisted["last_used_provider"],
		"Save must not persist 'test' as LastUsedProvider")
}

func TestLoad_SanitizesTestProviderFromPoisonedFile(t *testing.T) {
	// Simulate a config file that was already poisoned by an old
	// leaky test run — write "test" raw, then verify Load heals it.
	mgr, cleanup := NewTestManager(t)
	defer cleanup()

	poisonedJSON := []byte(`{"version":3,"last_used_provider":"test"}`)
	configPath := filepath.Join(mgr.configDir, ConfigFileName)
	require.NoError(t, os.WriteFile(configPath, poisonedJSON, 0600))

	loaded, err := LoadConfigWithLayers(configPath, "", "", mgr.configDir)
	require.NoError(t, err)
	assert.Empty(t, loaded.LastUsedProvider,
		"Load should clear 'test' from disk-poisoned config")
}

func TestNewManagerWithDir_DoesNotPersistTestSentinel(t *testing.T) {
	// Layer 3: NewManagerWithDir used to write LastUsedProvider="test"
	// to disk for "test predictability". This was the original leak
	// source. After the fix, fresh test configs start empty.
	tmpDir := t.TempDir()
	configDir := filepath.Join(tmpDir, ".sprout")
	mgr, err := NewManagerWithDir(configDir)
	require.NoError(t, err)

	cfg := mgr.GetConfig()
	assert.Empty(t, cfg.LastUsedProvider,
		"fresh test config must not preload the test sentinel")

	// Confirm the on-disk file also doesn't have it.
	configPath := filepath.Join(configDir, ConfigFileName)
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)
	var onDisk map[string]any
	require.NoError(t, json.Unmarshal(data, &onDisk))
	val, _ := onDisk["last_used_provider"].(string)
	assert.NotEqual(t, "test", val,
		"on-disk last_used_provider must not be 'test'")
}
