package mcp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/santhosh-tekuri/jsonschema/v6"
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
	t.Run("empty schema passes", func(t *testing.T) {
		w := newTestWrapper("srv", "tool")
		assert.NoError(t, w.ValidateArgs(nil))
		assert.NoError(t, w.ValidateArgs(map[string]interface{}{"key": "val"}))
	})

	t.Run("nil schema passes", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name: "tool", ServerName: "srv",
			InputSchema: nil,
		}, m)
		assert.NoError(t, w.ValidateArgs(nil))
		assert.NoError(t, w.ValidateArgs(map[string]interface{}{"x": 1}))
	})

	t.Run("required field missing returns InvalidArgsError", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "search",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"query"},
			},
		}, m)

		err := w.ValidateArgs(map[string]interface{}{})
		assert.Error(t, err)
		invErr, ok := err.(*InvalidArgsError)
		assert.True(t, ok, "should return InvalidArgsError")
		assert.Equal(t, "search", invErr.Tool)
		assert.Equal(t, "srv", invErr.Server)
		assert.NotEmpty(t, invErr.Failures)
	})

	t.Run("required field present passes", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "search",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"query"},
			},
		}, m)

		assert.NoError(t, w.ValidateArgs(map[string]interface{}{"query": "hello"}))
	})

	t.Run("wrong type fails", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "count",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"limit": map[string]interface{}{"type": "integer"},
				},
			},
		}, m)

		err := w.ValidateArgs(map[string]interface{}{"limit": "not_a_number"})
		assert.Error(t, err)
		invErr, ok := err.(*InvalidArgsError)
		assert.True(t, ok)
		assert.NotEmpty(t, invErr.Failures)
	})

	t.Run("FormatForLLM produces usable output", func(t *testing.T) {
		err := &InvalidArgsError{
			Tool:   "search",
			Server: "files",
			Failures: []ValidationFailure{
				{Path: ".query", Reason: "is required"},
				{Path: ".limit", Reason: "must be of type integer"},
			},
		}
		msg := FormatForLLM(err)
		assert.Contains(t, msg, "search")
		assert.Contains(t, msg, "files")
		assert.Contains(t, msg, ".query")
		assert.Contains(t, msg, "is required")
		assert.Contains(t, msg, ".limit")
		assert.Contains(t, msg, "integer")
		assert.Contains(t, msg, "Please correct these arguments")
	})

	t.Run("lazy compilation via sync.Once", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "search",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"query": map[string]interface{}{"type": "string"},
				},
				"required": []interface{}{"query"},
			},
		}, m)

		// First call compiles the schema
		err1 := w.ValidateArgs(map[string]interface{}{})
		assert.Error(t, err1)

		// Second call reuses compiled schema (should also fail consistently)
		err2 := w.ValidateArgs(map[string]interface{}{})
		assert.Error(t, err2)

		// Third call with valid args should pass
		err3 := w.ValidateArgs(map[string]interface{}{"query": "test"})
		assert.NoError(t, err3)
	})

	t.Run("malformed schema fails open", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "broken",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"$ref": "#/definitions/nonexistent",
			},
		}, m)

		// Must not panic and must return nil (fail-open)
		err := w.ValidateArgs(map[string]interface{}{"x": 1})
		assert.Nil(t, err, "ValidateArgs should return nil when schema compilation fails (fail-open)")
	})

	t.Run("malformed schema fails open idempotently", func(t *testing.T) {
		m := NewMCPManager(nil)
		m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
		w := NewMCPToolWrapper(MCPTool{
			Name:       "broken",
			ServerName: "srv",
			InputSchema: map[string]interface{}{
				"$ref": "#/definitions/nonexistent",
			},
		}, m)

		// Multiple calls must all return nil (idempotent fail-open)
		for i := 0; i < 3; i++ {
			err := w.ValidateArgs(map[string]interface{}{"x": i})
			assert.Nil(t, err, "ValidateArgs should return nil on call %d for malformed schema", i+1)
		}
	})
}

func TestMCPToolWrapper_CanExecute_ServerNotFound(t *testing.T) {
	// Wrapper points to a server name that doesn't exist in the manager
	m := NewMCPManager(nil)
	// Don't add any server - wrapper references nonexistent "missing" server
	w := NewMCPToolWrapper(MCPTool{Name: "tool", ServerName: "missing"}, m)
	assert.False(t, w.CanExecute(nil, Parameters{}))
}

func TestMCPToolWrapper_CanExecute_InvalidArgs(t *testing.T) {
	// Server exists and is running, but args fail validation
	mockMgr := &mockMCPManager{
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}
	w := NewMCPToolWrapper(MCPTool{
		Name:       "search",
		ServerName: "srv",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"query"},
		},
	}, mockMgr)

	// Missing required field should return false
	assert.False(t, w.CanExecute(nil, Parameters{
		Kwargs: map[string]interface{}{},
	}), "CanExecute should return false when required arg is missing")

	// Present required field should return true (server is running)
	assert.True(t, w.CanExecute(nil, Parameters{
		Kwargs: map[string]interface{}{"query": "hello"},
	}), "CanExecute should return true when validation passes")
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

// ---------------------------------------------------------------------------
// Mock Manager for Execute Testing
// ---------------------------------------------------------------------------

// mockMCPManager implements MCPManager interface for testing Execute method
type mockMCPManager struct {
	callToolFunc func(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*MCPToolCallResult, error)
	getServer    func(name string) (MCPServer, bool)
}

func (m *mockMCPManager) AddServer(config MCPServerConfig) error {
	return nil
}

func (m *mockMCPManager) RemoveServer(name string) error {
	return nil
}

func (m *mockMCPManager) GetServer(name string) (MCPServer, bool) {
	if m.getServer != nil {
		return m.getServer(name)
	}
	return newMockMCPServer(name), true
}

func (m *mockMCPManager) ListServers() []MCPServer {
	return nil
}

func (m *mockMCPManager) StartAll(ctx context.Context) error {
	return nil
}

func (m *mockMCPManager) StopAll(ctx context.Context) error {
	return nil
}

func (m *mockMCPManager) GetAllTools(ctx context.Context) ([]MCPTool, error) {
	return nil, nil
}

func (m *mockMCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*MCPToolCallResult, error) {
	if m.callToolFunc != nil {
		return m.callToolFunc(ctx, serverName, toolName, args)
	}
	return nil, nil
}

// ---------------------------------------------------------------------------
// Execute Method Tests
// ---------------------------------------------------------------------------

func TestMCPToolWrapper_Execute_ValidatesArgs(t *testing.T) {
	// Arrange
	serverName := "test-server"
	toolName := "search_files"

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			// This should NOT be reached due to validation failure
			t.Fatal("CallTool should not be called when validation fails")
			return nil, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Search for files",
		ServerName:  serverName,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"query"},
		},
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act — call without required "query"
	result, err := w.Execute(context.Background(), Parameters{
		Kwargs: map[string]interface{}{},
	})

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success, "Execute should return failure on validation error")
	assert.NotEmpty(t, result.Errors, "Should include validation error in Errors")
	assert.NotEmpty(t, result.Output, "Output should contain LLM-friendly message")
	assert.Contains(t, result.Output.(string), "Please correct these arguments")
	assert.Equal(t, true, result.Metadata["validation_error"])
}

func TestMCPToolWrapper_Execute_ValidArgsProceeds(t *testing.T) {
	// Arrange
	serverName := "test-server"
	toolName := "search_files"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{{Type: "text", Text: "OK"}},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Search for files",
		ServerName:  serverName,
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"query"},
		},
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act — call WITH required "query"
	result, err := w.Execute(context.Background(), Parameters{
		Kwargs: map[string]interface{}{"query": "test"},
	})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "OK", result.Output)
}

func TestMCPToolWrapper_Execute_Success_SingleTextContent(t *testing.T) {
	// Arrange
	serverName := "test-server"
	toolName := "search_files"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Search found 5 results",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			assert.Equal(t, serverName, sn)
			assert.Equal(t, toolName, tn)
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Search for files",
		ServerName:  serverName,
		InputSchema: map[string]interface{}{"type": "object"},
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{
		Kwargs: map[string]interface{}{"query": "test"},
	})

	// Assert
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, "Search found 5 results", result.Output)
	assert.Empty(t, result.Errors)
	assert.NotNil(t, result.ExecutionTime)
	assert.Greater(t, result.ExecutionTime, time.Duration(0))
}

func TestMCPToolWrapper_Execute_Success_ImageContent(t *testing.T) {
	// Arrange
	serverName := "image-server"
	toolName := "generate_image"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type:     "image",
				Data:     "base64imagedata...",
				MimeType: "image/png",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Generate an image",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)

	outputMap, ok := result.Output.(map[string]interface{})
	assert.True(t, ok, "Output should be a map for image content")
	assert.Equal(t, "image", outputMap["type"])
	assert.Equal(t, "base64imagedata...", outputMap["data"])
	assert.Equal(t, "image/png", outputMap["mimeType"])
}

func TestMCPToolWrapper_Execute_Success_ResourceContent(t *testing.T) {
	// Arrange
	serverName := "resource-server"
	toolName := "read_file"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type:     "resource",
				Text:     "File content here",
				Data:     "binary data",
				MimeType: "text/plain",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Read a file",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)

	outputMap, ok := result.Output.(map[string]interface{})
	assert.True(t, ok, "Output should be a map for resource content")
	assert.Equal(t, "resource", outputMap["type"])
	assert.Equal(t, "File content here", outputMap["text"])
	assert.Equal(t, "binary data", outputMap["data"])
	assert.Equal(t, "text/plain", outputMap["mimeType"])
}

func TestMCPToolWrapper_Execute_Success_MultipleContent(t *testing.T) {
	// Arrange
	serverName := "multi-server"
	toolName := "mixed_output"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "First text content",
			},
			{
				Type:     "image",
				Data:     "imagedata",
				MimeType: "image/jpeg",
			},
			{
				Type: "text",
				Text: "Second text content",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Multiple outputs",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)

	outputs, ok := result.Output.([]interface{})
	assert.True(t, ok, "Output should be a slice for multiple content items")
	assert.Len(t, outputs, 3)

	// First item - text
	firstText, ok := outputs[0].(string)
	assert.True(t, ok)
	assert.Equal(t, "First text content", firstText)

	// Second item - image
	secondImage, ok := outputs[1].(map[string]interface{})
	assert.True(t, ok)
	assert.Equal(t, "image", secondImage["type"])
	assert.Equal(t, "imagedata", secondImage["data"])

	// Third item - text
	thirdText, ok := outputs[2].(string)
	assert.True(t, ok)
	assert.Equal(t, "Second text content", thirdText)
}

func TestMCPToolWrapper_Execute_ToolError(t *testing.T) {
	// Arrange
	serverName := "error-server"
	toolName := "failing_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Tool execution failed: invalid parameter",
			},
		},
		IsError: true,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "A tool that fails",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err, "Execute should not return error even when tool fails")
	assert.NotNil(t, result)
	assert.False(t, result.Success, "Result.Success should be false when tool returns error")
	assert.Empty(t, result.Output, "Output should be empty when tool fails")
	assert.NotEmpty(t, result.Errors, "Errors should be populated")
	assert.Len(t, result.Errors, 1)
	assert.Equal(t, "Tool execution failed: invalid parameter", result.Errors[0])
}

func TestMCPToolWrapper_Execute_CallError(t *testing.T) {
	// Arrange
	serverName := "busy-server"
	toolName := "timeout_tool"

	expectedError := errors.New("connection timeout")

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return nil, expectedError
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "A tool that times out",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	// Note: Execute returns nil error but includes error in Result.Errors
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
	assert.Contains(t, result.Errors[0], "connection timeout")
}

func TestMCPToolWrapper_Execute_EmptyArguments(t *testing.T) {
	// Arrange
	serverName := "args-server"
	toolName := "no_args_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Executed with no args",
			},
		},
		IsError: false,
	}

	var receivedArgs map[string]interface{}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			receivedArgs = args
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Tool with no arguments",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act with nil Kwargs
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, receivedArgs, "Args should be initialized to empty map if nil")
	assert.Empty(t, receivedArgs, "Args should be empty map when Parameters.Kwargs is nil")

	// Act with empty map Kwargs
	receivedArgs = nil
	result, err = w.Execute(context.Background(), Parameters{
		Kwargs: map[string]interface{}{},
	})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, receivedArgs, "Args should be passed through when provided")
	assert.Empty(t, receivedArgs, "Args should be empty when provided as empty map")
}

func TestMCPToolWrapper_Execute_ExecutionTimeTracked(t *testing.T) {
	// Arrange
	serverName := "timing-server"
	toolName := "slow_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Done",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			// Simulate some processing time
			time.Sleep(10 * time.Millisecond)
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Slow tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Greater(t, result.ExecutionTime, time.Duration(0), "ExecutionTime should be positive")
	assert.GreaterOrEqual(t, result.ExecutionTime, 10*time.Millisecond,
		"ExecutionTime should be at least 10ms (the sleep duration)")
}

func TestMCPToolWrapper_Execute_MetadataIncluded(t *testing.T) {
	// Arrange
	serverName := "metadata-server"
	toolName := "search"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Search results",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Search tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Metadata)

	assert.Equal(t, serverName, result.Metadata["server_name"], "Metadata should include server_name")
	assert.Equal(t, toolName, result.Metadata["tool_name"], "Metadata should include tool_name")
	assert.Equal(t, 1, result.Metadata["content_count"], "Metadata should include content_count")
	assert.Equal(t, true, result.Metadata["mcp_source"], "Metadata should include mcp_source flag")
}

func TestMCPToolWrapper_Execute_MetadataIncludesAnnotations(t *testing.T) {
	// Arrange
	serverName := "annotated-server"
	toolName := "annotated_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Result with annotations",
				Annotations: map[string]interface{}{
					"priority":  1,
					"category":  "important",
					"processed": true,
				},
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Tool with annotated results",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, result.Metadata)

	annotations, ok := result.Metadata["annotations"].(map[string]interface{})
	assert.True(t, ok, "Metadata should include annotations when present in content")
	assert.Equal(t, 1, annotations["priority"])
	assert.Equal(t, "important", annotations["category"])
	assert.Equal(t, true, annotations["processed"])
}

func TestMCPToolWrapper_Execute_EmptyContent(t *testing.T) {
	// Arrange
	serverName := "empty-server"
	toolName := "empty_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Empty tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "", result.Output, "Empty content should result in empty string output")
	assert.Equal(t, 0, result.Metadata["content_count"], "Metadata should reflect 0 content items")
}

func TestMCPToolWrapper_Execute_MultipleErrorsInContent(t *testing.T) {
	// Arrange
	serverName := "multi-error-server"
	toolName := "multi_error_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Error 1: Invalid input",
			},
			{
				Type: "text",
				Text: "Error 2: Permission denied",
			},
			{
				Type: "text",
				Text: "Error 3: Resource not found",
			},
		},
		IsError: true,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Multi-error tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.False(t, result.Success)
	assert.NotEmpty(t, result.Errors)
	assert.Len(t, result.Errors, 3)
	assert.Contains(t, result.Errors, "Error 1: Invalid input")
	assert.Contains(t, result.Errors, "Error 2: Permission denied")
	assert.Contains(t, result.Errors, "Error 3: Resource not found")
}

func TestMCPToolWrapper_Execute_UnknownContentType(t *testing.T) {
	// Arrange
	serverName := "unknown-server"
	toolName := "unknown_type_tool"

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "unknown_type",
				Text: "Some unknown content",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Unknown type tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	// Act
	result, err := w.Execute(context.Background(), Parameters{})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	// Unknown content types should default to Text field
	assert.Equal(t, "Some unknown content", result.Output)
}

func TestMCPToolWrapper_Execute_ArgumentsPassedThrough(t *testing.T) {
	// Arrange
	serverName := "args-server"
	toolName := "arg_test_tool"

	var receivedArgs map[string]interface{}

	expectedResult := &MCPToolCallResult{
		Content: []MCPContent{
			{
				Type: "text",
				Text: "Args received",
			},
		},
		IsError: false,
	}

	mockMgr := &mockMCPManager{
		callToolFunc: func(ctx context.Context, sn, tn string, args map[string]interface{}) (*MCPToolCallResult, error) {
			receivedArgs = args
			return expectedResult, nil
		},
		getServer: func(name string) (MCPServer, bool) {
			mockSrv := newMockMCPServer(name)
			mockSrv.Start(context.Background())
			return mockSrv, true
		},
	}

	tool := MCPTool{
		Name:        toolName,
		Description: "Args test tool",
		ServerName:  serverName,
	}

	w := NewMCPToolWrapper(tool, mockMgr)

	inputArgs := map[string]interface{}{
		"query":   "test",
		"limit":   10,
		"filters": []string{"a", "b", "c"},
		"nested": map[string]interface{}{
			"key": "value",
		},
	}

	// Act
	result, err := w.Execute(context.Background(), Parameters{
		Kwargs: inputArgs,
	})

	// Assert
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.NotNil(t, receivedArgs)
	assert.Equal(t, inputArgs["query"], receivedArgs["query"])
	assert.Equal(t, inputArgs["limit"], receivedArgs["limit"])
	assert.Equal(t, inputArgs["filters"], receivedArgs["filters"])
	assert.Equal(t, inputArgs["nested"], receivedArgs["nested"])
}

// ---------------------------------------------------------------------------
// Helper function tests
// ---------------------------------------------------------------------------

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", "(root)"},
		{"#", "(root)"},
		{".", "(root)"},
		{"/query", ".query"},
		{"/filters/0", ".filters.0"},
		{"/nested/deep/field", ".nested.deep.field"},
		{"query", "query"}, // already normalized
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, normalizePath(tt.input))
		})
	}
}

func TestExtractLocationAndReason(t *testing.T) {
	t.Run("jsonschema/v6 at quoted format", func(t *testing.T) {
		detail := "at '/query': got number, want string"
		failure, ok := extractLocationAndReason(detail)
		assert.True(t, ok)
		assert.Equal(t, ".query", failure.Path)
		assert.Equal(t, "got number, want string", failure.Reason)
	})

	t.Run("jsonschema/v6 root missing property", func(t *testing.T) {
		detail := "at '': missing property 'query'"
		failure, ok := extractLocationAndReason(detail)
		assert.True(t, ok)
		assert.Equal(t, "(root)", failure.Path)
		assert.Equal(t, "missing property 'query'", failure.Reason)
	})

	t.Run("at prefix format no quotes", func(t *testing.T) {
		detail := "at /limit: must be of type integer"
		failure, ok := extractLocationAndReason(detail)
		assert.True(t, ok)
		assert.Equal(t, ".limit", failure.Path)
		assert.Equal(t, "must be of type integer", failure.Reason)
	})

	t.Run("location error format", func(t *testing.T) {
		detail := "location: '/query'; error: 'is required'"
		failure, ok := extractLocationAndReason(detail)
		assert.True(t, ok)
		assert.Equal(t, ".query", failure.Path)
		assert.Equal(t, "is required", failure.Reason)
	})

	t.Run("unrecognized format", func(t *testing.T) {
		detail := "something weird happened"
		_, ok := extractLocationAndReason(detail)
		assert.False(t, ok)
	})
}

func TestExtractValidationFailures(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.Nil(t, extractValidationFailures(nil))
	})

	t.Run("location format", func(t *testing.T) {
		// Simulate a real jsonschema validation error
		compiler := jsonschema.NewCompiler()
		compiler.AddResource("schema", map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"query"},
		})
		schema, err := compiler.Compile("schema")
		assert.NoError(t, err)

		err = schema.Validate(map[string]interface{}{})
		assert.Error(t, err)

		failures := extractValidationFailures(err)
		assert.NotEmpty(t, failures)
		assert.NotEmpty(t, failures[0].Reason)
	})

	t.Run("generic error fallback", func(t *testing.T) {
		err := errors.New("some unknown error")
		failures := extractValidationFailures(err)
		assert.Len(t, failures, 1)
		assert.Equal(t, "(root)", failures[0].Path)
		assert.Equal(t, "some unknown error", failures[0].Reason)
	})
}

func TestInvalidArgsError_Error(t *testing.T) {
	t.Run("with failures", func(t *testing.T) {
		err := &InvalidArgsError{
			Tool:   "search",
			Server: "files",
			Failures: []ValidationFailure{
				{Path: ".query", Reason: "is required"},
				{Path: ".limit", Reason: "must be of type integer"},
			},
		}
		msg := err.Error()
		assert.Contains(t, msg, "search")
		assert.Contains(t, msg, "files")
		assert.Contains(t, msg, ".query")
		assert.Contains(t, msg, "is required")
		assert.Contains(t, msg, ".limit")
	})

	t.Run("with empty failures", func(t *testing.T) {
		err := &InvalidArgsError{
			Tool:   "search",
			Server: "files",
		}
		msg := err.Error()
		assert.Contains(t, msg, "search")
		assert.Contains(t, msg, "files")
	})
}

func TestInvalidArgsError_Unwrap(t *testing.T) {
	wrapped := errors.New("inner error")
	err := &InvalidArgsError{
		Tool:    "search",
		Server:  "files",
		wrapped: wrapped,
	}
	assert.Equal(t, wrapped, err.Unwrap())
	assert.ErrorIs(t, err, wrapped)
}

func TestFormatForLLM(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		assert.Equal(t, "", FormatForLLM(nil))
	})

	t.Run("empty failures", func(t *testing.T) {
		err := &InvalidArgsError{Tool: "t", Server: "s"}
		msg := FormatForLLM(err)
		assert.Contains(t, msg, "t")
		assert.Contains(t, msg, "s")
	})

	t.Run("with failures", func(t *testing.T) {
		err := &InvalidArgsError{
			Tool:   "search",
			Server: "files",
			Failures: []ValidationFailure{
				{Path: "", Reason: "root error"},
				{Path: ".", Reason: "dot root"},
				{Path: "#", Reason: "hash root"},
				{Path: ".query", Reason: "is required"},
			},
		}
		msg := FormatForLLM(err)
		assert.Contains(t, msg, "(root)")
		assert.Contains(t, msg, ".query")
		assert.Contains(t, msg, "1. (root): root error")
		assert.Contains(t, msg, "4. .query: is required")
		assert.Contains(t, msg, "Please correct these arguments")
	})
}

func TestGetMCPValidationFailures(t *testing.T) {
	// Reset counter before test
	before := GetMCPValidationFailures()

	m := NewMCPManager(nil)
	m.AddServer(MCPServerConfig{Name: "srv", Command: "npx"})
	w := NewMCPToolWrapper(MCPTool{
		Name:       "tool",
		ServerName: "srv",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string"},
			},
			"required": []interface{}{"query"},
		},
	}, m)

	// Trigger validation failure
	_ = w.ValidateArgs(map[string]interface{}{})

	after := GetMCPValidationFailures()
	assert.Equal(t, before+1, after, "validation failure counter should increment by 1")

	// Valid args should not increment
	_ = w.ValidateArgs(map[string]interface{}{"query": "hello"})
	assert.Equal(t, before+1, GetMCPValidationFailures(), "counter should not change on valid args")
}
