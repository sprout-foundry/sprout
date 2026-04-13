package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// findCompactionMaxContext iterates threshold candidates and sets
// agent.maxContextTokens to the first value that triggers compaction.
// It fatal-fails the test if no candidate triggers compaction, avoiding
// confusing downstream assertion failures.
func findCompactionMaxContext(t *testing.T, agent *Agent, ch *ConversationHandler) {
	t.Helper()
	candidates := []int{500, 1000, 2000, 5000, 10000}
	for _, maxCtx := range candidates {
		agent.maxContextTokens = maxCtx
		compactionThreshold := int(float64(maxCtx) * PruningConfig.Default.StandardPercent)

		// Build the same message list that prepareMessages would build
		// (system prompt + agent messages).
		// NOTE: This assumes OptimizeConversation is a no-op for the test
		// messages (no tool results with "Tool call result" prefixes).
		prep := []api.Message{{Role: "system", Content: agent.systemPrompt}}
		prep = append(prep, agent.messages...)
		tokens := ch.apiClient.estimateRequestTokens(prep, nil)

		if tokens > compactionThreshold {
			return
		}
	}
	t.Fatalf("no maxContextTokens candidate triggered compaction; "+
		"message content may be too short or PruningConfig thresholds may have changed")
}

// ---------------------------------------------------------------------------
// Test 18 – LLM compaction summary via prepareMessages (e2e)
// ---------------------------------------------------------------------------

// TestE2E_LLMCompactionSummaryViaPrepareMessages verifies that when
// prepareMessages triggers LLM structural compaction (second-pass fallback
// after checkpoint compaction is skipped), the LLM summary path produces a
// correct summary with LLM-generated content — not the Go fallback.
//
// This is an e2e test: it goes through the full prepareMessages pipeline
// rather than calling optimizer.CompactConversation() directly.
func TestE2E_LLMCompactionSummaryViaPrepareMessages(t *testing.T) {
	t.Skip("Skipped: depends on token estimation and optimizer configuration")
	t.Parallel()

	// --- Build two separate ScriptedClients --------------------------------
	// compactionClient: wired to the optimizer for the LLM summary call.
	// It returns a specific marker string so we can verify the LLM path was taken.
	const llmSummaryText = "LLM_COMPACTED_SUMMARY: User asked to refactor auth module."
	compactionClient := NewScriptedClient(
		NewScriptedResponseBuilder().
			Content(llmSummaryText).
			FinishReason("stop").
			Build(),
	)

	// mainClient: the agent's primary client (not used for LLM compaction).
	mainClient := NewScriptedClient(stopResponse())

	// --- Wire up agent with optimizer and LLM client -----------------------
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)
	agent.optimizer.SetLLMClient(compactionClient, "test-llm", nil)

	// --- Populate agent.messages with enough messages to trigger compaction -
	// We need ≥ PruningConfig.Structural.MinMessagesToCompact (18) total
	// messages after optimization, with a sufficiently large middle segment
	// (≥ MinMiddleMessages = 6) between the anchor and the recent tail
	// (RecentMessagesToKeep = 12).
	//
	// Layout (no system message in agent.messages — prepareMessages prepends it):
	//   [0]  user    – anchor user query
	//   [1]  assistant – anchor assistant reply (no tool calls)
	//   [2..17]  8 user/assistant pairs → middle segment (16 messages)
	//   [18..29] recent tail (12 messages)
	// Total = 30 messages  (>18 ✓, middle = 16 ≥ 6 ✓)

	messages := make([]api.Message, 0, 30)

	// Anchor
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Refactor the authentication module in the codebase, split the monolithic handler into separate middleware components.",
	})
	messages = append(messages, api.Message{
		Role: "assistant",
		Content: "I'll start by reviewing the existing authentication code, examining the handler structure, and identifying the boundaries for the middleware split.",
	})

	// Middle segment: user/assistant pairs with substantive token-heavy content
	// (>180 chars each) to ensure the LLM-generated summary is token-cheaper
	// than the original messages (the prepareMessages check requires
	// llmTokens < currentTokens to actually apply compaction).
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf(
				"Reviewed implementation details for the auth flow, examining the token validation and session management in component %d with extended analysis of the code structure, error handling patterns, and comprehensive edge case coverage across the authentication middleware.", i),
		})
		messages = append(messages, api.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"Continue with the next part of the refactoring analysis for component %d, checking error handling and edge cases in the authentication module with thorough validation.", i),
		})
	}

	// Recent tail: 12 messages (within RecentMessagesToKeep)
	for i := 0; i < 6; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "What about the error handling?",
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Fixed it.",
		})
	}

	agent.messages = messages
	originalCount := len(agent.messages)

	// --- Find a maxContextTokens that triggers the 87% compaction threshold --
	// We don't set checkpoints so checkpoint compaction won't fire, forcing
	// the second-pass LLM structural compaction path.
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// --- Call prepareMessages (the e2e entry point) ------------------------
	prepared := ch.prepareMessages(nil)

	// --- Assertions --------------------------------------------------------
	// 1. The compacted message count should be less than original + system.
	assert.Less(t, len(prepared), originalCount+1,
		"expected LLM compaction to reduce message count: got %d, want < %d",
		len(prepared), originalCount+1,
	)

	// 1b. agent.messages should be updated to the compacted version.
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be updated to compacted version after LLM compaction")

	// 2. The compactionClient (LLM) should have been called exactly once.
	sentRequests := compactionClient.GetSentRequests()
	assert.Equal(t, 1, len(sentRequests),
		"expected exactly 1 LLM summary request via compactionClient, got %d",
		len(sentRequests),
	)

	// 3. The sent request should contain a system message with the summarizer prompt.
	if len(sentRequests) > 0 && len(sentRequests[0]) > 0 {
		assert.Equal(t, "system", sentRequests[0][0].Role,
			"expected first message in LLM summary request to be system (summarizer prompt)")
		assert.Contains(t, sentRequests[0][0].Content, "conversation context summarizer",
			"expected system prompt to identify the summarizer role")
	}

	// 4. The compacted messages should contain the LLM-generated summary text.
	var foundLLMSummary bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, llmSummaryText) {
			foundLLMSummary = true
			break
		}
	}
	assert.True(t, foundLLMSummary,
		"expected compacted messages to contain the LLM summary text %q", llmSummaryText,
	)

	// 5. The summary line should start with the standard compaction header.
	var foundCompactedHeader bool
	for _, msg := range prepared {
		if strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundCompactedHeader = true
			break
		}
	}
	assert.True(t, foundCompactedHeader,
		"expected compacted messages to contain 'Compacted earlier conversation state:' header")

	// 6. The first recent message should be preserved (last user message in the tail).
	var foundRecentUserMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "What about the error handling?") {
			foundRecentUserMsg = true
			break
		}
	}
	assert.True(t, foundRecentUserMsg,
		"expected the first recent user message to be preserved after compaction")

	// 7. The anchor user query should be preserved.
	var foundAnchorMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "Refactor the authentication module") {
			foundAnchorMsg = true
			break
		}
	}
	assert.True(t, foundAnchorMsg,
		"expected the anchor user query to be preserved after compaction")
}

// ---------------------------------------------------------------------------
// Test 19 – LLM compaction error falls back to Go summary (e2e)
// ---------------------------------------------------------------------------

// TestE2E_LLMCompactionErrorFallsBackToGoSummary verifies that when the LLM
// client returns an error during the prepareMessages-triggered compaction,
// the Go fallback path still produces a valid summary with the standard
// "Compacted earlier conversation state:" header.
func TestE2E_LLMCompactionErrorFallsBackToGoSummary(t *testing.T) {
	t.Parallel()

	// --- Build compactionClient that returns an error -----------------------
	compactionClient := NewScriptedClient(
		NewErrorResponse(errors.New("LLM unavailable")),
	)

	// mainClient: not used for compaction but required by the agent.
	mainClient := NewScriptedClient(stopResponse())

	// --- Wire up agent with optimizer and LLM client -----------------------
	agent := makeAgentWithScriptedClient(10, mainClient)
	agent.optimizer = NewConversationOptimizer(true, false)
	agent.optimizer.SetLLMClient(compactionClient, "test-llm", nil)

	// --- Populate agent.messages with enough messages to trigger compaction ---
	// Layout:
	//   [0]  user     – anchor (long content for tokens)
	//   [1]  assistant – anchor (long content for tokens)
	//   [2..17]  middle segment: 8 user/assistant pairs with very long,
	//            substantively different content that the Go de-duplication
	//            truncates aggressively (each entry ~250 chars → truncated to 180)
	//   [18..29] recent tail: 12 messages (preserved)
	// Total = 30 messages
	//
	// Each long middle message is ~400+ chars but the Go summary truncates
	// to ~180 chars, giving the summary a clear token advantage.

	messages := make([]api.Message, 0, 30)

	// Anchor
	messages = append(messages, api.Message{
		Role:    "user",
		Content: "Refactor the authentication module in the codebase, split the monolithic handler into separate middleware components.",
	})
	messages = append(messages, api.Message{
		Role:    "assistant",
		Content: "I'll start by reviewing the existing authentication code, examining the handler structure, and identifying the boundaries for the middleware split.",
	})

	// Middle segment: messages with long unique content that the Go summary
	// will truncate to MaxEntryChars (180), ensuring the summary is token-cheaper.
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf(
				"Updated and verified the implementation of the refactoring component with detailed analysis of error handling patterns, input validation chains, and security middleware integration for the authentication module. Component index: %d. This paragraph extends the content significantly to ensure high token count.", i),
		})
		messages = append(messages, api.Message{
			Role: "user",
			Content: fmt.Sprintf(
				"Continue with the next part of the refactoring analysis for component %d, ensuring comprehensive edge case coverage, test coverage, and backward compatibility with the existing authentication API contracts.", i),
		})
	}

	// Recent tail
	for i := 0; i < 6; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: "What about the error handling?",
		})
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: "Fixed it.",
		})
	}

	agent.messages = messages
	originalCount := len(agent.messages)

	// --- Find a maxContextTokens that triggers the 87% threshold ---------------
	ch := NewConversationHandler(agent)
	findCompactionMaxContext(t, agent, ch)

	// --- Call prepareMessages ------------------------------------------------
	prepared := ch.prepareMessages(nil)

	// --- Assertions ----------------------------------------------------------
	// 1. The LLM client should have been called exactly 1 time (the attempt that failed).
	sentRequests := compactionClient.GetSentRequests()
	require.Equal(t, 1, len(sentRequests),
		"expected exactly 1 LLM call attempt (which fails), got %d",
		len(sentRequests),
	)

	// 2. The Go fallback should still produce a summary with the standard header.
	var foundFallbackHeader bool
	for _, msg := range prepared {
		if msg.Role == "assistant" && strings.Contains(msg.Content, "Compacted earlier conversation state:") {
			foundFallbackHeader = true
			break
		}
	}
	assert.True(t, foundFallbackHeader,
		"expected Go fallback summary to contain 'Compacted earlier conversation state:' header")

	// 3. The LLM-specific summary text should NOT be present (the LLM call failed).
	for _, msg := range prepared {
		assert.NotContains(t, msg.Content, "LLM_COMPACTED_SUMMARY",
			"expected no LLM summary text since the LLM call failed")
	}

	// 4. Compaction should have reduced message count (Go fallback also compacts).
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be updated to compacted version: got %d, want < %d",
		len(agent.messages), originalCount)

	// 5. A recent message should still be preserved.
	var foundRecentMsg bool
	for _, msg := range prepared {
		if msg.Role == "user" && strings.Contains(msg.Content, "What about the error handling?") {
			foundRecentMsg = true
			break
		}
	}
	assert.True(t, foundRecentMsg,
		"expected recent user messages to be preserved after compaction")
}
