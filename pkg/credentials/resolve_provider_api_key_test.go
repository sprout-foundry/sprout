package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestResolveProviderAPIKey_EnvVarSet(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "sk-test-12345")

	key, err := ResolveProviderAPIKey("openai", "OpenAI")
	assert.NoError(t, err)
	assert.Equal(t, "sk-test-12345", key)
}

func TestResolveProviderAPIKey_StoredCredential(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Setenv("OPENAI_API_KEY", "")
	ResetStorageBackend()

	err := Save(Store{"openai": "sk-stored-key"})
	assert.NoError(t, err)

	key, err := ResolveProviderAPIKey("openai", "OpenAI")
	assert.NoError(t, err)
	assert.Equal(t, "sk-stored-key", key)
}

func TestResolveProviderAPIKey_EmptyValue_WithEnvVar(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	_, err := ResolveProviderAPIKey("openai", "OpenAI")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "OPENAI_API_KEY")
	assert.Contains(t, err.Error(), "not set")
}

func TestResolveProviderAPIKey_EmptyValue_NoEnvVar(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	_, err := ResolveProviderAPIKey("unknown-provider", "Unknown")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no stored API key configured")
}

func TestResolveProviderAPIKey_DisplayNameInError(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "")

	_, err := ResolveProviderAPIKey("openai", "My OpenAI Provider")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "My OpenAI Provider")
}

func TestResolveProviderAPIKey_TrimsWhitespace(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", configDir)
	t.Setenv("OPENAI_API_KEY", "  sk-test-key  ")

	key, err := ResolveProviderAPIKey("openai", "OpenAI")
	assert.NoError(t, err)
	assert.Equal(t, "sk-test-key", key)
}
