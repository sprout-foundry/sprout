// e2e_rate_limit_test.go – End-to-end rate limit handling tests
//
// This file validates the rate limit error path through the full
// ProcessQuery → APIClient → ErrorHandler pipeline, using
// rate limit error injection via the ScriptedClient.
//
// Rate limit behavior under test:
//   - ScriptedClient.RateLimitAfter triggers RateLimitExceededError after N requests
//   - RateLimitExceededError is returned by APIClient after maxRetries exceeded
//   - ErrorHandler.HandleAPIFailure detects RateLimitExceededError and returns a user-friendly message
//   - The conversation context is preserved (messages, file changes, etc.)
//   - ProcessQuery returns (userMessage, nil) instead of propagating the error

package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1 – Rate limit error → user-friendly message, conversation preserved
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_UserMessageAndPreservedContext verifies that when
// a rate limit error occurs, the user receives a helpful error message and the
// conversation context is preserved.
//
// Flow:
//   - Iteration 0: user query → SendWithRetry → normal success
//   - Iteration 1: SendWithRetry → rate limit error (after retries) → ErrorHandler.HandleAPIFailure
//   - ProcessQuery returns (userMessage, nil) with helpful rate limit guidance
//
// Queue: [stopResponse("Response 1"), response with RateLimitAfter=1]
// Expected: ProcessQuery succeeds (no Go error), returns user-friendly message
func TestE2E_RateLimitExceeded_UserMessageAndPreservedContext(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		// First response succeeds normally
		NewStopResponse("Response 1"),
		// Second response triggers rate limit error
		&ScriptedResponse{
			Content:        "This response will trigger rate limit",
			RateLimitAfter: 1, // Trigger rate limit after 1 request
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Tell me something")

	// ErrorHandler.HandleAPIFailure wraps RateLimitExceededError into a user message
	require.NoError(t, err, "ProcessQuery should succeed with rate limit error converted to user message")
	assert.NotEmpty(t, result, "expected a non-empty user-facing error message from HandleAPIFailure")

	// Verify the message contains rate limit guidance
	assert.Contains(t, result, "rate limit", "expected message to mention rate limit")
	assert.Contains(t, result, "preserved", "expected message to confirm context is preserved")

	// Note: termination reason is NOT set when ErrorHandler.HandleAPIFailure handles an error
	// The conversation ends via the error handler return path without a specific termination reason
}

// ---------------------------------------------------------------------------
// Test 2 – Rate limit after multiple successful iterations
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_AfterMultipleIterations verifies that rate limit
// errors can occur after several successful iterations and are handled correctly.
//
// Flow:
//   - Iteration 0-2: successful responses
//   - Iteration 3: rate limit error triggered
//
// Queue: [keepGoing, keepGoing, keepGoing, rate limit response]
// Expected: ProcessQuery succeeds, 4 iterations total, helpful error message
func TestE2E_RateLimitExceeded_AfterMultipleIterations(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		// Three successful iterations
		keepGoingResponse(),
		keepGoingResponse(),
		keepGoingResponse(),
		// Fourth iteration triggers rate limit
		&ScriptedResponse{
			Content:        "Continuing...",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Do a complex task")

	require.NoError(t, err, "ProcessQuery should succeed with rate limit error")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify we went through multiple iterations before hitting rate limit
	assert.Equal(t, 4, agent.GetCurrentIteration()+1,
		"expected 4 iterations (3 successful + 1 rate limit)")

	// Verify the error message is user-friendly
	assert.Contains(t, result, "rate limit", "expected message to mention rate limit")
	assert.Contains(t, result, "preserved", "expected message to confirm context preservation")
}

// ---------------------------------------------------------------------------
// Test 3 – Rate limit during tool call iteration
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_DuringToolCall verifies that a rate limit error
// during a tool call iteration is handled correctly and conversation context
// (including tool results) is preserved.
//
// Flow:
//   - Iteration 0: tool call → success → tool executes → result appended
//   - Iteration 1: SendWithRetry → rate limit error → error handler
//
// Queue: [tool_call_response, rate limit response]
// Expected: ProcessQuery succeeds, tool result in history, helpful error message
func TestE2E_RateLimitExceeded_DuringToolCall(t *testing.T) {
	t.Parallel()

	// Create a real temp file so read_file succeeds
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "config.txt")
	fileContent := "Setting1=value1\nSetting2=value2"
	require.NoError(t, os.WriteFile(tempFile, []byte(fileContent), 0o644), "failed to create temp file")

	toolCallResp := NewScriptedResponseBuilder().
		Content("Let me read the config file.").
		ToolCall(api.ToolCall{
			ID:     "call_read_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tempFile)},
		}).
		Build()

	responses := []*ScriptedResponse{
		// First iteration: tool call succeeds
		toolCallResp,
		// Second iteration: rate limit error
		&ScriptedResponse{
			Content:        "Processing the config...",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Read the config and analyze it")

	require.NoError(t, err, "ProcessQuery should succeed with rate limit error during tool iteration")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify tool result is in conversation history (context preserved)
	var foundToolResult bool
	for _, msg := range agent.messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Setting1=value1") {
			foundToolResult = true
			break
		}
	}
	assert.True(t, foundToolResult, "expected tool result to be preserved in conversation history")

	// Verify error message is user-friendly
	assert.Contains(t, result, "rate limit", "expected message to mention rate limit")
	assert.Contains(t, result, "preserved", "expected message to confirm context preservation")
}

// ---------------------------------------------------------------------------
// Test 4 – Rate limit error message includes provider information
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_MessageIncludesProviderInfo verifies that the
// rate limit error message includes provider information when available.
//
// Flow:
//   - Trigger rate limit error (ScriptedClient returns "test" as provider)
//   - Verify message includes provider name
//
// Queue: [rate limit response]
// Expected: Message includes provider name in the error guidance
func TestE2E_RateLimitExceeded_MessageIncludesProviderInfo(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		&ScriptedResponse{
			Content:        "Rate limited immediately",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)

	result, err := agent.ProcessQuery("Test provider info in rate limit message")

	require.NoError(t, err, "ProcessQuery should succeed")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// ScriptedClient.GetProvider() returns "test", verify it's included in message
	assert.Contains(t, result, "Test", "expected message to include provider name 'Test'")
}

// ---------------------------------------------------------------------------
// Test 5 – Rate limit after tool execution preserves file changes
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_FileChangesPreserved verifies that file changes
// made before hitting a rate limit are preserved and not lost.
//
// Flow:
//   - Iteration 0: tool call (write_file) → file created
//   - Iteration 1: rate limit error → conversation ends with helpful message
//   - Verify file still exists and contains correct content
//
// Queue: [tool_call_response(write_file), rate limit response]
// Expected: File exists with correct content, helpful error message
func TestE2E_RateLimitExceeded_FileChangesPreserved(t *testing.T) {
	t.Parallel()

	// Create a temp directory for the test
	tempDir := t.TempDir()
	testFile := filepath.Join(tempDir, "test_output.txt")
	testContent := "This file was created before rate limit hit"

	toolCallResp := NewScriptedResponseBuilder().
		Content("Creating a test file.").
		ToolCall(api.ToolCall{
			ID:     "call_write_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "write_file",
				Arguments: fmt.Sprintf(`{"path":"%s","content":"%s"}`, testFile, testContent),
			},
		}).
		Build()

	responses := []*ScriptedResponse{
		// First iteration: write file
		toolCallResp,
		// Second iteration: rate limit error
		&ScriptedResponse{
			Content:        "Continuing with more work...",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Create a test file")

	require.NoError(t, err, "ProcessQuery should succeed with rate limit error")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify file exists and has correct content
	fileContent, err := os.ReadFile(testFile)
	require.NoError(t, err, "expected file to exist after rate limit")
	assert.Equal(t, testContent, string(fileContent), "expected file content to be preserved")

	// Verify error message is user-friendly
	assert.Contains(t, result, "rate limit", "expected message to mention rate limit")
	assert.Contains(t, result, "preserved", "expected message to confirm context preservation")
}

// ---------------------------------------------------------------------------
// Test 6 – Multiple rate limit scenarios (RateLimitAfter > 1)
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_AfterThreshold verifies that RateLimitAfter works
// correctly when set to a value greater than 1.
//
// Flow:
//   - RateLimitAfter=3 means rate limit triggers on the 3rd request
//   - First 2 requests succeed, 3rd fails with RateLimitExceededError
//
// Queue: [response1, response2, response3 with RateLimitAfter=3]
// Expected: First 2 succeed, 3rd triggers rate limit
func TestE2E_RateLimitExceeded_AfterThreshold(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		keepGoingResponse(),   // Request 1: success
		keepGoingResponse(),   // Request 2: success
		keepGoingResponse(),   // Request 3: triggers rate limit (RateLimitAfter=3)
	}

	// Set RateLimitAfter on the third response
	responses[2].RateLimitAfter = 3

	agent, _, client := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Test rate limit threshold")

	require.NoError(t, err, "ProcessQuery should succeed")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify all 3 responses were consumed
	assert.Equal(t, 3, client.GetIndex(), "expected all 3 scripted responses to be consumed")

	// Note: The iteration count (5) is higher than the scripted response count (3)
	// because SendWithRetry internally retries rate limit errors with backoff,
	// and each retry attempt increments the iteration counter.
	assert.Equal(t, 5, agent.GetCurrentIteration()+1, "expected 5 iterations (3 responses + 2 retries)")

	// Verify error message
	assert.Contains(t, result, "rate limit", "expected message to mention rate limit")
}

// ---------------------------------------------------------------------------
// Test 7 – Rate limit error provides actionable next steps
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_ActionableNextSteps verifies that the rate limit
// error message provides actionable guidance for users.
//
// Flow:
//   - Trigger rate limit error
//   - Verify message includes actionable suggestions
//
// Queue: [rate limit response]
// Expected: Message includes: wait, switch provider, reduce scope
func TestE2E_RateLimitExceeded_ActionableNextSteps(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		&ScriptedResponse{
			Content:        "Rate limited",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Trigger rate limit")

	require.NoError(t, err, "ProcessQuery should succeed")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify message includes actionable guidance
	assert.Contains(t, result, "Wait", "expected message to suggest waiting")
	assert.Contains(t, result, "Switch", "expected message to suggest switching provider (capitalized)")
	assert.Contains(t, result, "provider", "expected message to mention provider")
}

// ---------------------------------------------------------------------------
// Test 8 – Rate limit error with existing conversation history
// ---------------------------------------------------------------------------

// TestE2E_RateLimitExceeded_WithConversationHistory verifies that when a rate
// limit error occurs with an existing conversation history, the history is
// preserved and the user can continue from where they left off.
//
// Flow:
//   - Pre-populate conversation history
//   - Trigger rate limit error
//   - Verify history is preserved
//
// Queue: [rate limit response]
// Expected: Original messages preserved, helpful error message
func TestE2E_RateLimitExceeded_WithConversationHistory(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		&ScriptedResponse{
			Content:        "Rate limited with history",
			RateLimitAfter: 1,
		},
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)

	// Pre-populate conversation history
	agent.messages = []api.Message{
		{Role: "user", Content: "First question"},
		{Role: "assistant", Content: "First answer"},
		{Role: "user", Content: "Second question"},
		{Role: "assistant", Content: "Second answer"},
	}

	initialMessageCount := len(agent.messages)

	// Trigger rate limit
	result, err := agent.ProcessQuery("This will trigger rate limit")

	require.NoError(t, err, "ProcessQuery should succeed")
	assert.NotEmpty(t, result, "expected non-empty error message")

	// Verify history is preserved (should have initial messages + new user message)
	assert.GreaterOrEqual(t, len(agent.messages), initialMessageCount+1,
		"expected conversation history to be preserved with new message added")

	// Verify original messages are still there
	var foundFirstMessage bool
	for _, msg := range agent.messages {
		if msg.Role == "user" && msg.Content == "First question" {
			foundFirstMessage = true
			break
		}
	}
	assert.True(t, foundFirstMessage, "expected original messages to be preserved in history")
}
