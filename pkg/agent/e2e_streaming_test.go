// e2e_streaming_test.go – End-to-end streaming response tests
//
// This file validates the streaming response path through the full
// ProcessQuery → APIClient → ScriptedClient pipeline, using the
// StreamConfig pattern to simulate chunked delivery.
//
// Key behaviors verified:
//   - Streaming callbacks fire for each chunk delivered by the client
//   - Content accumulates correctly in the agent's streamingBuffer
//   - The conversation handler prefers the streaming buffer content over
//     the response choice's Content field (critical when they differ)
//   - Termination reason and iteration count are correct for a single-turn
//     streaming completion
//
// Streamed content must be ≥10 words to pass the ResponseValidator's
// IsIncomplete check; otherwise the handler loops expecting more content.

package agent

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// TestE2E_StreamingResponses
// ---------------------------------------------------------------------------

// TestE2E_StreamingResponses verifies the complete streaming response flow
// through ProcessQuery. A ScriptedClient is configured with a StreamConfig
// that delivers three chunks simulating a phrase, and the response's
// Content field is set to a different sentinel value ("THIS_SHOULD_NOT_APPEAR").
// This proves that when streaming is enabled, the handler uses the accumulated
// streaming buffer rather than the raw choice content for the assistant
// message added to conversation history.
func TestE2E_StreamingResponses(t *testing.T) {
	t.Parallel()

	// --- Build a scripted response with streaming configuration ---
	// The streamed content must be long enough (≥10 words) to pass the
	// response validator's IsIncomplete check, otherwise the handler loops
	// again expecting more content.
	streamedFull := "Hello world! This is a complete streaming response with enough words."
	streamResp := NewScriptedResponseBuilder().
		StreamConfig(&StreamConfig{
			Chunks:       []string{"Hello", " world! This is a complete ", "streaming response with enough words."},
			FinishReason: "stop",
		}).
		// Intentionally set Content to something different from the streamed
		// content so we can prove the buffer is preferred over choice content.
		Content("THIS_SHOULD_NOT_APPEAR").
		// FinishReason on the ScriptedResponse itself is irrelevant when
		// StreamConfig is set because the stream code overrides it — but we
		// leave it empty to avoid any accidental match.
		FinishReason("").
		Build()

	agent, _ := buildE2EAgent(t, 10, streamResp)

	// --- Enable streaming on the agent ---
	agent.SetStreamingEnabled(true) // sets outputMutex

	// Track callback invocations in a thread-safe manner.
	var (
		callbackMu   sync.Mutex
		callbackCount int
		callbackChunks []string
	)

	agent.SetStreamingCallback(func(chunk string) {
		callbackMu.Lock()
		defer callbackMu.Unlock()
		callbackCount++
		callbackChunks = append(callbackChunks, chunk)
	})

	// Ensure outputRouter is nil (it is by default from makeAgentWithScriptedClient)
	// so PublishStreamChunk falls through to the callback path.

	// --- Execute the query ---
	result, err := agent.ProcessQuery("test query")
	require.NoError(t, err)

	// When streaming is enabled and content was streamed, finalizeConversation
	// returns "" to avoid duplicate display.
	assert.Equal(t, "", result,
		"ProcessQuery should return empty string when streaming produced content")

	// --- Assert callback was invoked for each chunk ---
	callbackMu.Lock()
	defer callbackMu.Unlock()

	assert.Equal(t, 3, callbackCount,
		"expected streaming callback to fire exactly 3 times (one per chunk)")

	require.Len(t, callbackChunks, 3, "expected 3 callback chunks")
	assert.Equal(t, "Hello", callbackChunks[0], "first chunk")
	assert.Equal(t, " world! This is a complete ", callbackChunks[1], "second chunk")
	assert.Equal(t, "streaming response with enough words.", callbackChunks[2], "third chunk")

	// --- Assert streaming buffer accumulated correctly ---
	assert.Equal(t, streamedFull, agent.streamingBuffer.String(),
		"streamingBuffer should contain concatenated chunk content")

	// --- Assert termination and iteration ---
	// After a single-loop break, the post-increment never executes (break
	// exits before the for-loop's i++ clause), so currentIteration stays at 0.
	assert.Equal(t, 0, agent.GetCurrentIteration(),
		"expected currentIteration=0 after a single-loop break")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"streaming stop response should complete the query")

	// --- Assert expected message structure ---
	// user + assistant = 2 (system prompt is stored in agent.systemPrompt,
	// not in agent.messages; it gets prepended only during prepareMessages)
	require.Len(t, agent.messages, 2, "expected user + assistant")
	assert.Equal(t, "user", agent.messages[0].Role)
	assert.Equal(t, "assistant", agent.messages[1].Role)
	assert.Equal(t, streamedFull, agent.messages[1].Content,
		"last assistant message should use streaming buffer content, not choice content")
	assert.NotEqual(t, "THIS_SHOULD_NOT_APPEAR", agent.messages[1].Content,
		"last assistant message must NOT contain the raw choice content")
}
