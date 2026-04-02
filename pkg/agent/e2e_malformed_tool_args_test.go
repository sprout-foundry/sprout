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

// findAssistantByContent searches messages for the first assistant message matching
// the given content and returns its index and true, or 0 and false if not found.
func findAssistantByContent(messages []api.Message, content string) (int, bool) {
	for i, msg := range messages {
		if msg.Role == "assistant" && msg.Content == content {
			return i, true
		}
	}
	return 0, false
}

// TestE2E_MalformedToolArgsRejectionAndRecovery verifies the full malformed
// tool-arguments rejection-and-recovery flow through ProcessQuery:
//
//  1. Model returns a tool_call with malformed JSON arguments.
//  2. normalizeToolCallsForExecution detects the bad args, separates them into
//     a malformed list, sets choice.Message.ToolCalls = nil, and enqueues a
//     transient reminder telling the model to re-emit with valid JSON.
//  3. The conversation loop continues to the next iteration (the malformed
//     response does NOT execute any tool, and its empty finish_reason /
//     tool_calls finish_reason keeps the loop going).
//  4. On the next iteration the model re-emits the same tool call with VALID
//     JSON arguments.
//  5. The tool executes successfully and the model completes with a stop
//     response.
//
// See conversation_handler.go → processResponse → normalizeToolCallsForExecution
// for the malformed-detection logic, and tool_executor.go → parseToolArgumentsWithRepair
// for the JSON repair strategies that fail on truly broken input.
func TestE2E_MalformedToolArgsRejectionAndRecovery(t *testing.T) {
	t.Parallel()

	// Create a real temp file so read_file succeeds on the valid (recovery) attempt.
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test.txt")
	fileContent := "Hello from malformed args test"
	require.NoError(t, os.WriteFile(tempFile, []byte(fileContent), 0o644),
		"failed to create temp test file")

	// -----------------------------------------------------------------------
	// 1st scripted response: tool call with MALFORMED JSON arguments.
	//
	// We use an unclosed string value that can't be repaired:
	//   {"file_path": "/tmp/test
	// - json.Unmarshal fails (unclosed string, missing })
	// - stripMarkdownCodeFence → same
	// - extractFirstBalancedJSONObject → no balanced }, returns ""
	// - extractOuterJSONObject → no }, returns ""
	// - closeJSONDelimiters(raw) → adds } but string still unclosed → unmarshal fails
	// - removeJSONTrailingCommas / closeJSONDelimiters combos → same
	//
	// The content must be long enough (>10 words) and end with punctuation so
	// IsIncomplete() returns false, ensuring the code falls through to the
	// final `return ch.finalizeTurn(turn, false)` which simply continues the loop.
	//
	// FinishReason "tool_calls" signals a tool-call turn. handleFinishReason returns
	// (false, _) so execution falls through to the blank/incomplete checks and
	// eventually reaches `finalizeTurn(turn, false)`, continuing the loop.
	malformedArgs := `{"file_path": "/tmp/test` // unclosed string value

	// Programmatically verify that the repair logic truly cannot fix this input.
	// This catches regressions if parseToolArgumentsWithRepair is enhanced later.
	_, repaired, parseErr := parseToolArgumentsWithRepair(malformedArgs)
	require.Error(t, parseErr, "malformedArgs should fail to parse through all repair strategies")
	require.False(t, repaired, "malformedArgs should not be repairable")

	// NOTE: The content text for the malformed response is deliberately chosen to pass
	// ResponseValidator.IsIncomplete() and ValidateToolCalls() checks so the malformed-tool
	// rejection path is exercised cleanly. It must: (1) end with period punctuation, (2)
	// contain ≥10 words, (3) not end with "...", and (4) not match any patterns in
	// containsAttemptedToolCalls() (e.g. "Let me use the").
	// If those heuristic checks change, this content may need adjustment.
	malformedResp := NewScriptedResponseBuilder().
		Content("Let me read the test file for you to find the information you requested.").
		ToolCall(api.ToolCall{
			ID:   "call_read_malformed",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: malformedArgs,
			},
		}).
		FinishReason("tool_calls").
		Build()

	// -----------------------------------------------------------------------
	// 2nd scripted response: model re-emits the same tool with VALID args.
	//
	// On the previous iteration, the transient reminder
	// "Your previous tool call arguments were incomplete or invalid JSON …"
	// was enqueued and will be consumed by prepareMessages before this call.
	validArgs := fmt.Sprintf(`{"file_path":"%s"}`, tempFile)
	validResp := NewScriptedResponseBuilder().
		Content("I have corrected the arguments and will read the file now.").
		ToolCall(api.ToolCall{
			ID:   "call_read_valid",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: validArgs,
			},
		}).
		Build()

	// -----------------------------------------------------------------------
	// 3rd scripted response: model sees tool result and completes.
	thirdResp := stopResponse()

	// -----------------------------------------------------------------------
	// Build agent and run.
	agent, _, client := buildE2EAgentWithClient(t, 10, malformedResp, validResp, thirdResp)
	result, err := agent.ProcessQuery("Read the test file")

	require.NoError(t, err, "ProcessQuery should succeed after malformed-tool recovery")
	assert.Equal(t, "Done.", result, "final result should match stopResponse content")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"run should complete successfully")

	// Expected 3 iterations:
	//   iteration 0: malformed args rejected (no tool execution)
	//   iteration 1: valid args → tool executes → tool result appended
	//   iteration 2: stop response
	// GetCurrentIteration() is 0-indexed, so add 1 to get human-readable count.
	assert.Equal(t, 3, agent.GetCurrentIteration()+1,
		"expected 3 iterations (malformed reject + valid tool + stop)")

	// -----------------------------------------------------------------------
	// Verify exactly 1 tool result message exists (only the valid call executed).
	toolMsgs := findToolMessages(agent.messages)
	require.Equal(t, 1, len(toolMsgs),
		"expected exactly 1 tool result (only the valid call should execute)")
	assert.Contains(t, toolMsgs[0].Content, "Hello from malformed args test",
		"tool result should contain the actual file content")

	// -----------------------------------------------------------------------
	// Verify message ordering in agent.messages:
	//
	//   user → assistant(no tools, malformed rejected)
	//        → assistant(tool_call, valid)
	//        → tool(result)
	//        → assistant(stop)
	//
	// The transient reminder is consumed from the queue by prepareMessages and
	// appended to the *sent request* but NOT to agent.messages.
	assertMessageOrdering(t, agent.messages,
		[]string{"user", "assistant", "assistant", "tool", "assistant"})

	// Verify exact non-system message count to catch silent extra messages
	nonSystemCount := 0
	for _, msg := range agent.messages {
		if msg.Role != "system" {
			nonSystemCount++
		}
	}
	assert.Equal(t, 5, nonSystemCount,
		"expected exactly 5 non-system messages (user + 2 assistants + tool + assistant)")

	// -----------------------------------------------------------------------
	// Verify the malformed iteration's assistant message has NO tool calls
	// (they were set to nil by normalizeToolCallsForExecution).
	malformedAssistantIdx, found := findAssistantByContent(agent.messages,
		"Let me read the test file for you to find the information you requested.")
	require.True(t, found, "should find the assistant message from the malformed iteration")
	assert.Empty(t, agent.messages[malformedAssistantIdx].ToolCalls,
		"assistant message from malformed iteration should have nil/empty tool calls")

	// -----------------------------------------------------------------------
	// Verify that the valid iteration's assistant message HAS tool calls.
	validAssistantIdx, found := findAssistantByContent(agent.messages,
		"I have corrected the arguments and will read the file now.")
	require.True(t, found, "should find the assistant message from the valid iteration")
	require.NotEmpty(t, agent.messages[validAssistantIdx].ToolCalls,
		"assistant message from valid iteration should have tool calls")
	assert.Equal(t, "read_file", agent.messages[validAssistantIdx].ToolCalls[0].Function.Name,
		"valid iteration tool call should be read_file")

	// -----------------------------------------------------------------------
	// Verify sent requests contain the transient reminder and tool results.
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3,
		"expected at least 3 sent requests (initial + after malformed + after valid tool)")

	// The 2nd sent request (index 1) should contain the transient reminder about
	// incomplete or invalid JSON, prepended/appended by prepareMessages.
	foundTransient := false
	for _, msg := range sentReqs[1] {
		if msg.Role == "user" && strings.Contains(msg.Content, "incomplete or invalid JSON") {
			foundTransient = true
			break
		}
	}
	assert.True(t, foundTransient,
		"2nd sent request should contain the transient reminder about invalid JSON arguments")

	// The 3rd sent request (index 2) should contain the tool result with file content,
	// proving the valid tool call was executed and the result was fed back to the model.
	foundToolResultInSent := false
	for _, msg := range sentReqs[2] {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Hello from malformed args test") {
			foundToolResultInSent = true
			break
		}
	}
	assert.True(t, foundToolResultInSent,
		"3rd sent request should contain the tool result from the valid read_file call")

	// -----------------------------------------------------------------------
	// Verify ToolCallId linking between the valid assistant tool call and its
	// tool result message in the conversation history.
	validAssistantMsg := agent.messages[validAssistantIdx]
	require.NotEmpty(t, validAssistantMsg.ToolCalls, "valid assistant should have tool calls")
	validToolCallID := validAssistantMsg.ToolCalls[0].ID
	require.NotEmpty(t, validToolCallID, "valid tool call should have an ID")

	// Find the tool result that follows the valid assistant message
	foundLinkedResult := false
	for i := validAssistantIdx + 1; i < len(agent.messages); i++ {
		if agent.messages[i].Role == "tool" {
			assert.Equal(t, validToolCallID, agent.messages[i].ToolCallId,
				"tool result's ToolCallId should match the valid assistant's tool call ID")
			foundLinkedResult = true
			break
		}
	}
	assert.True(t, foundLinkedResult,
		"should find a tool result message after the valid assistant message")
}
