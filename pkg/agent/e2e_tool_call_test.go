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
// Helper: assert tool message ordering
// ---------------------------------------------------------------------------

// assertMessageOrdering verifies that the messages in agent.messages follow
// the expected role sequence. For example, for a single tool call test:
//
//	user → assistant(tool_call) → tool → assistant(stop)
//
// For nested tool calls:
//
//	user → assistant(tc1) → tool → assistant(tc2) → tool → assistant(stop)
//
// The expectedRoles slice uses shorthand: "user", "assistant", "tool".
func assertMessageOrdering(t *testing.T, messages []api.Message, expectedRoles []string) {
	t.Helper()
	require.GreaterOrEqual(t, len(messages), len(expectedRoles),
		"expected at least %d messages, got %d", len(expectedRoles), len(messages))

	// Collect non-system message roles from the tail of the message list
	// (matching the conversation portion after ProcessQuery appends the user message)
	roles := make([]string, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != "system" {
			roles = append(roles, msg.Role)
		}
	}

	// Match from the end (most recent messages) since older messages may contain
	// previous turn context
	startIdx := len(roles) - len(expectedRoles)
	require.GreaterOrEqual(t, startIdx, 0,
		"not enough non-system messages to match expected pattern: got %d roles %v, need %d %v",
		len(roles), roles, len(expectedRoles), expectedRoles)

	actual := roles[startIdx:]
	assert.Equal(t, expectedRoles, actual,
		"message role ordering mismatch: got %v, want %v", actual, expectedRoles)
}

// findToolMessages returns all messages with role "tool" from the message list.
func findToolMessages(messages []api.Message) []api.Message {
	var result []api.Message
	for _, msg := range messages {
		if msg.Role == "tool" {
			result = append(result, msg)
		}
	}
	return result
}

// buildE2EAgentWithClient is like buildE2EAgent but also returns the scripted client
// for sent-message verification.
func buildE2EAgentWithClient(t *testing.T, maxIter int, responses ...*ScriptedResponse) (*Agent, *ConversationHandler, *ScriptedClient) {
	t.Helper()
	client := NewScriptedClient(responses...)
	agent := makeAgentWithScriptedClient(maxIter, client)
	ch := NewConversationHandler(agent)
	return agent, ch, client
}

// ---------------------------------------------------------------------------
// Test 1 – Tool call execution through ProcessQuery
// ---------------------------------------------------------------------------

// TestE2E_ToolCallExecution verifies the full tool call loop:
// 1. Model returns tool_call → tool executes → result appended
// 2. Model sees result and continues → stops when done
func TestE2E_ToolCallExecution(t *testing.T) {
	t.Parallel()

	// Create a real temp file so read_file succeeds
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.txt")
	fileContent := "Hello from test.txt\nLine 2: this is real content."
	require.NoError(t, os.WriteFile(tempFile, []byte(fileContent), 0o644), "failed to create temp file")

	// First response: model wants to use a tool (read_file)
	firstResp := NewScriptedResponseBuilder().
		Content("Let me read the file to check its contents.").
		ToolCall(api.ToolCall{
			ID:     "call_read_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tempFile)},
		}).
		Build()

	// Second response: model sees tool result and completes
	secondResp := stopResponse()

	agent, _, client := buildE2EAgentWithClient(t, 10, firstResp, secondResp)
	result, err := agent.ProcessQuery("What is in test.txt?")

	require.NoError(t, err, "ProcessQuery should succeed with tool call execution")
	assert.Equal(t, "Done.", result, "ProcessQuery should return Done after tool execution")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(), "Should complete successfully")
	// Expected 2 iterations: tool call + stop response
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (tool call + stop)")

	// Verify tool result contains actual file content
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected at least one tool result message")
	assert.Contains(t, toolMsgs[0].Content, "Hello from test.txt",
		"tool result should contain actual file content")
	assert.NotContains(t, toolMsgs[0].Content, "Error",
		"tool result should not contain error")

	// Verify message ordering: user → assistant(tool_call) → tool → assistant(stop)
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "assistant"})

	// ---- Verify the model actually SAW the tool result in its second call ----
	// This is the critical end-to-end verification: the tool result message must
	// appear in the messages sent to the model on the second iteration, proving
	// the agent correctly feeds tool results back into the conversation.
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 2, "expected at least 2 sent requests (initial + after tool result)")

	secondReqMsgs := sentReqs[1]

	// The second request should contain a tool message with the file content
	foundToolResultInSent := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Hello from test.txt") {
			foundToolResultInSent = true
			break
		}
	}
	assert.True(t, foundToolResultInSent,
		"second request to model should contain a tool message with the file content")

	// ---- Verify ToolCallId linking between assistant tool call and tool result ----
	// The assistant message with tool calls (from iteration 0's response) is appended
	// to the message history, so it appears in the second request sent to the model.
	// We already have secondReqMsgs = sentReqs[1] above.
	var assistantToolCallID string
	for _, msg := range secondReqMsgs {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			assistantToolCallID = msg.ToolCalls[0].ID
			break
		}
	}
	require.NotEmpty(t, assistantToolCallID,
		"second request should contain the assistant message from iteration 0 with a tool call ID")

	// Find the tool result message in the second request and verify ToolCallId matches
	var toolResultMsgInSent *api.Message
	for i := range secondReqMsgs {
		if secondReqMsgs[i].Role == "tool" {
			toolResultMsgInSent = &secondReqMsgs[i]
			break
		}
	}
	require.NotNil(t, toolResultMsgInSent, "second request should contain a tool result message")
	assert.Equal(t, assistantToolCallID, toolResultMsgInSent.ToolCallId,
		"tool result's ToolCallId should match the assistant's tool call ID")
}

// ---------------------------------------------------------------------------
// Test 2 – Multiple tool calls in sequence
// ---------------------------------------------------------------------------

// TestE2E_MultipleToolCalls verifies that multiple tool calls in a single
// response are executed, and the model continues after seeing all results.
func TestE2E_MultipleToolCalls(t *testing.T) {
	t.Parallel()

	// Create real temp files so both read_file calls succeed
	tempDir := t.TempDir()
	file1 := filepath.Join(tempDir, "file1.txt")
	file1Content := "Content of file 1: alpha Bravo"
	require.NoError(t, os.WriteFile(file1, []byte(file1Content), 0o644), "failed to create file1")
	file2 := filepath.Join(tempDir, "file2.txt")
	file2Content := "Content of file 2: Charlie Delta"
	require.NoError(t, os.WriteFile(file2, []byte(file2Content), 0o644), "failed to create file2")

	// First response: model makes two tool calls
	firstResp := NewScriptedResponseBuilder().
		Content("Let me check both files.").
		ToolCalls([]api.ToolCall{
			{
				ID:     "call_read_001",
				Type:   "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, file1)},
			},
			{
				ID:     "call_read_002",
				Type:   "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, file2)},
			},
		}).
		Build()

	// Second response: model sees both results and continues.
	// IMPORTANT: The trailing "..." in the content is intentional — it triggers
	// ResponseValidator.IsIncomplete() (via hasIncompletePatterns checking for
	// ellipsis suffix) which causes the loop to continue to a 3rd iteration
	// instead of accepting this response as complete.
	secondResp := NewScriptedResponseBuilder().
		Content("Now I have both file contents. Let me analyze them...").
		Build()

	// Third response: model completes
	thirdResp := stopResponse()

	agent, _, client := buildE2EAgentWithClient(t, 10, firstResp, secondResp, thirdResp)
	result, err := agent.ProcessQuery("Check file1.txt and file2.txt")

	require.NoError(t, err, "ProcessQuery should succeed with multiple tool calls")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Expected 3 iterations: firstResp (tool calls) → secondResp (continuation) → thirdResp (stop)
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 3, agent.GetCurrentIteration()+1, "expected 3 iterations (tool calls + continuation + stop)")

	// Verify tool results contain actual file content
	toolMsgs := findToolMessages(agent.messages)
	assert.GreaterOrEqual(t, len(toolMsgs), 2, "expected at least 2 tool result messages")
	assert.Contains(t, toolMsgs[0].Content, "alpha Bravo",
		"first tool result should contain file1 content")
	assert.Contains(t, toolMsgs[1].Content, "Charlie Delta",
		"second tool result should contain file2 content")

	// Verify message ordering:
	// user → assistant(tool_calls:2) → tool → tool → assistant(continuation) → assistant(stop)
	// Note: the two tool results appear for the two parallel tool calls
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "tool", "assistant", "assistant"})

	// ---- Verify the model SAW both tool results in the second request ----
	// When multiple tool calls execute in parallel, both tool results must
	// appear in the next request's message history.
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3, "expected 3 sent requests (3 iterations)")

	secondReqMsgs := sentReqs[1]

	// Verify BOTH tool results are present in the second request
	foundAlpha := false
	foundCharlie := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" {
			if strings.Contains(msg.Content, "alpha Bravo") {
				foundAlpha = true
			}
			if strings.Contains(msg.Content, "Charlie Delta") {
				foundCharlie = true
			}
		}
	}
	assert.True(t, foundAlpha,
		"second request should contain tool result from first read_file (alpha Bravo)")
	assert.True(t, foundCharlie,
		"second request should contain tool result from second read_file (Charlie Delta)")
}

// ---------------------------------------------------------------------------
// Test 3 – Tool call followed by continuation then stop
// ---------------------------------------------------------------------------

// TestE2E_ToolCallContinuationStop verifies that after a tool call executes,
// the model can continue working (multiple iterations) before finally stopping.
func TestE2E_ToolCallContinuationStop(t *testing.T) {
	t.Parallel()

	// Create a real temp file so read_file succeeds
	tempDir := t.TempDir()
	configFile := filepath.Join(tempDir, "config.yaml")
	configContent := "server:\n  port: 8080\n  host: localhost"
	require.NoError(t, os.WriteFile(configFile, []byte(configContent), 0o644), "failed to create config file")

	// First response: model makes a tool call
	firstResp := NewScriptedResponseBuilder().
		Content("Let me read the file first.").
		ToolCall(api.ToolCall{
			ID:     "call_read_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, configFile)},
		}).
		Build()

	// Second and third responses use keepGoingResponse() which returns content
	// "Still working..." — the trailing "..." triggers IsIncomplete() (via
	// hasIncompletePatterns) so the loop continues. Without "..." suffix,
	// ResponseValidator would accept the response as complete (no finish_reason
	// but content appears complete) and stop prematurely after 2 iterations
	// instead of continuing to the 4th iteration.
	secondResp := keepGoingResponse()

	// Third response: model continues analysis
	thirdResp := keepGoingResponse()

	// Fourth response: model completes
	fourthResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, firstResp, secondResp, thirdResp, fourthResp)
	result, err := agent.ProcessQuery("Read config.yaml and analyze it")

	require.NoError(t, err, "ProcessQuery should succeed with tool call and continuation")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Expected 4 iterations: tool call + 2 continuations + stop
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 4, agent.GetCurrentIteration()+1, "expected 4 iterations (tool + 2 continuations + stop)")

	// Verify tool result contains actual file content
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected at least one tool result message")
	assert.Contains(t, toolMsgs[0].Content, "port: 8080",
		"tool result should contain config content")

	// Verify message ordering:
	// user → assistant(tool_call) → tool → assistant(continuation) → assistant(continuation) → assistant(stop)
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "assistant", "assistant", "assistant"})
}

// ---------------------------------------------------------------------------
// Test 4 – Tool call with empty finish_reason (continue)
// ---------------------------------------------------------------------------

// TestE2E_ToolCallEmptyFinishReason verifies that tool calls with empty
// finish_reason (implicit continue) work correctly through the full loop.
func TestE2E_ToolCallEmptyFinishReason(t *testing.T) {
	t.Parallel()

	// Model returns tool call with empty finish_reason (standard behavior).
	// Using "echo hello" as a deterministic command that works everywhere.
	toolResp := NewScriptedResponseBuilder().
		Content("I need to check the logs.").
		ToolCall(api.ToolCall{
			ID:     "call_shell_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"echo hello"}`},
		}).
		Build()

	// After tool executes, model completes
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, toolResp, stopResp)
	result, err := agent.ProcessQuery("Check the application logs")

	require.NoError(t, err)
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Expected 2 iterations: tool + stop
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (tool + stop)")

	// Verify tool result contains expected output from echo hello
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected tool result message")
	assert.Contains(t, toolMsgs[0].Content, "hello",
		"shell tool result should contain 'hello' from echo command")

	// Verify message ordering: user → assistant(tool_call) → tool → assistant(stop)
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "assistant"})
}

// ---------------------------------------------------------------------------
// Test 5 – Nested tool calls (tool call after seeing tool result)
// ---------------------------------------------------------------------------

// TestE2E_NestedToolCalls verifies that a model can make a tool call,
// see the result, then make another tool call before stopping.
func TestE2E_NestedToolCalls(t *testing.T) {
	t.Parallel()

	// Create a real temp file for read_file
	tempDir := t.TempDir()
	mainFile := filepath.Join(tempDir, "main.go")
	mainContent := "package main\n\nfunc main() {\n\tfmt.Println(\"nested test\")\n}"
	require.NoError(t, os.WriteFile(mainFile, []byte(mainContent), 0o644), "failed to create main.go")

	// First: model calls read_file
	firstResp := NewScriptedResponseBuilder().
		Content("Reading the main file.").
		ToolCall(api.ToolCall{
			ID:     "call_read_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, mainFile)},
		}).
		Build()

	// Second: model sees result and calls shell_command.
	// Using "echo hello" instead of "go build" for deterministic cross-platform behavior.
	secondResp := NewScriptedResponseBuilder().
		Content("Now let me run a command to verify.").
		ToolCall(api.ToolCall{
			ID:     "call_shell_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "shell_command", Arguments: `{"command":"echo hello"}`},
		}).
		Build()

	// Third: model completes after seeing shell result
	thirdResp := stopResponse()

	agent, _, client := buildE2EAgentWithClient(t, 10, firstResp, secondResp, thirdResp)
	result, err := agent.ProcessQuery("Read main.go and build it")

	require.NoError(t, err)
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Expected 3 iterations: read_file → shell_command → stop
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 3, agent.GetCurrentIteration()+1, "expected 3 iterations (2 tool calls + stop)")

	// Verify tool results: first is read_file, second is shell_command
	toolMsgs := findToolMessages(agent.messages)
	assert.GreaterOrEqual(t, len(toolMsgs), 2, "expected at least 2 tool result messages")

	// The first tool result should contain file content
	assert.Contains(t, toolMsgs[0].Content, "nested test",
		"first tool result (read_file) should contain file content")

	// The second tool result should contain echo output
	assert.Contains(t, toolMsgs[1].Content, "hello",
		"second tool result (shell_command) should contain echo output")

	// Verify nested message ordering:
	// user → assistant(tc1) → tool → assistant(tc2) → tool → assistant(stop)
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "assistant", "tool", "assistant"})

	// ---- Verify the model SAW each tool result in the subsequent request ----
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3, "expected 3 sent requests (3 iterations)")

	// The 2nd request (index 1) should contain the read_file tool result
	secondReqMsgs := sentReqs[1]
	foundReadResult2 := false
	for _, msg := range secondReqMsgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "nested test") {
			foundReadResult2 = true
			break
		}
	}
	assert.True(t, foundReadResult2,
		"second request should contain the read_file tool result with file content")

	// The 3rd request (index 2) should contain the shell_command tool result
	thirdReqMsgs := sentReqs[2]
	foundShellResult3 := false
	for _, msg := range thirdReqMsgs {
		if msg.Role == "tool" && strings.Contains(msg.Content, "hello") {
			foundShellResult3 = true
			break
		}
	}
	assert.True(t, foundShellResult3,
		"third request should contain the shell_command tool result with echo output")
}

// ---------------------------------------------------------------------------
// Test 6 – Tool call with tool_choice forced
// ---------------------------------------------------------------------------

// TestE2E_ToolCallWithToolChoice verifies that when the model is forced to
// use a tool (via tool_choice), the tool executes and the conversation
// continues normally.
func TestE2E_ToolCallWithToolChoice(t *testing.T) {
	t.Parallel()

	// Model returns tool call (simulating forced tool_choice).
	// search_files searching for "TODO" in *.go is fine — it will work and
	// return results (possibly empty if no TODOs found in the test directory).
	toolResp := NewScriptedResponseBuilder().
		Content("I will search for the relevant files.").
		ToolCall(api.ToolCall{
			ID:     "call_search_001",
			Type:   "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "search_files", Arguments: `{"search_pattern":"TODO","file_glob":"*.go"}`},
		}).
		Build()

	// Model sees results and completes
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, toolResp, stopResp)
	result, err := agent.ProcessQuery("Find all TODO comments in Go files")

	require.NoError(t, err)
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Expected 2 iterations: search + stop
	// Note: GetCurrentIteration() is 0-indexed, so we add 1 to get the human-readable count
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (search + stop)")

	// Verify tool result message exists with non-empty content
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected tool result message for search_files")
	assert.NotEmpty(t, toolMsgs[0].Content,
		"search_files result should have non-empty content")

	// Verify message ordering: user → assistant(tool_call) → tool → assistant(stop)
	assertMessageOrdering(t, agent.messages, []string{"user", "assistant", "tool", "assistant"})
}
