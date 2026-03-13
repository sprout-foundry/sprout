package configuration

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

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
