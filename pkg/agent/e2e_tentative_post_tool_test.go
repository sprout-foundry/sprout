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

// createTestFile is a test helper that creates a file in a temp dir and returns its path.
func createTestFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644), "failed to create test file %s", name)
	return path
}

// ---------------------------------------------------------------------------
// TestE2E_TentativePostToolRejection
//
// Verifies the tentative post-tool rejection flow through full ProcessQuery:
//
//  1. Model makes a tool call (read_file) → tool executes → result appended
//  2. Model returns finish_reason="stop" with tentative planning content
//     → followsRecentToolResults() = true (tool result is directly before)
//     → LooksLikeTentativePostToolResponse() = true (planning prefix, ≤ 40 words)
//     → tentativeRejectionCount incremented, transient rejection message enqueued
//     → returns false (continue)
//  3. Model returns finish_reason="stop" with a concrete, non-tentative answer
//     → followsRecentToolResults() = false (previous message is assistant, not tool)
//     → accepted normally via regular "stop" completion path
//  4. Conversation completes.
// ---------------------------------------------------------------------------

func TestE2E_TentativePostToolRejection(t *testing.T) {
	t.Parallel()

	tempFile := createTestFile(t, "data.txt", "Important data for analysis")

	// Response 1: Model makes a tool call to read the file
	toolCallResp := NewScriptedResponseBuilder().
		Content("Let me read the data file.").
		ToolCall(api.ToolCall{
			ID:   "call_read_001",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tempFile)},
		}).
		Build()

	// Response 2: First tentative post-tool response (will be rejected).
	// Starts with "Let me " (planning prefix), ≤ 40 words.
	// Must be > 10 words to pass the IsIncomplete check (isUnusuallyShort threshold)
	// and reach the tentative post-tool rejection logic in handleFinishReason.
	tentativeResp := NewScriptedResponseBuilder().
		Content("Let me check the file contents carefully and then analyze them thoroughly to provide insights.").
		FinishReason("stop").
		Build()

	// Response 3: Concrete answer (> 40 words, no planning prefix).
	// Accepted via regular stop path since followsRecentToolResults() = false
	// (previous message is assistant, not tool result).
	concreteResp := NewScriptedResponseBuilder().
		Content("Based on the file contents, here is my analysis: " +
			"The meeting notes document several important agenda items including " +
			"architecture discussion, pull request reviews, and sprint planning. " +
			"These items should be addressed in the upcoming engineering meeting.").
		FinishReason("stop").
		Build()

	agent, _, client := buildE2EAgentWithClient(t, 10, toolCallResp, tentativeResp, concreteResp)

	result, err := agent.ProcessQuery("Analyze the data file")
	require.NoError(t, err, "ProcessQuery should succeed after tentative rejection and recovery")

	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"should complete successfully after model provides concrete answer")

	// 3 iterations: tool call + tentative rejection + concrete answer
	assert.Equal(t, 3, agent.GetCurrentIteration()+1,
		"expected 3 iterations (tool call + tentative rejection + concrete answer)")

	// Verify the returned content is the concrete answer
	assert.Contains(t, result, "my analysis",
		"should return the concrete answer content")
	assert.Contains(t, result, "architecture discussion",
		"returned content should contain details from the concrete answer")

	// Verify message ordering in agent.messages:
	// user → assistant(tool_call) → tool → assistant(tentative) → assistant(concrete)
	// Note: the transient rejection message is injected by prepareMessages into the API
	// request but is NOT persisted to agent.messages — it is consumed and cleared.
	assertMessageOrdering(t, agent.messages,
		[]string{"user", "assistant", "tool", "assistant", "assistant"})

	// Verify the transient rejection message was sent to the model in the 3rd API request.
	// Transient messages are consumed by prepareMessages, so we verify via sent requests.
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3,
		"expected at least 3 sent requests (tool call + tentative + concrete)")

	var transientFoundInSent bool
	for _, msg := range sentReqs[2] {
		if msg.Role == "user" && strings.Contains(msg.Content, "You just received tool results") &&
			strings.Contains(msg.Content, "Do not stop with a planning note") {
			transientFoundInSent = true
			break
		}
	}
	assert.True(t, transientFoundInSent,
		"expected transient rejection message to be sent to the model in the 3rd API request")

	// Verify that the tool result contains actual file content
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected at least one tool result message")
	assert.Contains(t, toolMsgs[0].Content, "Important data for analysis",
		"tool result should contain actual file content")
}

// ---------------------------------------------------------------------------
// TestE2E_TentativePostToolRejectionThenConcreteAccepted
//
// Verifies that after a tentative rejection, a model that continues with
// another substantive (non-tentative) stop response completes correctly.
//
// This differs from TestE2E_TentativePostToolRejection in that the second
// response is also relatively short but does NOT match any tentative planning
// prefix — it's a definitive statement that gets accepted immediately.
//
//  1. Model tool call → tool executes → result appended
//  2. Model returns tentative stop → rejected (transient enqueued)
//  3. Model returns concrete stop (no planning prefix) → accepted
// ---------------------------------------------------------------------------

func TestE2E_TentativePostToolRejectionThenConcreteAccepted(t *testing.T) {
	t.Parallel()

	tempFile := createTestFile(t, "notes.txt", "Meeting notes: discuss architecture, review PRs, plan sprint")

	// Response 1: Model makes a tool call to read the file
	toolCallResp := NewScriptedResponseBuilder().
		Content("I need to read the notes file.").
		ToolCall(api.ToolCall{
			ID:   "call_read_001",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{Name: "read_file", Arguments: fmt.Sprintf(`{"file_path":"%s"}`, tempFile)},
		}).
		Build()

	// Response 2: Tentative post-tool response (will be rejected).
	tentativeResp := NewScriptedResponseBuilder().
		Content("Let me check the file contents carefully and then analyze them thoroughly to provide insights.").
		FinishReason("stop").
		Build()

	// Response 3: Concrete answer with a non-planning prefix (> 40 words).
	// Even if followsRecentToolResults() were true, LooksLikeTentativePostToolResponse
	// would return false because the content does not start with any planning prefix
	// and exceeds the 40-word threshold.
	concreteResp := NewScriptedResponseBuilder().
		Content("The file contains meeting notes covering three key topics: " +
			"architecture discussion, pull request reviews, and sprint planning. " +
			"Each topic should be allotted adequate time during the next " +
			"engineering sync to ensure thorough coverage and action item assignment.").
		FinishReason("stop").
		Build()

	agent, _, client := buildE2EAgentWithClient(t, 10, toolCallResp, tentativeResp, concreteResp)

	result, err := agent.ProcessQuery("What does the notes file say?")
	require.NoError(t, err, "ProcessQuery should succeed after tentative recovery")

	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"should complete successfully after model provides concrete answer")

	assert.Equal(t, 3, agent.GetCurrentIteration()+1,
		"expected 3 iterations (tool call + tentative rejection + concrete answer)")

	assert.Contains(t, result, "meeting notes",
		"should return the concrete answer content")

	// Verify message ordering: user → assistant(tool_call) → tool → assistant(tentative) → assistant(concrete)
	assertMessageOrdering(t, agent.messages,
		[]string{"user", "assistant", "tool", "assistant", "assistant"})

	// Verify transient rejection message was sent to the model
	sentReqs := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentReqs), 3,
		"expected at least 3 sent requests (tool call + tentative + concrete)")

	var transientFoundInSent bool
	for _, msg := range sentReqs[2] {
		if msg.Role == "user" && strings.Contains(msg.Content, "You just received tool results") {
			transientFoundInSent = true
			break
		}
	}
	assert.True(t, transientFoundInSent,
		"expected transient rejection message to be sent to the model in the 3rd API request")

	// Verify tool result
	toolMsgs := findToolMessages(agent.messages)
	require.NotEmpty(t, toolMsgs, "expected tool result message")
	assert.Contains(t, toolMsgs[0].Content, "Meeting notes",
		"tool result should contain the file content")
}
