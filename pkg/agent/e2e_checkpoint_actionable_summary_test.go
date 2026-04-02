package agent

import (
	"fmt"
	"strings"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_CheckpointActionableSummaryRoundTrip verifies the full round-trip:
// ProcessQuery completes → async checkpoint records actionable summary → next
// ProcessQuery triggers compaction → actionable summary with "User request: ..."
// is injected into messages the model receives.
func TestE2E_CheckpointActionableSummaryRoundTrip(t *testing.T) {
	t.Parallel()

	// --- Build agent with scripted client and optimizer ---
	// Use keepGoing iterations so ProcessQuery #1 produces enough messages for the
	// checkpoint to cover a meaningful range. 3 keepGoing responses + 1 stop = 4
	// assistant messages, so checkpoint covers [user, assistant, assistant,
	// assistant, assistant] (5 messages → 1 summary = net −4, which easily
	// survives the +2 overhead from ProcessQuery #2).
	client := NewScriptedClient(
		keepGoingResponse(),
		keepGoingResponse(),
		stopResponse(),
	)
	agent := makeAgentWithScriptedClient(10, client)
	agent.optimizer = NewConversationOptimizer(true, false)

	// --- ProcessQuery #1: complete a task ---
	firstQuery := "Fix the authentication bug in the user service and update the login handler"
	_, err := agent.ProcessQuery(firstQuery)
	require.NoError(t, err, "first ProcessQuery should succeed")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// --- Wait for async checkpoint to be recorded ---
	waitForCheckpoints(t, agent, 2*time.Second)
	require.True(t, agent.HasTurnCheckpoints(), "expected checkpoint to be recorded after first ProcessQuery")

	// --- Verify checkpoint has both Summary and ActionableSummary ---
	cps := agent.copyTurnCheckpoints()
	require.Equal(t, 1, len(cps), "expected exactly 1 checkpoint")
	assert.NotEmpty(t, cps[0].Summary, "checkpoint summary should not be empty")
	assert.NotEmpty(t, cps[0].ActionableSummary, "checkpoint actionable summary should not be empty")

	// Store the actionable summary for later verification
	actionableSummary := cps[0].ActionableSummary

	// The actionable summary should contain the "User request:" header from the first query
	assert.Contains(t, actionableSummary, "User request:",
		"actionable summary should contain 'User request:'")
	assert.Contains(t, actionableSummary, firstQuery,
		"actionable summary should contain the first query text")

	// --- Pad agent.messages to exceed the compaction threshold ---
	// Add synthetic messages with substantive content to push token count over threshold
	for i := 0; i < 25; i++ {
		agent.messages = append(agent.messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Continue analysis step %d with detailed investigation of the authentication module, checking all edge cases and error conditions thoroughly", i),
		})
		agent.messages = append(agent.messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf("Completed analysis step %d, reviewed the authentication flow including token validation, session management, and middleware integration across the user service", i),
		})
	}
	originalCount := len(agent.messages)

	// --- Find a maxContextTokens that triggers the 87% compaction threshold ---
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// Verify our assumption: OptimizeConversation must not change message count
	// for this test's messages (no redundant file reads / shell commands).
	optimized := agent.optimizer.OptimizeConversation(agent.messages)
	assert.Equal(t, len(agent.messages), len(optimized),
		"findCompactionMaxContext assumption violated: OptimizeConversation modified message count")

	// --- Prepare for ProcessQuery #2: set fresh responses ---
	client.SetResponses([]*ScriptedResponse{stopResponse()})
	client.ClearSentRequests()

	// --- ProcessQuery #2: this triggers compaction in prepareMessages ---
	_, err = agent.ProcessQuery("Now add unit tests for the authentication fix")
	require.NoError(t, err, "second ProcessQuery should succeed")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// --- Verify compaction occurred ---
	// The compacted messages should have fewer messages than we had before ProcessQuery #2
	// (the checkpoint range was replaced by a single summary message)
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be reduced after checkpoint compaction: got %d, want < %d",
		len(agent.messages), originalCount)
	t.Logf("Checkpoint actionable summary round-trip: messages before PQ#2=%d, after PQ#2=%d",
		originalCount, len(agent.messages))

	// --- Verify the model received the actionable summary in its context ---
	sentRequests := client.GetSentRequests()
	require.GreaterOrEqual(t, len(sentRequests), 1,
		"expected at least 1 sent request during the second ProcessQuery")

	// Search through all sent requests to find the actionable summary text.
	// The checkpoint compaction replaces the checkpointed message range with a
	// single assistant-role summary message containing ActionableSummary + Summary.
	// We verify the actionable summary appears specifically in an assistant message
	// (the compaction summary), not just anywhere in the request.
	var foundActionableSummary bool
	var foundOrderingValid bool
	for _, req := range sentRequests {
		for _, msg := range req {
			if msg.Role == "assistant" && strings.Contains(msg.Content, actionableSummary) {
				foundActionableSummary = true
				// Verify that the actionable summary appears before the base
				// compaction summary (ordering enforced by BuildCheckpointCompactedMessages).
				actionableIdx := strings.Index(msg.Content, "User request:")
				compactedIdx := strings.Index(msg.Content, "Compacted earlier conversation state:")
				if actionableIdx >= 0 && compactedIdx >= 0 && actionableIdx < compactedIdx {
					foundOrderingValid = true
				}
				break
			}
		}
		if foundActionableSummary {
			break
		}
	}
	assert.True(t, foundActionableSummary,
		"expected the actionable summary to appear in an assistant message sent to the model during ProcessQuery #2")
	assert.True(t, foundOrderingValid,
		"expected actionable summary (User request:) to appear before base compaction summary (Compacted earlier conversation state:)")
}
