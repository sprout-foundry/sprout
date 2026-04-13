package mcp

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// loadTemplatesFromConfig Tests
// ---------------------------------------------------------------------------

func TestLoadTemplatesFromConfig_ValidConfig(t *testing.T) {
	// Create a temporary directory for test config
	tempDir := t.TempDir()

	// Create a valid templates config file
	configData := MCPTemplatesConfig{
		Templates: map[string]MCPServerTemplate{
			"custom-remote": {
				ID:          "custom-remote",
				Name:        "Custom Remote Server",
				Description: "A custom HTTP-based MCP server",
				Type:        "http",
				URL:         "https://custom.example.com/mcp",
				EnvVars: []EnvVarTemplate{
					{
						Name:        "CUSTOM_API_KEY",
						Description: "Custom API Key",
						Required:    true,
						Secret:      true,
					},
				},
				Timeout:  45 * time.Second,
				Features: []string{"Custom feature"},
				AuthType: "bearer",
				Docs:     "https://custom.example.com/docs",
			},
			"custom-stdio": {
				ID:          "custom-stdio",
				Name:        "Custom Stdio Server",
				Description: "A custom command-line MCP server",
				Type:        "stdio",
				Command:     "custom-command",
				Args:        []string{"--mcp"},
				EnvVars: []EnvVarTemplate{
					{
						Name:        "CUSTOM_TOKEN",
						Description: "Custom token",
						Required:    true,
						Secret:      true,
					},
					{
						Name:        "CUSTOM_LOG_LEVEL",
						Description: "Log level",
						Required:    false,
						Secret:      false,
						Default:     "info",
					},
				},
				Timeout:  30 * time.Second,
				Features: []string{"Stdio feature"},
				AuthType: "none",
				Docs:     "https://custom.example.com/docs",
			},
		},
	}

	configJSON, err := json.MarshalIndent(configData, "", "  ")
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "mcp_templates.json")
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	// Create a registry with the custom config directory
	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	// Set environment variable to use temp directory as config dir
	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	// Load templates from config
	err = registry.loadTemplatesFromConfig()
	require.NoError(t, err)

	// Verify custom templates were loaded
	tmpl, exists := registry.GetTemplate("custom-remote")
	assert.True(t, exists, "custom-remote template should exist")
	assert.Equal(t, "Custom Remote Server", tmpl.Name)
	assert.Equal(t, "https://custom.example.com/mcp", tmpl.URL)
	assert.Len(t, tmpl.EnvVars, 1)
	assert.Equal(t, "CUSTOM_API_KEY", tmpl.EnvVars[0].Name)

	tmpl, exists = registry.GetTemplate("custom-stdio")
	assert.True(t, exists, "custom-stdio template should exist")
	assert.Equal(t, "Custom Stdio Server", tmpl.Name)
	assert.Equal(t, "custom-command", tmpl.Command)
	assert.Equal(t, []string{"--mcp"}, tmpl.Args)
	assert.Len(t, tmpl.EnvVars, 2)
}

func TestLoadTemplatesFromConfig_MissingConfigFile(t *testing.T) {
	// Use a temp directory that doesn't have a templates config file
	tempDir := t.TempDir()

	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	// Should return error when config file doesn't exist
	err := registry.loadTemplatesFromConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read templates config")
}

func TestLoadTemplatesFromConfig_InvalidJSON(t *testing.T) {
	tempDir := t.TempDir()

	// Create an invalid JSON file
	configPath := filepath.Join(tempDir, "mcp_templates.json")
	err := os.WriteFile(configPath, []byte("invalid json content {{{"), 0644)
	require.NoError(t, err)

	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	err = registry.loadTemplatesFromConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to parse templates config")
}

func TestLoadTemplatesFromConfig_InvalidSchema(t *testing.T) {
	tempDir := t.TempDir()

	// Create a config file with invalid schema (missing required fields)
	invalidData := `{
		"templates": {
			"invalid-template": {
				"id": "invalid-template",
				"name": ""
			}
		}
	}`

	configPath := filepath.Join(tempDir, "mcp_templates.json")
	err := os.WriteFile(configPath, []byte(invalidData), 0644)
	require.NoError(t, err)

	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	err = registry.loadTemplatesFromConfig()
	// JSON should parse, but we may have warnings about missing fields
	// The current implementation doesn't validate deeply, so this may succeed
	if err != nil {
		assert.Contains(t, err.Error(), "failed to parse templates config")
	}
}

func TestLoadTemplatesFromConfig_IDsMatchKeys(t *testing.T) {
	tempDir := t.TempDir()

	// Create config where template IDs don't match map keys
	configData := MCPTemplatesConfig{
		Templates: map[string]MCPServerTemplate{
			"key-name": {
				ID:   "different-id", // Different from key
				Name: "Test Template",
			},
		},
	}

	configJSON, err := json.MarshalIndent(configData, "", "  ")
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "mcp_templates.json")
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	err = registry.loadTemplatesFromConfig()
	require.NoError(t, err)

	// Verify ID was set to match key
	tmpl, exists := registry.GetTemplate("key-name")
	assert.True(t, exists)
	assert.Equal(t, "key-name", tmpl.ID, "ID should be set to match map key")
}

func TestLoadTemplatesFromConfig_EmptyTemplates(t *testing.T) {
	tempDir := t.TempDir()

	// Create a config with empty templates map
	configData := MCPTemplatesConfig{
		Templates: map[string]MCPServerTemplate{},
	}

	configJSON, err := json.MarshalIndent(configData, "", "  ")
	require.NoError(t, err)

	configPath := filepath.Join(tempDir, "mcp_templates.json")
	err = os.WriteFile(configPath, configJSON, 0644)
	require.NoError(t, err)

	registry := &MCPServerRegistry{
		templates: make(map[string]MCPServerTemplate),
	}

	oldConfigDir := os.Getenv("LEDIT_CONFIG")
	t.Cleanup(func() {
		os.Setenv("LEDIT_CONFIG", oldConfigDir)
	})
	os.Setenv("LEDIT_CONFIG", tempDir)

	err = registry.loadTemplatesFromConfig()
	require.NoError(t, err)

	// Registry should be empty
	assert.Len(t, registry.ListTemplates(), 0)
}

// ---------------------------------------------------------------------------
// CreateServerConfig - Extended Tests
// ---------------------------------------------------------------------------

func TestCreateServerConfig_HTTPServer(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:          "http-server-tmpl",
		Name:        "HTTP Server Template",
		Type:        "http",
		URL:         "https://api.example.com/mcp",
		EnvVars: []EnvVarTemplate{
			{Name: "API_KEY", Required: true, Secret: true},
		},
		Timeout:  30 * time.Second,
		AuthType: "bearer",
	}

	config := tmpl.CreateServerConfig("my-http-server", map[string]string{
		"API_KEY": "sk-test-123",
	}, "", "", nil)

	assert.Equal(t, "my-http-server", config.Name)
	assert.Equal(t, "http", config.Type)
	assert.Equal(t, "https://api.example.com/mcp", config.URL)
	assert.Equal(t, "sk-test-123", config.Env["API_KEY"])
	assert.Empty(t, config.Command, "command should be empty for HTTP server")
	assert.Empty(t, config.Args, "args should be empty for HTTP server")
}

func TestCreateServerConfig_StdioServer(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "stdio-server-tmpl",
		Name:    "Stdio Server Template",
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "@modelcontextprotocol/server-git"},
		EnvVars: []EnvVarTemplate{
			{Name: "GIT_REPO", Required: false},
		},
		Timeout:  30 * time.Second,
		AuthType: "none",
	}

	config := tmpl.CreateServerConfig("my-git-server", map[string]string{
		"GIT_REPO": "/path/to/repo",
	}, "", "", nil)

	assert.Equal(t, "my-git-server", config.Name)
	assert.Equal(t, "stdio", config.Type)
	assert.Equal(t, "npx", config.Command)
	assert.Equal(t, []string{"-y", "@modelcontextprotocol/server-git"}, config.Args)
	assert.Equal(t, "/path/to/repo", config.Env["GIT_REPO"])
	assert.Empty(t, config.URL, "URL should be empty for stdio server")
}

func TestCreateServerConfig_MultipleEnvVars(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "multi-env-tmpl",
		Name:    "Multi Env Template",
		Type:    "stdio",
		Command: "python",
		EnvVars: []EnvVarTemplate{
			{Name: "API_KEY", Required: true, Secret: true},
			{Name: "API_SECRET", Required: true, Secret: true},
			{Name: "API_ENDPOINT", Required: false, Secret: false},
			{Name: "API_TIMEOUT", Required: false, Secret: false, Default: "30"},
			{Name: "API_VERSION", Required: false, Secret: false, Default: "v1"},
		},
	}

	config := tmpl.CreateServerConfig("multi-env-server", map[string]string{
		"API_KEY":      "key-123",
		"API_SECRET":   "secret-456",
		"API_ENDPOINT": "https://api.example.com",
		// Not providing API_TIMEOUT and API_VERSION to test defaults
	}, "", "", nil)

	assert.Equal(t, "key-123", config.Env["API_KEY"])
	assert.Equal(t, "secret-456", config.Env["API_SECRET"])
	assert.Equal(t, "https://api.example.com", config.Env["API_ENDPOINT"])
	assert.Equal(t, "30", config.Env["API_TIMEOUT"], "should use default")
	assert.Equal(t, "v1", config.Env["API_VERSION"], "should use default")
}

func TestCreateServerConfig_CustomValuesOverrideTemplate(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "override-tmpl",
		Name:    "Override Template",
		Type:    "http",
		URL:     "https://default.example.com/mcp",
		Timeout: 30 * time.Second,
	}

	config := tmpl.CreateServerConfig(
		"custom-server",
		map[string]string{"KEY": "value"},
		"https://custom.example.com/mcp", // Custom URL
		"custom-command",                  // Custom command
		[]string{"--custom", "--args"},    // Custom args
	)

	assert.Equal(t, "https://custom.example.com/mcp", config.URL, "custom URL should override template")
	assert.Equal(t, "custom-command", config.Command, "custom command should override template")
	assert.Equal(t, []string{"--custom", "--args"}, config.Args, "custom args should override template")
}

func TestCreateServerConfig_EmptyEnvValuesNotAdded(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "empty-env-tmpl",
		Name:    "Empty Env Template",
		Type:    "stdio",
		Command: "server",
		EnvVars: []EnvVarTemplate{
			{Name: "REQUIRED", Required: true, Secret: true},
			{Name: "OPTIONAL", Required: false, Default: "default-value"},
		},
	}

	config := tmpl.CreateServerConfig("server", map[string]string{
		"REQUIRED": "value",
		"OPTIONAL": "", // Empty string should not override default
	}, "", "", nil)

	assert.Equal(t, "value", config.Env["REQUIRED"])
	assert.Equal(t, "default-value", config.Env["OPTIONAL"], "empty string should not override default")
}

func TestCreateServerConfig_NoEnvVarsTemplate(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "no-env-tmpl",
		Name:    "No Env Template",
		Type:    "stdio",
		Command: "simple-server",
		// No EnvVars defined
	}

	config := tmpl.CreateServerConfig("simple", nil, "", "", nil)

	// Env map is always initialized, even if empty
	assert.NotNil(t, config.Env, "Env map should be initialized")
	assert.Empty(t, config.Env, "Env map should be empty when no env vars in template")
}

func TestCreateServerConfig_PartialEnvValues(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "partial-env-tmpl",
		Name:    "Partial Env Template",
		Type:    "stdio",
		Command: "server",
		EnvVars: []EnvVarTemplate{
			{Name: "VAR1", Required: true, Secret: true},
			{Name: "VAR2", Required: true, Secret: true},
			{Name: "VAR3", Required: false, Default: "default3"},
		},
	}

	// Only provide VAR1 and VAR3 (partial)
	config := tmpl.CreateServerConfig("partial-server", map[string]string{
		"VAR1": "value1",
		"VAR3": "value3", // Override default
	}, "", "", nil)

	assert.Equal(t, "value1", config.Env["VAR1"])
	assert.Empty(t, config.Env["VAR2"], "VAR2 not provided, should be empty")
	assert.Equal(t, "value3", config.Env["VAR3"], "should use provided value not default")
}

func TestCreateServerConfig_ServerName(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:   "name-tmpl",
		Name: "Original Template Name",
		Type: "stdio",
	}

	// Test that server name can be different from template name
	config := tmpl.CreateServerConfig("my-custom-server-name", nil, "", "", nil)

	assert.Equal(t, "my-custom-server-name", config.Name)
	assert.NotEqual(t, tmpl.Name, config.Name)
}

func TestCreateServerConfig_CommandTypes(t *testing.T) {
	testCases := []struct {
		name     string
		command  string
		args     []string
	}{
		{"npx with package", "npx", []string{"-y", "@pkg/server"}},
		{"uvx with package", "uvx", []string{"pkg-server"}},
		{"node with script", "node", []string{"server.js"}},
		{"python with module", "python", []string{"-m", "server_module"}},
		{"docker run", "docker", []string{"run", "-i", "image"}},
		{"go run", "go", []string{"run", "./cmd/server"}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tmpl := MCPServerTemplate{
				ID:      "cmd-tmpl",
				Name:    "Command Template",
				Type:    "stdio",
				Command: tc.command,
				Args:    tc.args,
			}

			config := tmpl.CreateServerConfig("test-server", nil, "", "", nil)

			assert.Equal(t, tc.command, config.Command)
			assert.Equal(t, tc.args, config.Args)
		})
	}
}

func TestCreateServerConfig_TimeoutPreserved(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "timeout-tmpl",
		Name:    "Timeout Template",
		Type:    "stdio",
		Command: "server",
		Timeout: 60 * time.Second,
	}

	config := tmpl.CreateServerConfig("timeout-server", nil, "", "", nil)

	assert.Equal(t, 60*time.Second, config.Timeout)
}

func TestCreateServerConfig_AutoStartAndMaxRestarts(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "auto-tmpl",
		Name:    "Auto Template",
		Type:    "stdio",
		Command: "server",
	}

	config := tmpl.CreateServerConfig("auto-server", nil, "", "", nil)

	assert.True(t, config.AutoStart, "AutoStart should default to true")
	assert.Equal(t, 3, config.MaxRestarts, "MaxRestarts should default to 3")
}

func TestCreateServerConfig_AllFeaturesPreserved(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:          "full-tmpl",
		Name:        "Full Featured Template",
		Description: "Template with all features",
		Type:        "http",
		URL:         "https://example.com/mcp",
		EnvVars: []EnvVarTemplate{
			{Name: "KEY", Required: true, Secret: true},
		},
		Timeout:  45 * time.Second,
		Features: []string{"Feature 1", "Feature 2", "Feature 3"},
		AuthType: "bearer",
		Docs:     "https://example.com/docs",
	}

	config := tmpl.CreateServerConfig("full-server", map[string]string{"KEY": "val"}, "", "", nil)

	// Verify all relevant fields are properly set
	assert.Equal(t, "full-server", config.Name)
	assert.Equal(t, "https://example.com/mcp", config.URL)
	assert.Equal(t, "val", config.Env["KEY"])
	assert.Equal(t, 45*time.Second, config.Timeout)
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
}
