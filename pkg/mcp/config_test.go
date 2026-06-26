package mcp

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// ValidateConfig Tests
// ---------------------------------------------------------------------------

func TestValidateConfig_ValidConfig(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-github"},
				AutoStart:   true,
				MaxRestarts: 3,
				Timeout:     30 * time.Second,
			},
		},
	}

	err := config.ValidateConfig()
	assert.NoError(t, err)
}

func TestValidateConfig_EmptyServers(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no MCP servers configured")
}

func TestValidateConfig_ServerNameMismatch(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "wrong-name", // Different from key
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
				Timeout:     30 * time.Second,
			},
		},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "name mismatch")
}

func TestValidateConfig_HTTP_MissingURL(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:      "github",
				Type:      "http",
				URL:       "", // Missing URL for HTTP server
				AutoStart: true,
			},
		},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL cannot be empty")
}

func TestValidateConfig_Stdio_MissingCommand(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:      "github",
				Command:   "", // Missing command for stdio server
				AutoStart: true,
			},
		},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command cannot be empty")
}

func TestValidateConfig_NegativeMaxRestarts(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: -1, // Negative
			},
		},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "max_restarts cannot be negative")
}

func TestValidateConfig_NegativeTimeout(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:      "github",
				Command:   "npx",
				AutoStart: true,
				Timeout:   -1 * time.Second, // Negative
			},
		},
	}

	err := config.ValidateConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "timeout cannot be negative")
}

func TestValidateConfig_DisabledConfig(t *testing.T) {
	config := MCPConfig{
		Enabled: false, // Disabled - validation should pass
		Servers: map[string]MCPServerConfig{},
	}

	err := config.ValidateConfig()
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// AddServer Tests
// ---------------------------------------------------------------------------

func TestAddServer_EmptyName(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name:    "",
		Command: "npx",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "server name cannot be empty")
}

func TestAddServer_HTTP_MissingURL(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name: "http-server",
		Type: "http",
		URL:  "",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "URL cannot be empty")
}

func TestAddServer_Stdio_MissingCommand(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name:    "stdio-server",
		Type:    "stdio",
		Command: "",
	})

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command cannot be empty")
}

func TestAddServer_DefaultMaxRestarts(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name:    "github",
		Command: "npx",
	})
	require.NoError(t, err)

	// Check defaults were applied
	assert.Equal(t, 3, config.Servers["github"].MaxRestarts)
}

func TestAddServer_DefaultTimeout(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name:    "github",
		Command: "npx",
	})
	require.NoError(t, err)

	// Check default timeout was applied
	assert.Equal(t, 30*time.Second, config.Servers["github"].Timeout)
}

func TestAddServer_Success(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	err := config.AddServer(MCPServerConfig{
		Name:        "custom",
		Command:     "npx",
		Args:        []string{"-y", "some-server"},
		AutoStart:   true,
		MaxRestarts: 5,
		Timeout:     60 * time.Second,
	})
	require.NoError(t, err)

	assert.Equal(t, "custom", config.Servers["custom"].Name)
	assert.Equal(t, "npx", config.Servers["custom"].Command)
	assert.Equal(t, []string{"-y", "some-server"}, config.Servers["custom"].Args)
	assert.Equal(t, true, config.Servers["custom"].AutoStart)
	assert.Equal(t, 5, config.Servers["custom"].MaxRestarts)
	assert.Equal(t, 60*time.Second, config.Servers["custom"].Timeout)
}

// ---------------------------------------------------------------------------
// GetEnabledServers Tests
// ---------------------------------------------------------------------------

func TestGetEnabledServers_ReturnsAutoStartServers(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:      "github",
				Command:   "npx",
				AutoStart: true,
			},
			"git": {
				Name:      "git",
				Command:   "uvx",
				AutoStart: false,
			},
		},
	}

	enabled := config.GetEnabledServers()
	assert.Len(t, enabled, 1)
	assert.Equal(t, "github", enabled[0].Name)
}

func TestGetEnabledServers_EmptyWhenDisabled(t *testing.T) {
	config := MCPConfig{
		Enabled: false,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:      "github",
				Command:   "npx",
				AutoStart: true,
			},
		},
	}

	enabled := config.GetEnabledServers()
	assert.Nil(t, enabled)
}

func TestGetEnabledServers_EmptyWhenNoServers(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{},
	}

	enabled := config.GetEnabledServers()
	assert.Empty(t, enabled)
}

// ---------------------------------------------------------------------------
// GetConfigSummary Tests
// ---------------------------------------------------------------------------

func TestGetConfigSummary_IncludesServerInfo(t *testing.T) {
	config := MCPConfig{
		Enabled:      true,
		AutoStart:    true,
		AutoDiscover: true,
		Timeout:      30 * time.Second,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				Args:        []string{"-y", "server"},
				AutoStart:   true,
				MaxRestarts: 3,
				Timeout:     30 * time.Second,
				Env: map[string]string{
					"PATH": "/usr/bin",
				},
			},
		},
	}

	summary := config.GetConfigSummary()

	assert.Equal(t, true, summary["enabled"])
	assert.Equal(t, true, summary["auto_start"])
	assert.Equal(t, true, summary["auto_discover"])
	assert.Equal(t, "30s", summary["timeout"])
	assert.Equal(t, 1, summary["server_count"])

	serverSummary := summary["servers"].(map[string]interface{})["github"].(map[string]interface{})
	assert.Equal(t, "npx", serverSummary["command"])
	assert.Equal(t, []string{"-y", "server"}, serverSummary["args"])
	assert.Equal(t, true, serverSummary["auto_start"])
	assert.Equal(t, 3, serverSummary["max_restarts"])
	assert.Equal(t, "30s", serverSummary["timeout"])
}

func TestGetConfigSummary_HidesEnvValues(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:    "github",
				Command: "npx",
				Env: map[string]string{
					"GITHUB_TOKEN": "secret-token-123", // Should be hidden
					"PATH":         "/usr/bin",         // Non-secret, should show
				},
			},
		},
	}

	summary := config.GetConfigSummary()

	serverSummary := summary["servers"].(map[string]interface{})["github"].(map[string]interface{})
	// The env vars should be exposed as keys only (not values)
	envVars := serverSummary["env_vars"].([]string)
	assert.Contains(t, envVars, "GITHUB_TOKEN")
	assert.Contains(t, envVars, "PATH")
}

// ---------------------------------------------------------------------------
// HasGitHubToken Tests
// ---------------------------------------------------------------------------

func TestHasGitHubToken_WithToken(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name: "github",
				Env: map[string]string{
					"GITHUB_PERSONAL_ACCESS_TOKEN": "ghp_xxx",
				},
			},
		},
	}

	assert.True(t, config.HasGitHubToken())
}

func TestHasGitHubToken_NoToken(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name: "github",
				Env: map[string]string{
					"PATH": "/usr/bin",
				},
			},
		},
	}

	assert.False(t, config.HasGitHubToken())
}

func TestHasGitHubToken_EmptyServer(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{},
	}

	assert.False(t, config.HasGitHubToken())
}

// ---------------------------------------------------------------------------
// DefaultMCPConfig Tests
// ---------------------------------------------------------------------------

func TestDefaultMCPConfig(t *testing.T) {
	config := DefaultMCPConfig()

	assert.True(t, config.Enabled)
	assert.False(t, config.AutoStart)
	assert.True(t, config.AutoDiscover)
	assert.Equal(t, 30*time.Second, config.Timeout)
	assert.NotNil(t, config.Servers)
}

// ---------------------------------------------------------------------------
// AddGitHubServer Tests
// ---------------------------------------------------------------------------

func TestAddGitHubServer_CreatesServerEntry(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()

	config := MCPConfig{
		Enabled: false,
		Servers: map[string]MCPServerConfig{},
	}

	config.AddGitHubServer("ghp_test_token_12345")

	// Verify server was added
	assert.Contains(t, config.Servers, "github")
	server := config.Servers["github"]
	assert.Equal(t, "github", server.Name)
	assert.Equal(t, "npx", server.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, server.Args)
	assert.True(t, server.AutoStart)
	assert.Equal(t, 3, server.MaxRestarts)
	assert.Equal(t, 30*time.Second, server.Timeout)

	// Verify the token was migrated to Credentials (not left in Env)
	assert.NotNil(t, server.Credentials, "Credentials map should be populated after migration")
	assert.Contains(t, server.Credentials, "GITHUB_PERSONAL_ACCESS_TOKEN",
		"token should be in Credentials, not Env")
	assert.True(t, IsSecretRef(server.Credentials["GITHUB_PERSONAL_ACCESS_TOKEN"]),
		"token should be a secret ref placeholder")
	_, inEnv := server.Env["GITHUB_PERSONAL_ACCESS_TOKEN"]
	assert.False(t, inEnv, "token should NOT remain in Env after migration")

	// Verify the actual credential was stored in the backend
	val, _, err := credentials.GetFromActiveBackend("mcp/github/GITHUB_PERSONAL_ACCESS_TOKEN")
	require.NoError(t, err)
	assert.Equal(t, "ghp_test_token_12345", val)
}

func TestAddGitHubServer_SetsEnabled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()

	config := MCPConfig{
		Enabled: false,
		Servers: map[string]MCPServerConfig{},
	}

	config.AddGitHubServer("ghp_test_token_12345")

	// Verify Enabled was set to true
	assert.True(t, config.Enabled)
}

func TestAddGitHubServer_InitializesServersMap(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	t.Cleanup(func() { credentials.ResetStorageBackend() })
	credentials.ResetStorageBackend()

	config := MCPConfig{
		Enabled: false,
		Servers: nil, // nil map
	}

	config.AddGitHubServer("ghp_test_token_12345")

	// Verify Servers map was initialized
	assert.NotNil(t, config.Servers)
	assert.Contains(t, config.Servers, "github")
}

// ---------------------------------------------------------------------------
// RemoveServer Tests
// ---------------------------------------------------------------------------

func TestRemoveServer_RemovesServerFromMap(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
			},
		},
	}

	config.RemoveServer("github")

	// Verify server was removed
	assert.NotContains(t, config.Servers, "github")
}

func TestRemoveServer_SetsDisabledWhenEmpty(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
			},
		},
	}

	config.RemoveServer("github")

	// Verify Enabled was set to false since no servers remain
	assert.False(t, config.Enabled)
}

func TestRemoveServer_NoOpWhenServerDoesNotExist(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
			},
		},
	}

	// Should not panic or error
	config.RemoveServer("nonexistent")

	// Original server should still be there
	assert.Contains(t, config.Servers, "github")
	assert.True(t, config.Enabled)
}

func TestRemoveServer_RemovesOneServerKeepsEnabled(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
			},
			"git": {
				Name:        "git",
				Command:     "uvx",
				AutoStart:   true,
				MaxRestarts: 3,
			},
		},
	}

	config.RemoveServer("github")

	// Verify server was removed but config remains enabled
	assert.NotContains(t, config.Servers, "github")
	assert.Contains(t, config.Servers, "git")
	assert.True(t, config.Enabled)
}

func TestRemoveServer_CleansUpCredentials(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_CREDENTIAL_BACKEND", "file")
	credentials.ResetStorageBackend()

	config := MCPConfig{
		Enabled: true,
		Servers: map[string]MCPServerConfig{
			"testserver": {
				Name:        "testserver",
				Command:     "npx",
				AutoStart:   true,
				MaxRestarts: 3,
				Credentials: map[string]string{
					"API_KEY":    "{{credential:mcp/testserver/API_KEY}}",
					"AUTH_TOKEN": "{{credential:mcp/testserver/AUTH_TOKEN}}",
				},
			},
		},
	}

	// Store credentials in the backend
	require.NoError(t, credentials.SetToActiveBackend("mcp/testserver/API_KEY", "secret-key-123"))
	require.NoError(t, credentials.SetToActiveBackend("mcp/testserver/AUTH_TOKEN", "secret-token-456"))

	// Verify credentials exist
	val1, _, err1 := credentials.GetFromActiveBackend("mcp/testserver/API_KEY")
	require.NoError(t, err1)
	assert.Equal(t, "secret-key-123", val1)

	val2, _, err2 := credentials.GetFromActiveBackend("mcp/testserver/AUTH_TOKEN")
	require.NoError(t, err2)
	assert.Equal(t, "secret-token-456", val2)

	// Remove server (should clean up credentials)
	config.RemoveServer("testserver")

	// Verify server was removed
	assert.NotContains(t, config.Servers, "testserver")

	// Verify credentials were deleted from the backend
	// FileBackend returns empty string for missing keys
	val1, _, _ = credentials.GetFromActiveBackend("mcp/testserver/API_KEY")
	assert.Equal(t, "", val1, "API_KEY credential should be deleted")

	val2, _, _ = credentials.GetFromActiveBackend("mcp/testserver/AUTH_TOKEN")
	assert.Equal(t, "", val2, "AUTH_TOKEN credential should be deleted")
}

// ---------------------------------------------------------------------------
// LoadMCPConfig Environment Variable Override Tests
// ---------------------------------------------------------------------------

func TestLoadMCPConfig_EnvOverrideEnabledFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_MCP_ENABLED", "false")
	t.Setenv("LEDIT_MCP_AUTO_START", "false")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.False(t, config.Enabled)
}

func TestLoadMCPConfig_EnvOverrideEnabledTrue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_MCP_ENABLED", "true")
	t.Setenv("LEDIT_MCP_AUTO_START", "false")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.True(t, config.Enabled)
}

func TestLoadMCPConfig_EnvOverrideEnabledOne(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_MCP_ENABLED", "1")
	t.Setenv("LEDIT_MCP_AUTO_START", "false")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.True(t, config.Enabled)
}

func TestLoadMCPConfig_EnvOverrideAutoStartTrue(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_MCP_ENABLED", "true")
	t.Setenv("LEDIT_MCP_AUTO_START", "true")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.True(t, config.AutoStart)
}

func TestLoadMCPConfig_EnvOverrideAutoDiscoverFalse(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", dir)
	t.Setenv("SPROUT_CONFIG", dir)
	t.Setenv("LEDIT_MCP_ENABLED", "true")
	t.Setenv("LEDIT_MCP_AUTO_START", "false")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "false")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.False(t, config.AutoDiscover)
}

func TestLoadMCPConfig_LoadsFromCustomConfigDir(t *testing.T) {
	customDir := t.TempDir()
	t.Setenv("LEDIT_CONFIG", customDir)
	t.Setenv("SPROUT_CONFIG", customDir)
	t.Setenv("LEDIT_MCP_ENABLED", "")
	t.Setenv("LEDIT_MCP_AUTO_START", "")
	t.Setenv("LEDIT_MCP_AUTO_DISCOVER", "")
	t.Setenv("GITHUB_PERSONAL_ACCESS_TOKEN", "")

	// Create a custom config file
	// Note: auto_discover must be false to prevent auto-discovery from setting enabled=true
	configData := `{
		"enabled": false,
		"auto_discover": false,
		"servers": {
			"custom": {
				"name": "custom",
				"command": "custom-cmd",
				"auto_start": false,
				"max_restarts": 5
			}
		}
	}`
	err := os.WriteFile(filepath.Join(customDir, "mcp_config.json"), []byte(configData), 0600)
	require.NoError(t, err)

	config, err := LoadMCPConfig()
	require.NoError(t, err)
	assert.False(t, config.Enabled)
	assert.Contains(t, config.Servers, "custom")
	assert.Equal(t, "custom-cmd", config.Servers["custom"].Command)
}
