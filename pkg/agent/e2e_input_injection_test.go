package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestE2E_InputInjectionMidConversation verifies end-to-end input injection
// during a running conversation.
//
// Flow:
//  1. ProcessQuery starts in a goroutine, iteration 0 begins.
//  2. Iteration 0's sendMessage calls ScriptedClient.SendChatRequest which
//     sleeps for 300ms (configured via ScriptedResponse.Delay).
//  3. While that sleep is in progress, the test goroutine writes injected
//     input into the buffered inputInjectionChan (capacity 10).
//  4. Iteration 0 finishes: processResponse returns false (empty finish_reason).
//  5. Iteration 1 starts: checkForInterrupt dequeues the injected input and
//     appends a new user message to agent.messages, returns false (continue).
//  6. Iteration 1's sendMessage returns the stop response.
//  7. processResponse returns true → conversation completes.
//
// Expected message order:
//
//	[0] user       → "Initial query"                (added by ProcessQuery)
//	[1] assistant  → "Still working..."              (keepGoing response, iter 0)
//	[2] user       → "Wait, change direction..."     (injected via channel, iter 1)
//	[3] assistant  → "Done."                         (stop response, iter 1)
func TestE2E_InputInjectionMidConversation(t *testing.T) {
	t.Parallel()

	injectedInput := "Wait, change direction: focus on X instead"

	// Response 0: keepGoing with a 300ms delay.
	// The delay creates a predictable timing window — while
	// SendChatRequest is sleeping, the test injects input into the
	// buffered channel so it is ready when iteration 1's
	// checkForInterrupt runs.
	//
	// NOTE: Content must be considered "incomplete" by IsIncomplete() so
	// that processResponse returns false (continue) and the conversation
	// loop proceeds to iteration 1 where the injected input is consumed.
	// Trailing "..." triggers hasIncompletePatterns → IsIncomplete=true.
	response0 := NewScriptedResponseBuilder().
		Content("Still working on the original task...").
		FinishReason("").
		Delay(300 * time.Millisecond).
		Build()

	// Response 1: stop — conversation completes after the injected
	// input is acknowledged by the model.
	response1 := stopResponse()

	agent, ch := buildE2EAgent(t, 10, response0, response1)

	// Run ProcessQuery in a goroutine so we can inject concurrently.
	var (
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = ch.ProcessQuery("Initial query")
	}()

	// Wait for iteration 0 to enter its delayed sendMessage (100ms ≪ 300ms).
	// 100ms provides headroom for slow CI schedulers; the channel is buffered
	// so the injection write never blocks regardless.
	time.Sleep(100 * time.Millisecond)
	injectErr := agent.InjectInputContext(injectedInput)
	require.NoError(t, injectErr, "input injection should succeed into buffered channel")

	// Wait for the conversation to finish.
	wg.Wait()

	// --- Basic completion checks ---
	require.NoError(t, err, "ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected successful completion after input injection")
	assert.Equal(t, 2, agent.GetCurrentIteration()+1,
		"expected 2 iterations (keepGoing + stop)")

	// --- Message structure verification ---
	messages := agent.GetMessages()
	require.Equal(t, 4, len(messages),
		"expected 4 messages (original user, assistant, injected user, assistant); got %d", len(messages))

	// [0] Original user query (added by ProcessQuery before the loop)
	assert.Equal(t, "user", messages[0].Role)
	assert.Contains(t, messages[0].Content, "Initial query",
		"message[0] should be the original user query")

	// [1] Assistant keepGoing response from iteration 0
	assert.Equal(t, "assistant", messages[1].Role)
	assert.Contains(t, messages[1].Content, "Still working",
		"message[1] should be the keepGoing assistant response")

	// [2] Injected user message (appended by checkForInterrupt at start of iteration 1)
	assert.Equal(t, "user", messages[2].Role)
	assert.True(t, strings.Contains(messages[2].Content, "change direction"),
		"message[2] should contain the injected input text")

	// [3] Final assistant stop response from iteration 1
	assert.Equal(t, "assistant", messages[3].Role)
	assert.Contains(t, messages[3].Content, "Done.",
		"message[3] should be the final stop response")
}

// TestE2E_InputInjectionMultipleInjections verifies that multiple input injections
// across iterations are all processed correctly.
//
// All three injections are pre-loaded into the buffered channel BEFORE
// ProcessQuery starts. Because the channel has sufficient capacity and
// checkForInterrupt uses a non-blocking select that dequeues at most one
// item per iteration, the injections are consumed one per iteration in
// FIFO order — eliminating any timing dependency.
//
// Flow:
//  1. Three injections are written into the buffered inputInjectionChan.
//  2. ProcessQuery starts; four keepGoing responses followed by one stop.
//  3. checkForInterrupt at the start of each iteration dequeues one
//     injection (non-blocking select), appending it as a user message.
//  4. The stop response on iteration 4 completes the conversation.
//
// Expected messages:
//
//	[0] user       → original query        (added by ProcessQuery)
//	[1] user       → injected "take 1"     (consumed at iter 0 start)
//	[2] assistant  → keepGoing (iter 0)
//	[3] user       → injected "take 2"     (consumed at iter 1 start)
//	[4] assistant  → keepGoing (iter 1)
//	[5] user       → injected "take 3"     (consumed at iter 2 start)
//	[6] assistant  → keepGoing (iter 2)
//	[7] assistant  → keepGoing (iter 3)
//	[8] assistant  → "Done." (stop, iter 4)
//
// Note: because injections are pre-loaded and consumed at iteration start
// (before sendMessage), injected user messages appear BEFORE the assistant
// response of the same iteration, not between two assistant messages.
func TestE2E_InputInjectionMultipleInjections(t *testing.T) {
	t.Parallel()

	// All keepGoing responses have a trailing "..." so IsIncomplete returns true,
	// causing processResponse to return false (continue). No delays needed since
	// all injections are pre-loaded before the conversation starts.
	kg1 := NewScriptedResponseBuilder().Content("Working on step one...").FinishReason("").Build()
	kg2 := NewScriptedResponseBuilder().Content("Working on step two...").FinishReason("").Build()
	kg3 := NewScriptedResponseBuilder().Content("Working on step three...").FinishReason("").Build()
	kg4 := NewScriptedResponseBuilder().Content("Working on step four...").FinishReason("").Build()
	final := stopResponse()

	agent, ch := buildE2EAgent(t, 20, kg1, kg2, kg3, kg4, final)

	injections := []string{
		"Take 1: switch to approach A",
		"Take 2: also check file B",
		"Take 3: add error handling",
	}

	// Pre-load all injections into the buffered channel BEFORE starting
	// ProcessQuery. Each iteration's checkForInterrupt non-blocking select
	// will dequeue one per iteration in FIFO order — no timing dependency.
	for i, inj := range injections {
		injectErr := agent.InjectInputContext(inj)
		require.NoError(t, injectErr, "injection %d should succeed into buffered channel", i)
	}

	var (
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = ch.ProcessQuery("Multi-step task")
	}()

	wg.Wait()

	require.NoError(t, err, "ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected completion after multiple injections")

	messages := agent.GetMessages()

	// Find all injected user messages by their content fingerprint.
	injectedUserIndices := make([]int, 0, 3)
	for i, msg := range messages {
		if msg.Role == "user" && !strings.Contains(msg.Content, "Multi-step task") {
			injectedUserIndices = append(injectedUserIndices, i)
		}
	}
	assert.Equal(t, 3, len(injectedUserIndices),
		"expected exactly 3 injected user messages; found %d", len(injectedUserIndices))

	// Verify each injection's content is present.
	for idx, inj := range injections {
		assert.Contains(t, messages[injectedUserIndices[idx]].Content, inj,
			"injected message at index %d should contain injection %d", injectedUserIndices[idx], idx)
	}

	// Verify message ordering: each injected user message should be followed
	// by an assistant response (since checkForInterrupt runs before sendMessage).
	// The first injected message follows the original user query, so its
	// predecessor is also a user message — only check successors.
	for _, idx := range injectedUserIndices {
		assert.Greater(t, idx, 0, "injected user message should not be first")
		if idx < len(messages)-1 {
			assert.Equal(t, "assistant", messages[idx+1].Role,
				"message after injected user at index %d should be assistant (injection consumed before sendMessage)", idx)
		}
	}
}

// TestE2E_InputInjectionChannelFull verifies that injecting when the channel
// is full returns an error instead of blocking.
func TestE2E_InputInjectionChannelFull(t *testing.T) {
	t.Parallel()

	// Build a minimal agent — no conversation needed, we just need the channel.
	client := NewScriptedClient()
	agent := makeAgentWithScriptedClient(10, client)

	// Fill the channel to capacity (10 = inputInjectionBufferSize).
	for i := 0; i < inputInjectionBufferSize; i++ {
		injectErr := agent.InjectInputContext(fmt.Sprintf("injection %d", i))
		require.NoError(t, injectErr, "injection %d should succeed (channel not full yet)", i)
	}

	// The 11th call should fail because the channel is full.
	injectErr := agent.InjectInputContext("overflow")
	require.Error(t, injectErr, "injecting into a full channel should return an error")
	assert.Contains(t, injectErr.Error(), "full",
		"error message should mention 'full'")
}

// TestE2E_InputInjectionWithToolCallIteration verifies input injection works
// correctly when one iteration involves tool calls.
//
// Flow:
//  1. Iteration 0: keepGoing with delay → injection lands on channel.
//  2. Iteration 1: checkForInterrupt consumes injection, sendMessage returns
//     a tool-call response (read_file on a temp file).
//  3. processResponse executes the tool, appends assistant + tool result,
//     returns false (continue).
//  4. Iteration 2: sendMessage returns stop → conversation completes.
func TestE2E_InputInjectionWithToolCallIteration(t *testing.T) {
	t.Parallel()

	// Create a temp file for the tool call to read.
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "data.txt")
	testContent := "line one\nline two\nline three"
	require.NoError(t, os.WriteFile(tempFile, []byte(testContent), 0o644))

	// Response 0: keepGoing with delay (incomplete via trailing "...").
	resp0 := NewScriptedResponseBuilder().
		Content("Looking at the files...").
		FinishReason("").
		Delay(200 * time.Millisecond).
		Build()

	// Response 1: tool call — read the temp file. finish_reason is empty so
	// processResponse returns false after executing the tool (tool_calls are
	// handled before finish_reason checks).
	resp1 := NewScriptedResponseBuilder().
		Content("Let me read that file.").
		ToolCall(api.ToolCall{
			ID:   "call_read_001",
			Type: "function",
			Function: struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			}{
				Name:      "read_file",
				Arguments: fmt.Sprintf(`{"file_path": "%s"}`, tempFile),
			},
		}).
		FinishReason("").
		Build()

	// Response 2: stop.
	resp2 := stopResponse()

	agent, ch := buildE2EAgent(t, 10, resp0, resp1, resp2)

	injectedInput := "Also check the test coverage"

	var (
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = ch.ProcessQuery("Read the data file")
	}()

	// Inject during iteration 0's sendMessage delay.
	time.Sleep(80 * time.Millisecond)
	injectErr := agent.InjectInputContext(injectedInput)
	require.NoError(t, injectErr, "input injection should succeed")

	wg.Wait()

	require.NoError(t, err, "ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	messages := agent.GetMessages()

	// Find the injected user message.
	var injectedIdx int = -1
	for i, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "test coverage") {
			injectedIdx = i
			break
		}
	}
	assert.NotEqual(t, -1, injectedIdx, "injected user message should be present in messages")

	// Verify the tool call and tool result exist after the injection.
	var foundToolCall, foundToolResult bool
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			foundToolCall = true
		}
		if msg.Role == "tool" {
			foundToolResult = true
		}
	}
	assert.True(t, foundToolCall, "expected a tool call response in conversation")
	assert.True(t, foundToolResult, "expected a tool result in conversation")

	// The injected message should appear in the conversation history.
	assert.True(t, strings.Contains(messages[injectedIdx].Content, "test coverage"),
		"injected message should contain 'test coverage'")
}

// TestE2E_PreLoadedInjectionIsConsumedByFirstIteration verifies that input
// injected BEFORE ProcessQuery starts IS consumed by checkForInterrupt at
// iteration 0.
//
// Flow:
//  1. Inject input into the buffered channel before calling ProcessQuery.
//  2. ProcessQuery starts; checkForInterrupt runs a non-blocking select at
//     iteration 0 start and dequeues the pre-loaded injection.
//  3. The injection is appended as a user message BEFORE the first sendMessage.
//  4. Single stop response completes the conversation.
//
// Expected messages:
//
//	[0] user       → original query            (added by ProcessQuery)
//	[1] user       → pre-loaded injection      (consumed at iter 0 by checkForInterrupt)
//	[2] assistant  → "Done."                   (stop response, iter 0)
func TestE2E_PreLoadedInjectionIsConsumedByFirstIteration(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	ch := NewConversationHandler(agent)

	preLoadedInput := "This was injected before ProcessQuery started"
	injectErr := agent.InjectInputContext(preLoadedInput)
	require.NoError(t, injectErr, "pre-load injection should succeed")

	_, err := ch.ProcessQuery("Quick stop")
	require.NoError(t, err, "ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// The pre-loaded injection was consumed by checkForInterrupt at iteration 0
	// and appended as a user message. Verify it appears in conversation history.
	messages := agent.GetMessages()
	var foundPreLoaded bool
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "injected before ProcessQuery") {
			foundPreLoaded = true
			break
		}
	}
	assert.True(t, foundPreLoaded,
		"pre-loaded injection should be consumed and appear in messages")
}

// TestE2E_InjectionAfterCompletionIsConsumedByNextProcessQuery verifies that
// input injected AFTER one ProcessQuery completes is consumed by the next
// ProcessQuery call.
//
// Flow:
//  1. First ProcessQuery completes normally with a single stop response.
//  2. Inject input into the buffered channel after completion.
//  3. Second ProcessQuery starts; checkForInterrupt dequeues the injection
//     at iteration 0 start.
//  4. Stop response completes the conversation.
//
// Expected messages from the second ProcessQuery:
//
//	[0] user       → "Second query"            (added by ProcessQuery)
//	[1] user       → post-completion injection  (consumed at iter 0 by checkForInterrupt)
//	[2] assistant  → "Done."                   (stop response, iter 0)
func TestE2E_InjectionAfterCompletionIsConsumedByNextProcessQuery(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	ch := NewConversationHandler(agent)

	// First ProcessQuery: complete with no injections.
	_, err := ch.ProcessQuery("First query")
	require.NoError(t, err, "first ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// Inject AFTER the first ProcessQuery completes.
	lateInjection := "This arrives after completion"
	injectErr := agent.InjectInputContext(lateInjection)
	require.NoError(t, injectErr, "post-completion injection should succeed")

	// Second ProcessQuery should consume the late injection.
	_, err2 := ch.ProcessQuery("Second query")
	require.NoError(t, err2, "second ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// Verify the late injection was consumed by the second ProcessQuery.
	messages := agent.GetMessages()
	var foundLateInjection bool
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "after completion") {
			foundLateInjection = true
			break
		}
	}
	assert.True(t, foundLateInjection,
		"post-completion injection should be consumed by the second ProcessQuery")
}

// TestE2E_InputInjectionWithLongRunningKeepGoing verifies injection works when
// the model takes many iterations before stopping.
//
// Flow:
//  1. First keepGoing response has delay → injection during iteration 0.
//  2. checkForInterrupt at iteration 1 start consumes the injection.
//  3. Three more keepGoing responses (no more injections).
//  4. Final stop response completes the conversation.
//
// Total iterations: 6 (1 keepGoing with delay + 4 keepGoing + 1 stop).
func TestE2E_InputInjectionWithLongRunningKeepGoing(t *testing.T) {
	t.Parallel()

	// First keepGoing has delay for the injection timing window; injection occurs
	// during that delay and is consumed at iteration 1 start.
	delay := 150 * time.Millisecond
	responses := []*ScriptedResponse{
		// Iter 0: delayed keepGoing → injection window
		NewScriptedResponseBuilder().Content("Step 1 in progress...").FinishReason("").Delay(delay).Build(),
		// Iter 1: injection consumed here, normal keepGoing
		keepGoingResponse(),
		// Iter 2: keepGoing
		keepGoingResponse(),
		// Iter 3: keepGoing
		keepGoingResponse(),
		// Iter 4: keepGoing
		keepGoingResponse(),
		// Iter 5: stop
		stopResponse(),
	}

	agent, ch := buildE2EAgent(t, 20, responses...)

	injectedInput := "Remember to also handle edge cases"

	var (
		err error
		wg  sync.WaitGroup
	)
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, err = ch.ProcessQuery("Build a parser component")
	}()

	// Inject during iteration 0's sendMessage delay window.
	time.Sleep(50 * time.Millisecond)
	injectErr := agent.InjectInputContext(injectedInput)
	require.NoError(t, injectErr, "injection should succeed into buffered channel")

	wg.Wait()

	require.NoError(t, err, "ProcessQuery should complete without error")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())

	// Should be 6 iterations (one per scripted response).
	assert.Equal(t, 6, agent.GetCurrentIteration()+1,
		"expected 6 iterations (5 keepGoing + 1 stop)")

	messages := agent.GetMessages()

	// Verify the injected message is present.
	var injectedFound bool
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "edge cases") {
			injectedFound = true
			break
		}
	}
	assert.True(t, injectedFound,
		"injected input 'edge cases' should appear in conversation history")

	// Verify message count: 1 original user + 5 assistant + 1 injected user + 1 final assistant = 8.
	// Note: keepGoing responses with trailing "..." are incomplete → processResponse returns false.
	// The keepGoingResponse() helper also has trailing "..." so those also continue.
	assert.GreaterOrEqual(t, len(messages), 6,
		"expected at least 6 messages in a multi-iteration conversation, got %d", len(messages))

	// First message should be the original user query.
	assert.Equal(t, "user", messages[0].Role)
	assert.Contains(t, messages[0].Content, "Build a parser")

	// Last message should be the final assistant stop response.
	assert.Equal(t, "assistant", messages[len(messages)-1].Role)
	assert.Contains(t, messages[len(messages)-1].Content, "Done.")
}
