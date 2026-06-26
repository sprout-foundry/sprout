package mcp

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// MCPServerConfig.UnmarshalJSON Tests
// ---------------------------------------------------------------------------

func TestMCPServerConfig_UnmarshalJSON_TimeoutAsString(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx",
		"timeout": "30s"
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, "test-server", config.Name)
	assert.Equal(t, "npx", config.Command)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPServerConfig_UnmarshalJSON_TimeoutAsNumber(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx",
		"timeout": 60000000000
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, "test-server", config.Name)
	assert.Equal(t, "npx", config.Command)
	assert.Equal(t, 60*time.Second, config.Timeout)
}

func TestMCPServerConfig_UnmarshalJSON_TimeoutEmptyString(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx",
		"timeout": ""
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPServerConfig_UnmarshalJSON_TimeoutMissing(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx"
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPServerConfig_UnmarshalJSON_TimeoutUnrecognizedType(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx",
		"timeout": true
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPServerConfig_UnmarshalJSON_TimeoutInvalidString(t *testing.T) {
	data := []byte(`{
		"name": "test-server",
		"command": "npx",
		"timeout": "not-a-duration"
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout duration")
}

func TestMCPServerConfig_UnmarshalJSON_FullConfig(t *testing.T) {
	data := []byte(`{
		"name": "github",
		"type": "stdio",
		"command": "npx",
		"args": ["-y", "@modelcontextprotocol/server-github"],
		"working_dir": "/workspace",
		"timeout": "45s",
		"auto_start": true,
		"max_restarts": 5
	}`)

	var config MCPServerConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)

	assert.Equal(t, "github", config.Name)
	assert.Equal(t, "stdio", config.Type)
	assert.Equal(t, "npx", config.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, config.Args)
	assert.Equal(t, "/workspace", config.WorkingDir)
	assert.Equal(t, 45*time.Second, config.Timeout)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 5, config.MaxRestarts)
}

func TestMCPServerConfig_MarshalJSON_RoundTrip(t *testing.T) {
	original := MCPServerConfig{
		Name:        "test-server",
		Type:        "stdio",
		Command:     "uvx",
		Args:        []string{"-y", "some-server"},
		URL:         "http://example.com",
		WorkingDir:  "/workspace",
		Timeout:     60 * time.Second,
		AutoStart:   true,
		MaxRestarts: 10,
		Env: map[string]string{
			"PATH": "/usr/bin",
		},
		Credentials: map[string]string{
			"API_KEY": "{{credential:mcp/test-server/API_KEY}}",
		},
	}

	// Marshal
	data, err := json.Marshal(original)
	require.NoError(t, err)

	// Unmarshal
	var unmarshaled MCPServerConfig
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	// Verify all fields preserved
	assert.Equal(t, original.Name, unmarshaled.Name)
	assert.Equal(t, original.Type, unmarshaled.Type)
	assert.Equal(t, original.Command, unmarshaled.Command)
	assert.Equal(t, original.Args, unmarshaled.Args)
	assert.Equal(t, original.URL, unmarshaled.URL)
	assert.Equal(t, original.WorkingDir, unmarshaled.WorkingDir)
	assert.Equal(t, original.Timeout, unmarshaled.Timeout)
	assert.Equal(t, original.AutoStart, unmarshaled.AutoStart)
	assert.Equal(t, original.MaxRestarts, unmarshaled.MaxRestarts)
	assert.Equal(t, original.Env, unmarshaled.Env)
	assert.Equal(t, original.Credentials, unmarshaled.Credentials)
}

// ---------------------------------------------------------------------------
// MCPConfig.UnmarshalJSON Tests
// ---------------------------------------------------------------------------

func TestMCPConfig_UnmarshalJSON_TimeoutAsString(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"servers": {},
		"timeout": "45s"
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 45*time.Second, config.Timeout)
}

func TestMCPConfig_UnmarshalJSON_TimeoutAsNumber(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"servers": {},
		"timeout": 90000000000
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 90*time.Second, config.Timeout)
}

func TestMCPConfig_UnmarshalJSON_TimeoutEmptyString(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"servers": {},
		"timeout": ""
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPConfig_UnmarshalJSON_TimeoutMissing(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"servers": {}
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestMCPConfig_UnmarshalJSON_TimeoutInvalidString(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"servers": {},
		"timeout": "invalid"
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid timeout duration")
}

func TestMCPConfig_UnmarshalJSON_FullConfig(t *testing.T) {
	data := []byte(`{
		"enabled": true,
		"auto_start": true,
		"auto_discover": false,
		"timeout": "60s",
		"servers": {
			"github": {
				"name": "github",
				"command": "npx",
				"args": ["-y", "@modelcontextprotocol/server-github"],
				"auto_start": true,
				"max_restarts": 3,
				"timeout": "30s"
			},
			"git": {
				"name": "git",
				"command": "uvx",
				"args": ["mcp-server-git"],
				"auto_start": false,
				"max_restarts": 5,
				"timeout": "45s"
			}
		}
	}`)

	var config MCPConfig
	err := json.Unmarshal(data, &config)
	require.NoError(t, err)

	assert.True(t, config.Enabled)
	assert.True(t, config.AutoStart)
	assert.False(t, config.AutoDiscover)
	assert.Equal(t, 60*time.Second, config.Timeout)
	assert.Len(t, config.Servers, 2)

	// Check github server
	assert.Equal(t, "github", config.Servers["github"].Name)
	assert.Equal(t, "npx", config.Servers["github"].Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-github"}, config.Servers["github"].Args)
	assert.True(t, config.Servers["github"].AutoStart)
	assert.Equal(t, 3, config.Servers["github"].MaxRestarts)
	assert.Equal(t, 30*time.Second, config.Servers["github"].Timeout)

	// Check git server
	assert.Equal(t, "git", config.Servers["git"].Name)
	assert.Equal(t, "uvx", config.Servers["git"].Command)
	assert.Equal(t, []string{"mcp-server-git"}, config.Servers["git"].Args)
	assert.False(t, config.Servers["git"].AutoStart)
	assert.Equal(t, 5, config.Servers["git"].MaxRestarts)
	assert.Equal(t, 45*time.Second, config.Servers["git"].Timeout)
}

// ---------------------------------------------------------------------------
// MCPError.Error() Tests
// ---------------------------------------------------------------------------

func TestMCPError_Error(t *testing.T) {
	err := &MCPError{
		Code:    -32601,
		Message: "Method not found",
		Data:    "someData",
	}

	assert.Equal(t, "MCP error -32601: Method not found", err.Error())
}

func TestMCPError_Error_WithoutData(t *testing.T) {
	err := &MCPError{
		Code:    -32700,
		Message: "Parse error",
	}

	assert.Equal(t, "MCP error -32700: Parse error", err.Error())
}

// ---------------------------------------------------------------------------
// MCPServerConfig Methods Tests
// ---------------------------------------------------------------------------

func TestMCPServerConfig_GetCredentialNames(t *testing.T) {
	config := MCPServerConfig{
		Name: "test",
		Credentials: map[string]string{
			"API_KEY":    "{{credential:mcp/test/API_KEY}}",
			"AUTH_TOKEN": "{{credential:mcp/test/AUTH_TOKEN}}",
		},
	}

	names := config.GetCredentialNames()
	assert.ElementsMatch(t, []string{"API_KEY", "AUTH_TOKEN"}, names)
}

func TestMCPServerConfig_GetCredentialNames_Nil(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test",
		Credentials: nil,
	}

	names := config.GetCredentialNames()
	assert.Nil(t, names)
}

func TestMCPServerConfig_GetCredentialNames_Empty(t *testing.T) {
	config := MCPServerConfig{
		Name:        "test",
		Credentials: map[string]string{},
	}

	names := config.GetCredentialNames()
	assert.Empty(t, names)
}

func TestMCPServerConfig_HasCredentials(t *testing.T) {
	t.Run("has credentials", func(t *testing.T) {
		config := MCPServerConfig{
			Name: "test",
			Credentials: map[string]string{
				"API_KEY": "{{credential:mcp/test/API_KEY}}",
			},
		}
		assert.True(t, config.HasCredentials())
	})

	t.Run("nil credentials map", func(t *testing.T) {
		config := MCPServerConfig{
			Name:        "test",
			Credentials: nil,
		}
		assert.False(t, config.HasCredentials())
	})

	t.Run("empty credentials map", func(t *testing.T) {
		config := MCPServerConfig{
			Name:        "test",
			Credentials: map[string]string{},
		}
		assert.False(t, config.HasCredentials())
	})
}
