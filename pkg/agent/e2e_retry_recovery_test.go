// e2e_retry_recovery_test.go – End-to-end API retry/error recovery tests
//
// This file validates the retry/backoff path through the full
// ProcessQuery → APIClient.SendWithRetry → client pipeline, using
// error injection via the ScriptedClient.
//
// Retry behaviour under test:
//   - APIClient.SendWithRetry retries transient errors (containing
//     "stream error", "INTERNAL_ERROR", "connection reset", "EOF",
//     "timeout") up to maxRetries (default 3) with exponential backoff.
//   - Each SendChatRequest call consumes one ScriptedResponse from the queue.
//   - SendWithRetry is internal to each conversation iteration; retries do NOT
//     advance the iteration counter — only a fully successful request/response
//     cycle counts as an iteration.
//   - When retries are exhausted, the error bubbles up through ProcessQuery
//     → ErrorHandler.HandleAPIFailure, which returns a user-facing string
//     with a nil Go error.

package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// Test 1 – Single transient error → retry → success
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_SingleTransientError verifies that a single transient
// error is retried by SendWithRetry, leading to a successful conversation.
// Queue: [error("stream error"), stopResponse()]
// Expected: 1 conversation iteration (retry is internal to SendWithRetry).
func TestE2E_RetryRecovery_SingleTransientError(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("stream error: upstream connection dropped")),
		stopResponse(),
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Hello, what is 2+2?")

	require.NoError(t, err, "ProcessQuery should succeed after retry recovers from transient error")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1,
		"expected 1 iteration (retry is internal to SendWithRetry, only one successful loop)")
}

// ---------------------------------------------------------------------------
// Test 2 – Multiple transient errors → retry → success
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_MultipleTransientErrors verifies that two consecutive
// transient errors are retried by SendWithRetry until success.
// Queue: [error("connection reset"), error("EOF"), stopResponse()]
// Expected: 1 conversation iteration.
func TestE2E_RetryRecovery_MultipleTransientErrors(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("connection reset by peer")),
		NewErrorResponse(errors.New("unexpected EOF")),
		stopResponse(),
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Build something")

	require.NoError(t, err, "ProcessQuery should succeed after 2 retries")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1,
		"expected 1 iteration (retries are internal to SendWithRetry)")
}

// ---------------------------------------------------------------------------
// Test 3 – Max retries exhausted → error message returned
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_MaxRetriesExhausted verifies that when transient errors
// exceed maxRetries (default 3), the conversation ends with a user-facing error
// message from ErrorHandler.HandleAPIFailure instead of panicking or silently
// failing.
// Queue: [error, error, error, error, stopResponse(never reached)]
// SendWithRetry loop: retry 0 → error, retry 1 → error, retry 2 → error,
// retry 3 → shouldRetry returns false (3 >= maxRetries) → error propagated.
func TestE2E_RetryRecovery_MaxRetriesExhausted(t *testing.T) {
	t.Parallel()

	// 4 transient errors (1 original attempt + 3 retries) exhaust the budget;
	// the 5th response (stopResponse) is never consumed.
	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("stream error")),
		NewErrorResponse(errors.New("INTERNAL_ERROR")),
		NewErrorResponse(errors.New("stream error")),
		NewErrorResponse(errors.New("stream error")),
		stopResponse(),
	}

	agent, _, client := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Do something")

	// ErrorHandler.HandleAPIFailure converts the exhausted-retry error into a
	// user-facing string and returns (string, nil) — not a Go error.
	require.NoError(t, err, "ProcessQuery returns nil error; error handler wraps it in a message")
	assert.NotEmpty(t, result, "expected a non-empty user-facing error message from HandleAPIFailure")

	// Verify exactly 4 responses consumed (initial attempt + 3 retries),
	// and the 5th (stopResponse) was never touched.
	assert.Equal(t, 4, client.GetIndex(),
		"expected exactly 4 scripted responses consumed (1 attempt + 3 retries)")
}

// ---------------------------------------------------------------------------
// Test 4 – Transient error during tool-call iteration → retry → tool call succeeds
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_TransientErrorDuringToolCall verifies that a transient
// error on the first attempt of what would be a tool-call iteration is retried,
// and the tool call succeeds on the retry attempt. The full flow is:
//   Iteration 0: SendWithRetry → error → retry → tool_call(read_file) → success
//                → processResponse executes tool → continues
//   Iteration 1: SendWithRetry → stopResponse → success → completes
//
// Queue: [error("stream error"), tool_call_response(read_file), stopResponse()]
// Expected: 2 conversation iterations, tool result in conversation history.
func TestE2E_RetryRecovery_TransientErrorDuringToolCall(t *testing.T) {
	t.Parallel()

	// Create a real temp file so the read_file tool succeeds
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "readme.md")
	fileContent := "Release notes for v2.0: added retry recovery tests."
	require.NoError(t, os.WriteFile(tempFile, []byte(fileContent), 0o644), "failed to create temp file")

	toolCallResp := NewScriptedResponseBuilder().
		Content("Let me read the release notes.").
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
		NewErrorResponse(errors.New("stream error: connection interrupted")),
		toolCallResp,
		stopResponse(),
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Read the release notes and summarize")

	require.NoError(t, err, "ProcessQuery should succeed after retry during tool call iteration")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// 2 iterations: tool call from retry (iteration 0) + stop (iteration 1)
	assert.Equal(t, 2, agent.GetCurrentIteration()+1,
		"expected 2 iterations (tool call recovered from retry + stop)")

	// Verify the tool result with actual file content appears in conversation history
	var foundToolResult bool
	for _, msg := range agent.messages {
		if msg.Role == "tool" && strings.Contains(msg.Content, "retry recovery tests") {
			foundToolResult = true
			break
		}
	}
	assert.True(t, foundToolResult, "expected tool result containing file content in conversation history")
}

// ---------------------------------------------------------------------------
// Test 5 – Non-retryable error propagated immediately (no retries)
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_NonRetryableErrorPropagatedImmediately verifies that
// errors that don't match any retryable keyword (e.g. authentication failures)
// propagate immediately without consuming additional responses from the queue.
func TestE2E_RetryRecovery_NonRetryableErrorPropagatedImmediately(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("authentication failed: invalid API key")),
		stopResponse(), // should never be consumed
	}

	agent, _, client := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Do something")

	// HandleAPIFailure wraps non-retryable errors into a user-facing string.
	require.NoError(t, err, "HandleAPIFailure wraps the error into a string")
	assert.NotEmpty(t, result, "expected a non-empty user-facing error message")

	// Non-retryable error should consume exactly 1 response (no retries).
	assert.Equal(t, 1, client.GetIndex(),
		"non-retryable error should consume exactly 1 response (no retries)")
}

// ---------------------------------------------------------------------------
// Test 6 – Multiple retryable error keywords across retries
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_VariousRetryableKeywords verifies that different
// retryable error keywords ("stream error", "INTERNAL_ERROR", "connection
// reset") each independently trigger retry behavior within SendWithRetry.
func TestE2E_RetryRecovery_VariousRetryableKeywords(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("stream error: chunk delivery failed")),
		NewErrorResponse(errors.New("INTERNAL_ERROR: provider fault")),
		NewErrorResponse(errors.New("connection reset by peer")),
		stopResponse(),
	}

	agent, _, client := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Build something complex")

	require.NoError(t, err, "ProcessQuery should succeed after 3 retries with varied keywords")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1,
		"expected 1 iteration (3 retries internal to SendWithRetry)")

	// Verify exactly 4 responses consumed (1 initial + 3 retries before success).
	assert.Equal(t, 4, client.GetIndex(),
		"expected exactly 4 scripted responses consumed (1 attempt + 3 retries)")
}

// ---------------------------------------------------------------------------
// Test 7 – "timeout" retryable keyword triggers retry
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_TimeoutErrorTriggersRetry verifies that the "timeout"
// retryable keyword (the 5th keyword checked by isRetryableError) triggers
// retry behavior, just like "stream error", "INTERNAL_ERROR", "connection reset",
// and "EOF".
// Queue: [error("operation timed out"), stopResponse()]
// Expected: 1 conversation iteration, 2 responses consumed (1 initial + 1 retry).
func TestE2E_RetryRecovery_TimeoutErrorTriggersRetry(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("timeout: operation timed out")),
		stopResponse(),
	}

	agent, _, client := buildE2EAgentWithClient(t, 10, responses...)
	result, err := agent.ProcessQuery("Do something slow")

	require.NoError(t, err, "ProcessQuery should succeed after retry recovers from timeout error")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1,
		"expected 1 iteration (retry is internal to SendWithRetry)")
	assert.Equal(t, 2, client.GetIndex(),
		"expected exactly 2 scripted responses consumed (1 initial attempt + 1 retry)")
}

// ---------------------------------------------------------------------------
// Test 8 – Exponential backoff delays are observed between retries
// ---------------------------------------------------------------------------

// TestE2E_RetryRecovery_BackoffDelaysObserved verifies that exponential backoff
// delays actually occur between retries by measuring wall-clock elapsed time.
// With baseRetryDelay=1s and 2 retries: first backoff ≥ 1s, second ≥ 2s,
// giving a minimum expected total of ~3s. A conservative 2s threshold still
// proves backoff is active compared to sub-millisecond execution without delays.
// Queue: [error("stream error"), error("stream error"), stopResponse()]
// Expected: total elapsed ≥ 2s.
func TestE2E_RetryRecovery_BackoffDelaysObserved(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		NewErrorResponse(errors.New("stream error")),
		NewErrorResponse(errors.New("stream error")),
		stopResponse(),
	}

	agent, _, _ := buildE2EAgentWithClient(t, 10, responses...)

	start := time.Now()
	result, err := agent.ProcessQuery("Test backoff timing")
	elapsed := time.Since(start)

	require.NoError(t, err, "ProcessQuery should succeed after 2 retries with backoff")
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1,
		"expected 1 iteration (2 retries are internal to SendWithRetry)")

	// NOTE: baseRetryDelay defaults to 1s (set in NewAPIClient).
	// This threshold assumes that value.
	// With 2 retries and baseRetryDelay=1s: first backoff ≥ 1s, second ≥ 2s.
	// Jitter adds [0, 0.5s) per retry. Minimum theoretical total: 3.0s.
	// Use a conservative 2s threshold to tolerate timing jitter and
	// scheduler variability while still proving backoff delays occur.
	assert.GreaterOrEqual(t, elapsed, 2*time.Second,
		"expected at least ~2s elapsed due to exponential backoff (2 retries × 1s base), got %v", elapsed)
	// Upper bound catches accidental delay increases (e.g. baseRetryDelay change).
	assert.Less(t, elapsed, 10*time.Second,
		"backoff took suspiciously long, got %v", elapsed)
}
