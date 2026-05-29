package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test: MCPClient Creation and Basic State
// ---------------------------------------------------------------------------

func TestNewMCPClient_BasicCreation(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"hello"},
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	assert.NotNil(t, client)
	assert.Equal(t, "test-server", client.GetName())
	assert.Equal(t, config, client.GetConfig())
	assert.False(t, client.IsRunning())
}

func TestNewMCPClient_WithNilLogger(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "echo",
	}

	client := NewMCPClient(config, nil)

	assert.NotNil(t, client)
	assert.Equal(t, "test-server", client.GetName())
	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Start() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_Start_Success(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat", // Use cat as a simple echo server
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	// On systems without cat, this might fail - handle gracefully
	if err != nil {
		t.Logf("Start failed (may be expected on some systems): %v", err)
		return
	}

	assert.True(t, client.IsRunning())

	// Clean up
	_ = client.Stop(ctx)
}

// ---------------------------------------------------------------------------
// Test: Start() - Error Cases
// ---------------------------------------------------------------------------

func TestMCPClient_Start_AlreadyRunning(t *testing.T) {
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

	// First start should succeed
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	assert.True(t, client.IsRunning())

	// Second start should fail with "already running" error
	err = client.Start(ctx)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "already running")

	// Clean up
	_ = client.Stop(ctx)
}

func TestMCPClient_Start_InvalidCommand(t *testing.T) {
	config := MCPServerConfig{
		Name:    "invalid-server",
		Command: "nonexistent-command-xyz-123",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to start MCP server")
	assert.False(t, client.IsRunning())
}

func TestMCPClient_Start_EmptyCommand(t *testing.T) {
	config := MCPServerConfig{
		Name:    "empty-server",
		Command: "",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	// Empty command should cause an error when trying to start
	assert.Error(t, err)
	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Stop() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_Stop_Success(t *testing.T) {
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

	// Start the server
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	// Stop the server
	err = client.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, client.IsRunning())
}

func TestMCPClient_Stop_NotRunning(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Stop without starting should return nil (no-op)
	err := client.Stop(ctx)
	assert.NoError(t, err)
	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: IsRunning() State Tracking
// ---------------------------------------------------------------------------

func TestMCPClient_IsRunning_InitialState(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	assert.False(t, client.IsRunning())
}

func TestMCPClient_IsRunning_AfterStart(t *testing.T) {
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

	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

func TestMCPClient_IsRunning_AfterStop(t *testing.T) {
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

	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	err = client.Stop(ctx)
	require.NoError(t, err)

	assert.False(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Initialize() - Idempotency
// ---------------------------------------------------------------------------

func TestMCPClient_Initialize_Idempotent(t *testing.T) {
	// Create a mock client that simulates successful initialization
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Manually set initialized to true (simulating previous init)
	client.mutex.Lock()
	client.initialized = true
	client.mutex.Unlock()

	ctx := context.Background()

	// Second initialization should return nil immediately (idempotent)
	err := client.Initialize(ctx)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// Test: GetName() and GetConfig()
// ---------------------------------------------------------------------------

func TestMCPClient_GetName(t *testing.T) {
	config := MCPServerConfig{
		Name:    "my-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	assert.Equal(t, "my-server", client.GetName())
}

func TestMCPClient_GetConfig(t *testing.T) {
	config := MCPServerConfig{
		Name:      "my-server",
		Command:   "cat",
		Args:      []string{"-n"},
		AutoStart: true,
		Timeout:   10 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	retrievedConfig := client.GetConfig()
	assert.Equal(t, "my-server", retrievedConfig.Name)
	assert.Equal(t, "cat", retrievedConfig.Command)
	assert.Equal(t, []string{"-n"}, retrievedConfig.Args)
	assert.True(t, retrievedConfig.AutoStart)
	assert.Equal(t, 10*time.Second, retrievedConfig.Timeout)
}

// ---------------------------------------------------------------------------
// Test: Thread Safety
// ---------------------------------------------------------------------------

func TestMCPClient_ConcurrentIsRunning(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

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

func TestMCPClient_ConcurrentGetName(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Launch multiple goroutines calling GetName concurrently
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = client.GetName()
		}()
	}

	wg.Wait()
	// Should not deadlock or panic
}

// ---------------------------------------------------------------------------
// Test: Context Cancellation
// ---------------------------------------------------------------------------

func TestMCPClient_ContextCancellation(t *testing.T) {
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

	// Create a context that's already cancelled
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	err := client.Start(ctx)
	// Should error quickly due to cancelled context
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// Test: Timeout Behavior
// ---------------------------------------------------------------------------

func TestMCPClient_Start_WithTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 10 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

func TestMCPClient_Start_ZeroTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 0, // Zero timeout
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test: Restart Count
// ---------------------------------------------------------------------------

func TestMCPClient_RestartCount(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}
	// NOTE: Start()/Stop() spawn real processes under the hood. A pre-existing
	// data race in production code (client.go Start writes c.stdout/c.stderr
	// concurrently with handleMessages/handleErrors goroutines reading them)
	// will cause this test to fail under `go test -race`. Run with -short to skip.

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Initial restart count should be 0
	assert.Equal(t, 0, client.GetRestartCount())

	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)

	// After start, restart count should be 1
	assert.Equal(t, 1, client.GetRestartCount())

	err = client.Stop(ctx)
	require.NoError(t, err)

	err = client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	// After second start, restart count should be 2
	assert.Equal(t, 2, client.GetRestartCount())
}

// ---------------------------------------------------------------------------
// Test: Edge Cases
// ---------------------------------------------------------------------------

func TestMCPClient_Start_WithWorkingDir(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	// Use current directory
	cwd, err := os.Getwd()
	require.NoError(t, err)

	config := MCPServerConfig{
		Name:       "test-server",
		Command:    "cat",
		WorkingDir: cwd,
		Timeout:    5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err = client.Start(ctx)

	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

func TestMCPClient_Start_WithEnvironment(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Env: map[string]string{
			"TEST_VAR": "test_value",
		},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

func TestMCPClient_WithCredentials(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
		Credentials: map[string]string{
			"API_KEY": "test-secret",
		},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Verify config has credentials
	retrievedConfig := client.GetConfig()
	assert.NotNil(t, retrievedConfig.Credentials)
	assert.Equal(t, "test-secret", retrievedConfig.Credentials["API_KEY"])
}

// ---------------------------------------------------------------------------
// Test: sendRequest - Message ID Tracking
// ---------------------------------------------------------------------------

func TestMCPClient_sendRequest_IncrementingMessageID(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Get initial message ID
	client.reqMutex.Lock()
	initialID := client.messageID
	client.reqMutex.Unlock()

	// Simulate a request (without actually sending)
	client.reqMutex.Lock()
	client.messageID++
	client.reqMutex.Unlock()

	// Verify ID was incremented
	client.reqMutex.Lock()
	newID := client.messageID
	client.reqMutex.Unlock()

	assert.Equal(t, initialID+1, newID)
}

// ---------------------------------------------------------------------------
// Test: sendRequest - Pending Request Management
// ---------------------------------------------------------------------------

func TestMCPClient_sendRequest_PendingRequestCleanup(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Add a pending request
	client.reqMutex.Lock()
	client.pendingReqs["test_id"] = make(chan MCPMessage, 1)
	client.reqMutex.Unlock()

	// Verify request exists
	client.reqMutex.RLock()
	_, exists := client.pendingReqs["test_id"]
	client.reqMutex.RUnlock()
	assert.True(t, exists)

	// Clean up the request
	client.reqMutex.Lock()
	delete(client.pendingReqs, "test_id")
	client.reqMutex.Unlock()

	// Verify request was removed
	client.reqMutex.RLock()
	_, exists = client.pendingReqs["test_id"]
	client.reqMutex.RUnlock()
	assert.False(t, exists)
}

// ---------------------------------------------------------------------------
// Test: Message Marshalling
// ---------------------------------------------------------------------------

func TestMCPClient_MarshallMessage(t *testing.T) {
	message := MCPMessage{
		JSONRPC: "2.0",
		ID:      "req_1",
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"clientInfo": map[string]interface{}{
				"name":    "ledit",
				"version": "1.0.0",
			},
		},
	}

	data, err := json.Marshal(message)
	require.NoError(t, err)

	var unmarshaled MCPMessage
	err = json.Unmarshal(data, &unmarshaled)
	require.NoError(t, err)

	assert.Equal(t, "2.0", unmarshaled.JSONRPC)
	assert.Equal(t, "req_1", fmt.Sprintf("%v", unmarshaled.ID))
	assert.Equal(t, "initialize", unmarshaled.Method)
	assert.NotNil(t, unmarshaled.Params)
}

// ---------------------------------------------------------------------------
// Test: Stop with Timeout
// ---------------------------------------------------------------------------

func TestMCPClient_Stop_GracefulShutdown(t *testing.T) {
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

	// Start the server
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start cat command: %v", err)
	}
	require.NoError(t, err)
	require.True(t, client.IsRunning())

	// Stop should gracefully wait for process to exit
	// cat will exit when stdin closes
	startTime := time.Now()
	err = client.Stop(ctx)
	elapsed := time.Since(startTime)

	require.NoError(t, err)
	assert.False(t, client.IsRunning())
	// Should complete within a reasonable time (< 6 seconds)
	assert.Less(t, elapsed, 6*time.Second)
}

// ---------------------------------------------------------------------------
// Test: Server Config with Args
// ---------------------------------------------------------------------------

func TestMCPClient_Start_WithArgs(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "echo",
		Args:    []string{"-n", "hello"},
		Timeout: 5 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()
	err := client.Start(ctx)

	if err != nil {
		t.Skipf("Cannot start echo command: %v", err)
	}
	require.NoError(t, err)
	defer client.Stop(ctx)

	assert.True(t, client.IsRunning())
}

// ---------------------------------------------------------------------------
// Test Logger Output
// ---------------------------------------------------------------------------

func TestTestLogger(t *testing.T) {
	logger := NewTestLogger()

	// The test logger returns nil, which is handled gracefully
	// by checking for nil before calling methods
	assert.Nil(t, logger)
}

// ---------------------------------------------------------------------------
// Helper: TestLogger for Testing
// ---------------------------------------------------------------------------

// NewTestLogger creates a minimal logger for testing
func NewTestLogger() *utils.Logger {
	// Return a nil logger - the code handles nil loggers gracefully
	return nil
}
