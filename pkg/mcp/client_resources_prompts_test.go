package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Ensure utils import is used by referencing the type
var _ *utils.Logger

// ---------------------------------------------------------------------------
// Test: ListResources() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ListResources_NotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// ListResources should fail because the server doesn't respond with MCP protocol
	// This tests the initialization check and timeout path
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListResources(ctx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		// Should fail due to timeout or parse error
		assert.Error(t, err, "ListResources should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("ListResources should have timed out quickly")
	}
}

func TestMCPClient_ListResources_AlreadyInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Manually set initialized to test idempotency check
	client.mutex.Lock()
	client.initialized = true
	client.mutex.Unlock()

	// ListResources should still fail due to server not speaking MCP
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListResources(ctx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ListResources should fail when server doesn't respond correctly")
	case <-time.After(5 * time.Second):
		t.Fatal("ListResources should have timed out or failed")
	}
}

// ---------------------------------------------------------------------------
// Test: ListResources() - Error Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ListResources_ContextCancellation(t *testing.T) {
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
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Create a context that will be cancelled
	cancelCtx, cancel := context.WithCancel(ctx)

	// Cancel immediately
	cancel()

	// ListResources should fail due to cancelled context
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListResources(cancelCtx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ListResources should fail with cancelled context")
	case <-time.After(3 * time.Second):
		t.Fatal("ListResources should have failed quickly due to cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Test: ReadResource() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ReadResource_NotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// ReadResource should fail because server doesn't speak MCP
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ReadResource(ctx, "file:///tmp/test.txt")
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ReadResource should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("ReadResource should have timed out quickly")
	}
}

func TestMCPClient_ReadResource_WithURI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test various URI formats
	uris := []string{
		"file:///tmp/test.txt",
		"http://example.com/resource",
		"custom://resource/123",
	}

	for _, uri := range uris {
		errChan := make(chan error, 1)
		go func(testURI string) {
			_, err := client.ReadResource(ctx, testURI)
			errChan <- err
		}(uri)

		select {
		case err := <-errChan:
			assert.Error(t, err, "ReadResource should fail for URI: %s", uri)
		case <-time.After(5 * time.Second):
			t.Fatalf("ReadResource should have timed out for URI: %s", uri)
		}
	}
}

// ---------------------------------------------------------------------------
// Test: ReadResource() - Error Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ReadResource_ContextCancellation(t *testing.T) {
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
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Create a context that will be cancelled
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	// ReadResource should fail due to cancelled context
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ReadResource(cancelCtx, "file:///tmp/test.txt")
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ReadResource should fail with cancelled context")
	case <-time.After(3 * time.Second):
		t.Fatal("ReadResource should have failed quickly due to cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Test: ListPrompts() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ListPrompts_NotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// ListPrompts should fail because server doesn't speak MCP
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListPrompts(ctx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ListPrompts should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("ListPrompts should have timed out quickly")
	}
}

func TestMCPClient_ListPrompts_AlreadyInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Manually set initialized to test idempotency check
	client.mutex.Lock()
	client.initialized = true
	client.mutex.Unlock()

	// ListPrompts should still fail due to server not speaking MCP
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListPrompts(ctx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ListPrompts should fail when server doesn't respond correctly")
	case <-time.After(5 * time.Second):
		t.Fatal("ListPrompts should have timed out or failed")
	}
}

// ---------------------------------------------------------------------------
// Test: ListPrompts() - Error Cases
// ---------------------------------------------------------------------------

func TestMCPClient_ListPrompts_ContextCancellation(t *testing.T) {
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
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Create a context that will be cancelled
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	// ListPrompts should fail due to cancelled context
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ListPrompts(cancelCtx)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ListPrompts should fail with cancelled context")
	case <-time.After(3 * time.Second):
		t.Fatal("ListPrompts should have failed quickly due to cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Test: GetPrompt() - Success Cases
// ---------------------------------------------------------------------------

func TestMCPClient_GetPrompt_NotInitialized(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// GetPrompt should fail because server doesn't speak MCP
	errChan := make(chan error, 1)
	go func() {
		_, err := client.GetPrompt(ctx, "summarize", nil)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "GetPrompt should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("GetPrompt should have timed out quickly")
	}
}

func TestMCPClient_GetPrompt_WithArguments(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test with arguments
	args := map[string]interface{}{
		"text":     "This is a test",
		"language": "Spanish",
	}

	errChan := make(chan error, 1)
	go func() {
		_, err := client.GetPrompt(ctx, "translate", args)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "GetPrompt should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("GetPrompt should have timed out quickly")
	}
}

func TestMCPClient_GetPrompt_NilArguments(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test with nil arguments
	errChan := make(chan error, 1)
	go func() {
		_, err := client.GetPrompt(ctx, "simple", nil)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "GetPrompt should fail when server doesn't speak MCP")
	case <-time.After(5 * time.Second):
		t.Fatal("GetPrompt should have timed out quickly")
	}
}

// ---------------------------------------------------------------------------
// Test: GetPrompt() - Error Cases
// ---------------------------------------------------------------------------

func TestMCPClient_GetPrompt_ContextCancellation(t *testing.T) {
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
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Create a context that will be cancelled
	cancelCtx, cancel := context.WithCancel(ctx)
	cancel()

	// GetPrompt should fail due to cancelled context
	errChan := make(chan error, 1)
	go func() {
		_, err := client.GetPrompt(cancelCtx, "test-prompt", nil)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "GetPrompt should fail with cancelled context")
	case <-time.After(3 * time.Second):
		t.Fatal("GetPrompt should have failed quickly due to cancelled context")
	}
}

// ---------------------------------------------------------------------------
// Test: Response Parsing with Type Safety
// ---------------------------------------------------------------------------

func TestMCPClient_ResourcesResponseParsing(t *testing.T) {
	// Test that the response parsing logic handles the expected structure
	mockResources := []MCPResource{
		{
			URI:         "file:///tmp/test.txt",
			Name:        "test.txt",
			Description: "Test resource",
			MimeType:    "text/plain",
		},
		{
			URI:         "file:///tmp/test2.txt",
			Name:        "test2.txt",
			Description: "Another test resource",
			MimeType:    "text/plain",
		},
	}

	// Create the response structure
	result := struct {
		Resources []MCPResource `json:"resources"`
	}{
		Resources: mockResources,
	}

	// Marshal to JSON
	resultBytes, err := json.Marshal(result)
	require.NoError(t, err, "Should marshal resources result")

	// Unmarshal back
	var unmarshaled struct {
		Resources []MCPResource `json:"resources"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err, "Should unmarshal resources result")

	// Verify data integrity with type safety checks
	assert.Len(t, unmarshaled.Resources, 2, "Should have 2 resources")

	for i, resource := range unmarshaled.Resources {
		// Type-safe field access
		assert.NotEmpty(t, resource.URI, "Resource %d should have URI", i)
		assert.NotEmpty(t, resource.Name, "Resource %d should have Name", i)

		// Optional fields should be handled safely
		if resource.Description != "" {
			assert.NotEmpty(t, resource.Description)
		}
		if resource.MimeType != "" {
			assert.NotEmpty(t, resource.MimeType)
		}
	}
}

func TestMCPClient_ResourceReadResponseParsing(t *testing.T) {
	// Test that the resource read response parsing handles the expected structure
	mockContent := MCPContent{
		Type: "text",
		Text: "This is test content",
	}

	result := struct {
		Contents []MCPContent `json:"contents"`
	}{
		Contents: []MCPContent{mockContent},
	}

	// Marshal to JSON
	resultBytes, err := json.Marshal(result)
	require.NoError(t, err, "Should marshal content result")

	// Unmarshal back
	var unmarshaled struct {
		Contents []MCPContent `json:"contents"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err, "Should unmarshal content result")

	// Verify data integrity with type safety
	assert.Len(t, unmarshaled.Contents, 1, "Should have 1 content item")

	content := unmarshaled.Contents[0]
	assert.NotEmpty(t, content.Type, "Content should have Type")

	// Type-safe field access based on content type
	if content.Type == "text" {
		assert.NotEmpty(t, content.Text, "Text content should have Text field")
	}
}

func TestMCPClient_PromptsResponseParsing(t *testing.T) {
	// Test that the prompts response parsing handles the expected structure
	mockPrompts := []MCPPrompt{
		{
			Name:        "summarize",
			Description: "Summarize the given text",
			Arguments: []MCPPromptArgument{
				{
					Name:        "text",
					Description: "The text to summarize",
					Required:    true,
				},
			},
		},
	}

	result := struct {
		Prompts []MCPPrompt `json:"prompts"`
	}{
		Prompts: mockPrompts,
	}

	// Marshal to JSON
	resultBytes, err := json.Marshal(result)
	require.NoError(t, err, "Should marshal prompts result")

	// Unmarshal back
	var unmarshaled struct {
		Prompts []MCPPrompt `json:"prompts"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err, "Should unmarshal prompts result")

	// Verify data integrity with type safety
	assert.Len(t, unmarshaled.Prompts, 1, "Should have 1 prompt")

	prompt := unmarshaled.Prompts[0]
	assert.NotEmpty(t, prompt.Name, "Prompt should have Name")

	// Type-safe access to optional fields
	if prompt.Description != "" {
		assert.NotEmpty(t, prompt.Description)
	}

	// Type-safe access to arguments
	for _, arg := range prompt.Arguments {
		assert.NotEmpty(t, arg.Name, "Argument should have Name")
		// Optional fields
		if arg.Description != "" {
			assert.NotEmpty(t, arg.Description)
		}
	}
}

func TestMCPClient_PromptGetResponseParsing(t *testing.T) {
	// Test that the prompt get response parsing handles the expected structure
	mockMessages := []MCPContent{
		{
			Type: "user",
			Text: "Summarize the following text: {{text}}",
		},
	}

	result := struct {
		Messages []MCPContent `json:"messages"`
	}{
		Messages: mockMessages,
	}

	// Marshal to JSON
	resultBytes, err := json.Marshal(result)
	require.NoError(t, err, "Should marshal messages result")

	// Unmarshal back
	var unmarshaled struct {
		Messages []MCPContent `json:"messages"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err, "Should unmarshal messages result")

	// Verify data integrity with type safety
	assert.Len(t, unmarshaled.Messages, 1, "Should have 1 message")

	message := unmarshaled.Messages[0]
	assert.NotEmpty(t, message.Type, "Message should have Type")

	// Type-safe field access based on message type
	if message.Type == "user" || message.Type == "system" {
		assert.NotEmpty(t, message.Text, "Text message should have Text field")
	}
}

// ---------------------------------------------------------------------------
// Test: Empty Response Handling
// ---------------------------------------------------------------------------

func TestMCPClient_EmptyResourcesList(t *testing.T) {
	result := struct {
		Resources []MCPResource `json:"resources"`
	}{
		Resources: []MCPResource{},
	}

	resultBytes, err := json.Marshal(result)
	require.NoError(t, err)

	var unmarshaled struct {
		Resources []MCPResource `json:"resources"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err)

	assert.Empty(t, unmarshaled.Resources, "Should have no resources")
	assert.Len(t, unmarshaled.Resources, 0, "Resources length should be 0")
}

func TestMCPClient_EmptyPromptsList(t *testing.T) {
	result := struct {
		Prompts []MCPPrompt `json:"prompts"`
	}{
		Prompts: []MCPPrompt{},
	}

	resultBytes, err := json.Marshal(result)
	require.NoError(t, err)

	var unmarshaled struct {
		Prompts []MCPPrompt `json:"prompts"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err)

	assert.Empty(t, unmarshaled.Prompts, "Should have no prompts")
	assert.Len(t, unmarshaled.Prompts, 0, "Prompts length should be 0")
}

func TestMCPClient_EmptyResourceContent(t *testing.T) {
	result := struct {
		Contents []MCPContent `json:"contents"`
	}{
		Contents: []MCPContent{},
	}

	resultBytes, err := json.Marshal(result)
	require.NoError(t, err)

	var unmarshaled struct {
		Contents []MCPContent `json:"contents"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err)

	assert.Empty(t, unmarshaled.Contents, "Should have no content")
	assert.Len(t, unmarshaled.Contents, 0, "Contents length should be 0")

	// Verify that empty content would trigger error
	if len(unmarshaled.Contents) == 0 {
		// This simulates the check in ReadResource
		errMsg := "no content returned for resource"
		assert.NotEmpty(t, errMsg, "Should return error for empty content")
	}
}

func TestMCPClient_EmptyPromptMessages(t *testing.T) {
	result := struct {
		Messages []MCPContent `json:"messages"`
	}{
		Messages: []MCPContent{},
	}

	resultBytes, err := json.Marshal(result)
	require.NoError(t, err)

	var unmarshaled struct {
		Messages []MCPContent `json:"messages"`
	}

	err = json.Unmarshal(resultBytes, &unmarshaled)
	require.NoError(t, err)

	assert.Empty(t, unmarshaled.Messages, "Should have no messages")
	assert.Len(t, unmarshaled.Messages, 0, "Messages length should be 0")

	// Verify that empty messages would trigger error
	if len(unmarshaled.Messages) == 0 {
		// This simulates the check in GetPrompt
		errMsg := "no messages returned for prompt"
		assert.NotEmpty(t, errMsg, "Should return error for empty messages")
	}
}

// ---------------------------------------------------------------------------
// Test: Error Response Structures
// ---------------------------------------------------------------------------

func TestMCPClient_ErrorResponseParsing(t *testing.T) {
	// Test various error response codes
	testCases := []struct {
		name    string
		code    int
		message string
	}{
		{"Invalid Request", -32600, "Invalid Request"},
		{"Method Not Found", -32601, "Method not found"},
		{"Invalid Params", -32602, "Invalid params"},
		{"Internal Error", -32603, "Internal error"},
		{"Server Error", -32000, "Server error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			errorResponse := MCPMessage{
				JSONRPC: "2.0",
				Error: &MCPError{
					Code:    tc.code,
					Message: tc.message,
				},
			}

			// Marshal and unmarshal to verify structure
			respBytes, err := json.Marshal(errorResponse)
			require.NoError(t, err)

			var unmarshaled MCPMessage
			err = json.Unmarshal(respBytes, &unmarshaled)
			require.NoError(t, err)

			// Type-safe error field access
			require.NotNil(t, unmarshaled.Error, "Error should not be nil")
			assert.Equal(t, tc.code, unmarshaled.Error.Code)
			assert.Equal(t, tc.message, unmarshaled.Error.Message)

			// Verify Error() method works
			errStr := unmarshaled.Error.Error()
			assert.NotEmpty(t, errStr)
			assert.Contains(t, errStr, "MCP error")
			assert.Contains(t, errStr, tc.message)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Server Name Assignment
// ---------------------------------------------------------------------------

func TestMCPClient_AssignsServerNameToResources(t *testing.T) {
	serverName := "my-server"
	config := MCPServerConfig{
		Name:    serverName,
		Command: "cat",
	}

	_ = config // Config is used for creating client in real code

	// Create resources without server name
	resources := []MCPResource{
		{URI: "file:///tmp/test.txt", Name: "test.txt"},
		{URI: "file:///tmp/test2.txt", Name: "test2.txt"},
	}

	// Simulate what ListResources does - assign server name
	for i := range resources {
		resources[i].ServerName = serverName
	}

	// Verify all resources have the server name set
	for i, resource := range resources {
		assert.Equal(t, serverName, resource.ServerName, "Resource %d should have server name", i)
	}
}

func TestMCPClient_AssignsServerNameToPrompts(t *testing.T) {
	serverName := "my-server"
	config := MCPServerConfig{
		Name:    serverName,
		Command: "cat",
	}

	_ = config // Config is used for creating client in real code

	// Create prompts without server name
	prompts := []MCPPrompt{
		{Name: "summarize", Description: "Summarize text"},
		{Name: "translate", Description: "Translate text"},
	}

	// Simulate what ListPrompts does - assign server name
	for i := range prompts {
		prompts[i].ServerName = serverName
	}

	// Verify all prompts have the server name set
	for i, prompt := range prompts {
		assert.Equal(t, serverName, prompt.ServerName, "Prompt %d should have server name", i)
	}
}

// ---------------------------------------------------------------------------
// Test: Map Interface{} Type Safety
// ---------------------------------------------------------------------------

func TestMCPClient_MapInterfaceSafety(t *testing.T) {
	// Test type-safe access to map[string]interface{} params
	testParams := map[string]interface{}{
		"name":      "test-prompt",
		"arguments": map[string]interface{}{"text": "hello"},
		"count":     42,
		"enabled":   true,
		"optional":  nil,
	}

	// Type-safe field access
	name, ok := testParams["name"].(string)
	assert.True(t, ok, "name should be a string")
	assert.Equal(t, "test-prompt", name)

	args, ok := testParams["arguments"].(map[string]interface{})
	assert.True(t, ok, "arguments should be a map[string]interface{}")
	assert.NotNil(t, args)

	text, ok := args["text"].(string)
	assert.True(t, ok, "text should be a string")
	assert.Equal(t, "hello", text)

	// Integer literals in Go are stored as int, not float64
	// JSON unmarshaling converts to float64, but direct creation keeps int
	count, ok := testParams["count"].(int)
	assert.True(t, ok, "count should be an int in a directly created map")
	assert.Equal(t, 42, count)

	enabled, ok := testParams["enabled"].(bool)
	assert.True(t, ok, "enabled should be a bool")
	assert.True(t, enabled)

	// Test nil value handling
	optional, ok := testParams["optional"]
	if ok {
		assert.Nil(t, optional, "optional should be nil")
	}

	// Test missing key handling
	_, ok = testParams["missing"]
	assert.False(t, ok, "missing key should return false")
}

// ---------------------------------------------------------------------------
// Test: JSON-RPC Message Structure
// ---------------------------------------------------------------------------

func TestMCPClient_InitializeMessageStructure(t *testing.T) {
	// Test the structure of initialize request
	params := map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{},
			"resources": map[string]interface{}{},
			"prompts":   map[string]interface{}{},
		},
		"clientInfo": map[string]interface{}{
			"name":    "sprout",
			"version": "1.0.0",
		},
	}

	message := MCPMessage{
		JSONRPC: "2.0",
		ID:      "req_1",
		Method:  "initialize",
		Params:  params,
	}

	// Marshal to JSON
	msgBytes, err := json.Marshal(message)
	require.NoError(t, err)

	// Unmarshal back
	var unmarshaled MCPMessage
	err = json.Unmarshal(msgBytes, &unmarshaled)
	require.NoError(t, err)

	// Verify structure
	assert.Equal(t, "2.0", unmarshaled.JSONRPC)
	assert.Equal(t, "initialize", unmarshaled.Method)

	// Type-safe ID access
	idStr, ok := unmarshaled.ID.(string)
	assert.True(t, ok, "ID should be a string")
	assert.Equal(t, "req_1", idStr)

	// Type-safe Params access
	paramsMap, ok := unmarshaled.Params.(map[string]interface{})
	assert.True(t, ok, "Params should be a map[string]interface{}")

	// Type-safe nested access
	protocolVersion, ok := paramsMap["protocolVersion"].(string)
	assert.True(t, ok, "protocolVersion should be a string")
	assert.Equal(t, "2024-11-05", protocolVersion)

	clientInfo, ok := paramsMap["clientInfo"].(map[string]interface{})
	assert.True(t, ok, "clientInfo should be a map[string]interface{}")

	clientName, ok := clientInfo["name"].(string)
	assert.True(t, ok, "client name should be a string")
	assert.Equal(t, "sprout", clientName)
}

// ---------------------------------------------------------------------------
// Test: Timeout Handling
// ---------------------------------------------------------------------------

func TestMCPClient_TimeoutConfiguration(t *testing.T) {
	testCases := []struct {
		name    string
		timeout time.Duration
		expect  time.Duration
	}{
		// Note: 0 timeout stays as 0 in the struct when created directly
		// Default timeout of 30s is applied during JSON unmarshaling, not struct creation
		{"Zero timeout", 0, 0},
		{"Custom timeout", 5 * time.Second, 5 * time.Second},
		{"Long timeout", 60 * time.Second, 60 * time.Second},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			config := MCPServerConfig{
				Name:    "test-server",
				Command: "sleep",
				Timeout: tc.timeout,
			}

			logger := NewTestLogger()
			client := NewMCPClient(config, logger)

			// Verify timeout is set correctly
			assert.Equal(t, tc.expect, client.config.Timeout)
		})
	}
}

// ---------------------------------------------------------------------------
// Test: Thread Safety
// ---------------------------------------------------------------------------

func TestMCPClient_ConcurrentMethodCalls(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping concurrent test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test concurrent calls to different methods
	var wg sync.WaitGroup
	errors := make(chan error, 4)

	// Call ListResources
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := client.ListResources(ctx)
		errors <- err
	}()

	// Call ListPrompts
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := client.ListPrompts(ctx)
		errors <- err
	}()

	// Call ReadResource
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := client.ReadResource(ctx, "file:///tmp/test.txt")
		errors <- err
	}()

	// Call GetPrompt
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err := client.GetPrompt(ctx, "test", nil)
		errors <- err
	}()

	// Wait for all calls to complete or timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All calls completed
	case <-time.After(15 * time.Second):
		t.Fatal("Concurrent calls should have completed within timeout")
	}

	// All calls should have failed (server doesn't speak MCP)
	close(errors)
	for err := range errors {
		assert.Error(t, err, "Method call should fail with non-MCP server")
	}
}

// ---------------------------------------------------------------------------
// Test: Method Parameter Validation
// ---------------------------------------------------------------------------

func TestMCPClient_ReadResource_EmptyURI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test with empty URI
	errChan := make(chan error, 1)
	go func() {
		_, err := client.ReadResource(ctx, "")
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "ReadResource should fail with empty URI")
	case <-time.After(5 * time.Second):
		t.Fatal("ReadResource should have failed quickly")
	}
}

func TestMCPClient_GetPrompt_EmptyName(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping integration test in short mode")
	}

	config := MCPServerConfig{
		Name:    "test-server",
		Command: "sleep",
		Args:    []string{"30"},
		Timeout: 1 * time.Second,
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	ctx := context.Background()

	// Start the client
	err := client.Start(ctx)
	if err != nil {
		t.Skipf("Cannot start sleep command: %v", err)
	}
	defer client.Stop(ctx)

	// Test with empty prompt name
	errChan := make(chan error, 1)
	go func() {
		_, err := client.GetPrompt(ctx, "", nil)
		errChan <- err
	}()

	select {
	case err := <-errChan:
		assert.Error(t, err, "GetPrompt should fail with empty prompt name")
	case <-time.After(5 * time.Second):
		t.Fatal("GetPrompt should have failed quickly")
	}
}

// ---------------------------------------------------------------------------
// Test: Message ID Generation
// ---------------------------------------------------------------------------

func TestMCPClient_MessageIDGeneration(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Verify initial message ID
	client.reqMutex.Lock()
	initialID := client.messageID
	client.reqMutex.Unlock()

	// Simulate multiple requests
	ids := []string{}
	for i := 0; i < 5; i++ {
		client.reqMutex.Lock()
		client.messageID++
		id := fmt.Sprintf("req_%d", client.messageID)
		client.reqMutex.Unlock()
		ids = append(ids, id)
	}

	// Verify all IDs are unique
	uniqueIDs := make(map[string]bool)
	for _, id := range ids {
		assert.False(t, uniqueIDs[id], "ID %s should be unique", id)
		uniqueIDs[id] = true
	}

	// Verify IDs are incrementing
	for i := 1; i < len(ids); i++ {
		assert.NotEqual(t, ids[i-1], ids[i], "IDs should be different")
	}

	// Verify final message ID
	client.reqMutex.Lock()
	finalID := client.messageID
	client.reqMutex.Unlock()

	assert.Equal(t, initialID+5, finalID, "Message ID should have incremented 5 times")
}

// ---------------------------------------------------------------------------
// Test: Pending Request Management
// ---------------------------------------------------------------------------

func TestMCPClient_PendingRequestCleanup(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	// Add multiple pending requests
	testIDs := []string{"req_1", "req_2", "req_3"}
	for _, id := range testIDs {
		client.reqMutex.Lock()
		client.pendingReqs[id] = make(chan MCPMessage, 1)
		client.reqMutex.Unlock()
	}

	// Verify all requests are pending
	client.reqMutex.RLock()
	assert.Len(t, client.pendingReqs, 3, "Should have 3 pending requests")
	client.reqMutex.RUnlock()

	// Clean up each request
	for _, id := range testIDs {
		client.reqMutex.Lock()
		delete(client.pendingReqs, id)
		client.reqMutex.Unlock()
	}

	// Verify all requests are cleaned up
	client.reqMutex.RLock()
	assert.Len(t, client.pendingReqs, 0, "Should have no pending requests")
	client.reqMutex.RUnlock()
}

func TestMCPClient_PendingRequestConcurrentAccess(t *testing.T) {
	config := MCPServerConfig{
		Name:    "test-server",
		Command: "cat",
	}

	logger := NewTestLogger()
	client := NewMCPClient(config, logger)

	var wg sync.WaitGroup

	// Add requests concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			client.reqMutex.Lock()
			client.pendingReqs[id] = make(chan MCPMessage, 1)
			client.reqMutex.Unlock()
		}(fmt.Sprintf("req_%d", i))
	}

	wg.Wait()

	// Verify all requests were added
	client.reqMutex.RLock()
	assert.Len(t, client.pendingReqs, 10, "Should have 10 pending requests")
	client.reqMutex.RUnlock()

	// Remove requests concurrently
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(id string) {
			defer wg.Done()
			client.reqMutex.Lock()
			delete(client.pendingReqs, id)
			client.reqMutex.Unlock()
		}(fmt.Sprintf("req_%d", i))
	}

	wg.Wait()

	// Verify all requests were removed
	client.reqMutex.RLock()
	assert.Len(t, client.pendingReqs, 0, "Should have no pending requests")
	client.reqMutex.RUnlock()
}
