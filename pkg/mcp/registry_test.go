package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// NewMCPServerRegistry — builtin templates
// ---------------------------------------------------------------------------

func TestNewMCPServerRegistry_HasBuiltinTemplates(t *testing.T) {
	r := NewMCPServerRegistry()
	templates := r.ListTemplates()
	assert.GreaterOrEqual(t, len(templates), 6, "should have at least 6 builtin templates")
}

// ---------------------------------------------------------------------------
// GetTemplate
// ---------------------------------------------------------------------------

func TestGetTemplate_Exists(t *testing.T) {
	r := NewMCPServerRegistry()

	expectedIDs := []string{
		"github-remote", "github-docker", "git-uvx",
		"chrome-devtools", "http-generic", "stdio-generic",
	}

	for _, id := range expectedIDs {
		tmpl, ok := r.GetTemplate(id)
		assert.True(t, ok, "template %s should exist", id)
		assert.NotEmpty(t, tmpl.Name, "template %s should have a name", id)
		assert.Equal(t, id, tmpl.ID)
	}
}

func TestGetTemplate_NotFound(t *testing.T) {
	r := NewMCPServerRegistry()
	_, ok := r.GetTemplate("nonexistent-template")
	assert.False(t, ok)
}

// ---------------------------------------------------------------------------
// ListTemplates
// ---------------------------------------------------------------------------

func TestListTemplates_Count(t *testing.T) {
	r := NewMCPServerRegistry()
	templates := r.ListTemplates()
	count := len(templates)
	assert.GreaterOrEqual(t, count, 6)

	// Adding a custom template should increase count
	r.AddTemplate(MCPServerTemplate{
		ID:   "custom-1",
		Name: "Custom Server",
	})
	assert.Equal(t, count+1, len(r.ListTemplates()))
}

func TestListTemplates_AllHaveID(t *testing.T) {
	r := NewMCPServerRegistry()
	for _, tmpl := range r.ListTemplates() {
		assert.NotEmpty(t, tmpl.ID, "template should have non-empty ID")
	}
}

// ---------------------------------------------------------------------------
// GetTemplatesByType
// ---------------------------------------------------------------------------

func TestGetTemplatesByType_Stdio(t *testing.T) {
	r := NewMCPServerRegistry()
	stdio := r.GetTemplatesByType("stdio")
	assert.NotEmpty(t, stdio)
	for _, tmpl := range stdio {
		assert.Equal(t, "stdio", tmpl.Type)
	}
}

func TestGetTemplatesByType_Http(t *testing.T) {
	r := NewMCPServerRegistry()
	http := r.GetTemplatesByType("http")
	assert.NotEmpty(t, http)
	for _, tmpl := range http {
		assert.Equal(t, "http", tmpl.Type)
	}
}

func TestGetTemplatesByType_NoMatch(t *testing.T) {
	r := NewMCPServerRegistry()
	result := r.GetTemplatesByType("websocket")
	assert.Empty(t, result)
}

// ---------------------------------------------------------------------------
// SearchTemplates
// ---------------------------------------------------------------------------

func TestSearchTemplates_ByExactName(t *testing.T) {
	r := NewMCPServerRegistry()
	results := r.SearchTemplates("Git MCP Server")
	assert.NotEmpty(t, results)
	// Should find the git-uvx template (named "Git MCP Server")
	found := false
	for _, tmpl := range results {
		if tmpl.ID == "git-uvx" {
			found = true
			break
		}
	}
	assert.True(t, found, "should find git-uvx template by name")
}

func TestSearchTemplates_CaseInsensitive(t *testing.T) {
	r := NewMCPServerRegistry()
	results := r.SearchTemplates("GitHub MCP Server (Remote)")
	assert.NotEmpty(t, results)
	found := false
	for _, tmpl := range results {
		if tmpl.ID == "github-remote" {
			found = true
			break
		}
	}
	assert.True(t, found, "should find github-remote template by partial name")
}

func TestSearchTemplates_ByDescription(t *testing.T) {
	r := NewMCPServerRegistry()
	results := r.SearchTemplates("Git operations")
	assert.NotEmpty(t, results, "should find git-uvx template by description containing 'Git operations'")
}

func TestSearchTemplates_NoMatch(t *testing.T) {
	r := NewMCPServerRegistry()
	results := r.SearchTemplates("zzznonexistent12345")
	assert.Empty(t, results)
}

func TestSearchTemplates_EmptyQuery(t *testing.T) {
	r := NewMCPServerRegistry()
	results := r.SearchTemplates("")
	allTemplates := r.ListTemplates()
	// Empty query should match everything
	assert.Equal(t, len(allTemplates), len(results))
}

// ---------------------------------------------------------------------------
// AddTemplate
// ---------------------------------------------------------------------------

func TestAddTemplate_Valid(t *testing.T) {
	r := NewMCPServerRegistry()

	tmpl := MCPServerTemplate{
		ID:          "custom-test",
		Name:        "Custom Test Server",
		Description: "A custom test server",
		Type:        "stdio",
		Command:     "node",
		Args:        []string{"server.js"},
		Timeout:     15 * time.Second,
		Features:    []string{"Testing"},
		AuthType:    "none",
	}

	err := r.AddTemplate(tmpl)
	require.NoError(t, err)

	// Should be retrievable
	got, ok := r.GetTemplate("custom-test")
	assert.True(t, ok)
	assert.Equal(t, "Custom Test Server", got.Name)

	// Should appear in list
	found := false
	for _, t := range r.ListTemplates() {
		if t.ID == "custom-test" {
			found = true
			break
		}
	}
	assert.True(t, found)
}

func TestAddTemplate_EmptyID(t *testing.T) {
	r := NewMCPServerRegistry()
	err := r.AddTemplate(MCPServerTemplate{
		ID:   "",
		Name: "No ID",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template ID cannot be empty")
}

func TestAddTemplate_EmptyName(t *testing.T) {
	r := NewMCPServerRegistry()
	err := r.AddTemplate(MCPServerTemplate{
		ID:   "test-id",
		Name: "",
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "template name cannot be empty")
}

func TestAddTemplate_DefaultsType(t *testing.T) {
	r := NewMCPServerRegistry()
	err := r.AddTemplate(MCPServerTemplate{
		ID:   "type-default",
		Name: "Type Default",
		// Type intentionally left empty
	})
	require.NoError(t, err)
	tmpl, ok := r.GetTemplate("type-default")
	require.True(t, ok)
	assert.Equal(t, "stdio", tmpl.Type, "empty type should default to stdio")
}

// ---------------------------------------------------------------------------
// CreateServerConfig
// ---------------------------------------------------------------------------

func TestCreateServerConfig_Basic(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:          "test-tmpl",
		Name:        "Test Template",
		Type:        "stdio",
		Command:     "npx",
		Args:        []string{"-y", "some-mcp-server"},
		Timeout:     30 * time.Second,
		EnvVars: []EnvVarTemplate{
			{Name: "API_KEY", Required: true, Secret: true, Description: "API key"},
			{Name: "MODEL", Required: false, Secret: false, Default: "gpt-4"},
		},
	}

	config := tmpl.CreateServerConfig("test-server", map[string]string{"API_KEY": "sk-test"}, "", "", nil)

	assert.Equal(t, "test-server", config.Name)
	assert.Equal(t, "stdio", config.Type)
	assert.Equal(t, "npx", config.Command)
	assert.Equal(t, []string{"-y", "some-mcp-server"}, config.Args)
	assert.Equal(t, "sk-test", config.Env["API_KEY"])
	assert.Equal(t, "gpt-4", config.Env["MODEL"], "default should be used when not provided")
	assert.True(t, config.AutoStart)
	assert.Equal(t, 3, config.MaxRestarts)
	assert.Equal(t, 30*time.Second, config.Timeout)
}

func TestCreateServerConfig_CustomURL(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:  "http-tmpl",
		Name: "HTTP Template",
		Type: "http",
		URL:  "https://default.example.com/mcp",
	}

	config := tmpl.CreateServerConfig("custom-url-server", nil, "https://custom.example.com/mcp", "", nil)
	assert.Equal(t, "https://custom.example.com/mcp", config.URL)
}

func TestCreateServerConfig_CustomCommand(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "cmd-tmpl",
		Name:    "Cmd Template",
		Type:    "stdio",
		Command: "npx",
	}

	config := tmpl.CreateServerConfig("custom-cmd-server", nil, "", "uvx", nil)
	assert.Equal(t, "uvx", config.Command)
}

func TestCreateServerConfig_CustomArgs(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "args-tmpl",
		Name:    "Args Template",
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "default-pkg"},
	}

	config := tmpl.CreateServerConfig("custom-args-server", nil, "", "", []string{"-y", "custom-pkg"})
	assert.Equal(t, []string{"-y", "custom-pkg"}, config.Args)
}

func TestCreateServerConfig_EnvValues(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "env-tmpl",
		Name:    "Env Template",
		Type:    "stdio",
		Command: "node",
		EnvVars: []EnvVarTemplate{
			{Name: "TOKEN", Required: true, Secret: true},
			{Name: "HOST", Required: false},
		},
	}

	config := tmpl.CreateServerConfig("env-server", map[string]string{
		"TOKEN": "secret-value",
		"HOST":  "0.0.0.0",
	}, "", "", nil)
	assert.Equal(t, "secret-value", config.Env["TOKEN"])
	assert.Equal(t, "0.0.0.0", config.Env["HOST"])
}

func TestCreateServerConfig_EnvVarDefaults(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "defaults-tmpl",
		Name:    "Defaults Template",
		Type:    "stdio",
		Command: "node",
		EnvVars: []EnvVarTemplate{
			{Name: "REQUIRED", Required: true},
			{Name: "WITH_DEFAULT", Required: false, Default: "default-value"},
		},
	}

	// Only provide the required var, not the one with a default
	config := tmpl.CreateServerConfig("defaults-server", map[string]string{
		"REQUIRED": "provided-value",
	}, "", "", nil)

	assert.Equal(t, "provided-value", config.Env["REQUIRED"])
	assert.Equal(t, "default-value", config.Env["WITH_DEFAULT"], "default should be used")
}

func TestCreateServerConfig_Defaults(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "defaults-check",
		Name:    "Defaults Check",
		Type:    "stdio",
		Command: "npx",
		Timeout: 45 * time.Second,
	}

	config := tmpl.CreateServerConfig("server", nil, "", "", nil)
	assert.True(t, config.AutoStart, "AutoStart should default to true")
	assert.Equal(t, 3, config.MaxRestarts, "MaxRestarts should default to 3")
	assert.Equal(t, 45*time.Second, config.Timeout, "Timeout should come from template")
}

func TestCreateServerConfig_ArgsCopiedNotShared(t *testing.T) {
	tmpl := MCPServerTemplate{
		ID:      "copy-check",
		Name:    "Copy Check",
		Type:    "stdio",
		Command: "npx",
		Args:    []string{"-y", "original"},
	}

	config := tmpl.CreateServerConfig("server", nil, "", "", nil)

	// Mutate template args AFTER creating config
	tmpl.Args[1] = "mutated"

	// Config args should NOT be affected
	assert.Equal(t, "original", config.Args[1], "config args should be independent copy")
}
