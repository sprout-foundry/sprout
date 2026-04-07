package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRequiresAPIKey_LocalProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	assert.False(t, RequiresAPIKey("ollama"))
	assert.False(t, RequiresAPIKey("ollama-local"))
	assert.False(t, RequiresAPIKey("lmstudio"))
	assert.False(t, RequiresAPIKey("test"))
}

func TestRequiresAPIKey_CloudProviders(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	assert.True(t, RequiresAPIKey("openai"))
	assert.True(t, RequiresAPIKey("openrouter"))
	assert.True(t, RequiresAPIKey("deepinfra"))
	assert.True(t, RequiresAPIKey("deepseek"))
	assert.True(t, RequiresAPIKey("mistral"))
}

func TestRequiresAPIKey_EmptyProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Empty provider → true (safe default)
	assert.True(t, RequiresAPIKey(""))
}

func TestRequiresAPIKey_CustomProvider_WithEnvVar(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	customProvider := CustomProviderConfig{
		Name:           "my-gateway",
		Endpoint:       "https://example.com/v1",
		EnvVar:         "MY_GATEWAY_KEY",
		RequiresAPIKey: true,
	}
	err := SaveCustomProvider(customProvider)
	assert.NoError(t, err)

	assert.True(t, RequiresAPIKey("my-gateway"))
}

func TestRequiresAPIKey_UnknownProvider(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)

	// Unknown provider defaults to true
	assert.True(t, RequiresAPIKey("totally-unknown-provider"))
}
