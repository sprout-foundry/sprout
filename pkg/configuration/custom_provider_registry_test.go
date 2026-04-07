package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeCustomProviderConfigNormalizesEndpoint(t *testing.T) {
	cfg, err := NormalizeCustomProviderConfig(CustomProviderConfig{
		Name:     "My-Gateway",
		Endpoint: "https://example.com/v1/models",
		EnvVar:   "MY_GATEWAY_API_KEY",
	})
	require.NoError(t, err)

	assert.Equal(t, "my-gateway", cfg.Name)
	assert.Equal(t, "https://example.com/v1/chat/completions", cfg.Endpoint)
	assert.Equal(t, "https://example.com/v1/models", cfg.ModelsEndpoint())
	assert.True(t, cfg.RequiresAPIKey)
	assert.Equal(t, 32768, cfg.ContextSize)
}

func TestSaveAndLoadCustomProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	err := SaveCustomProvider(CustomProviderConfig{
		Name:     "gateway",
		Endpoint: "https://example.com/v1",
		EnvVar:   "GATEWAY_API_KEY",
	})
	require.NoError(t, err)

	providers, err := LoadCustomProviders()
	require.NoError(t, err)
	require.Contains(t, providers, "gateway")

	assert.Equal(t, "https://example.com/v1/chat/completions", providers["gateway"].Endpoint)
	assert.Equal(t, "GATEWAY_API_KEY", providers["gateway"].EnvVar)
}

func TestConfigSaveOmitsInlineCustomProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	cfg := NewConfig()
	cfg.CustomProviders["gateway"] = CustomProviderConfig{
		Name:     "gateway",
		Endpoint: "https://example.com/v1",
	}

	require.NoError(t, SaveCustomProvider(cfg.CustomProviders["gateway"]))
	require.NoError(t, cfg.Save())

	data, err := os.ReadFile(filepath.Join(configDir, ConfigFileName))
	require.NoError(t, err)

	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, exists := raw["custom_providers"]
	assert.False(t, exists)
}

func TestMigrateConfigFileAPIKeys(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json with custom_providers containing api_key values
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "my-gateway": {
      "name": "my-gateway",
      "endpoint": "https://example.com/v1/chat/completions",
      "env_var": "MY_GATEWAY_KEY",
      "api_key": "sk-test-old-key-12345"
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	// Call the migration function
	err := MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	// Verify api_key was removed from config.json
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	myGateway, ok := customProviders["my-gateway"].(map[string]interface{})
	require.True(t, ok)

	// Verify api_key was removed
	_, hasAPIKey := myGateway["api_key"]
	assert.False(t, hasAPIKey, "api_key should be removed from config.json")

	// Verify the key was stored in the credential store
	storedKey, _, err := credentials.GetFromActiveBackend("my-gateway")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-old-key-12345", storedKey)
}

func TestMigrateConfigFileAPIKeys_NoAPIKeys(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json without api_key values
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "my-gateway": {
      "name": "my-gateway",
      "endpoint": "https://example.com/v1/chat/completions",
      "env_var": "MY_GATEWAY_KEY"
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	// Call the migration function
	err := MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	// Verify config.json is still valid JSON
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	myGateway, ok := customProviders["my-gateway"].(map[string]interface{})
	require.True(t, ok)

	// Verify no api_key was added
	_, hasAPIKey := myGateway["api_key"]
	assert.False(t, hasAPIKey, "api_key should not be present")

	// Verify the file is still valid JSON (may have formatting changes but should be valid)
	assert.NotNil(t, rawConfig)
}

func TestMigrateConfigFileAPIKeys_Idempotent(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json with custom_providers containing api_key values
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "my-gateway": {
      "name": "my-gateway",
      "endpoint": "https://example.com/v1/chat/completions",
      "env_var": "MY_GATEWAY_KEY",
      "api_key": "sk-test-old-key-12345"
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	// Call the migration function twice
	err := MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	err = MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	// Verify config.json is still valid and api_key is not present
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	myGateway, ok := customProviders["my-gateway"].(map[string]interface{})
	require.True(t, ok)

	_, hasAPIKey := myGateway["api_key"]
	assert.False(t, hasAPIKey, "api_key should be removed after first migration")
}

func TestMigrateConfigFileAPIKeys_MultipleProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json with multiple custom_providers, some with api_key, some without
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "provider-with-key": {
      "name": "provider-with-key",
      "endpoint": "https://example1.com/v1/chat/completions",
      "api_key": "sk-key-1"
    },
    "provider-without-key": {
      "name": "provider-without-key",
      "endpoint": "https://example2.com/v1/chat/completions"
    },
    "provider-with-empty-key": {
      "name": "provider-with-empty-key",
      "endpoint": "https://example3.com/v1/chat/completions",
      "api_key": ""
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	// Call the migration function
	err := MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	// Verify config.json
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	// Check provider-with-key
	provider1, ok := customProviders["provider-with-key"].(map[string]interface{})
	require.True(t, ok)
	_, hasAPIKey1 := provider1["api_key"]
	assert.False(t, hasAPIKey1, "api_key should be removed from provider-with-key")

	// Check provider-without-key (should remain unchanged)
	provider2, ok := customProviders["provider-without-key"].(map[string]interface{})
	require.True(t, ok)
	_, hasAPIKey2 := provider2["api_key"]
	assert.False(t, hasAPIKey2, "api_key should not be present in provider-without-key")

	// Check provider-with-empty-key (empty api_key should remain unchanged)
	provider3, ok := customProviders["provider-with-empty-key"].(map[string]interface{})
	require.True(t, ok)
	_, hasAPIKey3 := provider3["api_key"]
	assert.True(t, hasAPIKey3, "empty api_key should remain in provider-with-empty-key")

	// Verify only the provider with a non-empty api_key was migrated
	storedKey1, _, err := credentials.GetFromActiveBackend("provider-with-key")
	require.NoError(t, err)
	assert.Equal(t, "sk-key-1", storedKey1)
}

func TestLoad_MigratesConfigFileAPIKeys(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json with custom_providers containing api_key
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "my-gateway": {
      "name": "my-gateway",
      "endpoint": "https://example.com/v1/chat/completions",
      "env_var": "MY_GATEWAY_KEY",
      "api_key": "sk-test-old-key-12345"
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	// Call Load() which triggers the migration
	cfg, err := Load()
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// Verify the key was migrated to the credential store
	storedKey, _, err := credentials.GetFromActiveBackend("my-gateway")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-old-key-12345", storedKey)

	// Verify the config.json on disk no longer contains api_key
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	myGateway, ok := customProviders["my-gateway"].(map[string]interface{})
	require.True(t, ok)

	_, hasAPIKey := myGateway["api_key"]
	assert.False(t, hasAPIKey, "api_key should be removed from config.json after Load()")
}

func TestMigrateConfigFileAPIKeys_NonStringAPIKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a config.json with a non-string api_key (should be silently left alone)
	configPath := filepath.Join(configDir, ConfigFileName)
	configData := `{
  "version": "2.0",
  "custom_providers": {
    "numeric-key": {
      "name": "numeric-key",
      "endpoint": "https://example.com/v1/chat/completions",
      "api_key": 12345
    }
  }
}`
	require.NoError(t, os.WriteFile(configPath, []byte(configData), 0600))

	err := MigrateConfigFileAPIKeys(configPath)
	require.NoError(t, err)

	// Verify the non-string api_key was not migrated
	storedKey, _, err := credentials.GetFromActiveBackend("numeric-key")
	require.NoError(t, err)
	assert.Equal(t, "", storedKey, "non-string api_key should not be migrated to credential store")

	// Verify the config.json is still valid and has the api_key field untouched
	data, err := os.ReadFile(configPath)
	require.NoError(t, err)

	var rawConfig map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &rawConfig))

	customProviders, ok := rawConfig["custom_providers"].(map[string]interface{})
	require.True(t, ok)

	numericProvider, ok := customProviders["numeric-key"].(map[string]interface{})
	require.True(t, ok)

	// The api_key should still be present (non-string values are not migrated)
	_, hasAPIKey := numericProvider["api_key"]
	assert.True(t, hasAPIKey, "non-string api_key should remain untouched")
}

func TestMigrateEmbeddedAPIKeys_MigratesKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Create a provider JSON file with an embedded api_key
	providersDir, err := GetProvidersDir()
	require.NoError(t, err)
	providerFile := filepath.Join(providersDir, "my-provider.json")
	require.NoError(t, os.WriteFile(providerFile, []byte(`{
  "name": "my-provider",
  "endpoint": "https://example.com/v1/chat/completions",
  "env_var": "MY_PROVIDER_KEY",
  "api_key": "sk-legacy-embedded-key-12345"
}`), 0600))

	providers := map[string]CustomProviderConfig{
		"my-provider": {Name: "my-provider", Endpoint: "https://example.com/v1/chat/completions"},
	}
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// Verify api_key was removed from the provider JSON file
	data, err := os.ReadFile(providerFile)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasAPIKey := raw["api_key"]
	assert.False(t, hasAPIKey, "api_key should be removed from provider JSON file")

	// Verify the key was migrated to the credential store
	storedKey, _, err := credentials.GetFromActiveBackend("my-provider")
	require.NoError(t, err)
	assert.Equal(t, "sk-legacy-embedded-key-12345", storedKey)

	// Verify the marker file was created
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	_, err = os.Stat(markerPath)
	assert.NoError(t, err, "migration marker should be created")
}

func TestMigrateEmbeddedAPIKeys_SkipsWhenMarkerExists(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)

	// Pre-create the marker
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	require.NoError(t, os.WriteFile(markerPath, nil, 0600))

	// Create a provider JSON file with an embedded api_key
	providerFile := filepath.Join(providersDir, "my-provider.json")
	require.NoError(t, os.WriteFile(providerFile, []byte(`{
  "name": "my-provider",
  "endpoint": "https://example.com/v1/chat/completions",
  "api_key": "sk-should-not-migrate"
}`), 0600))

	providers := map[string]CustomProviderConfig{
		"my-provider": {Name: "my-provider", Endpoint: "https://example.com/v1/chat/completions"},
	}
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// api_key should NOT be migrated because the marker already exists
	storedKey, _, err := credentials.GetFromActiveBackend("my-provider")
	require.NoError(t, err)
	assert.Equal(t, "", storedKey, "api_key should not be migrated when marker already exists")

	// api_key should still be present in the file
	data, err := os.ReadFile(providerFile)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	_, hasAPIKey := raw["api_key"]
	assert.True(t, hasAPIKey, "api_key should remain in file when marker exists")
}

func TestMigrateEmbeddedAPIKeys_Idempotent(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)

	providerFile := filepath.Join(providersDir, "my-provider.json")
	require.NoError(t, os.WriteFile(providerFile, []byte(`{
  "name": "my-provider",
  "endpoint": "https://example.com/v1/chat/completions",
  "api_key": "sk-test-key"
}`), 0600))

	providers := map[string]CustomProviderConfig{
		"my-provider": {Name: "my-provider", Endpoint: "https://example.com/v1/chat/completions"},
	}

	// First call migrates and creates marker
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// Second call is a no-op due to marker
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// Verify key was migrated exactly once
	storedKey, _, err := credentials.GetFromActiveBackend("my-provider")
	require.NoError(t, err)
	assert.Equal(t, "sk-test-key", storedKey)

	// Marker should exist
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	_, err = os.Stat(markerPath)
	assert.NoError(t, err, "migration marker should exist after first call")
}

func TestMigrateEmbeddedAPIKeys_CreatesMarkerWithNoProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)

	// No provider files, empty providers map
	providers := map[string]CustomProviderConfig{}
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// Marker should still be created
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	_, err = os.Stat(markerPath)
	assert.NoError(t, err, "migration marker should be created even with no providers")
}

func TestMigrateEmbeddedAPIKeys_SkipsFilesWithoutAPIKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)

	// Provider JSON without api_key
	providerFile := filepath.Join(providersDir, "my-provider.json")
	require.NoError(t, os.WriteFile(providerFile, []byte(`{
  "name": "my-provider",
  "endpoint": "https://example.com/v1/chat/completions",
  "env_var": "MY_PROVIDER_KEY"
}`), 0600))

	providers := map[string]CustomProviderConfig{
		"my-provider": {Name: "my-provider", Endpoint: "https://example.com/v1/chat/completions"},
	}
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// File should be untouched
	data, err := os.ReadFile(providerFile)
	require.NoError(t, err)
	assert.Contains(t, string(data), "env_var")

	// Marker should still be created
	markerPath := filepath.Join(providersDir, apiKeysMigratedMarker)
	_, err = os.Stat(markerPath)
	assert.NoError(t, err, "migration marker should be created even when no api_keys found")
}

func TestMigrateEmbeddedAPIKeys_SkipsEmptyAPIKey(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	providersDir, err := GetProvidersDir()
	require.NoError(t, err)

	// Provider JSON with empty api_key
	providerFile := filepath.Join(providersDir, "my-provider.json")
	require.NoError(t, os.WriteFile(providerFile, []byte(`{
  "name": "my-provider",
  "endpoint": "https://example.com/v1/chat/completions",
  "api_key": ""
}`), 0600))

	providers := map[string]CustomProviderConfig{
		"my-provider": {Name: "my-provider", Endpoint: "https://example.com/v1/chat/completions"},
	}
	require.NoError(t, MigrateEmbeddedAPIKeys(providers))

	// Empty api_key should remain in the file (not migrated, not deleted)
	data, err := os.ReadFile(providerFile)
	require.NoError(t, err)
	var raw map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &raw))
	apiKey, hasAPIKey := raw["api_key"].(string)
	assert.True(t, hasAPIKey, "empty api_key should remain in file")
	assert.Equal(t, "", apiKey)

	// Nothing should be in the credential store
	storedKey, _, err := credentials.GetFromActiveBackend("my-provider")
	require.NoError(t, err)
	assert.Equal(t, "", storedKey)
}
