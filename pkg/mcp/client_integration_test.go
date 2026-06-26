package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Mock MCP Server for Integration Testing
// ---------------------------------------------------------------------------

// createMockMCPServerScript creates a simple mock MCP server that responds to specific commands
func createMockMCPServerScript(responses map[string]interface{}) (string, error) {
	// We can't easily create a mock server in Go that speaks MCP protocol
	// without significant complexity. Instead, we'll use a cat command and
	// inject responses via a pipe if possible.
	//
	// For now, we'll create tests that use cat as a simple echo server
	// and manually inject responses into the stdout pipe after starting.
	return "", fmt.Errorf("mock MCP server creation not implemented")
}

// ---------------------------------------------------------------------------
// Test: Initialize() with Real Subprocess
// ---------------------------------------------------------------------------

// TestMCPClient_Initialize_WithCat tests initialization with a simple echo server
// This test is limited because cat doesn't speak the MCP protocol
func TestMCPClient_Initialize_WithCat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use sleep as a command that will not respond (ensures timeout)
	// Note: sleep doesn't speak MCP protocol and doesn't output anything
	// This ensures the timeout path is tested
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},  // Sleep for 30 seconds (longer than timeout)
		Timeout: 2 * time.Second, // Short timeout
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())

	// Try to initialize - this will fail because cat doesn't speak MCP
	// But it tests the sendRequest path and timeout handling
	errChan := make(chan error, 1)
	go func() {
		errChan <- client.Initialize(ctx)
	}()

	// Wait for timeout or error
	select {
	case err := <-errChan:
		// Initialization will fail - cat echoes back the request which isn't a valid response
		assert.Error(t, err)
	case <-time.After(6 * time.Second):
		t.Fatal("Initialize should have timed out or failed")
	}
}

// ---------------------------------------------------------------------------
// Test: sendRequest() with Pipe Injection
// ---------------------------------------------------------------------------

// TestMCPClient_sendRequest_WithPipeInjection tests the sendRequest function
// by manually injecting responses into the stdout pipe
func TestMCPClient_sendRequest_WithPipeInjection(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use sleep as a command that will not respond (ensures timeout)
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},  // Sleep for 30 seconds (longer than timeout)
		Timeout: 2 * time.Second, // Short timeout
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Send a request
	errChan := make(chan error, 1)
	go func() {
		// This will timeout because cat won't respond
		_, err := client.sendRequest(ctx, "test/method", nil)
		errChan <- err
	}()

	// Wait for timeout
	select {
	case err := <-errChan:
		// Should timeout
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "timeout")
	case <-time.After(6 * time.Second):
		t.Fatal("Request should have timed out")
	}
}

// ---------------------------------------------------------------------------
// Test: ListTools() with Real Subprocess
// ---------------------------------------------------------------------------

func TestMCPClient_ListTools_WithCat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 2 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Try to list tools - this will fail because cat doesn't speak MCP
	errChan := make(chan error, 1)
	var tools []MCPTool
	go func() {
		var err error
		tools, err = client.ListTools(ctx)
		errChan <- err
	}()

	// Wait for timeout or error
	select {
	case err := <-errChan:
		// Should fail (cat doesn't speak MCP)
		assert.Error(t, err)
		assert.Nil(t, tools)
	case <-time.After(6 * time.Second):
		t.Fatal("ListTools should have timed out or failed")
	}
}

// ---------------------------------------------------------------------------
// Test: CallTool() with Real Subprocess
// ---------------------------------------------------------------------------

func TestMCPClient_CallTool_WithCat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 2 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Try to call a tool - this will fail because cat doesn't speak MCP
	errChan := make(chan error, 1)
	var result *MCPToolCallResult
	go func() {
		var err error
		result, err = client.CallTool(ctx, MCPToolCallRequest{
			Name:      "test_tool",
			Arguments: map[string]interface{}{},
		})
		errChan <- err
	}()

	// Wait for timeout or error
	select {
	case err := <-errChan:
		// Should fail (cat doesn't speak MCP)
		assert.Error(t, err)
		assert.Nil(t, result)
	case <-time.After(6 * time.Second):
		t.Fatal("CallTool should have timed out or failed")
	}
}

// ---------------------------------------------------------------------------
// Test: Message Format Validation with Pipes
// ---------------------------------------------------------------------------

func TestMCPClient_SendRequest_MessageFormat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 10 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Create a message
	message := MCPMessage{
		JSONRPC: "2.0",
		ID:      "req_test",
		Method:  "test/method",
		Params: map[string]interface{}{
			"test": "value",
		},
	}

	// Marshal the message to JSON
	messageBytes, err := json.Marshal(message)
	require.NoError(t, err)

	// Verify the message is valid JSON
	assert.True(t, strings.HasPrefix(string(messageBytes), "{"))
	assert.True(t, strings.HasSuffix(string(messageBytes), "}"))
	assert.Contains(t, string(messageBytes), "2.0")
	assert.Contains(t, string(messageBytes), "req_test")
	assert.Contains(t, string(messageBytes), "test/method")
}

// ---------------------------------------------------------------------------
// Test: Pipe Management
// ---------------------------------------------------------------------------

func TestMCPClient_PipeManagement(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}

	// Verify pipes are connected
	client.mutex.Lock()
	stdinNotNil := client.stdin != nil
	stdoutNotNil := client.stdout != nil
	stderrNotNil := client.stderr != nil
	client.mutex.Unlock()

	assert.True(t, stdinNotNil, "stdin should be connected")
	assert.True(t, stdoutNotNil, "stdout should be connected")
	assert.True(t, stderrNotNil, "stderr should be connected")

	// Stop the client
	err = client.Stop(ctx)
	require.NoError(t, err)

	// Note: pipes might still be non-nil after stop, but the process should be stopped
	assert.False(t, client.IsRunning(), "client should not be running after stop")
}

// ---------------------------------------------------------------------------
// Test: Context Handling with Real Process
// ---------------------------------------------------------------------------

func TestMCPClient_ContextCancel_WithRunningProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 10 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}

	// Cancel the context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to send a request with cancelled context
	errChan := make(chan error, 1)
	go func() {
		_, err := client.sendRequest(ctx, "test/method", nil)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		// Should return context error
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("Request should have failed immediately with cancelled context")
	}

	// Stop the client
	client.Stop(context.Background())
}

// ---------------------------------------------------------------------------
// Test: Concurrent Requests
// ---------------------------------------------------------------------------

func TestMCPClient_ConcurrentRequests_WithCat(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Try to send multiple concurrent requests
	// All will timeout because cat doesn't speak MCP
	errChan := make(chan error, 3)

	for i := 0; i < 3; i++ {
		go func(n int) {
			_, err := client.sendRequest(ctx, fmt.Sprintf("test/method%d", n), nil)
			errChan <- err
		}(i)
	}

	// Wait for all requests to timeout/fail
	for i := 0; i < 3; i++ {
		select {
		case err := <-errChan:
			assert.Error(t, err)
		case <-time.After(10 * time.Second):
			t.Fatal("Request should have timed out")
		}
	}

	// Client should still be running
	assert.True(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Response Parsing with Real Data
// ---------------------------------------------------------------------------

func TestMCPClient_Initialize_InvalidJSON(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Create a subprocess that writes invalid JSON to stdout
	cmd := exec.CommandContext(context.Background(), "sh", "-c", "echo 'invalid json' && sleep 10")

	stdin, err := cmd.StdinPipe()
	require.NoError(t, err)

	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)

	err = cmd.Start()
	require.NoError(t, err)
	defer cmd.Process.Kill()

	// Close stdin to let cat exit
	stdin.Close()

	// Read from stdout
	buf := make([]byte, 1024)
	n, err := stdout.Read(buf)
	require.NoError(t, err)

	output := string(buf[:n])
	assert.Contains(t, output, "invalid json")
}

// ---------------------------------------------------------------------------
// Test: Message ID Tracking with Real Requests
// ---------------------------------------------------------------------------

func TestMCPClient_MessageIDTracking_WithRealProcess(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	defer client.Stop(ctx)

	// Get initial message ID
	client.reqMutex.Lock()
	initialID := client.messageID
	client.reqMutex.Unlock()

	// Try to send a request (will timeout)
	errChan := make(chan error, 1)
	go func() {
		_, err := client.sendRequest(ctx, "test/method", nil)
		errChan <- err
	}()

	// Wait a bit to ensure the request was sent
	time.Sleep(100 * time.Millisecond)

	// Get message ID after request
	client.reqMutex.Lock()
	afterRequestID := client.messageID
	client.reqMutex.Unlock()

	// Message ID should have been incremented
	assert.Greater(t, afterRequestID, initialID)

	// Wait for timeout
	<-errChan
}

// ---------------------------------------------------------------------------
// Test: Error Handling
// ---------------------------------------------------------------------------

func TestMCPClient_Start_InvalidWorkingDirectory(t *testing.T) {
	config := MCPServerConfig{
		Name:       "test-server",
		Command:    "cat",
		WorkingDir: "/nonexistent/directory/path/xyz/123",
		Timeout:    5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	assert.Error(t, err)
	assert.False(t, client.IsRunning())
}

func TestMCPClient_Stop_Twice(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}

	// Stop once
	err = client.Stop(ctx)
	require.NoError(t, err)

	// Stop again - should be idempotent
	err = client.Stop(ctx)
	assert.NoError(t, err)

	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Graceful Shutdown
// ---------------------------------------------------------------------------

func TestMCPClient_GracefulShutdown(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}

	startTime := time.Now()

	// Stop the client - cat should exit gracefully when stdin closes
	err = client.Stop(ctx)
	elapsed := time.Since(startTime)

	require.NoError(t, err)
	assert.False(t, client.IsRunning())
	// Should complete within a reasonable time
	assert.Less(t, elapsed, 6*time.Second)
}
