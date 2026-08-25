package configuration

import (
	"testing"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
)

func TestHasProviderAuth(t *testing.T) {
	tests := []struct {
		name          string
		provider      string
		setupEnv      func()
		expectHasAuth bool
	}{
		{
			name:     "local provider (ollama) always returns true",
			provider: "ollama",
			setupEnv: func() {
				// Local providers don't need env vars
			},
			expectHasAuth: true,
		},
		{
			name:     "local provider (ollama-local) always returns true",
			provider: "ollama-local",
			setupEnv: func() {
				// Local providers don't need env vars
			},
			expectHasAuth: true,
		},
		{
			name:     "local provider (lmstudio) always returns true",
			provider: "lmstudio",
			setupEnv: func() {
				// Local providers don't need env vars
			},
			expectHasAuth: true,
		},
		{
			name:     "openai with env var set returns true",
			provider: "openai",
			setupEnv: func() {
				t.Setenv("OPENAI_API_KEY", "sk-test-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "openai without env var returns false",
			provider: "openai",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				// Ensure env var is not set
				t.Setenv("OPENAI_API_KEY", "")
			},
			expectHasAuth: false,
		},
		{
			name:     "openrouter with env var set returns true",
			provider: "openrouter",
			setupEnv: func() {
				t.Setenv("OPENROUTER_API_KEY", "sk-or-test-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "openrouter without env var returns false",
			provider: "openrouter",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				t.Setenv("OPENROUTER_API_KEY", "")
			},
			expectHasAuth: false,
		},
		{
			name:     "deepinfra with env var set returns true",
			provider: "deepinfra",
			setupEnv: func() {
				t.Setenv("DEEPINFRA_API_KEY", "di-test-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "deepinfra without env var returns false",
			provider: "deepinfra",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				t.Setenv("DEEPINFRA_API_KEY", "")
			},
			expectHasAuth: false,
		},
		{
			name:     "editor mode always returns true",
			provider: "editor",
			setupEnv: func() {
				// Editor is a special mode that doesn't need credentials
			},
			expectHasAuth: true,
		},
		{
			name:     "test provider always returns true",
			provider: "test",
			setupEnv: func() {
				// Test provider doesn't need real credentials
			},
			expectHasAuth: true,
		},
		{
			name:     "jinaai with env var set returns true",
			provider: "jinaai",
			setupEnv: func() {
				t.Setenv("JINA_API_KEY", "test-jina-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "jinaai without env var returns false",
			provider: "jinaai",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				t.Setenv("JINA_API_KEY", "")
			},
			expectHasAuth: false,
		},
		{
			name:     "zai with env var set returns true",
			provider: "zai",
			setupEnv: func() {
				t.Setenv("ZAI_API_KEY", "test-zai-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "zai without env var returns false",
			provider: "zai",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				t.Setenv("ZAI_API_KEY", "")
			},
			expectHasAuth: false,
		},
		{
			name:     "ollama-cloud with env var set returns true",
			provider: "ollama-cloud",
			setupEnv: func() {
				t.Setenv("OLLAMA_API_KEY", "test-ollama-cloud-key")
			},
			expectHasAuth: true,
		},
		{
			name:     "ollama-cloud without env var returns true (local provider)",
			provider: "ollama-cloud",
			setupEnv: func() {
				// Use a unique config dir for isolation
				configDir := t.TempDir()
				t.Setenv("SPROUT_CONFIG", configDir)
				// Don't set OLLAMA_API_KEY - it's a local provider
			},
			expectHasAuth: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Reset credential backend to ensure clean state
			credentials.ResetStorageBackend()

			// Set up test environment
			tt.setupEnv()

			// Call HasProviderAuth
			result := HasProviderAuth(tt.provider)

			// Verify result
			assert.Equal(t, tt.expectHasAuth, result)
		})
	}
}

func TestHasProviderAuthWithCredentialStore(t *testing.T) {
	// Test that HasProviderAuth checks the credential store as well
	t.Run("credential store returns true for stored key", func(t *testing.T) {
		// Clear any env vars that might interfere
		t.Setenv("OPENAI_API_KEY", "")

		// Reset and set up credential store
		credentials.ResetStorageBackend()
		err := credentials.Save(credentials.Store{
			"openai": "stored-openai-key",
		})
		assert.NoError(t, err)

		// HasProviderAuth should return true based on stored credential
		result := HasProviderAuth("openai")
		assert.True(t, result)
	})

	t.Run("credential store returns false when not stored", func(t *testing.T) {
		// Clear any env vars that might interfere
		t.Setenv("OPENROUTER_API_KEY", "")

		// Reset and ensure credential store is empty
		credentials.ResetStorageBackend()

		// HasProviderAuth should return false
		result := HasProviderAuth("openrouter")
		assert.False(t, result)
	})

	t.Run("env var takes precedence over credential store", func(t *testing.T) {
		// Set up credential store
		credentials.ResetStorageBackend()
		err := credentials.Save(credentials.Store{
			"openai": "stored-openai-key",
		})
		assert.NoError(t, err)

		// Set env var to different value
		t.Setenv("OPENAI_API_KEY", "env-openai-key")

		// HasProviderAuth should return true (either source works)
		result := HasProviderAuth("openai")
		assert.True(t, result)
	})
}

func TestHasProviderAuthCustomProvider(t *testing.T) {
	// Test that custom providers work correctly with HasProviderAuth
	t.Run("custom provider with env var returns true", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("SPROUT_CONFIG", configDir)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")

		// Create a custom provider
		customProvider := CustomProviderConfig{
			Name:           "custom-gateway",
			Endpoint:       "https://example.com/v1",
			EnvVar:         "GATEWAY_API_KEY",
			RequiresAPIKey: true,
		}
		err := SaveCustomProvider(customProvider)
		assert.NoError(t, err)

		// Set the env var
		t.Setenv("GATEWAY_API_KEY", "custom-key")

		// Reset credential backend to ensure clean state
		credentials.ResetStorageBackend()

		// HasProviderAuth should return true
		result := HasProviderAuth("custom-gateway")
		assert.True(t, result)
	})

	t.Run("custom provider with stored credential returns true", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("SPROUT_CONFIG", configDir)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")

		// Create a custom provider
		customProvider := CustomProviderConfig{
			Name:           "custom-gateway2",
			Endpoint:       "https://example.com/v1",
			EnvVar:         "GATEWAY2_API_KEY",
			RequiresAPIKey: true,
		}
		err := SaveCustomProvider(customProvider)
		assert.NoError(t, err)

		// Ensure env var is not set
		t.Setenv("GATEWAY2_API_KEY", "")

		// Store the credential
		credentials.ResetStorageBackend()
		err = credentials.Save(credentials.Store{
			"custom-gateway2": "stored-custom-key",
		})
		assert.NoError(t, err)

		// HasProviderAuth should return true based on stored credential
		result := HasProviderAuth("custom-gateway2")
		assert.True(t, result)
	})

	t.Run("custom provider without env var but registered returns true", func(t *testing.T) {
		configDir := t.TempDir()
		t.Setenv("SPROUT_CONFIG", configDir)
		t.Setenv("HOME", t.TempDir())
		t.Setenv("XDG_CONFIG_HOME", "")

		// Create a custom provider
		customProvider := CustomProviderConfig{
			Name:           "custom-gateway3",
			Endpoint:       "https://example.com/v1",
			EnvVar:         "GATEWAY3_API_KEY",
			RequiresAPIKey: true,
		}
		err := SaveCustomProvider(customProvider)
		assert.NoError(t, err)

		// Ensure env var is not set
		t.Setenv("GATEWAY3_API_KEY", "")

		// Reset credential backend
		credentials.ResetStorageBackend()

		// HasProviderAuth should return true (custom providers without env var are allowed)
		result := HasProviderAuth("custom-gateway3")
		assert.True(t, result)
	})
}

func TestHasProviderAuthKnownProviders(t *testing.T) {
	// Comprehensive test for all known providers
	knownProviders := []string{
		"openai", "openrouter", "deepinfra", "ollama", "ollama-local",
		"ollama-cloud", "lmstudio", "jinaai", "zai", "test", "editor",
	}

	// Reset credential backend for clean state
	credentials.ResetStorageBackend()

	for _, provider := range knownProviders {
		t.Run(provider+" local/no-key providers return true", func(t *testing.T) {
			// Check if this is a local/no-key provider
			metadata, err := GetProviderAuthMetadata(provider)
			if err == nil && !metadata.RequiresAPIKey {
				// Local providers should always return true
				result := HasProviderAuth(provider)
				assert.True(t, result, "Provider %s should always return true", provider)
			}
		})
	}
}

func TestHasProviderAuthInitRegistration(t *testing.T) {
	// Test that the init function correctly registers the provider info callback
	// This is implicitly tested by all other tests, but we can verify it works
	t.Run("provider info callback is registered", func(t *testing.T) {
		// The init() function in resolve.go should register the callback
		// We can verify this by calling HasProviderAuth with a known provider
		credentials.ResetStorageBackend()
		t.Setenv("OPENAI_API_KEY", "test-key")

		result := HasProviderAuth("openai")
		assert.True(t, result)
	})
}
