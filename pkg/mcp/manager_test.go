package mcp

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock Server for Testing
// ---------------------------------------------------------------------------

// mockMCPServer is a mock implementation of MCPServer for testing
type mockMCPServer struct {
	config       MCPServerConfig
	running      bool
	startDelay   time.Duration
	startError   error
	startCalled  bool
	stopCalled   bool
	stopError    error
	initCalled   bool
	initError    error
	listToolsErr error
	tools        []MCPTool
	mu           sync.RWMutex
}

func newMockMCPServer(name string) *mockMCPServer {
	return &mockMCPServer{
		config: MCPServerConfig{
			Name:        name,
			Command:     "mock",
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
		},
		running: false,
		tools:   []MCPTool{},
	}
}

func (m *mockMCPServer) Start(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.startCalled = true

	if m.startDelay > 0 {
		// Simulate delay before error or success
		time.Sleep(m.startDelay)
	}

	if m.startError != nil {
		return m.startError
	}

	m.running = true
	return nil
}

func (m *mockMCPServer) Stop(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopCalled = true

	if m.stopError != nil {
		return m.stopError
	}

	m.running = false
	return nil
}

func (m *mockMCPServer) IsRunning() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.running
}

func (m *mockMCPServer) GetName() string {
	return m.config.Name
}

func (m *mockMCPServer) GetConfig() MCPServerConfig {
	return m.config
}

func (m *mockMCPServer) Initialize(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.initCalled = true
	return m.initError
}

func (m *mockMCPServer) ListTools(ctx context.Context) ([]MCPTool, error) {
	if m.listToolsErr != nil {
		return nil, m.listToolsErr
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools, nil
}

func (m *mockMCPServer) CallTool(ctx context.Context, request MCPToolCallRequest) (*MCPToolCallResult, error) {
	return &MCPToolCallResult{}, nil
}

func (m *mockMCPServer) ListResources(ctx context.Context) ([]MCPResource, error) {
	return []MCPResource{}, nil
}

func (m *mockMCPServer) ReadResource(ctx context.Context, uri string) (*MCPContent, error) {
	return &MCPContent{}, nil
}

func (m *mockMCPServer) ListPrompts(ctx context.Context) ([]MCPPrompt, error) {
	return []MCPPrompt{}, nil
}

func (m *mockMCPServer) GetPrompt(ctx context.Context, name string, args map[string]interface{}) (*MCPContent, error) {
	return &MCPContent{}, nil
}

// ---------------------------------------------------------------------------
// DefaultMCPManager Tests
// ---------------------------------------------------------------------------

func TestNewMCPManager(t *testing.T) {
	manager := NewMCPManager(nil)

	assert.NotNil(t, manager)
}

func TestMCPManager_AddServer(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:        "github",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		AutoStart:   true,
		MaxRestarts: 3,
	}

	err := manager.AddServer(config)
	require.NoError(t, err)

	// Verify server was added
	server, exists := manager.GetServer("github")
	assert.True(t, exists)
	assert.NotNil(t, server)
	assert.Equal(t, "github", server.GetName())
}

func TestMCPManager_AddServer_Duplicate(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:    "github",
		Command: "npx",
	}

	err := manager.AddServer(config)
	require.NoError(t, err)

	// Try to add duplicate
	err = manager.AddServer(config)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already exists")
}

func TestMCPManager_RemoveServer(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:    "github",
		Command: "npx",
	}
	manager.AddServer(config)

	err := manager.RemoveServer("github")
	require.NoError(t, err)

	// Verify server was removed
	_, exists := manager.GetServer("github")
	assert.False(t, exists)
}

func TestMCPManager_RemoveServer_NotFound(t *testing.T) {
	manager := NewMCPManager(nil)

	err := manager.RemoveServer("nonexistent")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
}

func TestMCPManager_GetServer(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:    "github",
		Command: "npx",
	}
	manager.AddServer(config)

	server, exists := manager.GetServer("github")
	assert.True(t, exists)
	assert.Equal(t, "github", server.GetName())
}

func TestMCPManager_GetServer_NotFound(t *testing.T) {
	manager := NewMCPManager(nil)

	_, exists := manager.GetServer("nonexistent")
	assert.False(t, exists)
}

func TestMCPManager_ListServers(t *testing.T) {
	manager := NewMCPManager(nil)

	manager.AddServer(MCPServerConfig{Name: "server1", Command: "npx"})
	manager.AddServer(MCPServerConfig{Name: "server2", Command: "uvx"})

	servers := manager.ListServers()
	assert.Len(t, servers, 2)
}

func TestMCPManager_ListServers_Empty(t *testing.T) {
	manager := NewMCPManager(nil)

	servers := manager.ListServers()
	assert.Empty(t, servers)
}

func TestMCPManager_GetServerStats(t *testing.T) {
	manager := NewMCPManager(nil)

	manager.AddServer(MCPServerConfig{Name: "github", Command: "npx", AutoStart: true})

	stats := manager.GetServerStats()

	assert.Equal(t, 1, stats["total_servers"])
	assert.Equal(t, 0, stats["running_servers"])

	serverStats := stats["servers"].(map[string]interface{})["github"].(map[string]interface{})
	assert.NotNil(t, serverStats)
}

func TestMCPManager_GetServerStats_MultipleServers(t *testing.T) {
	manager := NewMCPManager(nil)

	manager.AddServer(MCPServerConfig{Name: "github", Command: "npx", AutoStart: true})
	manager.AddServer(MCPServerConfig{Name: "git", Command: "uvx", AutoStart: false})

	stats := manager.GetServerStats()

	assert.Equal(t, 2, stats["total_servers"])
}

// ---------------------------------------------------------------------------
// StartAll / StopAll Tests - Error Handling
// ---------------------------------------------------------------------------

func TestMCPManager_StartAll_NoServers(t *testing.T) {
	manager := NewMCPManager(nil)

	err := manager.StartAll(context.Background())
	assert.NoError(t, err)
}

func TestMCPManager_StartAll_AllServersStartSuccessfully(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add servers
	err := manager.AddServer(MCPServerConfig{Name: "server1", Command: "npx", AutoStart: true})
	require.NoError(t, err)
	err = manager.AddServer(MCPServerConfig{Name: "server2", Command: "uvx", AutoStart: true})
	require.NoError(t, err)
	err = manager.AddServer(MCPServerConfig{Name: "server3", Command: "node", AutoStart: false})
	require.NoError(t, err)

	// Note: This test will succeed in the sense that StartAll won't error,
	// even though the actual server processes won't start without real commands
	err = manager.StartAll(context.Background())
	// We expect no error even if real servers fail - that's handled by server implementation
	assert.NoError(t, err)
}

func TestMCPManager_StopAll_NoServers(t *testing.T) {
	manager := NewMCPManager(nil)

	err := manager.StopAll(context.Background())
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// StartAll with Mock Servers - Error Handling Tests
// ---------------------------------------------------------------------------

// Helper to add a mock server directly to the manager's internal map
func addMockServerToManager(manager *DefaultMCPManager, mock *mockMCPServer) error {
	manager.mutex.Lock()
	defer manager.mutex.Unlock()

	if _, exists := manager.servers[mock.GetName()]; exists {
		return errors.New("server already exists")
	}

	manager.servers[mock.GetName()] = mock
	return nil
}

func TestMCPManager_StartAll_AllMockServersSucceed(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers that will succeed
	mock1 := newMockMCPServer("mock1")
	mock2 := newMockMCPServer("mock2")
	mock3 := newMockMCPServer("mock3")

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock3)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.NoError(t, err)

	// Verify all servers were started
	assert.True(t, mock1.startCalled)
	assert.True(t, mock2.startCalled)
	assert.True(t, mock3.startCalled)
	assert.True(t, mock1.IsRunning())
	assert.True(t, mock2.IsRunning())
	assert.True(t, mock3.IsRunning())
}

func TestMCPManager_StartAll_OneServerFails(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers
	mock1 := newMockMCPServer("mock1")
	mock1.startError = errors.New("mock1 failed to start")

	mock2 := newMockMCPServer("mock2")
	mock2.startError = nil

	mock3 := newMockMCPServer("mock3")
	mock3.startError = nil

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock3)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start some MCP servers")
	assert.Contains(t, err.Error(), "mock1")

	// mock2 and mock3 should have been started
	assert.True(t, mock2.startCalled)
	assert.True(t, mock3.startCalled)
	assert.True(t, mock2.IsRunning())
	assert.True(t, mock3.IsRunning())

	// mock1 should have failed
	assert.True(t, mock1.startCalled)
	assert.False(t, mock1.IsRunning())
}

func TestMCPManager_StartAll_MultipleServersFail(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers with mixed success/failure
	mock1 := newMockMCPServer("mock1")
	mock1.startError = errors.New("mock1 failed")

	mock2 := newMockMCPServer("mock2")
	mock2.startError = nil

	mock3 := newMockMCPServer("mock3")
	mock3.startError = errors.New("mock3 failed")

	mock4 := newMockMCPServer("mock4")
	mock4.startError = nil

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock3)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock4)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start some MCP servers")
	assert.Contains(t, err.Error(), "mock1")
	assert.Contains(t, err.Error(), "mock3")

	// Check which servers are running
	assert.False(t, mock1.IsRunning())
	assert.True(t, mock2.IsRunning())
	assert.False(t, mock3.IsRunning())
	assert.True(t, mock4.IsRunning())
}

func TestMCPManager_StartAll_AllServersFail(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers that all fail
	mock1 := newMockMCPServer("mock1")
	mock1.startError = errors.New("mock1 failed")

	mock2 := newMockMCPServer("mock2")
	mock2.startError = errors.New("mock2 failed")

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start some MCP servers")
	assert.Contains(t, err.Error(), "mock1")
	assert.Contains(t, err.Error(), "mock2")

	// No servers should be running
	assert.False(t, mock1.IsRunning())
	assert.False(t, mock2.IsRunning())
}

func TestMCPManager_StartAll_SomeServersNotAutoStart(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers with different AutoStart settings
	mock1 := newMockMCPServer("mock1")
	mock1.config.AutoStart = true

	mock2 := newMockMCPServer("mock2")
	mock2.config.AutoStart = false

	mock3 := newMockMCPServer("mock3")
	mock3.config.AutoStart = true

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock3)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.NoError(t, err)

	// Only mock1 and mock3 should be started (AutoStart=true)
	assert.True(t, mock1.startCalled)
	assert.False(t, mock2.startCalled)
	assert.True(t, mock3.startCalled)

	assert.True(t, mock1.IsRunning())
	assert.False(t, mock2.IsRunning())
	assert.True(t, mock3.IsRunning())
}

func TestMCPManager_StartAll_ServerAlreadyRunning(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add a mock server that's already running
	mock1 := newMockMCPServer("mock1")
	mock1.running = true // Pre-mark as running

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StartAll(ctx)
	assert.NoError(t, err)

	// Start should not have been called (already running)
	assert.False(t, mock1.startCalled)
	assert.True(t, mock1.IsRunning())
}

func TestMCPManager_StopAll_WithMockServers(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers and mark them as running
	mock1 := newMockMCPServer("mock1")
	mock1.running = true
	mock2 := newMockMCPServer("mock2")
	mock2.running = true

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StopAll(ctx)
	assert.NoError(t, err)

	// Verify all servers were stopped
	assert.True(t, mock1.stopCalled)
	assert.True(t, mock2.stopCalled)
	assert.False(t, mock1.IsRunning())
	assert.False(t, mock2.IsRunning())
}

func TestMCPManager_StopAll_OneServerFailsToStop(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mock servers
	mock1 := newMockMCPServer("mock1")
	mock1.running = true
	mock1.stopError = errors.New("mock1 failed to stop")

	mock2 := newMockMCPServer("mock2")
	mock2.running = true
	mock2.stopError = nil

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StopAll(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stop some MCP servers")
	assert.Contains(t, err.Error(), "mock1")

	// Both should have attempted to stop
	assert.True(t, mock1.stopCalled)
	assert.True(t, mock2.stopCalled)
}

func TestMCPManager_StopAll_OnlyStopsRunningServers(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add mix of running and non-running servers
	mock1 := newMockMCPServer("mock1")
	mock1.running = true

	mock2 := newMockMCPServer("mock2")
	mock2.running = false

	mock3 := newMockMCPServer("mock3")
	mock3.running = true

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock3)
	require.NoError(t, err)

	ctx := context.Background()
	err = manager.StopAll(ctx)
	assert.NoError(t, err)

	// Only running servers should be stopped
	assert.True(t, mock1.stopCalled)
	assert.False(t, mock2.stopCalled)
	assert.True(t, mock3.stopCalled)
}

// ---------------------------------------------------------------------------
// GetAllTools / CallTool Tests
// ---------------------------------------------------------------------------

func TestMCPManager_GetAllTools_NoRunningServers(t *testing.T) {
	manager := NewMCPManager(nil)

	manager.AddServer(MCPServerConfig{Name: "github", Command: "npx"})

	tools, err := manager.GetAllTools(context.Background())
	// Should return empty, no error since no servers are running
	assert.Empty(t, tools)
	assert.NoError(t, err)
}

func TestMCPManager_GetAllTools_WithRunningMockServers(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add running mock servers with tools
	mock1 := newMockMCPServer("mock1")
	mock1.running = true
	mock1.tools = []MCPTool{
		{Name: "tool1", Description: "Test tool 1"},
		{Name: "tool2", Description: "Test tool 2"},
	}

	mock2 := newMockMCPServer("mock2")
	mock2.running = true
	mock2.tools = []MCPTool{
		{Name: "tool3", Description: "Test tool 3"},
	}

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)

	tools, err := manager.GetAllTools(context.Background())
	assert.NoError(t, err)
	assert.Len(t, tools, 3)
}

func TestMCPManager_GetAllTools_SomeServersListToolsError(t *testing.T) {
	manager := NewMCPManager(nil)

	// Add running mock servers with errors
	mock1 := newMockMCPServer("mock1")
	mock1.running = true
	mock1.tools = []MCPTool{{Name: "tool1"}}
	mock1.listToolsErr = nil

	mock2 := newMockMCPServer("mock2")
	mock2.running = true
	mock2.listToolsErr = errors.New("mock2 error")

	err := addMockServerToManager(manager, mock1)
	require.NoError(t, err)
	err = addMockServerToManager(manager, mock2)
	require.NoError(t, err)

	tools, err := manager.GetAllTools(context.Background())
	// Should return partial results, no error
	assert.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "tool1", tools[0].Name)
}

func TestMCPManager_CallTool_ServerNotFound(t *testing.T) {
	manager := NewMCPManager(nil)

	result, err := manager.CallTool(context.Background(), "nonexistent", "tool", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not found")
	assert.Nil(t, result)
}

func TestMCPManager_CallTool_ServerNotRunning(t *testing.T) {
	manager := NewMCPManager(nil)

	manager.AddServer(MCPServerConfig{Name: "github", Command: "npx"})

	result, err := manager.CallTool(context.Background(), "github", "tool", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not running")
	assert.Nil(t, result)
}

// ---------------------------------------------------------------------------
// MCPServerConfig Type Handling Tests
// ---------------------------------------------------------------------------

func TestMCPManager_AddServer_StdioType(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:    "stdio-server",
		Type:    "stdio",
		Command: "npx",
	}

	err := manager.AddServer(config)
	require.NoError(t, err)

	server, _ := manager.GetServer("stdio-server")
	assert.NotNil(t, server)
}

func TestMCPManager_AddServer_HTTPType(t *testing.T) {
	manager := NewMCPManager(nil)

	config := MCPServerConfig{
		Name:    "http-server",
		Type:    "http",
		URL:     "https://example.com/mcp",
	}

	err := manager.AddServer(config)
	require.NoError(t, err)

	server, _ := manager.GetServer("http-server")
	assert.NotNil(t, server)
}

func TestMCPManager_AddServer_DefaultType(t *testing.T) {
	manager := NewMCPManager(nil)

	// No Type specified - should default to stdio
	config := MCPServerConfig{
		Name:    "default-server",
		Command: "npx",
	}

	err := manager.AddServer(config)
	require.NoError(t, err)

	server, _ := manager.GetServer("default-server")
	assert.NotNil(t, server)
}

// ---------------------------------------------------------------------------
// Server Name Validation Tests
// ---------------------------------------------------------------------------

// Note: The current implementation only validates that server names are not empty.
// More comprehensive validation (e.g., character restrictions) could be added in the future.
// These tests document the current behavior.

func TestServerNameValidation_EmptyName_AddServer(t *testing.T) {
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

func TestServerNameValidation_ValidNames(t *testing.T) {
	// Test various valid name formats
	validNames := []string{
		"server",
		"my-server",
		"my_server",
		"my-server-123",
		"server_123",
		"a", // Single character
		"server-1-2-3",
		"SERVER_NAME",
		"test123server",
	}

	for _, name := range validNames {
		t.Run(name, func(t *testing.T) {
			config := MCPConfig{
				Enabled: true,
				Servers: make(map[string]MCPServerConfig),
			}

			err := config.AddServer(MCPServerConfig{
				Name:    name,
				Command: "npx",
			})

			// Currently, all non-empty names are accepted
			assert.NoError(t, err, "Name '%s' should be valid", name)
		})
	}
}

func TestServerNameValidation_SpecialCharacters(t *testing.T) {
	// Test names with special characters
	// Note: Currently, even these would be accepted as long as they're not empty
	// This documents the current behavior
	specialNames := []string{
		"server.",
		"server!",
		"server@",
		"server#",
		"server$",
		"server%",
		"server^",
		"server&",
		"server*",
		"server(",
		"server)",
		"server[",
		"server]",
		"server{",
		"server}",
		"server|",
		"server\\",
		"server:",
		"server;",
		"server\"",
		"server'",
		"server<",
		"server>",
		"server,",
		"server?",
		"server/",
		" server",      // Leading space
		"server ",      // Trailing space
		"server name",  // Middle space
		"\tserver",     // Leading tab
		"server\n",     // Trailing newline
		"日本語",         // Unicode characters
		"server@domain", // @ symbol
	}

	for _, name := range specialNames {
		t.Run(name, func(t *testing.T) {
			config := MCPConfig{
				Enabled: true,
				Servers: make(map[string]MCPServerConfig),
			}

			err := config.AddServer(MCPServerConfig{
				Name:    name,
				Command: "npx",
			})

			// Currently, all non-empty names are accepted (even special chars)
			// This documents current behavior - future improvements could add validation
			assert.NoError(t, err, "Name '%q' is currently accepted (may want to validate in future)", name)
		})
	}
}

func TestServerNameValidation_DuplicateNames(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	// Add first server
	err := config.AddServer(MCPServerConfig{
		Name:    "github",
		Command: "npx",
	})
	require.NoError(t, err)

	// Try to add duplicate - should just overwrite in map
	// AddServer doesn't prevent duplicates, it just adds/overwrites
	err = config.AddServer(MCPServerConfig{
		Name:    "github",
		Command: "uvx",
	})
	assert.NoError(t, err)

	// Verify it was overwritten (the map behavior)
	assert.Equal(t, "uvx", config.Servers["github"].Command)
}

func TestServerNameValidation_CaseSensitivity(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	// Add server with lowercase name
	err := config.AddServer(MCPServerConfig{
		Name:    "github",
		Command: "npx",
	})
	require.NoError(t, err)

	// Try to add server with uppercase name - they are different keys in map
	err = config.AddServer(MCPServerConfig{
		Name:    "GITHUB",
		Command: "uvx",
	})
	assert.NoError(t, err)

	// Both should exist
	assert.Len(t, config.Servers, 2)
	assert.Contains(t, config.Servers, "github")
	assert.Contains(t, config.Servers, "GITHUB")
}

func TestServerNameValidation_VeryLongName(t *testing.T) {
	config := MCPConfig{
		Enabled: true,
		Servers: make(map[string]MCPServerConfig),
	}

	// Create a very long name
	longName := ""
	for i := 0; i < 1000; i++ {
		longName += "a"
	}

	err := config.AddServer(MCPServerConfig{
		Name:    longName,
		Command: "npx",
	})

	// Currently accepted - might want to add length validation in future
	assert.NoError(t, err)
}
