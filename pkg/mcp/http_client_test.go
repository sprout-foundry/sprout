package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test: HTTP Client Creation
// ---------------------------------------------------------------------------

func TestNewMCPHTTPClient_BasicCreation(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	assert.NotNil(t, client)
	assert.Equal(t, "test-server", client.GetName())
	assert.Equal(t, config, client.GetConfig())
	assert.False(t, client.IsRunning())
}

func TestNewMCPHTTPClient_WithNilLogger(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	client := NewMCPHTTPClient(config, nil)

	assert.NotNil(t, client)
	assert.Equal(t, "test-server", client.GetName())
	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Start() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_Start_Success(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	assert.NoError(t, err)
	assert.True(t, client.IsRunning())
}

func TestMCPHTTPClient_Start_AlreadyRunning(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	// First start
	err := client.Start(ctx)
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	// Second start should be idempotent (return nil)
	err = client.Start(ctx)
	assert.NoError(t, err)
	assert.True(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Stop() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_Stop_Success(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	// Stop the client
	err = client.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, client.IsRunning())
}

func TestMCPHTTPClient_Stop_ClearsSessionID(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	// Start and set a session ID
	err := client.Start(ctx)
	require.NoError(t, err)

	client.mu.Lock()
	client.sessionID = "test-session-123"
	client.mu.Unlock()

	// Stop should clear session ID
	err = client.Stop(ctx)
	require.NoError(t, err)

	client.mu.RLock()
	assert.Empty(t, client.sessionID)
	client.mu.RUnlock()
}

func TestMCPHTTPClient_Stop_NotRunning(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	// Stop without starting should be a no-op
	err := client.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: IsRunning() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_IsRunning_InitialState(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	assert.False(t, client.IsRunning())
}

func TestMCPHTTPClient_IsRunning_AfterStart(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	assert.True(t, client.IsRunning())
}

func TestMCPHTTPClient_IsRunning_AfterStop(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	err := client.Start(ctx)
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	err = client.Stop(ctx)
	require.NoError(t, err)

	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Initialize() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_Initialize_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Parse request
		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)
		assert.Equal(t, "initialize", reqBody["method"])

		// Send response with session ID
		w.Header().Set("Mcp-Session-Id", "test-session-123")
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"protocolVersion": "2025-06-18",
				"serverInfo": map[string]interface{}{
					"name":    "test-server",
					"version": "1.0.0",
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	assert.NoError(t, err)

	// Verify session ID was captured
	client.mu.RLock()
	assert.Equal(t, "test-session-123", client.sessionID)
	assert.True(t, client.initialized)
	client.mu.RUnlock()
}

func TestMCPHTTPClient_Initialize_NotStarted(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Initialize(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client not started")
}

func TestMCPHTTPClient_Initialize_Idempotent(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()

	err := client.Start(ctx)
	require.NoError(t, err)

	// Manually set initialized to true
	client.mu.Lock()
	client.initialized = true
	client.mu.Unlock()

	// Second initialization should be idempotent
	err = client.Initialize(ctx)
	assert.NoError(t, err)
}

func TestMCPHTTPClient_Initialize_HTTPError(t *testing.T) {
	// Create a mock server that returns an error
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"error": map[string]interface{}{
				"code":    -32603,
				"message": "Internal server error",
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: ListTools() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ListTools_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "test_tool_1",
						"description": "Test tool 1",
						"inputSchema": map[string]interface{}{
							"type": "object",
						},
					},
					{
						"name":        "test_tool_2",
						"description": "Test tool 2",
						"inputSchema": map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	tools, err := client.ListTools(ctx)
	assert.NoError(t, err)
	assert.Len(t, tools, 2)
	assert.Equal(t, "test_tool_1", tools[0].Name)
	assert.Equal(t, "test_tool_2", tools[1].Name)
	assert.Equal(t, "test-server", tools[0].ServerName)
}

func TestMCPHTTPClient_ListTools_NotStarted(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	_, err := client.ListTools(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "client not started")
}

func TestMCPHTTPClient_ListTools_AutoInitialize(t *testing.T) {
	// Create a mock server that handles both initialize and tools/list
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"tools": []map[string]interface{}{
					{
						"name":        "test_tool",
						"description": "Test tool",
						"inputSchema": map[string]interface{}{
							"type": "object",
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	// ListTools should auto-initialize if not already initialized
	tools, err := client.ListTools(ctx)
	assert.NoError(t, err)
	assert.Len(t, tools, 1)
	assert.Equal(t, "test_tool", tools[0].Name)
}

// ---------------------------------------------------------------------------
// Test: CallTool() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_CallTool_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"content": []map[string]interface{}{
					{
						"type": "text",
						"text": "Tool result",
					},
				},
				"isError": false,
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	request := MCPToolCallRequest{
		Name: "test_tool",
		Arguments: map[string]interface{}{
			"param1": "value1",
		},
	}

	result, err := client.CallTool(ctx, request)
	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.IsError)
	assert.Len(t, result.Content, 1)
	assert.Equal(t, "text", result.Content[0].Type)
	assert.Equal(t, "Tool result", result.Content[0].Text)
}

func TestMCPHTTPClient_CallTool_NotStarted(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	request := MCPToolCallRequest{Name: "test_tool"}
	_, err := client.CallTool(ctx, request)

	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: ListResources() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ListResources_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"resources": []map[string]interface{}{
					{
						"uri":         "file:///test/file.txt",
						"name":        "test file",
						"description": "A test file",
						"mimeType":    "text/plain",
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	resources, err := client.ListResources(ctx)
	assert.NoError(t, err)
	assert.Len(t, resources, 1)
	assert.Equal(t, "file:///test/file.txt", resources[0].URI)
	assert.Equal(t, "test file", resources[0].Name)
	assert.Equal(t, "test-server", resources[0].ServerName)
}

func TestMCPHTTPClient_ListResources_NotInitialized(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	_, err = client.ListResources(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "not started or initialized")
}

// ---------------------------------------------------------------------------
// Test: ReadResource() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ReadResource_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"contents": []map[string]interface{}{
					{
						"type":     "text",
						"text":     "File content",
						"mimeType": "text/plain",
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	content, err := client.ReadResource(ctx, "file:///test/file.txt")
	assert.NoError(t, err)
	assert.NotNil(t, content)
	assert.Equal(t, "text", content.Type)
	assert.Equal(t, "File content", content.Text)
	assert.Equal(t, "text/plain", content.MimeType)
}

func TestMCPHTTPClient_ReadResource_EmptyContent(t *testing.T) {
	// Create a mock server that returns empty contents
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"contents": []interface{}{},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	_, err = client.ReadResource(ctx, "file:///test/file.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no content returned")
}

// ---------------------------------------------------------------------------
// Test: ListPrompts() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ListPrompts_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"prompts": []map[string]interface{}{
					{
						"name":        "test_prompt",
						"description": "Test prompt",
						"arguments": []map[string]interface{}{
							{
								"name":     "arg1",
								"required": true,
							},
						},
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	prompts, err := client.ListPrompts(ctx)
	assert.NoError(t, err)
	assert.Len(t, prompts, 1)
	assert.Equal(t, "test_prompt", prompts[0].Name)
	assert.Equal(t, "Test prompt", prompts[0].Description)
	assert.Equal(t, "test-server", prompts[0].ServerName)
}

// ---------------------------------------------------------------------------
// Test: GetPrompt() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_GetPrompt_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"messages": []map[string]interface{}{
					{
						"type": "text",
						"text": "Prompt message",
					},
				},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	content, err := client.GetPrompt(ctx, "test_prompt", map[string]interface{}{"arg1": "value1"})
	assert.NoError(t, err)
	assert.NotNil(t, content)
	assert.Equal(t, "text", content.Type)
	assert.Equal(t, "Prompt message", content.Text)
}

func TestMCPHTTPClient_GetPrompt_EmptyMessages(t *testing.T) {
	// Create a mock server that returns empty messages
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)
		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-123")
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"messages": []interface{}{},
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	_, err = client.GetPrompt(ctx, "test_prompt", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no messages returned")
}

// ---------------------------------------------------------------------------
// Test: Session ID Tracking
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_SessionID_InitializeCaptures(t *testing.T) {
	// Create a mock server that sets session ID
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		w.Header().Set("Mcp-Session-Id", "captured-session-id")
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	client.mu.RLock()
	assert.Equal(t, "captured-session-id", client.sessionID)
	client.mu.RUnlock()
}

func TestMCPHTTPClient_SessionID_SentOnSubsequentRequests(t *testing.T) {
	var receivedSessionIDs []string
	var mu sync.Mutex

	// Create a mock server that tracks session ID headers
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		method := reqBody["method"].(string)

		// Track session ID header
		mu.Lock()
		if sessionID := r.Header.Get("Mcp-Session-Id"); sessionID != "" {
			receivedSessionIDs = append(receivedSessionIDs, fmt.Sprintf("%s:%s", method, sessionID))
		}
		mu.Unlock()

		if method == "initialize" {
			w.Header().Set("Mcp-Session-Id", "test-session-456")
		}

		result := map[string]interface{}{}
		if method == "tools/list" {
			result["tools"] = []interface{}{}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result":  result,
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	// Initialize
	err = client.Initialize(ctx)
	require.NoError(t, err)

	// List tools
	_, err = client.ListTools(ctx)
	require.NoError(t, err)

	// Verify session ID was sent on tools/list but not on initialize
	mu.Lock()
	defer mu.Unlock()
	assert.Contains(t, receivedSessionIDs, "tools/list:test-session-456")
	// initialize should NOT have session ID header
	for _, item := range receivedSessionIDs {
		assert.False(t, strings.HasPrefix(item, "initialize:"), "initialize should not send session ID")
	}
}

// ---------------------------------------------------------------------------
// Test: sendRequest() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_sendRequest_Success(t *testing.T) {
	// Create a mock server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		var reqBody map[string]interface{}
		err := json.NewDecoder(r.Body).Decode(&reqBody)
		require.NoError(t, err)

		assert.Equal(t, "test_method", reqBody["method"])
		assert.NotNil(t, reqBody["id"])

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result": map[string]interface{}{
				"status": "success",
			},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	response, err := client.sendRequest(ctx, "test_method", map[string]interface{}{"key": "value"})

	assert.NoError(t, err)
	assert.NotNil(t, response)
	assert.Nil(t, response.Error)
	assert.NotNil(t, response.Result)
}

func TestMCPHTTPClient_sendRequest_NonOKStatus(t *testing.T) {
	// Create a mock server that returns 404
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, "Not Found")
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	_, err := client.sendRequest(ctx, "test_method", nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "404")
}

func TestMCPHTTPClient_sendRequest_InvalidResponse(t *testing.T) {
	// Create a mock server that returns invalid JSON
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, "invalid json{{{")
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	_, err := client.sendRequest(ctx, "test_method", nil)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal")
}

func TestMCPHTTPClient_sendRequest_NetworkError(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:99999/not-a-real-server",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	_, err := client.sendRequest(ctx, "test_method", nil)

	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: Thread Safety - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ConcurrentIsRunning(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	// Launch multiple goroutines calling IsRunning concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.IsRunning()
		}()
	}

	wg.Wait()
	// Should not deadlock or panic
}

func TestMCPHTTPClient_ConcurrentRequests(t *testing.T) {
	// Create a mock server
	var requestCount int
	var mu sync.Mutex

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		requestCount++
		mu.Unlock()

		var reqBody map[string]interface{}
		json.NewDecoder(r.Body).Decode(&reqBody)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      reqBody["id"],
			"result":  map[string]interface{}{},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)
	require.NoError(t, err)

	err = client.Initialize(ctx)
	require.NoError(t, err)

	// Reset counter after Initialize (which sends its own request)
	mu.Lock()
	requestCount = 0
	mu.Unlock()

	// Launch multiple concurrent requests
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = client.sendRequest(ctx, "test_method", nil)
		}()
	}

	wg.Wait()

	// Verify all requests were processed
	mu.Lock()
	assert.Equal(t, 10, requestCount)
	mu.Unlock()
}

// ---------------------------------------------------------------------------
// Test: Context Cancellation - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_ContextCancellation(t *testing.T) {
	// Create a mock server that delays response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      1,
			"result":  map[string]interface{}{},
		})
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  server.URL,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	// Create a context with short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := client.sendRequest(ctx, "test_method", nil)

	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: Timeout Behavior - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_HTTPClientTimeout(t *testing.T) {
	// Create a mock server that doesn't respond
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
	}))
	defer server.Close()

	config := MCPServerConfig{
		Name:    "test-server",
		Type:    "http",
		URL:     server.URL,
		Timeout: 100 * time.Millisecond,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	ctx := context.Background()
	_, err := client.sendRequest(ctx, "test_method", nil)

	assert.Error(t, err)
	assert.True(t, strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(err.Error(), "deadline exceeded"))
}

// ---------------------------------------------------------------------------
// Test: Message ID Increment
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_MessageIDIncrement(t *testing.T) {
	config := MCPServerConfig{
		Name: "test-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	// Get initial ID
	client.mu.Lock()
	initialID := client.nextID
	client.mu.Unlock()

	// Simulate ID increment (happens in sendRequest)
	client.mu.Lock()
	client.nextID++
	client.mu.Unlock()

	// Verify ID was incremented
	client.mu.Lock()
	newID := client.nextID
	client.mu.Unlock()

	assert.Equal(t, initialID+1, newID)
}

// ---------------------------------------------------------------------------
// Test: GetName() and GetConfig() - HTTP Client
// ---------------------------------------------------------------------------

func TestMCPHTTPClient_GetName(t *testing.T) {
	config := MCPServerConfig{
		Name: "my-http-server",
		Type: "http",
		URL:  "http://localhost:8080/mcp",
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	assert.Equal(t, "my-http-server", client.GetName())
}

func TestMCPHTTPClient_GetConfig(t *testing.T) {
	config := MCPServerConfig{
		Name:      "my-http-server",
		Type:      "http",
		URL:       "http://localhost:8080/mcp",
		AutoStart: true,
		Timeout:   10 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPHTTPClient(config, logger)

	retrievedConfig := client.GetConfig()
	assert.Equal(t, "my-http-server", retrievedConfig.Name)
	assert.Equal(t, "http", retrievedConfig.Type)
	assert.Equal(t, "http://localhost:8080/mcp", retrievedConfig.URL)
	assert.True(t, retrievedConfig.AutoStart)
	assert.Equal(t, 10*time.Second, retrievedConfig.Timeout)
}
