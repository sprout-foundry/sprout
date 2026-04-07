package credentials

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderEnvVar(t *testing.T) {
	tests := []struct {
		provider string
		expected string
	}{
		// Known providers with API key env vars
		{"openai", "OPENAI_API_KEY"},
		{"openrouter", "OPENROUTER_API_KEY"},
		{"deepinfra", "DEEPINFRA_API_KEY"},
		{"deepseek", "DEEPSEEK_API_KEY"},
		{"zai", "ZAI_API_KEY"},
		{"z.ai", "ZAI_API_KEY"},
		{"ollama", "OLLAMA_API_KEY"},
		{"ollama-local", "OLLAMA_API_KEY"},
		{"ollama-turbo", "OLLAMA_API_KEY"},
		{"minimax", "MINIMAX_API_KEY"},
		{"mistral", "MISTRAL_API_KEY"},

		// Local providers that don't require API keys
		{"lmstudio", ""},
		{"test", ""},

		// Custom/unrecognized provider falls through to default
		{"chutes", ""},
		{"unknown-custom-provider", ""},
		{"", ""}, // empty string
	}

	for _, tc := range tests {
		t.Run(tc.provider, func(t *testing.T) {
			result := ProviderEnvVar(tc.provider)
			assert.Equal(t, tc.expected, result)
		})
	}
}

func TestProviderEnvVar_CaseInsensitive(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"OpenAI", "OPENAI_API_KEY"},
		{"OPENAI", "OPENAI_API_KEY"},
		{"OpenRouter", "OPENROUTER_API_KEY"},
		{"DeepSeek", "DEEPSEEK_API_KEY"},
	}
	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			assert.Equal(t, tc.expected, ProviderEnvVar(tc.input))
		})
	}
}

func TestProviderEnvVar_WhitespaceTrimmed(t *testing.T) {
	assert.Equal(t, "OPENAI_API_KEY", ProviderEnvVar(" openai "))
	assert.Equal(t, "DEEPSEEK_API_KEY", ProviderEnvVar("  deepseek  "))
	assert.Equal(t, "", ProviderEnvVar("  lmstudio  "))
}

func TestHasProviderCredential_LocalProviders(t *testing.T) {
	// Local providers should always return true without needing any credentials
	localProviders := []string{"ollama", "ollama-local", "lmstudio", "test"}
	for _, p := range localProviders {
		t.Run(p, func(t *testing.T) {
			assert.True(t, HasProviderCredential(p))
		})
	}
}

func TestHasProviderCredential_EnvVarSet(t *testing.T) {
	// Set up config dir (needed because HasProviderCredential may call Resolve)
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Unset any pre-existing OPENAI_API_KEY
	t.Setenv("OPENAI_API_KEY", "sk-test-key-12345")

	assert.True(t, HasProviderCredential("openai"))
}

func TestHasProviderCredential_NoCredential(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Ensure OPENAI_API_KEY is not set
	t.Setenv("OPENAI_API_KEY", "")

	assert.False(t, HasProviderCredential("openai"))
}

func TestHasProviderCredential_StoredKey(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", tmpDir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Ensure env var is not set so we test the stored credential path
	t.Setenv("OPENAI_API_KEY", "")

	// Save a credential via the file backend
	store := Store{"openai": "sk-stored-test-key"}
	err := Save(store)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}

	// Reset the backend so it picks up the freshly saved file
	ResetStorageBackend()

	assert.True(t, HasProviderCredential("openai"))
}

func TestHasProviderCredential_EnvVarTakesPrecedence(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Set env var
	t.Setenv("OPENAI_API_KEY", "sk-from-env")

	// Also save a different key in the store
	store := Store{"openai": "sk-from-store"}
	err := Save(store)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	ResetStorageBackend()

	// Should still return true (env var takes precedence in Resolve)
	assert.True(t, HasProviderCredential("openai"))
}

func TestHasProviderCredential_CaseInsensitive(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Set the canonical env var
	t.Setenv("OPENAI_API_KEY", "sk-mixed-case-test")

	// Use mixed-case provider name
	assert.True(t, HasProviderCredential("OpenAI"))
	assert.True(t, HasProviderCredential("OPENAI"))
	assert.True(t, HasProviderCredential("openai"))
}

func TestHasProviderCredential_WhitespaceTrimmed(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	t.Setenv("OPENAI_API_KEY", "sk-whitespace-test")

	assert.True(t, HasProviderCredential(" openai "))
	assert.True(t, HasProviderCredential("  openai  "))
}

func TestHasProviderCredential_EmptyEnvVarValue(t *testing.T) {
	t.Setenv("LEDIT_CONFIG", t.TempDir())
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	ResetStorageBackend()

	// Set OPENAI_API_KEY to whitespace-only value — should be treated as unset
	t.Setenv("OPENAI_API_KEY", "   ")

	// Also ensure no stored key
	store := Store{}
	err := Save(store)
	if err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	ResetStorageBackend()

	assert.False(t, HasProviderCredential("openai"),
		"whitespace-only env var should not count as a credential")
}

// TestProviderEnvVar_ConsistentWithHasProviderCredential verifies that
// the env var name returned by ProviderEnvVar is actually the one checked
// by HasProviderCredential when resolving from the environment.
func TestProviderEnvVar_ConsistentWithHasProviderCredential(t *testing.T) {
	providers := []struct {
		provider string
		envVar   string
	}{
		{"openai", "OPENAI_API_KEY"},
		{"openrouter", "OPENROUTER_API_KEY"},
		{"deepinfra", "DEEPINFRA_API_KEY"},
		{"deepseek", "DEEPSEEK_API_KEY"},
		{"minimax", "MINIMAX_API_KEY"},
		{"mistral", "MISTRAL_API_KEY"},
	}

	for _, tc := range providers {
		t.Run(tc.provider, func(t *testing.T) {
			assert.Equal(t, tc.envVar, ProviderEnvVar(tc.provider))

			// Now verify HasProviderCredential recognizes it via env var
			tmpDir := t.TempDir()
			t.Setenv("LEDIT_CONFIG", tmpDir)
			t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
			ResetStorageBackend()

			// Clear env and verify no credential
			t.Setenv(tc.envVar, "")
			assert.False(t, HasProviderCredential(tc.provider),
				"%s should not have a credential with empty env var", tc.provider)

			// Set env and verify credential found
			t.Setenv(tc.envVar, "test-key-"+tc.provider)
			assert.True(t, HasProviderCredential(tc.provider),
				"%s should have a credential with %s set", tc.provider, tc.envVar)
		})
	}
}
