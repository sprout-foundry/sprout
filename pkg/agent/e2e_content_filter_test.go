package agent

import (
	"testing"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// contentFilterResponse returns a ScriptedResponse with finish_reason
// "content_filter", simulating a model response that was blocked by a
// safety/content filter. The content contains a non-blank placeholder string
// so it does not trigger the blank-iteration guardrail (which would enqueue
// transient messages or error-stop independently of the content_filter path).
// An optional message parameter allows callers to provide distinct content
// when multiple content_filter responses are needed in sequence, avoiding
// isRepetitiveContent detection.
func contentFilterResponse(msg ...string) *ScriptedResponse {
	content := "Content was filtered by the safety system and cannot be displayed."
	if len(msg) > 0 {
		content = msg[0]
	}
	return NewScriptedResponseBuilder().
		Content(content).
		FinishReason("content_filter").
		Build()
}

// ---------------------------------------------------------------------------
// Test 1 – Single content_filter → conversation continues → stop completes
// ---------------------------------------------------------------------------

// TestE2E_ContentFilterContinuesConversation verifies that when the model
// returns finish_reason "content_filter", the conversation loop continues
// instead of stopping. After the filter event, a normal "stop" response
// completes the conversation with RunTerminationCompleted in exactly 2
// iterations (content_filter + stop).
func TestE2E_ContentFilterContinuesConversation(t *testing.T) {
	t.Parallel()

	filterResp := contentFilterResponse()
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, filterResp, stopResp)

	_, err := agent.ProcessQuery("Tell me something")
	require.NoError(t, err)

	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected termination reason RunTerminationCompleted after content_filter then stop")

	assert.Equal(t, 2, agent.GetCurrentIteration()+1,
		"expected 2 iterations (content_filter + stop)")
}

// ---------------------------------------------------------------------------
// Test 2 – Multiple consecutive content_filter responses still recover
// ---------------------------------------------------------------------------

// TestE2E_ContentFilterMultipleTimes verifies that multiple consecutive
// content_filter responses don't cause the conversation to error out or
// stop prematurely. Each content_filter returns false from handleFinishReason,
// keeping the loop alive. The responses use distinct content strings to
// avoid isRepetitiveContent detection, ensuring the test exercises only
// the content_filter path. After the filters, a normal stop completes the
// conversation with RunTerminationCompleted in exactly 3 iterations.
func TestE2E_ContentFilterMultipleTimes(t *testing.T) {
	t.Parallel()

	filterResp1 := contentFilterResponse("Content was filtered by the safety system and cannot be displayed.")
	filterResp2 := contentFilterResponse("The model's response was blocked by content moderation filters due to policy violation.")
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, filterResp1, filterResp2, stopResp)

	_, err := agent.ProcessQuery("Tell me something")
	require.NoError(t, err)

	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected termination reason RunTerminationCompleted after multiple content_filters then stop")

	assert.Equal(t, 3, agent.GetCurrentIteration()+1,
		"expected 3 iterations (2× content_filter + stop)")
}

// ---------------------------------------------------------------------------
// Test 3 – processResponse returns false for content_filter (low-level)
// ---------------------------------------------------------------------------

// TestE2E_ContentFilterViaProcessResponse verifies the low-level behavior
// of processResponse when given a content_filter finish_reason. Unlike blank
// iterations or length responses which may enqueue transient messages,
// content_filter simply returns false to continue the loop without any
// side effects. A subsequent stop response then returns true to complete.
func TestE2E_ContentFilterViaProcessResponse(t *testing.T) {
	t.Parallel()

	agent, ch := buildE2EAgent(t, 10)

	// Set up minimal conversation state so processResponse can function.
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "test query"})
	ch.pendingUserMessage = "test query"

	// First call: content_filter → should NOT stop the conversation.
	filterResp := contentFilterResponse()
	stopped := ch.processResponse(scriptedResponseToChatResponse(filterResp))
	assert.False(t, stopped, "content_filter should not stop the conversation")

	// Verify processResponse appended the assistant message to conversation history.
	assert.Equal(t, "assistant", ch.agent.messages[len(ch.agent.messages)-1].Role,
		"expected processResponse to append assistant message to agent.messages")

	// Second call: stop → should stop the conversation.
	stopResp := stopResponse()
	stopped = ch.processResponse(scriptedResponseToChatResponse(stopResp))
	assert.True(t, stopped, "stop response after content_filter should complete the conversation")
}

// ---------------------------------------------------------------------------
// Test 4 – content_filter does NOT enqueue transient messages
// ---------------------------------------------------------------------------

// TestE2E_ContentFilterNoTransientMessages verifies that the content_filter
// finish reason does NOT enqueue any transient messages into the
// ConversationHandler. This distinguishes content_filter from other
// non-stopping finish reasons (e.g., blank iteration, length) which DO
// inject reminders or nudges via transient messages.
func TestE2E_ContentFilterNoTransientMessages(t *testing.T) {
	t.Parallel()

	agent, ch := buildE2EAgent(t, 10)

	// Set up minimal conversation state.
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "test query"})
	ch.pendingUserMessage = "test query"

	// Call processResponse with a content_filter response.
	filterResp := contentFilterResponse()
	stopped := ch.processResponse(scriptedResponseToChatResponse(filterResp))
	assert.False(t, stopped, "content_filter should not stop the conversation")

	// Verify no transient messages were enqueued.
	ch.transientMessagesMu.Lock()
	tmCount := len(ch.transientMessages)
	ch.transientMessagesMu.Unlock()

	assert.Equal(t, 0, tmCount,
		"content_filter should not enqueue any transient messages, got %d", tmCount)
}
