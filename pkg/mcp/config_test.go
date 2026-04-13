package mcp

import (
	"testing"
	"time"

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
				Timeout:    30 * time.Second,
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
				Timeout:    30 * time.Second,
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
				Name:    "github",
				Type:    "http",
				URL:    "", // Missing URL for HTTP server
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
				Name:        "github",
				Command:     "npx",
				AutoStart:   true,
				Timeout:    -1 * time.Second, // Negative
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
		Name:  "http-server",
		Type:  "http",
		URL:   "",
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
		Timeout:    60 * time.Second,
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
		Timeout:     30 * time.Second,
		Servers: map[string]MCPServerConfig{
			"github": {
				Name:        "github",
				Command:     "npx",
				Args:        []string{"-y", "server"},
				AutoStart:   true,
				MaxRestarts: 3,
				Timeout:    30 * time.Second,
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
					"PATH":      "/usr/bin",        // Non-secret, should show
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
		Enabled:  true,
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