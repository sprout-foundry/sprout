package mcp

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// buildAuthHeaders
// ---------------------------------------------------------------------------

func TestBuildAuthHeaders(t *testing.T) {
	t.Run("empty config returns empty headers", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{}
		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Empty(t, headers)
	})

	t.Run("GITHUB_PERSONAL_ACCESS_TOKEN in Credentials is mapped to Authorization Bearer", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store the credential in the backend
		key := CredentialKey("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN")
		err := credentials.SetToActiveBackend(key, "ghp_testvalue123")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Credentials: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": SecretRef("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ghp_testvalue123", headers["Authorization"])
		assert.Len(t, headers, 1)
	})

	t.Run("GITHUB_PERSONAL_ACCESS_TOKEN in Env as placeholder is resolved and mapped to Authorization", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store the credential in the backend
		key := CredentialKey("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN")
		err := credentials.SetToActiveBackend(key, "ghp_fromenv456")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Env: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": SecretRef("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ghp_fromenv456", headers["Authorization"])
	})

	t.Run("Authorization credential directly sets Authorization Bearer", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store the credential
		key := CredentialKey("myserver", "Authorization")
		err := credentials.SetToActiveBackend(key, "my-secret-token")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Credentials: map[string]string{
				"Authorization": SecretRef("myserver", "Authorization"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer my-secret-token", headers["Authorization"])
	})

	t.Run("credentials with '-' in name (like X-API-Key) are set as HTTP headers", func(t *testing.T) {
		setupCredentialBackend(t)

		key := CredentialKey("myserver", "X-API-Key")
		err := credentials.SetToActiveBackend(key, "ak_test_api_key")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Credentials: map[string]string{
				"X-API-Key": SecretRef("myserver", "X-API-Key"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "ak_test_api_key", headers["X-Api-Key"])
	})

	t.Run("non-auth env vars like PATH are NOT mapped to headers", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{
				"PATH":  "/usr/bin:/bin",
				"HOME":  "/home/user",
				"MODEL": "gpt-4",
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Empty(t, headers)
	})

	t.Run("multiple credentials all get mapped correctly", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store multiple credentials
		err := credentials.SetToActiveBackend(CredentialKey("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN"), "ghp_multi")
		require.NoError(t, err)
		err = credentials.SetToActiveBackend(CredentialKey("myserver", "X-API-Key"), "ak_multiple")
		require.NoError(t, err)
		err = credentials.SetToActiveBackend(CredentialKey("myserver", "X-Auth-Token"), "tok_combo")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Credentials: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": SecretRef("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN"),
				"X-API-Key":                    SecretRef("myserver", "X-API-Key"),
				"X-Auth-Token":                 SecretRef("myserver", "X-Auth-Token"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ghp_multi", headers["Authorization"])
		assert.Equal(t, "ak_multiple", headers["X-Api-Key"])
		assert.Equal(t, "tok_combo", headers["X-Auth-Token"])
		assert.Len(t, headers, 3)
	})

	t.Run("credentials that resolve from store (not env) work", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store in backend, do NOT set as OS env var
		key := CredentialKey("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN")
		err := credentials.SetToActiveBackend(key, "ghp_store_only")
		require.NoError(t, err)

		// Ensure the OS env var is NOT set
		t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

		config := &MCPServerConfig{
			Credentials: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": SecretRef("myserver", "GITHUB_PERSONAL_ACCESS_TOKEN"),
			},
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, "Bearer ghp_store_only", headers["Authorization"])
	})

	t.Run("nil Credentials and nil Env returns empty headers", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Credentials: nil,
			Env:         nil,
		}

		headers, err := buildAuthHeaders("myserver", config)
		require.NoError(t, err)
		assert.Empty(t, headers)
	})
}

// ---------------------------------------------------------------------------
// normalizeHeaderName
// ---------------------------------------------------------------------------

func TestNormalizeHeaderName(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"underscores to hyphens, first letter caps each segment", "X_API_KEY", "X-Api-Key"},
		{"lowercase input becomes first-letter-caps segments", "x_auth_token", "X-Auth-Token"},
		{"single word is capitalized", "authorization", "Authorization"},
		{"empty string returns empty", "", ""},
		{"already has hyphens, splits and normalizes", "X-API-Key", "X-Api-Key"},
		{"mixed underscores and hyphens", "x-api_auth_token", "X-Api-Auth-Token"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeHeaderName(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}
