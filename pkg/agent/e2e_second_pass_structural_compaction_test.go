package agent

import (
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_SecondPassStructuralCompaction verifies the two-pass compaction
// path in prepareMessages: checkpoint compaction fires first (replacing a
// buffer zone with a summary), but the token count is still over the
// compaction threshold, so LLM structural compaction runs as a second-pass
// fallback.
//
// This specifically exercises:
//   - First pass:  BuildCheckpointCompactedMessages (agent.HasTurnCheckpoints() → true)
//   - Second pass: optimizer.CompactConversation  (tokens still over threshold)
//   - Side-effect: ch.agent.clearTurnCheckpoints() is called after second pass
//
// Contrast with TestE2E_LLMCompactionSummaryViaPrepareMessages which tests LLM
// compaction WITHOUT checkpoints (no first pass at all).
func TestE2E_SecondPassStructuralCompaction(t *testing.T) {
	t.Parallel()

	// ---------------------------------------------------------------------------
	// 1. Build the LLM compaction client that returns a recognisable marker
	// ---------------------------------------------------------------------------
	const llmSummaryText = "LLM_SECOND_PASS_SUMMARY: Further condensed the conversation."
	compactionClient := NewScriptedClient(
		NewScriptedResponseBuilder().
			Content(llmSummaryText).
			FinishReason("stop").
			Build(),
	)

	// mainClient is the agent's primary client (unused by this test).
	mainClient := NewScriptedClient(stopResponse())

	// ---------------------------------------------------------------------------
	// 2. Wire up agent with optimizer + LLM client
	// ---------------------------------------------------------------------------
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)
	agent.optimizer.SetLLMClient(compactionClient, "test-llm", nil)

	// ---------------------------------------------------------------------------
	// 3. Build message layout (34 messages)
	// ---------------------------------------------------------------------------
	//   Buffer zone  (0..5):   6 messages → replaced by checkpoint compaction
	//   Middle zone  (6..21): 16 messages (8 user/assistant pairs, long content)
	//   Recent tail  (22..33):12 messages → preserved by structural compaction
	//
	// After checkpoint compaction (first pass):
	//   [summary(1)] + [middle(16)] + [recent(12)] = 29 messages
	//
	// After LLM structural compaction (second pass):
	//   [anchor(3)] + [summary(1)] + [recent(12)] ≤ 16 messages
	//
	messages := make([]api.Message, 0, 34)

	// -- Buffer zone (6 messages, indices 0-5) --------------------------------
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Start task: refactor authentication module.",
	})
	for i := 0; i < 2; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Working on auth refactoring step %d, examining code structure and patterns.", i),
		})
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("Continue with step %d.", i),
		})
	}
	// 6th buffer message (index 5)
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "Acknowledged. Starting the refactoring analysis now.",
	})

	// -- Middle zone (16 messages, indices 6-21): (user, assistant) × 8 --------
	// Very long content so the token count stays high after checkpoint compaction.
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"Continue analyzing component %d, check error handling, edge cases, test coverage, backward compatibility, and integration points for the authentication refactoring across all supported platforms and environments.", i),
		})
		messages = append(messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf(
				"Detailed analysis of authentication component %d: reviewed the token validation logic, session management handlers, middleware chain configuration, error handling patterns, input validation strategies, and security considerations for the refactored module with comprehensive edge case coverage and backward compatibility analysis across all supported API endpoints and client integrations.", i),
		})
	}

	// -- Recent tail (12 messages, indices 22-33) ------------------------------
	for i := 0; i < 6; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "What about error handling?",
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Fixed it.",
		})
	}

	require.Len(t, messages, 34, "expected exactly 34 test messages")

	agent.messages = messages
	originalCount := len(agent.messages)

	// ---------------------------------------------------------------------------
	// 4. Record a checkpoint covering the buffer zone (indices 0-5)
	//    This makes HasTurnCheckpoints() return true so the first-pass fires.
	// ---------------------------------------------------------------------------
	agent.RecordTurnCheckpoint(0, 5)
	require.True(t, agent.HasTurnCheckpoints(), "expected turn checkpoints to be recorded")

	// ---------------------------------------------------------------------------
	// 5. Find a maxContextTokens that triggers the 87 % compaction threshold
	// ---------------------------------------------------------------------------
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// ---------------------------------------------------------------------------
	// 6. Call prepareMessages — this should exercise BOTH compaction passes
	// ---------------------------------------------------------------------------
	assert.Greater(t, agent.maxContextTokens, 0,
		"expected findCompactionMaxContext to set maxContextTokens > 0")
	prepared := ch.prepareMessages(nil)

	t.Logf("Second-pass compaction results: original=%d, final=%d, checkpoints_cleared=%v",
		originalCount, len(agent.messages), !agent.HasTurnCheckpoints())

	// ---------------------------------------------------------------------------
	// Assertions
	// ---------------------------------------------------------------------------

	// (a) Checkpoint compaction fired AND the second-pass LLM compaction
	//     succeeded → checkpoints should be cleared (second pass calls
	//     ch.agent.clearTurnCheckpoints() when it applies the compaction).
	assert.False(t, agent.HasTurnCheckpoints(),
		"expected turn checkpoints to be cleared after second-pass LLM structural compaction")

	// (b) agent.messages should be updated to the LLM-compacted version.
	//     Using assert.Less (not exact count) to avoid brittleness if config
	//     thresholds change; the exact count derivation is in the comments above.
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be reduced after both compaction passes: got %d, want < %d",
		len(agent.messages), originalCount)

	// (c) The LLM compaction client should have been called exactly once
	//     (for the second-pass structural compaction summary).
	sentRequests := compactionClient.GetSentRequests()
	assert.Equal(t, 1, len(sentRequests),
		"expected exactly 1 LLM summary request for second-pass compaction, got %d. "+
			"If 0, the post-checkpoint token count may have dropped below the compaction "+
			"threshold — check whether message content is long enough or maxContextTokens "+
			"(%d) is too small. If >1, the first-pass checkpoint may not have fired.",
		len(sentRequests), agent.maxContextTokens)

	// (d) The prepared messages should contain the LLM-generated summary.
	var foundLLMSummary bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, llmSummaryText) {
			foundLLMSummary = true
			break
		}
	}
	assert.True(t, foundLLMSummary,
		"expected prepared messages to contain the LLM second-pass summary text %q", llmSummaryText)

	// (e) Both checkpoint and LLM summaries share the standard compaction
	//     header. This is a sanity check; assertions (d) and (g) prove both
	//     passes ran.
	var foundHeader bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundHeader = true
			break
		}
	}
	assert.True(t, foundHeader,
		"expected compacted messages to contain the standard compaction header")

	// (f) Recent tail messages should be preserved.
	var foundRecentMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "What about error handling?") {
			foundRecentMsg = true
			break
		}
	}
	assert.True(t, foundRecentMsg,
		"expected recent tail messages to be preserved after both compaction passes")

	// (g) The checkpoint summary contains "User request:" drawn from the buffer
	//     zone, confirming that the first pass (BuildCheckpointCompactedMessages)
	//     actually executed. This distinguishes from the single-pass-only case
	//     tested in TestE2E_LLMCompactionSummaryViaPrepareMessages.
	var foundCheckpointSummaryRef bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "User request: Start task:") ||
			strings.Contains(msg.Content, "User request: Continue with step") {
			foundCheckpointSummaryRef = true
			break
		}
	}
	assert.True(t, foundCheckpointSummaryRef,
		"expected prepared messages to contain checkpoint summary content proving first pass ran")
}
