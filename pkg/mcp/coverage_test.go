package mcp

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

// ===========================================================================
// tool_wrapper.go - Additional coverage tests
// ===========================================================================

// TestMCPToolWrapper_CanExecute_ServerRunning tests CanExecute when the server
// is running (the positive case, already tested with negative cases)
func TestMCPToolWrapper_CanExecute_ServerRunning(t *testing.T) {
	// Create a manager with a mock server that reports as running
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)

	// CanExecute should return true when server is running
	assert.True(t, wrapper.CanExecute(context.Background(), Parameters{}))
}

// TestMCPToolWrapper_CanExecute_ServerNotRunning tests CanExecute when the server
// exists but is not running
func TestMCPToolWrapper_CanExecute_ServerNotRunning(t *testing.T) {
	// Create a manager with a mock server that reports as NOT running
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockStoppedServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)

	// CanExecute should return false when server is not running
	assert.False(t, wrapper.CanExecute(context.Background(), Parameters{}))
}

// TestMCPToolWrapper_IsAvailable_ServerRunningAndAvailable tests IsAvailable
// when the server is running AND the wrapper's available flag is true
func TestMCPToolWrapper_IsAvailable_ServerRunningAndAvailable(t *testing.T) {
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)
	wrapper.SetAvailable(true) // Default, but explicit for clarity

	// IsAvailable should return true when both conditions are met
	assert.True(t, wrapper.IsAvailable())
}

// TestMCPToolWrapper_IsAvailable_AvailableFlagFalse tests IsAvailable when the
// wrapper's available flag is set to false
func TestMCPToolWrapper_IsAvailable_AvailableFlagFalse(t *testing.T) {
	mgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			return &mockRunningServer{name: "testserver"}, true
		},
	}

	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, mgr)
	wrapper.SetAvailable(false)

	// IsAvailable should return false when available flag is false, even if server is running
	assert.False(t, wrapper.IsAvailable())
}

// TestMCPToolWrapper_EstimatedDuration_Default tests EstimatedDuration returns
// the default 30-second timeout when SetTimeout is not called
func TestMCPToolWrapper_EstimatedDuration_Default(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)

	// Default timeout should be 30 seconds
	assert.Equal(t, 30*time.Second, wrapper.EstimatedDuration())
}

// TestMCPToolWrapper_RequiredPermissions_NonGitHubServer tests that
// RequiredPermissions only returns network_access for non-github servers
func TestMCPToolWrapper_RequiredPermissions_NonGitHubServer(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		InputSchema: map[string]interface{}{"type": "object"},
		ServerName:  "myserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	perms := wrapper.RequiredPermissions()

	// Should only contain network_access, not github-specific permissions
	assert.Len(t, perms, 1)
	assert.Contains(t, perms, "network_access")
	assert.NotContains(t, perms, "mcp_github_access")
}

// TestMCPToolWrapper_Name_EdgeCases tests Name method with various edge cases
func TestMCPToolWrapper_Name_EdgeCases(t *testing.T) {
	testCases := []struct {
		name       string
		serverName string
		toolName   string
		expected   string
	}{
		{
			name:       "basic names",
			serverName: "server",
			toolName:   "tool",
			expected:   "mcp_server_tool",
		},
		{
			name:       "server with hyphens",
			serverName: "my-server",
			toolName:   "search_files",
			expected:   "mcp_my-server_search_files",
		},
		{
			name:       "server with underscores",
			serverName: "my_server",
			toolName:   "get_user",
			expected:   "mcp_my_server_get_user",
		},
		{
			name:       "tool with hyphens",
			serverName: "github",
			toolName:   "create-issue",
			expected:   "mcp_github_create-issue",
		},
		{
			name:       "empty tool name",
			serverName: "server",
			toolName:   "",
			expected:   "mcp_server_",
		},
		{
			name:       "empty server name",
			serverName: "",
			toolName:   "tool",
			expected:   "mcp__tool",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tool := MCPTool{
				Name:        tc.toolName,
				Description: "Test tool",
				ServerName:  tc.serverName,
			}
			wrapper := NewMCPToolWrapper(tool, nil)
			assert.Equal(t, tc.expected, wrapper.Name())
		})
	}
}

// ===========================================================================
// Mock servers for testing
// ===========================================================================

// mockRunningServer is a mock MCPServer that always reports as running
type mockRunningServer struct {
	name string
}

func (m *mockRunningServer) Start(ctx context.Context) error { return nil }
func (m *mockRunningServer) Stop(ctx context.Context) error  { return nil }
func (m *mockRunningServer) IsRunning() bool                 { return true }
func (m *mockRunningServer) GetName() string                 { return m.name }
func (m *mockRunningServer) GetConfig() MCPServerConfig {
	return MCPServerConfig{Name: m.name}
}
func (m *mockRunningServer) Initialize(ctx context.Context) error { return nil }
func (m *mockRunningServer) ListTools(ctx context.Context) ([]MCPTool, error) {
	return nil, nil
}
func (m *mockRunningServer) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	return nil, nil
}
func (m *mockRunningServer) ListResources(ctx context.Context) ([]MCPResource, error) {
	return nil, nil
}
func (m *mockRunningServer) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	return nil, nil
}
func (m *mockRunningServer) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	return nil, nil
}
func (m *mockRunningServer) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	return nil, nil
}

// mockStoppedServer is a mock MCPServer that always reports as NOT running
type mockStoppedServer struct {
	name string
}

func (m *mockStoppedServer) Start(ctx context.Context) error { return nil }
func (m *mockStoppedServer) Stop(ctx context.Context) error  { return nil }
func (m *mockStoppedServer) IsRunning() bool                 { return false }
func (m *mockStoppedServer) GetName() string                 { return m.name }
func (m *mockStoppedServer) GetConfig() MCPServerConfig {
	return MCPServerConfig{Name: m.name}
}
func (m *mockStoppedServer) Initialize(ctx context.Context) error { return nil }
func (m *mockStoppedServer) ListTools(ctx context.Context) ([]MCPTool, error) {
	return nil, nil
}
func (m *mockStoppedServer) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	return nil, nil
}
func (m *mockStoppedServer) ListResources(ctx context.Context) ([]MCPResource, error) {
	return nil, nil
}
func (m *mockStoppedServer) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	return nil, nil
}
func (m *mockStoppedServer) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	return nil, nil
}
func (m *mockStoppedServer) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	return nil, nil
}

// ===========================================================================
// Constants and type coverage
// ===========================================================================

// TestCategoryWebConstant verifies the CategoryWeb constant
func TestCategoryWebConstant(t *testing.T) {
	assert.Equal(t, "web", CategoryWeb)
}

// TestPermissionNetworkAccessConstant verifies the PermissionNetworkAccess constant
func TestPermissionNetworkAccessConstant(t *testing.T) {
	assert.Equal(t, "network_access", PermissionNetworkAccess)
}

// TestMCPToolWrapper_SetCategory tests SetCategory explicitly
func TestMCPToolWrapper_SetCategory(t *testing.T) {
	tool := MCPTool{
		Name:        "test_tool",
		Description: "A test tool",
		ServerName:  "testserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	assert.Equal(t, "web", wrapper.Category())

	wrapper.SetCategory("filesystem")
	assert.Equal(t, "filesystem", wrapper.Category())

	wrapper.SetCategory("database")
	assert.Equal(t, "database", wrapper.Category())
}

// TestMCPToolWrapper_ToAgentTool_FullSchema tests ToAgentTool with a complex schema
func TestMCPToolWrapper_ToAgentTool_FullSchema(t *testing.T) {
	schema := map[string]interface{}{
		"type": "object",
		"properties": map[string]interface{}{
			"path": map[string]interface{}{
				"type":        "string",
				"description": "The file path",
			},
			"line": map[string]interface{}{
				"type":        "integer",
				"description": "The line number",
			},
		},
		"required": []interface{}{"path"},
	}

	tool := MCPTool{
		Name:        "read_file",
		Description: "Read a file from the filesystem",
		InputSchema: schema,
		ServerName:  "myserver",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	agentTool := wrapper.ToAgentTool()

	assert.Equal(t, "function", agentTool.Type)
	assert.Equal(t, "mcp_myserver_read_file", agentTool.Function.Name)
	// Description() adds the [MCP:server] prefix
	assert.Equal(t, "[MCP:myserver] Read a file from the filesystem", agentTool.Function.Description)
	assert.Equal(t, schema, agentTool.Function.Parameters)
}

// TestMCPToolWrapper_Description_Long tests with long description
func TestMCPToolWrapper_Description_Long(t *testing.T) {
	longDesc := strings.Repeat("This is a very long description. ", 20)

	tool := MCPTool{
		Name:        "tool",
		Description: longDesc,
		ServerName:  "server",
	}

	wrapper := NewMCPToolWrapper(tool, nil)
	expected := "[MCP:server] " + longDesc
	assert.Equal(t, expected, wrapper.Description())
}
