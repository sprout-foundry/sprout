package mcp

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ResolveEnvVars - Additional Edge Case Tests
// ---------------------------------------------------------------------------

func TestResolveEnvVars_PassThroughNonSecretValues(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Create env map with non-secret values
	env := map[string]string{
		"PATH":       "/usr/bin:/usr/local/bin",
		"HOME":       "/home/user",
		"LANG":       "en_US.UTF-8",
		"TERM":       "xterm-256color",
		"SHELL":      "/bin/bash",
		"NODE_ENV":   "production",
		"MAX_TOKENS": "4096",
		"MODEL":      "gpt-4",
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("myserver", env)

	// Assert: All values should pass through unchanged
	require.NoError(t, err)
	assert.Equal(t, env, result, "non-secret values should pass through unchanged")
}

func TestResolveEnvVars_ResolveSecretPlaceholders(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Pre-store credentials in backend
	err := credentials.SetToActiveBackend("mcp/myserver/OPENAI_API_KEY", "sk-openai-secret")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/myserver/GITHUB_TOKEN", "ghp_github-token")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/myserver/ANTHROPIC_API_KEY", "sk-ant-anthropic-key")
	require.NoError(t, err)

	// Arrange: Create env map with secret placeholders
	env := map[string]string{
		"OPENAI_API_KEY":          SecretRef("myserver", "OPENAI_API_KEY"),
		"GITHUB_TOKEN":            SecretRef("myserver", "GITHUB_TOKEN"),
		"ANTHROPIC_API_KEY":       SecretRef("myserver", "ANTHROPIC_API_KEY"),
		"PATH":                    "/usr/bin", // Non-secret should pass through
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("myserver", env)

	// Assert: Secrets should be resolved, non-secrets pass through
	require.NoError(t, err)
	assert.Equal(t, "sk-openai-secret", result["OPENAI_API_KEY"])
	assert.Equal(t, "ghp_github-token", result["GITHUB_TOKEN"])
	assert.Equal(t, "sk-ant-anthropic-key", result["ANTHROPIC_API_KEY"])
	assert.Equal(t, "/usr/bin", result["PATH"])
}

func TestResolveEnvVars_FallbackToOSEnvironment(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Set OS environment variables
	t.Setenv("MY_API_KEY", "from-os-environment")
	t.Setenv("SERVICE_TOKEN", "token-from-env")
	t.Setenv("SECRET_PASSWORD", "password-from-os")

	// Arrange: Create env map with placeholders (not stored in credential backend)
	env := map[string]string{
		"MY_API_KEY":       SecretRef("fallback-server", "MY_API_KEY"),
		"SERVICE_TOKEN":    SecretRef("fallback-server", "SERVICE_TOKEN"),
		"SECRET_PASSWORD":  SecretRef("fallback-server", "SECRET_PASSWORD"),
		"PATH":             "/usr/bin", // Non-secret
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("fallback-server", env)

	// Assert: Should fall back to OS environment for missing credentials
	require.NoError(t, err)
	assert.Equal(t, "from-os-environment", result["MY_API_KEY"])
	assert.Equal(t, "token-from-env", result["SERVICE_TOKEN"])
	assert.Equal(t, "password-from-os", result["SECRET_PASSWORD"])
	assert.Equal(t, "/usr/bin", result["PATH"])
}

func TestResolveEnvVars_SkipInvalidCredentialPlaceholders(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Create env map with various invalid placeholders
	env := map[string]string{
		"VAR1": "{{credential:not-valid-format}}", // Not mcp/server/key format
		"VAR2": "{{credential:api/bad/key}}",      // Wrong first part (not "mcp")
		"VAR3": "{{credential:missing-parts}}",    // Only 2 parts
		"VAR4": "{{credential:mcp/extra/parts/here}}", // Too many parts
		"VAR5": "malformed-placeholder",           // Not a placeholder at all
		"VAR6": "{{credential:mcp/server/KEY}}",   // Valid reference
	}

	// Arrange: Store valid credential
	err := credentials.SetToActiveBackend("mcp/server/KEY", "valid-value")
	require.NoError(t, err)

	// Act: Resolve env vars
	result, err := ResolveEnvVars("server", env)

	// Assert: Invalid placeholders should be skipped
	require.NoError(t, err)
	assert.NotContains(t, result, "VAR1", "invalid format should be skipped")
	assert.NotContains(t, result, "VAR2", "wrong first part should be skipped")
	assert.NotContains(t, result, "VAR3", "missing parts should be skipped")
	assert.NotContains(t, result, "VAR4", "too many parts should be skipped")
	assert.Equal(t, "malformed-placeholder", result["VAR5"], "non-placeholder should pass through")
	assert.Equal(t, "valid-value", result["VAR6"], "valid placeholder should be resolved")
}

func TestResolveEnvVars_EmptyEnvMap(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Empty env map
	env := map[string]string{}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("myserver", env)

	// Assert: Should return empty map
	require.NoError(t, err)
	assert.Empty(t, result, "empty env should return empty result")
}

func TestResolveEnvVars_NilEnvMap(t *testing.T) {
	setupCredentialBackend(t)

	// Act: Resolve env vars with nil
	result, err := ResolveEnvVars("myserver", nil)

	// Assert: Should return empty map (not panic)
	require.NoError(t, err)
	assert.Empty(t, result, "nil env should return empty result")
}

func TestResolveEnvVars_EmptyCredentialValue(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Set OS environment as fallback
	t.Setenv("EMPTY_KEY", "from-os-fallback")

	// Arrange: Create env with placeholder for credential that doesn't exist
	// (will be treated as empty in backend and fall back to OS)
	env := map[string]string{
		"EMPTY_KEY": SecretRef("empty-server", "EMPTY_KEY"),
		"PATH":      "/usr/bin",
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("empty-server", env)

	// Assert: Should fall back to OS environment when credential not in backend
	require.NoError(t, err)
	assert.Equal(t, "from-os-fallback", result["EMPTY_KEY"], "should fall back to OS for missing credential")
	assert.Equal(t, "/usr/bin", result["PATH"])
}

func TestResolveEnvVars_MixedSecretsAndNonSecrets(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Pre-store some credentials
	err := credentials.SetToActiveBackend("mcp/mixed-server/API_KEY", "sk-api-key")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/mixed-server/TOKEN", "bearer-token")
	require.NoError(t, err)

	// Arrange: Set fallback OS env for missing credential
	t.Setenv("MISSING_KEY", "from-os")

	// Arrange: Mix of secrets and non-secrets
	env := map[string]string{
		"API_KEY":        SecretRef("mixed-server", "API_KEY"),
		"TOKEN":          SecretRef("mixed-server", "TOKEN"),
		"MISSING_KEY":    SecretRef("mixed-server", "MISSING_KEY"), // Will fall back to OS
		"PATH":           "/usr/bin:/usr/local/bin",
		"HOME":           "/home/user",
		"MAX_TOKENS":     "8192",
		"TEMP_DIR":       "/tmp",
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("mixed-server", env)

	// Assert: All values should be correctly resolved
	require.NoError(t, err)
	assert.Len(t, result, 7)

	// Resolved from credential store
	assert.Equal(t, "sk-api-key", result["API_KEY"])
	assert.Equal(t, "bearer-token", result["TOKEN"])

	// Fallback to OS
	assert.Equal(t, "from-os", result["MISSING_KEY"])

	// Pass through
	assert.Equal(t, "/usr/bin:/usr/local/bin", result["PATH"])
	assert.Equal(t, "/home/user", result["HOME"])
	assert.Equal(t, "8192", result["MAX_TOKENS"])
	assert.Equal(t, "/tmp", result["TEMP_DIR"])
}

func TestResolveEnvVars_WhitespaceHandling(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credential
	err := credentials.SetToActiveBackend("mcp/ws-server/API_KEY", "whitespace-test")
	require.NoError(t, err)

	// Arrange: Placeholders with whitespace
	env := map[string]string{
		"KEY1": "  {{credential:mcp/ws-server/API_KEY}}  ", // Outer whitespace
		"KEY2": "  value-with-whitespace  ",                // Non-secret with whitespace
	}

	// Act: Resolve env vars
	result, err := ResolveEnvVars("ws-server", env)

	// Assert: Whitespace should be trimmed appropriately
	require.NoError(t, err)
	assert.Equal(t, "whitespace-test", result["KEY1"], "placeholder should be resolved with trimmed whitespace")
	assert.Equal(t, "  value-with-whitespace  ", result["KEY2"], "non-secret whitespace should pass through")
}

// ---------------------------------------------------------------------------
// BuildFullEnvForServer - Complete Test Coverage
// ---------------------------------------------------------------------------

func TestBuildFullEnvForServer_MergeEnvAndCredentials(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credentials
	err := credentials.SetToActiveBackend("mcp/merge-server/OPENAI_API_KEY", "sk-openai")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/merge-server/GITHUB_TOKEN", "ghp-github")
	require.NoError(t, err)

	// Arrange: Server config with both Env and Credentials
	config := &MCPServerConfig{
		Name: "merge-server",
		Env: map[string]string{
			"PATH":       "/usr/bin",
			"HOME":       "/home/user",
			"MAX_TOKENS": "4096",
		},
		Credentials: map[string]string{
			"OPENAI_API_KEY": SecretRef("merge-server", "OPENAI_API_KEY"),
			"GITHUB_TOKEN":   SecretRef("merge-server", "GITHUB_TOKEN"),
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("merge-server", config)

	// Assert: Env and Credentials should be merged
	require.NoError(t, err)
	assert.Len(t, result, 5, "should have all env vars from both sources")

	// From Env (non-secrets)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "/home/user", result["HOME"])
	assert.Equal(t, "4096", result["MAX_TOKENS"])

	// From Credentials (resolved secrets)
	assert.Equal(t, "sk-openai", result["OPENAI_API_KEY"])
	assert.Equal(t, "ghp-github", result["GITHUB_TOKEN"])
}

func TestBuildFullEnvForServer_EmptyServerEnv(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credential
	err := credentials.SetToActiveBackend("mcp/empty-env-server/SECRET_KEY", "secret-value")
	require.NoError(t, err)

	// Arrange: Server config with only Credentials, no Env
	config := &MCPServerConfig{
		Name: "empty-env-server",
		Env:  map[string]string{}, // Empty but initialized
		Credentials: map[string]string{
			"SECRET_KEY": SecretRef("empty-env-server", "SECRET_KEY"),
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("empty-env-server", config)

	// Assert: Should only contain resolved credentials
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "secret-value", result["SECRET_KEY"])
}

func TestBuildFullEnvForServer_NilServerEnv(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credential
	err := credentials.SetToActiveBackend("mcp/nil-env-server/SECRET_KEY", "secret-value")
	require.NoError(t, err)

	// Arrange: Server config with Credentials but nil Env
	config := &MCPServerConfig{
		Name:         "nil-env-server",
		Env:          nil, // Not initialized
		Credentials: map[string]string{
			"SECRET_KEY": SecretRef("nil-env-server", "SECRET_KEY"),
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("nil-env-server", config)

	// Assert: Should only contain resolved credentials (no panic on nil Env)
	require.NoError(t, err)
	assert.Len(t, result, 1)
	assert.Equal(t, "secret-value", result["SECRET_KEY"])
}

func TestBuildFullEnvForServer_EmptyCredentials(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with only Env, no Credentials
	config := &MCPServerConfig{
		Name: "no-creds-server",
		Env: map[string]string{
			"PATH":   "/usr/bin",
			"HOME":   "/home/user",
			"MODEL":  "gpt-4",
		},
		Credentials: map[string]string{}, // Empty but initialized
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("no-creds-server", config)

	// Assert: Should only contain Env vars
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "/home/user", result["HOME"])
	assert.Equal(t, "gpt-4", result["MODEL"])
}

func TestBuildFullEnvForServer_NilCredentials(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with only Env, nil Credentials
	config := &MCPServerConfig{
		Name:         "nil-creds-server",
		Env: map[string]string{
			"PATH":  "/usr/bin",
			"MODEL": "gpt-4",
		},
		Credentials: nil, // Not initialized
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("nil-creds-server", config)

	// Assert: Should only contain Env vars (no panic on nil Credentials)
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "gpt-4", result["MODEL"])
}

func TestBuildFullEnvForServer_EnvWithPlaceholders(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credential (Env map still has placeholders in some cases)
	err := credentials.SetToActiveBackend("mcp/placeholder-server/API_KEY", "sk-from-env-placeholder")
	require.NoError(t, err)

	// Arrange: Server config with placeholders in Env (before migration to Credentials)
	config := &MCPServerConfig{
		Name: "placeholder-server",
		Env: map[string]string{
			"PATH":     "/usr/bin",
			"API_KEY":  SecretRef("placeholder-server", "API_KEY"), // Placeholder in Env
		},
		Credentials: nil, // Credentials not populated yet
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("placeholder-server", config)

	// Assert: Placeholders in Env should be resolved
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "sk-from-env-placeholder", result["API_KEY"], "placeholder in Env should be resolved")
}

func TestBuildFullEnvForServer_BothEnvAndCredentialsHavePlaceholders(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credentials
	err := credentials.SetToActiveBackend("mcp/both-server/ENV_KEY", "env-credential")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/both-server/CRED_KEY", "cred-credential")
	require.NoError(t, err)

	// Arrange: Server config with placeholders in both Env and Credentials
	config := &MCPServerConfig{
		Name: "both-server",
		Env: map[string]string{
			"PATH":     "/usr/bin",
			"ENV_KEY":  SecretRef("both-server", "ENV_KEY"), // Placeholder in Env
		},
		Credentials: map[string]string{
			"CRED_KEY": SecretRef("both-server", "CRED_KEY"), // Placeholder in Credentials
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("both-server", config)

	// Assert: Both placeholders should be resolved
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "env-credential", result["ENV_KEY"])
	assert.Equal(t, "cred-credential", result["CRED_KEY"])
}

func TestBuildFullEnvForServer_CredentialFallbackToOSEnv(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Set OS environment variable
	t.Setenv("FALLBACK_KEY", "from-os-env")

	// Arrange: Server config with placeholder not stored in credential backend
	config := &MCPServerConfig{
		Name: "fallback-server",
		Env: map[string]string{
			"PATH": "/usr/bin",
		},
		Credentials: map[string]string{
			"FALLBACK_KEY": SecretRef("fallback-server", "FALLBACK_KEY"),
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("fallback-server", config)

	// Assert: Should fall back to OS environment
	require.NoError(t, err)
	assert.Len(t, result, 2)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "from-os-env", result["FALLBACK_KEY"], "should fall back to OS when credential not in backend")
}

func TestBuildFullEnvForServer_AllEmptyMaps(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with empty maps
	config := &MCPServerConfig{
		Name:         "empty-all-server",
		Env:          map[string]string{},
		Credentials: map[string]string{},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("empty-all-server", config)

	// Assert: Should return empty map
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestBuildFullEnvForServer_AllNilMaps(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with nil maps
	config := &MCPServerConfig{
		Name:         "nil-all-server",
		Env:          nil,
		Credentials: nil,
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("nil-all-server", config)

	// Assert: Should return empty map (no panic)
	require.NoError(t, err)
	assert.Empty(t, result)
}

func TestBuildFullEnvForServer_MultipleCredentials(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store multiple credentials
	err := credentials.SetToActiveBackend("mcp/multi-server/OPENAI_API_KEY", "sk-openai")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/multi-server/ANTHROPIC_API_KEY", "sk-ant")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/multi-server/GITHUB_TOKEN", "ghp-github")
	require.NoError(t, err)
	err = credentials.SetToActiveBackend("mcp/multi-server/COHERE_API_KEY", "cohere-key")
	require.NoError(t, err)

	// Arrange: Server config with multiple credentials
	config := &MCPServerConfig{
		Name: "multi-server",
		Env: map[string]string{
			"PATH": "/usr/bin",
			"HOME": "/home/user",
		},
		Credentials: map[string]string{
			"OPENAI_API_KEY":      SecretRef("multi-server", "OPENAI_API_KEY"),
			"ANTHROPIC_API_KEY":   SecretRef("multi-server", "ANTHROPIC_API_KEY"),
			"GITHUB_TOKEN":        SecretRef("multi-server", "GITHUB_TOKEN"),
			"COHERE_API_KEY":      SecretRef("multi-server", "COHERE_API_KEY"),
		},
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("multi-server", config)

	// Assert: All credentials should be resolved
	require.NoError(t, err)
	assert.Len(t, result, 6)
	assert.Equal(t, "sk-openai", result["OPENAI_API_KEY"])
	assert.Equal(t, "sk-ant", result["ANTHROPIC_API_KEY"])
	assert.Equal(t, "ghp-github", result["GITHUB_TOKEN"])
	assert.Equal(t, "cohere-key", result["COHERE_API_KEY"])
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "/home/user", result["HOME"])
}

func TestBuildFullEnvForServer_NonPlaceholderCredentialValues(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with non-placeholder values in Credentials (edge case)
	config := &MCPServerConfig{
		Name: "non-placeholder-creds",
		Env: map[string]string{
			"PATH": "/usr/bin",
		},
		Credentials: map[string]string{
			"STATIC_VALUE": "static-config-value", // Not a placeholder, should pass through
			"SECRET_KEY":   SecretRef("non-placeholder-creds", "SECRET_KEY"), // Placeholder
		},
	}

	// Arrange: Store the secret
	err := credentials.SetToActiveBackend("mcp/non-placeholder-creds/SECRET_KEY", "sk-secret")
	require.NoError(t, err)

	// Act: Build full environment
	result, err := BuildFullEnvForServer("non-placeholder-creds", config)

	// Assert: Both static and resolved values should be present
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.Equal(t, "/usr/bin", result["PATH"])
	assert.Equal(t, "static-config-value", result["STATIC_VALUE"], "non-placeholder credential should pass through")
	assert.Equal(t, "sk-secret", result["SECRET_KEY"])
}

func TestBuildFullEnvForServer_InvalidPlaceholderInEnv(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Server config with invalid placeholder in Env
	config := &MCPServerConfig{
		Name: "invalid-ref-server",
		Env: map[string]string{
			"PATH":    "/usr/bin",
			"BAD_REF": "{{credential:not-valid-format}}", // Invalid format
		},
		Credentials: nil,
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("invalid-ref-server", config)

	// Assert: Invalid placeholder should remain in result (can't be resolved, so original value kept)
	require.NoError(t, err)
	assert.Equal(t, "/usr/bin", result["PATH"])
	// Invalid placeholders are not removed - they stay as-is when resolution fails
	assert.Equal(t, "{{credential:not-valid-format}}", result["BAD_REF"])
}

func TestBuildFullEnvForServer_ServerNameMismatch(t *testing.T) {
	setupCredentialBackend(t)

	// Arrange: Store credential for server-b
	err := credentials.SetToActiveBackend("mcp/server-b/API_KEY", "sk-server-b")
	require.NoError(t, err)

	// Arrange: Server config for server-b with a placeholder that uses server-a
	// The placeholder format includes server-a in the ref, but BuildFullEnvForServer
	// uses the serverName parameter to build the credential key
	config := &MCPServerConfig{
		Name: "server-b",
		Env: map[string]string{
			"API_KEY": SecretRef("server-a", "API_KEY"), // Reference to different server
		},
		Credentials: nil,
	}

	// Act: Build full environment
	result, err := BuildFullEnvForServer("server-b", config)

	// Assert: The function uses the serverName parameter ("server-b") to build the key,
	// not the server name in the placeholder. Since we stored mcp/server-b/API_KEY,
	// the credential should be found and resolved.
	require.NoError(t, err)
	assert.Equal(t, "sk-server-b", result["API_KEY"])
}
