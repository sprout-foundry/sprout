package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInitialize_DebugPrintConfig(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	config := NewConfig()
	config.LastUsedProvider = "openrouter"
	config.Version = "2.0"
	apiKeys, err := LoadAPIKeys()
	require.NoError(t, err)
	assert.NotPanics(t, func() {
		DebugPrintConfig(config, apiKeys)
	})
}

func TestInitialize_GetAvailableProviders(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	providers := GetAvailableProviders()
	assert.NotEmpty(t, providers, "should have at least some providers")
	providerSet := make(map[string]bool)
	for _, p := range providers {
		providerSet[p] = true
	}
	assert.True(t, providerSet["ollama-local"], "should include ollama-local")
	assert.True(t, providerSet["editor"], "should include editor")
	assert.False(t, providerSet["test"], "test must not be a selectable provider")
}

func TestInitialize_LoadOrInitConfig_SkipPrompt(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	config, err := LoadOrInitConfig(true)
	require.NoError(t, err)
	require.NotNil(t, config)
	assert.Equal(t, "2.0", config.Version)
}

func TestInitialize_ValidateProviderSetup_Editor(t *testing.T) {
	err := validateProviderSetup("editor")
	assert.NoError(t, err, "editor mode should always validate")
}

func TestInitialize_ValidateProviderSetup_Empty(t *testing.T) {
	err := validateProviderSetup("")
	assert.Error(t, err, "empty provider should fail validation")
}

func TestInitialize_ValidateProviderSetup_Test(t *testing.T) {
	t.Setenv("CI", "1")
	defer t.Setenv("CI", "")
	err := validateProviderSetup("test")
	assert.NoError(t, err, "test provider should validate")
}

func TestInitialize_ShowWelcomeMessage(t *testing.T) {
	assert.NotPanics(t, func() {
		ShowWelcomeMessage()
	})
}

func TestInitialize_ShowNextSteps(t *testing.T) {
	assert.NotPanics(t, func() {
		ShowNextSteps("editor", "/tmp/test-config")
	})
	assert.NotPanics(t, func() {
		ShowNextSteps("openrouter", "/tmp/test-config")
	})
}

func TestInitialize_CIEnvironment(t *testing.T) {
	t.Setenv("SPROUT_CONFIG", t.TempDir())
	t.Setenv("CI", "1")
	config, apiKeys, err := Initialize()
	require.NoError(t, err)
	require.NotNil(t, config)
	require.NotNil(t, apiKeys)
}

func TestHasProviderAuth_LocalProviders(t *testing.T) {
	assert.True(t, HasProviderAuth("ollama-local"))
	assert.True(t, HasProviderAuth("test"))
	assert.True(t, HasProviderAuth("editor"))
}
