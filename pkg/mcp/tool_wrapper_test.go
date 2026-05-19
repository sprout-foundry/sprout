package mcp

import (
	"context"
	"errors"
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
