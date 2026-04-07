package mcp

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// IsSecretEnvVar
// ---------------------------------------------------------------------------

func TestIsSecretEnvVar(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		// Known secret vars
		{"known secret GITHUB_PERSONAL_ACCESS_TOKEN", "GITHUB_PERSONAL_ACCESS_TOKEN", true},

		// Keyword matches
		{"API_KEY keyword", "OPENAI_API_KEY", true},
		{"SECRET keyword", "MY_SECRET", true},
		{"TOKEN keyword", "AUTH_TOKEN", true},
		{"PASSWORD keyword", "MY_PASSWORD", true},
		{"PRIVATE_KEY keyword", "MY_PRIVATE_KEY", true},
		{"BEARER keyword", "BEARER_TOKEN", true},
		{"CREDENTIAL keyword", "SOME_CREDENTIAL", true},
		{"PAT keyword", "MY_PAT", true},
		{"ACCESS_KEY keyword", "MY_ACCESS_KEY", true},
		{"SECRET_KEY keyword", "MY_SECRET_KEY", true},
		{"PASSWD keyword", "DB_PASSWD", true},

		// Safe prefixes – should be false even if they contain a keyword substring
		{"PATH prefix", "PATH", false},
		{"HOME prefix", "HOME", false},
		{"NODE_PATH prefix", "NODE_PATH", false},
		{"PYTHON_PATH prefix", "PYTHON_PATH", false},
		{"NPM_CONFIG_DIR prefix", "NPM_CONFIG_DIR", false},
		{"LEDIT_ prefix", "LEDIT_SOMETHING", false},
		{"MCP_ prefix", "MCP_ENABLED", false},
		{"GO prefix", "GOPATH", false},
		{"JAVA prefix", "JAVA_HOME", false},
		{"CARGO prefix", "CARGO_HOME", false},
		{"RUSTUP prefix", "RUSTUP_HOME", false},
		{"GOROOT prefix", "GOROOT", false},

		// No keyword, not in known list
		{"random var", "SOME_RANDOM_VAR", false},
		{"DISPLAY", "DISPLAY", false},
		{"LANG", "LANG", false},
		{"TERM", "TERM", false},

		// Empty
		{"empty string", "", false},

		// Case insensitivity
		{"lowercase secret", "my_secret", true},
		{"mixed case token", "My_Api_Token", true},

		// Edge: KEYRINGS_PATH starts with PATH's safe-prefix match? No, PATH is exact prefix.
		{"PATHS not matched by PATH prefix", "PATHS_TOOLS", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSecretEnvVar(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// CredentialKey
// ---------------------------------------------------------------------------

func TestCredentialKey(t *testing.T) {
	tests := []struct {
		server string
		envVar string
		want   string
	}{
		{"myserver", "API_KEY", "mcp/myserver/API_KEY"},
		{"server-name", "TOKEN", "mcp/server-name/TOKEN"},
		{"s", "K", "mcp/s/K"},
		{"", "KEY", "mcp//KEY"},
		{"srv", "", "mcp/srv/"},
	}

	for _, tt := range tests {
		name := tt.server + "/" + tt.envVar
		t.Run(name, func(t *testing.T) {
			got := CredentialKey(tt.server, tt.envVar)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// SecretRef
// ---------------------------------------------------------------------------

func TestSecretRef(t *testing.T) {
	tests := []struct {
		server string
		envVar string
		want   string
	}{
		{"myserver", "API_KEY", "{{credential:mcp/myserver/API_KEY}}"},
		{"server-name", "TOKEN", "{{credential:mcp/server-name/TOKEN}}"},
	}

	for _, tt := range tests {
		t.Run(tt.server+"/"+tt.envVar, func(t *testing.T) {
			got := SecretRef(tt.server, tt.envVar)
			assert.Equal(t, tt.want, got)
		})
	}

	// SecretRef is built on CredentialKey; verify consistency
	t.Run("consistent with CredentialKey", func(t *testing.T) {
		ref := SecretRef("sv", "ENV")
		key := CredentialKey("sv", "ENV")
		assert.Contains(t, ref, key)
		assert.Equal(t, "{{credential:"+key+"}}", ref)
	})
}

// ---------------------------------------------------------------------------
// IsSecretRef
// ---------------------------------------------------------------------------

func TestIsSecretRef(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"valid ref", "{{credential:mcp/server/key}}", true},
		{"plain value", "plain-value", false},
		{"empty string", "", false},
		{"whitespace around valid ref", "  {{credential:mcp/s/k}}  ", true},
		{"different braced var", "{{something_else}}", false},
		{"credential prefix only", "{{credential:}}", true}, // edge case but matches pattern
		{"missing closing braces", "{{credential:mcp/s/k}", false},
		{"missing opening braces", "credential:mcp/s/k}}", false},
		{"extra whitespace inside", "{{ credential:mcp/s/k }}", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := IsSecretRef(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// ParseSecretRef
// ---------------------------------------------------------------------------

func TestParseSecretRef(t *testing.T) {
	tests := []struct {
		name       string
		input      string
		wantServer string
		wantEnv    string
		wantOK     bool
	}{
		{
			name:       "valid ref",
			input:      "{{credential:mcp/server/key}}",
			wantServer: "server",
			wantEnv:    "key",
			wantOK:     true,
		},
		{
			name:       "plain value",
			input:      "plain",
			wantServer: "",
			wantEnv:    "",
			wantOK:     false,
		},
		{
			name:       "not enoughSlash parts",
			input:      "{{credential:bad}}",
			wantServer: "",
			wantEnv:    "",
			wantOK:     false,
		},
		{
			name:       "too many slash parts",
			input:      "{{credential:extra/parts/here/more}}",
			wantServer: "",
			wantEnv:    "",
			wantOK:     false,
		},
		{
			name:       "wrong first part",
			input:      "{{credential:api/server/key}}",
			wantServer: "",
			wantEnv:    "",
			wantOK:     false,
		},
		{
			name:       "empty string",
			input:      "",
			wantServer: "",
			wantEnv:    "",
			wantOK:     false,
		},
		{
			name:       "whitespace around valid ref",
			input:      "  {{credential:mcp/myserver/API_KEY}}  ",
			wantServer: "myserver",
			wantEnv:    "API_KEY",
			wantOK:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, env, ok := ParseSecretRef(tt.input)
			assert.Equal(t, tt.wantOK, ok)
			assert.Equal(t, tt.wantServer, server)
			assert.Equal(t, tt.wantEnv, env)
		})
	}
}

// ---------------------------------------------------------------------------
// MaskEnvValue
// ---------------------------------------------------------------------------

func TestMaskEnvValue(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"secret ref shows stored", "{{credential:mcp/server/key}}", "{{stored}}"},
		{"long value masked to first4+stars", "abcdefghij", "abcd****"},
		{"short value 3 chars", "abc", "****"},
		{"empty string", "", "****"},
		{"two chars", "ab", "****"},
		{"exactly 4 chars", "abcd", "****"},
		{"5 chars", "abcde", "abcd****"},
		{"whitespace around ref", "  {{credential:mcp/s/k}}  ", "{{stored}}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := MaskEnvValue(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

// ---------------------------------------------------------------------------
// MaskEnvVars
// ---------------------------------------------------------------------------

func TestMaskEnvVars(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		got := MaskEnvVars(nil)
		assert.Nil(t, got)
	})

	t.Run("empty map returns empty map", func(t *testing.T) {
		got := MaskEnvVars(map[string]string{})
		assert.Empty(t, got)
	})

	t.Run("secret ref shows stored", func(t *testing.T) {
		env := map[string]string{
			"OPENAI_API_KEY": "{{credential:mcp/myserver/OPENAI_API_KEY}}",
		}
		got := MaskEnvVars(env)
		assert.Equal(t, "{{stored}}", got["OPENAI_API_KEY"])
	})

	t.Run("plain secret shows masked", func(t *testing.T) {
		env := map[string]string{
			"OPENAI_API_KEY": "sk-abcdefghijklmnop",
		}
		got := MaskEnvVars(env)
		assert.Equal(t, "sk-a****", got["OPENAI_API_KEY"])
	})

	t.Run("non-secret passes through", func(t *testing.T) {
		env := map[string]string{
			"PATH": "/usr/bin:/bin",
			"HOME": "/home/user",
		}
		got := MaskEnvVars(env)
		assert.Equal(t, env, got)
	})

	t.Run("mixed map", func(t *testing.T) {
		env := map[string]string{
			"OPENAI_API_KEY":          "sk-abcdefghijklmnop",
			"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_1234567890",
			"PATH":                        "/usr/bin",
			"MODEL":                       "gpt-4",
		}
		got := MaskEnvVars(env)
		assert.Equal(t, "sk-a****", got["OPENAI_API_KEY"])
		assert.Equal(t, "ghp_****", got["GITHUB_PERSONAL_ACCESS_TOKEN"])
		assert.Equal(t, "/usr/bin", got["PATH"])
		assert.Equal(t, "gpt-4", got["MODEL"])
	})
}

// ---------------------------------------------------------------------------
// MigrateEnvSecretsFromServer  (needs credential backend)
// ---------------------------------------------------------------------------

// setupCredentialBackend creates a temp config dir, sets LEDIT_CONFIG,
// and forces the file-based credential backend so tests do not depend
// on an OS keyring being present.
func setupCredentialBackend(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()
}

func TestMigrateEnvSecretsFromServer(t *testing.T) {
	t.Run("secret vars get migrated and replaced with refs", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{
				"OPENAI_API_KEY": "sk-secret123",
				"PATH":           "/usr/bin",
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// The secret should now be a ref
		assert.Equal(t, "{{credential:mcp/myserver/OPENAI_API_KEY}}", config.Env["OPENAI_API_KEY"])
		// Non-secret should be unchanged
		assert.Equal(t, "/usr/bin", config.Env["PATH"])

		// Verify the credential was actually stored
		val, _, err := credentials.GetFromActiveBackend("mcp/myserver/OPENAI_API_KEY")
		require.NoError(t, err)
		assert.Equal(t, "sk-secret123", val)
	})

	t.Run("already-migrated refs are left alone", func(t *testing.T) {
		setupCredentialBackend(t)

		ref := SecretRef("myserver", "AUTH_TOKEN")
		config := &MCPServerConfig{
			Env: map[string]string{
				"AUTH_TOKEN": ref,
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		assert.Equal(t, ref, config.Env["AUTH_TOKEN"])
	})

	t.Run("non-secret vars pass through", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{
				"PATH":  "/usr/bin",
				"HOME":  "/home/user",
				"MODEL": "gpt-4",
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		assert.Equal(t, "/usr/bin", config.Env["PATH"])
		assert.Equal(t, "/home/user", config.Env["HOME"])
		assert.Equal(t, "gpt-4", config.Env["MODEL"])
	})

	t.Run("empty env returns 0 nil", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{}
		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("nil env returns 0 nil", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{Env: nil}
		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("empty secret values are skipped", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{
				"OPENAI_API_KEY": "",
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		// Value should remain empty, not replaced with a ref
		assert.Equal(t, "", config.Env["OPENAI_API_KEY"])
	})

	t.Run("{{stored}} sentinel is not treated as a secret value", func(t *testing.T) {
		setupCredentialBackend(t)

		// Store a real credential first
		err := credentials.SetToActiveBackend("mcp/myserver/OPENAI_API_KEY", "sk-real-secret")
		require.NoError(t, err)

		config := &MCPServerConfig{
			Env: map[string]string{
				"OPENAI_API_KEY": "{{stored}}", // Frontend display sentinel
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count) // Should NOT overwrite the stored credential
		assert.Equal(t, "{{stored}}", config.Env["OPENAI_API_KEY"])

		// Verify the original credential is still intact
		val, _, err := credentials.GetFromActiveBackend("mcp/myserver/OPENAI_API_KEY")
		require.NoError(t, err)
		assert.Equal(t, "sk-real-secret", val)
	})

	t.Run("multiple secrets all migrated", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{
				"OPENAI_API_KEY":          "sk-openai",
				"AUTH_TOKEN":              "bearer-xyz",
				"MY_SECRET":               "super-secret",
				"PATH":                    "/usr/bin",
			},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 3, count)

		// All secrets replaced with refs
		assert.True(t, IsSecretRef(config.Env["OPENAI_API_KEY"]))
		assert.True(t, IsSecretRef(config.Env["AUTH_TOKEN"]))
		assert.True(t, IsSecretRef(config.Env["MY_SECRET"]))
		// Non-secret unchanged
		assert.Equal(t, "/usr/bin", config.Env["PATH"])
	})

	t.Run("config.Env not mutated when no secrets to migrate", func(t *testing.T) {
		setupCredentialBackend(t)

		config := &MCPServerConfig{
			Env: map[string]string{"PATH": "/usr/bin", "HOME": "/home/user"},
		}

		count, err := MigrateEnvSecretsFromServer("myserver", config)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		// Verify no values were changed
		assert.Equal(t, "/usr/bin", config.Env["PATH"])
		assert.Equal(t, "/home/user", config.Env["HOME"])
	})
}

// ---------------------------------------------------------------------------
// ResolveEnvVars  (needs credential backend)
// ---------------------------------------------------------------------------

func TestResolveEnvVars(t *testing.T) {
	t.Run("secret refs resolved from backend", func(t *testing.T) {
		setupCredentialBackend(t)

		// Pre-store a credential
		key := CredentialKey("myserver", "OPENAI_API_KEY")
		err := credentials.SetToActiveBackend(key, "sk-actual-secret")
		require.NoError(t, err)

		env := map[string]string{
			"OPENAI_API_KEY": SecretRef("myserver", "OPENAI_API_KEY"),
		}

		result, err := ResolveEnvVars("myserver", env)
		require.NoError(t, err)
		assert.Equal(t, "sk-actual-secret", result["OPENAI_API_KEY"])
	})

	t.Run("non-secret values pass through", func(t *testing.T) {
		setupCredentialBackend(t)

		env := map[string]string{
			"PATH":  "/usr/bin",
			"MODEL": "gpt-4",
		}

		result, err := ResolveEnvVars("myserver", env)
		require.NoError(t, err)
		assert.Equal(t, "/usr/bin", result["PATH"])
		assert.Equal(t, "gpt-4", result["MODEL"])
	})

	t.Run("mixed secrets and non-secrets", func(t *testing.T) {
		setupCredentialBackend(t)

		// Pre-store a credential
		key := CredentialKey("mixserver", "AUTH_TOKEN")
		err := credentials.SetToActiveBackend(key, "bearer-real")
		require.NoError(t, err)

		env := map[string]string{
			"AUTH_TOKEN":     SecretRef("mixserver", "AUTH_TOKEN"),
			"PATH":           "/usr/local/bin",
			"MAX_TOKENS":     "4096",
		}

		result, err := ResolveEnvVars("mixserver", env)
		require.NoError(t, err)
		assert.Equal(t, "bearer-real", result["AUTH_TOKEN"])
		assert.Equal(t, "/usr/local/bin", result["PATH"])
		assert.Equal(t, "4096", result["MAX_TOKENS"])
	})

	t.Run("nil env returns empty map", func(t *testing.T) {
		setupCredentialBackend(t)

		result, err := ResolveEnvVars("myserver", nil)
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("empty env returns empty map", func(t *testing.T) {
		setupCredentialBackend(t)

		result, err := ResolveEnvVars("myserver", map[string]string{})
		require.NoError(t, err)
		assert.Empty(t, result)
	})

	t.Run("missing credential in backend falls back to os env", func(t *testing.T) {
		setupCredentialBackend(t)

		// Set the env var in the OS environment
		t.Setenv("MY_API_KEY", "from-os-env")

		env := map[string]string{
			"MY_API_KEY": SecretRef("fallbackserver", "MY_API_KEY"),
		}

		result, err := ResolveEnvVars("fallbackserver", env)
		require.NoError(t, err)
		// The credential isn't stored, so it should fall back to os.Getenv
		assert.Equal(t, "from-os-env", result["MY_API_KEY"])
	})

	t.Run("missing credential with no os fallback is skipped", func(t *testing.T) {
		setupCredentialBackend(t)

		// Ensure the env var is NOT set
		t.Setenv("NONEXISTENT_KEY", "")

		env := map[string]string{
			"NONEXISTENT_KEY": SecretRef("myserver", "NONEXISTENT_KEY"),
		}

		result, err := ResolveEnvVars("myserver", env)
		require.NoError(t, err)
		// Key should not be in result since credential not found anywhere
		_, exists := result["NONEXISTENT_KEY"]
		assert.False(t, exists)
	})
}

// ---------------------------------------------------------------------------
// MigrateEnvSecrets  (needs credential backend)
// ---------------------------------------------------------------------------

func TestMigrateEnvSecrets(t *testing.T) {
	t.Run("secrets stored and replaced", func(t *testing.T) {
		setupCredentialBackend(t)

		env := map[string]string{
			"OPENAI_API_KEY": "sk-test",
			"PATH":           "/usr/bin",
		}

		result, count, err := MigrateEnvSecrets("myserver", env)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
		assert.Equal(t, SecretRef("myserver", "OPENAI_API_KEY"), result["OPENAI_API_KEY"])
		assert.Equal(t, "/usr/bin", result["PATH"])
	})

	t.Run("already-migrated refs skipped", func(t *testing.T) {
		setupCredentialBackend(t)

		ref := SecretRef("myserver", "AUTH_TOKEN")
		env := map[string]string{
			"AUTH_TOKEN": ref,
		}

		result, count, err := MigrateEnvSecrets("myserver", env)
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		assert.Equal(t, ref, result["AUTH_TOKEN"])
	})

	t.Run("empty env returns empty", func(t *testing.T) {
		setupCredentialBackend(t)

		result, count, err := MigrateEnvSecrets("myserver", map[string]string{})
		require.NoError(t, err)
		assert.Equal(t, 0, count)
		assert.Empty(t, result)
	})

	t.Run("stored secrets round-trip with resolve", func(t *testing.T) {
		setupCredentialBackend(t)

		// Migrate
		env := map[string]string{
			"OPENAI_API_KEY": "sk-roundtrip",
		}
		migrated, count, err := MigrateEnvSecrets("rtserver", env)
		require.NoError(t, err)
		assert.Equal(t, 1, count)

		// Resolve
		resolved, err := ResolveEnvVars("rtserver", migrated)
		require.NoError(t, err)
		assert.Equal(t, "sk-roundtrip", resolved["OPENAI_API_KEY"])
	})
}


