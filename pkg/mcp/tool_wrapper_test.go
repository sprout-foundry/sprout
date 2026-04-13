package mcp

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func newTestWrapper(serverName, toolName string) *MCPToolWrapper {
	m := NewMCPManager(nil)
	m.AddServer(MCPServerConfig{Name: serverName, Command: "npx", AutoStart: false})
	tool := MCPTool{
		Name:        toolName,
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  serverName,
	}
	return NewMCPToolWrapper(tool, m)
}

func TestNewMCPToolWrapper(t *testing.T) {
	m := NewMCPManager(nil)
	m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
	w := NewMCPToolWrapper(MCPTool{
		Name: "tool1", ServerName: "srv",
	}, m)

	assert.NotNil(t, w)
	assert.Equal(t, "web", w.category)
	assert.True(t, w.available)
}

func TestMCPToolWrapper_Name(t *testing.T) {
	w := newTestWrapper("myserver", "search_files")
	assert.Equal(t, "mcp_myserver_search_files", w.Name())
}

func TestMCPToolWrapper_Description(t *testing.T) {
	t.Run("with description", func(t *testing.T) {
		w := newTestWrapper("srv", "tool")
		assert.Equal(t, "[MCP:srv] A test tool", w.Description())
	})

	t.Run("empty description falls back", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name: "mytool", ServerName: "srv", Description: "",
		}, m)
		assert.Equal(t, "[MCP:srv] MCP tool mytool from srv server", w.Description())
	})
}

func TestMCPToolWrapper_Category(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	assert.Equal(t, "web", w.Category())

	w.SetCategory("custom")
	assert.Equal(t, "custom", w.Category())
}

func TestMCPToolWrapper_SetTimeout(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	w.SetTimeout(60 * time.Second)
	assert.Equal(t, 60*time.Second, w.EstimatedDuration())
	assert.Equal(t, 60*time.Second, w.timeout)
}

func TestMCPToolWrapper_SetAvailable(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	// available flag should be true initially
	assert.True(t, w.available)
	w.SetAvailable(false)
	assert.False(t, w.available, "available flag should be false after SetAvailable(false)")
	w.SetAvailable(true)
	assert.True(t, w.available, "available flag should be true after SetAvailable(true)")
}

func TestMCPToolWrapper_GetMCPTool(t *testing.T) {
	tool := MCPTool{
		Name: "my_tool", Description: "desc", ServerName: "srv",
		InputSchema: map[string]interface{}{"type": "object"},
	}
	w := NewMCPToolWrapper(tool, NewMCPManager(nil))
	assert.Equal(t, tool, w.GetMCPTool())
}

func TestMCPToolWrapper_GetServerName(t *testing.T) {
	w := newTestWrapper("myserver", "tool")
	assert.Equal(t, "myserver", w.GetServerName())
}

func TestMCPToolWrapper_GetToolName(t *testing.T) {
	w := newTestWrapper("srv", "my_tool")
	assert.Equal(t, "my_tool", w.GetToolName())
}

func TestMCPToolWrapper_RequiredPermissions_Basic(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	perms := w.RequiredPermissions()
	assert.Contains(t, perms, "network_access")
}

func TestMCPToolWrapper_RequiredPermissions_GitHub(t *testing.T) {
	w := newTestWrapper("github", "tool")
	perms := w.RequiredPermissions()
	assert.Contains(t, perms, "network_access")
	assert.Contains(t, perms, "mcp_github_access")
}

func TestMCPToolWrapper_ToAgentTool(t *testing.T) {
	w := newTestWrapper("myserver", "search_files")
	agentTool := w.ToAgentTool()

	assert.Equal(t, "function", agentTool.Type)
	assert.Equal(t, "mcp_myserver_search_files", agentTool.Function.Name)
	assert.Contains(t, agentTool.Function.Description, "MCP")
	assert.Equal(t, map[string]interface{}{"type": "object"}, agentTool.Function.Parameters)
}

func TestMCPToolWrapper_ValidateArgs(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	assert.NoError(t, w.ValidateArgs(nil))
	assert.NoError(t, w.ValidateArgs(map[string]interface{}{"key": "val"}))
}

func TestMCPToolWrapper_CanExecute_ServerNotFound(t *testing.T) {
	// Wrapper points to a server name that doesn't exist in the manager
	m := NewMCPManager(nil)
	// Don't add any server - wrapper references nonexistent "missing" server
	w := NewMCPToolWrapper(MCPTool{Name: "tool", ServerName: "missing"}, m)
	assert.False(t, w.CanExecute(nil, Parameters{}))
}

func TestMCPToolWrapper_IsAvailable_ServerNotFound(t *testing.T) {
	m := NewMCPManager(nil)
	w := NewMCPToolWrapper(MCPTool{Name: "tool", ServerName: "missing"}, m)
	assert.False(t, w.IsAvailable())
}

func TestMCPToolWrapper_IsAvailable_ServerNotRunning(t *testing.T) {
	w := newTestWrapper("srv", "tool")
	// Server exists but is not running
	assert.False(t, w.IsAvailable(), "server not started should return false")
}
