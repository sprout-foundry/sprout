package mcp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// RedactServerConfig
// ---------------------------------------------------------------------------

func TestRedactServerConfig(t *testing.T) {
	t.Run("nil_credentials_and_env", func(t *testing.T) {
		server := MCPServerConfig{
			Name:      "test",
			Command:   "npx",
			Args:      []string{"-y", "test"},
			AutoStart: true,
		}

		redacted := RedactServerConfig(server)

		assert.Equal(t, server, redacted,
			"server with nil credentials and env should be unchanged")
	})

	t.Run("credentials_are_masked", func(t *testing.T) {
		server := MCPServerConfig{
			Name:      "test",
			Command:   "npx",
			Credentials: map[string]string{
				"GITHUB_TOKEN": "ghp_abcdefghijklmnopqrstuvwxyz1234567890",
				"API_KEY":      "sk-abc12345678901234567",
			},
		}

		redacted := RedactServerConfig(server)

		// Credentials should be masked (MaskValue shows first 4 chars + ****)
		assert.Equal(t, "ghp_****", redacted.Credentials["GITHUB_TOKEN"],
			"github token should be masked")
		assert.Equal(t, "sk-a****", redacted.Credentials["API_KEY"],
			"api key should be masked")
	})

	t.Run("credential_references_preserved", func(t *testing.T) {
		server := MCPServerConfig{
			Name: "test",
			Credentials: map[string]string{
				"GITHUB_TOKEN": "{{credential:mcp/server/GITHUB_TOKEN}}",
				"API_KEY":      "{{stored}}",
			},
		}

		redacted := RedactServerConfig(server)

		// Credential references should be preserved as-is
		assert.Equal(t, "{{credential:mcp/server/GITHUB_TOKEN}}", redacted.Credentials["GITHUB_TOKEN"])
		assert.Equal(t, "{{stored}}", redacted.Credentials["API_KEY"])
	})

	t.Run("env_sensitive_keys_redacted", func(t *testing.T) {
		server := MCPServerConfig{
			Name: "test",
			Env: map[string]string{
				"GITHUB_TOKEN":     "ghp_abc123",
				"PATH":             "/usr/bin",
				"MY_SECRET":        "secret123",
				"NODE_ENV":         "production",
			},
		}

		redacted := RedactServerConfig(server)

		// Sensitive env vars should be redacted
		assert.Equal(t, "[REDACTED]", redacted.Env["GITHUB_TOKEN"])
		assert.Equal(t, "[REDACTED]", redacted.Env["MY_SECRET"])

		// Non-sensitive env vars should be preserved
		assert.Equal(t, "/usr/bin", redacted.Env["PATH"])
		assert.Equal(t, "production", redacted.Env["NODE_ENV"])
	})

	t.Run("original_server_not_modified", func(t *testing.T) {
		original := MCPServerConfig{
			Name: "test",
			Credentials: map[string]string{
				"TOKEN": "ghp_abc123",
			},
			Env: map[string]string{
				"SECRET": "value",
			},
		}
		originalCopy := MCPServerConfig{
			Name: "test",
			Credentials: map[string]string{
				"TOKEN": "ghp_abc123",
			},
			Env: map[string]string{
				"SECRET": "value",
			},
		}

		_ = RedactServerConfig(original)

		assert.Equal(t, originalCopy, original,
			"original server should not be modified")
	})

	t.Run("preserves_all_fields", func(t *testing.T) {
		server := MCPServerConfig{
			Name:        "test",
			Type:        "stdio",
			Command:     "npx",
			Args:        []string{"-y", "@server"},
			URL:         "https://example.com",
			Env:         map[string]string{"PATH": "/usr/bin"},
			Credentials: map[string]string{"TOKEN": "secret"},
			WorkingDir:  "/tmp",
			Timeout:     30 * 1e9,
			AutoStart:   true,
			MaxRestarts: 3,
		}

		redacted := RedactServerConfig(server)

		assert.Equal(t, server.Name, redacted.Name)
		assert.Equal(t, server.Type, redacted.Type)
		assert.Equal(t, server.Command, redacted.Command)
		assert.Equal(t, server.Args, redacted.Args)
		assert.Equal(t, server.URL, redacted.URL)
		assert.Equal(t, server.WorkingDir, redacted.WorkingDir)
		assert.Equal(t, server.Timeout, redacted.Timeout)
		assert.Equal(t, server.AutoStart, redacted.AutoStart)
		assert.Equal(t, server.MaxRestarts, redacted.MaxRestarts)
	})
}

// ---------------------------------------------------------------------------
// RedactMCPConfig
// ---------------------------------------------------------------------------

func TestRedactMCPConfig(t *testing.T) {
	t.Run("empty_config", func(t *testing.T) {
		config := MCPConfig{
			Enabled:      true,
			Servers:      make(map[string]MCPServerConfig),
			AutoStart:    false,
			AutoDiscover: true,
		}

		redacted := RedactMCPConfig(config)

		assert.Equal(t, config.Enabled, redacted.Enabled)
		assert.Equal(t, config.AutoStart, redacted.AutoStart)
		assert.Equal(t, config.AutoDiscover, redacted.AutoDiscover)
		assert.NotNil(t, redacted.Servers)
		assert.Empty(t, redacted.Servers)
	})

	t.Run("redacts_all_server_credentials", func(t *testing.T) {
		config := MCPConfig{
			Enabled: true,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name: "server1",
					Credentials: map[string]string{
						"TOKEN": "ghp_abc123",
					},
				},
				"server2": {
					Name: "server2",
					Credentials: map[string]string{
						"API_KEY": "sk-abc123",
					},
				},
			},
		}

		redacted := RedactMCPConfig(config)

		assert.NotNil(t, redacted.Servers)
		assert.Equal(t, 2, len(redacted.Servers))
		assert.Equal(t, "ghp_****", redacted.Servers["server1"].Credentials["TOKEN"])
		assert.Equal(t, "sk-a****", redacted.Servers["server2"].Credentials["API_KEY"])
	})

	t.Run("preserves_non_sensitive_fields", func(t *testing.T) {
		config := MCPConfig{
			Enabled:      true,
			AutoStart:    false,
			AutoDiscover: true,
			Timeout:      30 * 1e9,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name:        "server1",
					Type:        "stdio",
					Command:     "npx",
					Args:        []string{"-y", "test"},
					AutoStart:   true,
					MaxRestarts: 3,
					Env: map[string]string{
						"PATH": "/usr/bin",
					},
				},
			},
		}

		redacted := RedactMCPConfig(config)

		assert.Equal(t, config.Enabled, redacted.Enabled)
		assert.Equal(t, config.AutoStart, redacted.AutoStart)
		assert.Equal(t, config.AutoDiscover, redacted.AutoDiscover)
		assert.Equal(t, config.Timeout, redacted.Timeout)
		assert.Equal(t, "stdio", redacted.Servers["server1"].Type)
		assert.Equal(t, "npx", redacted.Servers["server1"].Command)
		assert.Equal(t, "/usr/bin", redacted.Servers["server1"].Env["PATH"])
	})

	t.Run("original_config_not_modified", func(t *testing.T) {
		original := MCPConfig{
			Enabled: true,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name: "server1",
					Credentials: map[string]string{
						"TOKEN": "ghp_abc123",
					},
				},
			},
		}
		originalCopy := MCPConfig{
			Enabled: true,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name: "server1",
					Credentials: map[string]string{
						"TOKEN": "ghp_abc123",
					},
				},
			},
		}

		_ = RedactMCPConfig(original)

		assert.Equal(t, originalCopy, original,
			"original config should not be modified")
	})
}

// ---------------------------------------------------------------------------
// MarshalJSONWithRedaction
// ---------------------------------------------------------------------------

func TestMarshalJSONWithRedaction(t *testing.T) {
	t.Run("redacts_credentials_in_json", func(t *testing.T) {
		config := MCPConfig{
			Enabled: true,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name: "server1",
					Credentials: map[string]string{
						"GITHUB_TOKEN": "ghp_abcdefghijklmnopqrstuvwxyz1234567890",
					},
					Env: map[string]string{
						"MY_SECRET": "secret123",
						"PATH":      "/usr/bin",
					},
				},
			},
		}

		data, err := MarshalJSONWithRedaction(config)
		assert.NoError(t, err)
		assert.NotNil(t, data)

		// Should contain redacted values
		assert.Contains(t, string(data), "[REDACTED]")
		assert.NotContains(t, string(data), "ghp_abcdefghijklmnopqrstuvwxyz1234567890")
		assert.NotContains(t, string(data), "secret123")

		// Should preserve non-sensitive data
		assert.Contains(t, string(data), "server1")
		assert.Contains(t, string(data), "/usr/bin")
		assert.Contains(t, string(data), "true")
	})

	t.Run("returns_error_for_invalid_input", func(t *testing.T) {
		// This test is not applicable as MarshalJSONWithRedaction doesn't
		// take invalid input - it always works with valid MCPConfig structs
	})

	t.Run("json_formatting", func(t *testing.T) {
		config := MCPConfig{
			Enabled: true,
			Servers: map[string]MCPServerConfig{
				"server1": {
					Name: "server1",
				},
			},
		}

		data, err := MarshalJSONWithRedaction(config)
		assert.NoError(t, err)

		// Should be indented JSON
		assert.Contains(t, string(data), "\n")
		assert.Contains(t, string(data), "  ")
	})
}
