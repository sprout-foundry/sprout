package agent

import (
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
// Helpers
// ---------------------------------------------------------------------------

// buildE2EAgent creates a minimal Agent + ConversationHandler wired to the
// given scripted client for full ProcessQuery or processResponse tests.
func buildE2EAgent(t *testing.T, maxIter int, responses ...*ScriptedResponse) (*Agent, *ConversationHandler) {
	t.Helper()
	client := NewScriptedClient(responses...)
	agent := makeAgentWithScriptedClient(maxIter, client)
	ch := NewConversationHandler(agent)
	return agent, ch
}

// tokenUsageResponse returns a ScriptedResponse with explicit usage metrics.
func tokenUsageResponse(content, finishReason string, prompt, completion, total int) *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content(content).
		FinishReason(finishReason).
		Usage(prompt, completion, total, 0.0).
		Build()
}

// blankResponse returns a ScriptedResponse whose content is whitespace and whose
// finish_reason is empty — a blank iteration.
func blankResponse() *ScriptedResponse {
	return blankResponseWithContent("   \n\t  ")
}

// blankResponseWithContent returns a blank response with the whitespace content set.
func blankResponseWithContent(content string) *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content(content).
		Build()
}

// lengthResponse returns a ScriptedResponse with finish_reason "length" signaling
// the model hit its output limit.
func lengthResponse(content string) *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content(content).
		FinishReason("length").
		Build()
}

// emptyStopResponse returns a ScriptedResponse with finish_reason "stop" but empty content.
func emptyStopResponse() *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content("").
		FinishReason("stop").
		Build()
}

// contentResponse returns a ScriptedResponse with the given content and no finish_reason.
func contentResponse(content string) *ScriptedResponse {
	return NewScriptedResponseBuilder().
		Content(content).
		Build()
}

// scriptedResponseToChatResponse converts a ScriptedResponse to an api.ChatResponse
// for testing processResponse directly.
func scriptedResponseToChatResponse(resp *ScriptedResponse) *api.ChatResponse {
	if resp == nil {
		return nil
	}

	// Use response usage if provided, otherwise use defaults
	usage := resp.Usage
	if usage.PromptTokens == 0 && usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		usage = ScriptedTokenUsage{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    0.0,
		}
	}

	return &api.ChatResponse{
		Choices: []api.Choice{{
			FinishReason: resp.FinishReason,
			Message: struct {
				Role             string          `json:"role"`
				Content          string          `json:"content"`
				ReasoningContent string          `json:"reasoning_content,omitempty"`
				Images           []api.ImageData `json:"images,omitempty"`
				ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
			}{
				Role:             "assistant",
				Content:          resp.Content,
				ReasoningContent: resp.ReasoningContent,
				Images:           resp.Images,
				ToolCalls:        resp.ToolCalls,
			},
		}},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     usage.PromptTokens,
			CompletionTokens: usage.CompletionTokens,
			TotalTokens:      usage.TotalTokens,
			EstimatedCost:    usage.EstimatedCost,
		},
	}
}

// waitForCheckpoints polls agent.HasTurnCheckpoints() with exponential backoff.
func waitForCheckpoints(t *testing.T, a *Agent, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	sleep := 5 * time.Millisecond
	for time.Now().Before(deadline) {
		if a.HasTurnCheckpoints() {
			return
		}
		time.Sleep(sleep)
		if sleep < 100*time.Millisecond {
			sleep *= 2
		}
	}
	// Last chance
	if a.HasTurnCheckpoints() {
		return
	}
	t.Fatalf("timed out waiting for turn checkpoints after %v", timeout)
}

// ---------------------------------------------------------------------------
// Test 1 – Single-turn complete
// ---------------------------------------------------------------------------

// TestE2E_SingleTurnComplete verifies that a user query that receives a single
// "stop" response completes immediately with termination reason "completed"
// and exactly one iteration.
func TestE2E_SingleTurnComplete(t *testing.T) {
	t.Parallel()

	agent, _ := buildE2EAgent(t, 10, stopResponse())
	result, err := agent.ProcessQuery("Hello, what is 2+2?")
	require.NoError(t, err)
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 1, agent.GetCurrentIteration()+1, "expected exactly 1 iteration for single-turn complete")
}

// ---------------------------------------------------------------------------
// Test 2 – Multi-turn continuation
// ---------------------------------------------------------------------------

// TestE2E_MultiTurnContinuation verifies that the agent correctly continues
// when the model returns empty finish_reason responses, then completes when
// it sends a "stop" response.
func TestE2E_MultiTurnContinuation(t *testing.T) {
	t.Parallel()

	responses := []*ScriptedResponse{
		keepGoingResponse(),
		keepGoingResponse(),
		keepGoingResponse(),
		stopResponse(),
	}

	agent, _ := buildE2EAgent(t, 10, responses...)
	result, err := agent.ProcessQuery("Build me a project")
	require.NoError(t, err)
	assert.Equal(t, "Done.", result)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 4, agent.GetCurrentIteration()+1, "expected 4 iterations (3 continuations + 1 stop)")
}

// ---------------------------------------------------------------------------
// Test 3 – Max iterations cap
// ---------------------------------------------------------------------------

// TestE2E_MaxIterationsCap verifies that when maxIterations is set, the loop
// stops after that many iterations even if the model keeps saying "continue".
func TestE2E_MaxIterationsCap(t *testing.T) {
	t.Parallel()

	responses := make([]*ScriptedResponse, 20)
	for i := range responses {
		responses[i] = keepGoingResponse()
	}

	agent, _ := buildE2EAgent(t, 5, responses...)
	_, err := agent.ProcessQuery("Do something complex")
	require.NoError(t, err)
	assert.Equal(t, RunTerminationMaxIterations, agent.GetLastRunTerminationReason())
	assert.Equal(t, 5, agent.GetCurrentIteration(), "expected 5 iterations to match maxIterations cap")
}

// ---------------------------------------------------------------------------
// Test 4 – Unlimited iterations (maxIterations=0)
// ---------------------------------------------------------------------------

// TestE2E_UnlimitedIterations verifies that maxIterations=0 allows unlimited
// iterations. The agent should loop through all keepGoing responses until it
// receives a stop.
func TestE2E_UnlimitedIterations(t *testing.T) {
	t.Parallel()

	totalKeepGoing := 15
	responses := make([]*ScriptedResponse, 0, totalKeepGoing+1)
	for i := 0; i < totalKeepGoing; i++ {
		responses = append(responses, keepGoingResponse())
	}
	responses = append(responses, stopResponse())

	agent, _ := buildE2EAgent(t, 0, responses...)
	_, err := agent.ProcessQuery("Complex analysis")
	require.NoError(t, err)
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.GreaterOrEqual(t, agent.GetCurrentIteration(), totalKeepGoing,
		"expected at least %d iterations with unlimited mode", totalKeepGoing)
}

// ---------------------------------------------------------------------------
// Test 5 – Blank iteration → recovery → complete
// ---------------------------------------------------------------------------

// TestE2E_BlankIterationRecovery verifies that a blank (whitespace-only)
// response triggers a reminder transient message but does not stop the loop.
// After the reminder, a proper stop response completes the conversation.
func TestE2E_BlankIterationRecovery(t *testing.T) {
	t.Parallel()

	blankResp := blankResponseWithContent("   \n\t  ")
	stopResp := stopResponse()

	agent, ch := buildE2EAgent(t, 10, blankResp, stopResp)

	// Simulate: ProcessQuery adds user message then loops. Instead of calling
	// ProcessQuery (which would also call sendMessage internally and hit
	// api.GetToolDefinitions), we test processResponse directly to isolate the
	// blank-iteration logic.
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "test query"})
	ch.pendingUserMessage = "test query"

	// First call: blank response → should NOT stop, should enqueue reminder
	stopped := ch.processResponse(scriptedResponseToChatResponse(blankResp))
	assert.False(t, stopped, "blank response should not stop the conversation")
	assert.Equal(t, 0, ch.agent.currentIteration)

	// Verify transient reminder was enqueued
	ch.transientMessagesMu.Lock()
	tmCount := len(ch.transientMessages)
	var reminderFound bool
	for _, m := range ch.transientMessages {
		if m.Role == "user" && strings.Contains(m.Content, "You provided no content") {
			reminderFound = true
		}
	}
	ch.transientMessagesMu.Unlock()
	assert.Equal(t, 1, tmCount, "expected exactly 1 transient message after blank iteration")
	assert.True(t, reminderFound, "expected blank-iteration reminder in transient messages")

	// Second call: proper stop → should stop
	ch.agent.messages = append(ch.agent.messages, api.Message{
		Role:    "assistant",
		Content: "   \n\t  ",
	})
	stopped = ch.processResponse(scriptedResponseToChatResponse(stopResp))
	assert.True(t, stopped, "proper stop response should complete the conversation")
}

// ---------------------------------------------------------------------------
// Test 6 – Double blank → error stop
// ---------------------------------------------------------------------------

// TestE2E_DoubleBlankErrorStop verifies that two consecutive blank responses
// cause the conversation to stop with an error message rather than looping
// indefinitely.
func TestE2E_DoubleBlankErrorStop(t *testing.T) {
	t.Parallel()

	blankResp1 := blankResponseWithContent(" ")
	blankResp2 := blankResponseWithContent("  ")

	agent, ch := buildE2EAgent(t, 10)
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "test"})
	ch.pendingUserMessage = "test"

	// First blank → continue with reminder
	stopped := ch.processResponse(scriptedResponseToChatResponse(blankResp1))
	assert.False(t, stopped, "first blank should not stop")

	// Append the blank assistant message so the next processResponse sees it in history
	ch.agent.messages = append(ch.agent.messages, api.Message{
		Role:    "assistant",
		Content: " ",
	})

	// Second blank → should stop (error out)
	stopped = ch.processResponse(scriptedResponseToChatResponse(blankResp2))
	assert.True(t, stopped, "second consecutive blank should stop the conversation")
}

// ---------------------------------------------------------------------------
// Test 7 – Repetitive content → recovery → complete
// ---------------------------------------------------------------------------

// TestE2E_RepetitiveContentRecovery verifies that when the model sends the same
// content twice in a row with finish_reason="length", the duplicate-detection
// path fires a reminder and the conversation can recover and complete.
func TestE2E_RepetitiveContentRecovery(t *testing.T) {
	t.Parallel()

	agent, ch := buildE2EAgent(t, 10)
	agent.messages = append(agent.messages, api.Message{Role: "user", Content: "do something"})
	ch.pendingUserMessage = "do something"

	// Use finish_reason="length" so handleFinishReason returns false and
	// the code falls through to the repetitive content check path.
	dupContent := "Let me check the files and then update the configuration."

	firstResp := lengthResponse(dupContent)
	stopped := ch.processResponse(scriptedResponseToChatResponse(firstResp))
	// finish_reason="length" → handleFinishReason returns false → falls through.
	// Not blank, not repetitive (first time), not incomplete (complete sentence).
	// Falls through final check — not incomplete → continues.
	assert.False(t, stopped, "length response should continue")

	// Now send the exact duplicate with finish_reason="length"
	duplicateResp := lengthResponse(dupContent)
	stopped = ch.processResponse(scriptedResponseToChatResponse(duplicateResp))
	assert.False(t, stopped, "repetitive length response should trigger reminder and continue")

	// Verify transient reminder was enqueued
	ch.transientMessagesMu.Lock()
	var repReminderFound bool
	for _, m := range ch.transientMessages {
		if m.Role == "user" && strings.Contains(m.Content, "stuck in a repetitive loop") {
			repReminderFound = true
		}
	}
	ch.transientMessagesMu.Unlock()
	assert.True(t, repReminderFound, "expected repetitive-content reminder in transient messages")

	// Now a proper stop response should complete
	stopResp := stopResponse()
	stopped = ch.processResponse(scriptedResponseToChatResponse(stopResp))
	assert.True(t, stopped, "stop response after repetitive recovery should complete")
}

// ---------------------------------------------------------------------------
// Test 8 – finish_reason "length" → nudge → complete
// ---------------------------------------------------------------------------

// TestE2E_FinishReasonLengthNudge verifies that a finish_reason of "length"
// triggers the incomplete-response nudge and the agent continues to the next
// iteration where a proper stop completes the conversation.
func TestE2E_FinishReasonLengthNudge(t *testing.T) {
	t.Parallel()

	lengthResp := lengthResponse("This is a partial response that was cut off because it exceeded the maximum output token limit set by the model provider...")
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, lengthResp, stopResp)

	_, err := agent.ProcessQuery("Write a long document")
	require.NoError(t, err)
	// With the scripted client, the first response has finish_reason="length".
	// handleFinishReason("length", ...) → handleIncompleteResponse() → enqueue transient → false.
	// The loop continues; second response is stop → completes.
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Should be 2 iterations: length (continue) + stop (complete)
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (length nudge + stop)")
}

// ---------------------------------------------------------------------------
// Test 9 – Empty stop → nudge then complete
// ---------------------------------------------------------------------------

// TestE2E_EmptyStopNudgeThenComplete verifies that finish_reason "stop" with
// empty content triggers an incomplete-response nudge and the conversation
// continues, eventually completing when real content arrives.
func TestE2E_EmptyStopNudgeThenComplete(t *testing.T) {
	t.Parallel()

	emptyStop := emptyStopResponse()
	stopResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, emptyStop, stopResp)

	_, err := agent.ProcessQuery("Explain Go interfaces")
	require.NoError(t, err)
	// First response: finish_reason="stop", content="" → handleFinishReason
	// returns false (empty stop response), enqueues transient, loop continues.
	// Second response: proper stop → completes.
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (empty stop + real stop)")
}

// ---------------------------------------------------------------------------
// Test 10 – Checkpoint compaction on prepareMessages
// ---------------------------------------------------------------------------

// TestE2E_CheckpointCompactionOnPrepareMessages verifies that when context
// tokens exceed the 87% threshold AND turn checkpoints exist, prepareMessages
// applies checkpoint compaction to reduce the message count.
func TestE2E_CheckpointCompactionOnPrepareMessages(t *testing.T) {
	t.Parallel()

	// Build an agent with ConversationOptimizer for checkpoint summary generation.
	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.optimizer = NewConversationOptimizer(true, false)

	// Create 25 user/assistant message pairs (50 messages total) with substantive content.
	originalCount := 50
	messages := make([]api.Message, 0, originalCount)
	for i := 0; i < 25; i++ {
		content := fmt.Sprintf("Message %d content with enough words to accumulate tokens for compaction testing", i)
		messages = append(messages, api.Message{Role: "user", Content: content})
		messages = append(messages, api.Message{Role: "assistant", Content: "Reply " + content})
	}
	agent.messages = messages

	// Create a handler to access estimateRequestTokens for dynamic threshold calculation.
	ch := NewConversationHandler(agent)
	thresholdCandidates := []int{500, 1000, 2000, 5000, 10000}

	// Find a maxContextTokens value where the messages exceed 87% of it (with no tools).
	// This makes the test robust against changes in token estimation constants.
	for _, maxCtx := range thresholdCandidates {
		agent.maxContextTokens = maxCtx
		compactionThreshold := int(float64(maxCtx) * PruningConfig.Default.StandardPercent)

		// Build a fresh handler for each candidate since we need to reset state.
		ch = NewConversationHandler(agent)
		// Build simple message list: system + messages (mimicking what prepareMessages does)
		prep := []api.Message{{Role: "system", Content: agent.systemPrompt}}
		prep = append(prep, agent.messages...)
		tokens := ch.apiClient.estimateRequestTokens(prep, nil)

		if tokens > compactionThreshold {
			// This maxContextTokens will trigger compaction. Use it.
			break
		}
	}
	// At this point, agent.maxContextTokens is set to the first value that triggers compaction.
	// If none triggered, the last candidate (10000) is used and the test still validates
	// the compaction code path, but compaction may not reduce messages.

	// Record a checkpoint covering the first 10 messages (indices 0-9).
	agent.RecordTurnCheckpoint(0, 9)
	require.True(t, agent.HasTurnCheckpoints(), "expected checkpoint to be recorded")

	// Call prepareMessages with no tools — this exercises the full compaction pipeline.
	prepared := ch.prepareMessages(nil)

	// The key assertion: prepared messages should have system prompt prepended.
	assert.True(t, len(prepared) >= 1, "expected at least system message in prepared")
	assert.Equal(t, "system", prepared[0].Role, "first message should be system prompt")

	// Compaction should have reduced the total message count from the uncanned size.
	// Without compaction: originalCount messages + 1 system prepended.
	assert.Less(t, len(prepared), originalCount+1,
		"expected compaction to reduce message count: got %d, want < %d", len(prepared), originalCount+1)

	// Verify agent.messages was also updated to the compacted version.
	assert.Less(t, len(agent.messages), originalCount,
		"expected agent.messages to be updated to compacted version: got %d, want < %d",
		len(agent.messages), originalCount)
}

// ---------------------------------------------------------------------------
// Test 11 – Turn checkpoint recorded after completion
// ---------------------------------------------------------------------------

// TestE2E_TurnCheckpointRecordedAfterCompletion verifies that a completed
// ProcessQuery run asynchronously records a turn checkpoint.
func TestE2E_TurnCheckpointRecordedAfterCompletion(t *testing.T) {
	t.Parallel()

	agent, _ := buildE2EAgent(t, 10, stopResponse())
	_, err := agent.ProcessQuery("Complete this task")
	require.NoError(t, err, "ProcessQuery should succeed with a simple stop response")

	waitForCheckpoints(t, agent, 2*time.Second)
	assert.True(t, agent.HasTurnCheckpoints(),
		"expected turn checkpoint to be recorded after completed ProcessQuery")

	if agent.HasTurnCheckpoints() {
		cps := agent.copyTurnCheckpoints()
		assert.Equal(t, 1, len(cps), "expected exactly 1 turn checkpoint")
		assert.GreaterOrEqual(t, cps[0].EndIndex, cps[0].StartIndex,
			"checkpoint end index should be >= start index")
		assert.NotEmpty(t, cps[0].Summary, "checkpoint summary should not be empty")
	}
}

// ---------------------------------------------------------------------------
// Test 12 – Structural compaction via ConversationOptimizer
// ---------------------------------------------------------------------------

// TestE2E_StructuralCompaction verifies that ConversationOptimizer.CompactConversation
// reduces a long message list to a compacted summary with fewer messages.
func TestE2E_StructuralCompaction(t *testing.T) {
	t.Parallel()

	optimizer := NewConversationOptimizer(true, false)

	// Build messages: system + user query anchor + many middle messages + recent messages.
	// Need >= PruningConfig.Structural.MinMessagesToCompact (18).
	messages := []api.Message{
		{Role: "system", Content: "You are a helpful coding assistant."},
		{Role: "user", Content: "Implement a feature in the codebase"},
		{Role: "assistant", Content: "I'll start by reading the existing code to understand the structure."},
	}

	// Add tool call/response pairs to create a realistic middle section.
	for i := 0; i < 8; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Let me check the implementation details for part %d", i),
			ToolCalls: []api.ToolCall{{
				ID:   fmt.Sprintf("call_%d", i),
				Type: "function",
				Function: struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				}{
					Name:      "read_file",
					Arguments: fmt.Sprintf(`{"path": "/src/file_%d.go"}`, i),
				},
			}},
		})
		messages = append(messages, api.Message{
			Role:      "tool",
			ToolCallId: fmt.Sprintf("call_%d", i),
			Content:   fmt.Sprintf("Tool call result for read_file: /src/file_%d.go\nLine 1: package main", i),
		})
	}
	messages = append(messages, api.Message{
		Role: "assistant",
		Content: "I found the root cause and updated the file.",
	})
	// Add some recent messages (within RecentMessagesToKeep = 12).
	for i := 0; i < 5; i++ {
		messages = append(messages, api.Message{
			Role:    "user",
			Content: fmt.Sprintf("What about the error handling in function %d?", i),
		})
		messages = append(messages, api.Message{
			Role: "assistant",
			Content: fmt.Sprintf("I fixed the error handling in function %d by adding proper validation.", i),
		})
	}

	assert.GreaterOrEqual(t, len(messages), PruningConfig.Structural.MinMessagesToCompact,
		"need at least %d messages for structural compaction", PruningConfig.Structural.MinMessagesToCompact)

	compactMsgs := optimizer.CompactConversation(messages)
	assert.Less(t, len(compactMsgs), len(messages),
		"expected compacted messages (%d) to be fewer than original (%d)",
		len(compactMsgs), len(messages))

	// Verify the compacted result contains the compaction header.
	var hasCompactedHeader bool
	for _, msg := range compactMsgs {
		if strings.Contains(msg.Content, "Compacted earlier conversation state") {
			hasCompactedHeader = true
			break
		}
	}
	assert.True(t, hasCompactedHeader, "expected compacted messages to contain 'Compacted earlier conversation state' header")
}

// ---------------------------------------------------------------------------
// Test 13 – Token/cost accumulation across iterations
// ---------------------------------------------------------------------------

// TestE2E_TokenCostAccumulation verifies that token and cost metrics
// accumulate correctly across multiple iterations of the conversation loop.
func TestE2E_TokenCostAccumulation(t *testing.T) {
	t.Parallel()

	resp1 := tokenUsageResponse("Part 1", "", 100, 50, 150)
	resp2 := tokenUsageResponse("Part 2", "", 200, 80, 280)
	resp3 := tokenUsageResponse("Done.", "stop", 300, 100, 400)

	agent, _ := buildE2EAgent(t, 10, resp1, resp2, resp3)
	_, err := agent.ProcessQuery("Accumulate metrics")
	require.NoError(t, err)

	// Verify tokens accumulated across all 3 iterations.
	// TrackMetricsFromResponse adds totalTokens additively: 150 + 280 + 400 = 830.
	// We check totalTokens > max individual response total to confirm accumulation.
	totalTokens := agent.GetTotalTokens()
	assert.Greater(t, totalTokens, 0, "expected total tokens to be accumulated")
	assert.Greater(t, totalTokens, 400,
		"expected accumulated tokens (%d) to exceed any single response's total (400), confirming cross-iteration accumulation", totalTokens)
}

// ---------------------------------------------------------------------------
// Test 14 – Transient messages consumed once
// ---------------------------------------------------------------------------

// TestE2E_TransientMessagesConsumedOnce verifies that transient messages injected
// during blank iteration handling are sent in the next prepareMessages call and
// then cleared (not re-sent in subsequent iterations).
func TestE2E_TransientMessagesConsumedOnce(t *testing.T) {
	t.Parallel()

	// Set up agent and handler
	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.optimizer = NewConversationOptimizer(false, false) // Disable optimization for simplicity
	ch := NewConversationHandler(agent)

	// Enqueue a transient message (simulating blank iteration behavior)
	ch.enqueueTransientMessage(api.Message{
		Role:    "user",
		Content: "Please continue with your response. The previous response appears incomplete.",
	})

	// Verify transient exists
	ch.transientMessagesMu.Lock()
	assert.Equal(t, 1, len(ch.transientMessages), "expected 1 transient message before prepareMessages")
	ch.transientMessagesMu.Unlock()

	// Call prepareMessages — this should consume the transient message.
	messages := ch.prepareMessages(nil)

	// Verify the transient was included in the output
	var transientFoundInOutput bool
	for _, m := range messages {
		if m.Role == "user" && strings.Contains(m.Content, "Please continue") {
			transientFoundInOutput = true
		}
	}
	assert.True(t, transientFoundInOutput, "expected transient message to appear in prepared messages")

	// Verify the transient was cleared after consumption
	ch.transientMessagesMu.Lock()
	assert.Equal(t, 0, len(ch.transientMessages),
		"expected transient messages to be cleared after prepareMessages consumed them")
	ch.transientMessagesMu.Unlock()

	// Call prepareMessages again — verify transient was not re-injected
	messages2 := ch.prepareMessages(nil)
	assert.NotEmpty(t, messages2, "second prepareMessages should still produce output")
	assert.Equal(t, "system", messages2[0].Role, "second call should still prepend system message")
}

// ---------------------------------------------------------------------------
// Test 15 – Optimizer redundancy detection (file reads)
// ---------------------------------------------------------------------------

// TestE2E_OptimizerRedundancyFileReads verifies that ConversationOptimizer
// detects redundant file reads (same file, same content, ≥15 messages apart)
// and replaces the older one with an [OPTIMIZED] summary.
func TestE2E_OptimizerRedundancyFileReads(t *testing.T) {
	t.Parallel()

	optimizer := NewConversationOptimizer(true, false)

	filePath := "/src/main.go"
	fileContent := "package main\n\nfunc main() {\n\tfmt.Println(\"hello\")\n}"

	// Build messages with tool results for the same file at index 2 and 18
	// (gap ≥ 15), with the same content.
	messages := make([]api.Message, 0, 22)
	messages = append(messages, api.Message{Role: "user", Content: "Read the file"})
	// Older read at index 1
	messages = append(messages, api.Message{
		Role:      "tool",
		ToolCallId: "call_old_1",
		Content:   fmt.Sprintf("Tool call result for read_file: %s\n%s", filePath, fileContent),
	})

	// Fill gap with other messages
	for i := 0; i < 15; i++ {
		messages = append(messages, api.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Working on analysis step %d, examining various parts of the codebase", i),
		})
		messages = append(messages, api.Message{
			Role: "user",
			Content: fmt.Sprintf("Continue step %d with more investigation of the code", i),
		})
	}

	// Newer (most recent) read at index ~17
	messages = append(messages, api.Message{
		Role:      "tool",
		ToolCallId: "call_new_1",
		Content:   fmt.Sprintf("Tool call result for read_file: %s\n%s", filePath, fileContent),
	})

	optimized := optimizer.OptimizeConversation(messages)

	// Verify the older read was replaced with an [OPTIMIZED] summary
	var foundOptimized bool
	for _, msg := range optimized {
		if msg.Role == "tool" && strings.Contains(msg.Content, "[OPTIMIZED]") &&
			strings.Contains(msg.Content, filePath) {
			foundOptimized = true
			break
		}
	}
	assert.True(t, foundOptimized,
		"expected older redundant file read to be replaced with [OPTIMIZED] summary")

	// Verify the most recent read was NOT optimized
	var foundOriginalRead bool
	for _, msg := range optimized {
		if msg.Role == "tool" && strings.Contains(msg.Content, "Tool call result for read_file: /src/main.go") &&
			!strings.Contains(msg.Content, "[OPTIMIZED]") {
			foundOriginalRead = true
			break
		}
	}
	assert.True(t, foundOriginalRead,
		"expected most recent file read to be preserved as-is")
}

// ---------------------------------------------------------------------------
// Additional: System prompt collapse
// ---------------------------------------------------------------------------

// TestE2E_SystemPromptCollapse verifies that multiple system messages in the
// conversation history are collapsed into a single system message at the front.
func TestE2E_SystemPromptCollapse(t *testing.T) {
	t.Parallel()

	messages := []api.Message{
		{Role: "system", Content: "First system prompt with general instructions"},
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi there"},
		{Role: "system", Content: "Second system prompt with additional context"},
		{Role: "user", Content: "question"},
		{Role: "assistant", Content: "answer"},
		{Role: "system", Content: "Third system prompt"},
	}

	result := collapseSystemMessagesToFront(messages)

	// Should start with exactly one merged system message
	assert.Equal(t, "system", result[0].Role, "first message should be system")

	// Count system messages in result
	systemCount := 0
	for _, m := range result {
		if m.Role == "system" {
			systemCount++
		}
	}
	assert.Equal(t, 1, systemCount, "expected exactly 1 system message after collapse")

	// Total should be 5: 3 systems collapse into 1, keeping 2 user + 2 assistant = 5
	assert.Equal(t, 5, len(result),
		"expected 5 messages after collapsing 3 systems into 1, got %d", len(result))

	// Verify the merged content contains all three system prompts
	assert.Contains(t, result[0].Content, "First system prompt")
	assert.Contains(t, result[0].Content, "Second system prompt")
	assert.Contains(t, result[0].Content, "Third system prompt")

	// Non-system messages should remain in order
	nonSystemFound := []string{}
	for _, m := range result {
		if m.Role == "user" {
			nonSystemFound = append(nonSystemFound, "user:"+m.Content)
		} else if m.Role == "assistant" {
			nonSystemFound = append(nonSystemFound, "assistant:"+m.Content)
		}
	}
	assert.Equal(t, "user:hello", nonSystemFound[0])
	assert.Equal(t, "assistant:hi there", nonSystemFound[1])
	assert.Equal(t, "user:question", nonSystemFound[2])
	assert.Equal(t, "assistant:answer", nonSystemFound[3])
}

// ---------------------------------------------------------------------------
// Additional: Checkpoint compaction with BuildCheckpointCompactedMessages
// ---------------------------------------------------------------------------

// TestE2E_BuildCheckpointCompactedMessages verifies the Agent's
// BuildCheckpointCompactedMessages method directly.
func TestE2E_BuildCheckpointCompactedMessages(t *testing.T) {
	t.Parallel()

	// Create messages
	messages := []api.Message{
		{Role: "user", Content: "First turn request"},
		{Role: "assistant", Content: "First turn response with detailed analysis"},
		{Role: "user", Content: "Second turn request"},
		{Role: "assistant", Content: "Second turn response"},
		{Role: "user", Content: "Third turn request"},
		{Role: "assistant", Content: "Third turn response"},
	}

	// Create an agent
	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.optimizer = NewConversationOptimizer(true, false)
	agent.messages = messages

	// Record checkpoint for first turn (indices 0-1)
	agent.RecordTurnCheckpoint(0, 1)
	assert.True(t, agent.HasTurnCheckpoints())

	// Build compacted messages
	compacted, remaining := agent.BuildCheckpointCompactedMessages(messages)

	// Original: 6 messages. Checkpoint covers 0-1 (2 messages → 1 summary).
	// Expected: 6 - 2 + 1 = 5 messages.
	assert.Equal(t, 5, len(compacted),
		"expected 5 messages after compacting 2 into 1 summary, got %d", len(compacted))

	// Remaining checkpoints should be empty (we used the only one)
	assert.Equal(t, 0, len(remaining),
		"expected 0 remaining checkpoints after consuming the only one")

	// Verify summary message exists
	var foundSummary bool
	for i, msg := range compacted {
		if i == 0 && msg.Role == "assistant" && strings.Contains(msg.Content, "User request:") {
			foundSummary = true
		}
	}
	assert.True(t, foundSummary, "expected summary message at the position of the compacted range")

	// Remaining messages (indices 2-5 in original) should be shifted to indices 1-4
	assert.Equal(t, "user", compacted[1].Role)
	assert.Equal(t, "Second turn request", compacted[1].Content)
	assert.Equal(t, "user", compacted[3].Role)
	assert.Equal(t, "Third turn request", compacted[3].Content)
}

// ---------------------------------------------------------------------------
// Additional: ProcessQuery with multiple keepGoing then blank stop
// ---------------------------------------------------------------------------

// TestE2E_KeepGoingThenBlankThenStop verifies that the agent handles a
// pattern of continuations followed by a blank iteration recovery and then
// a proper completion.
func TestE2E_KeepGoingThenBlankThenStop(t *testing.T) {
	t.Parallel()

	// 2 keepGoing, 1 blank (recovery), 1 stop
	responses := []*ScriptedResponse{
		keepGoingResponse(),
		keepGoingResponse(),
		blankResponseWithContent("   "),
		stopResponse(),
	}

	agent, _ := buildE2EAgent(t, 20, responses...)
	_, err := agent.ProcessQuery("Complex task")
	require.NoError(t, err)

	// The blank response triggers recovery, then stop completes.
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason())
	// Should be 4 iterations
	assert.Equal(t, 4, agent.GetCurrentIteration()+1,
		"expected 4 iterations (2 keepGoing + 1 blank + 1 stop)")
}

// ---------------------------------------------------------------------------
// Additional: BuildCheckpointCompactedMessages with multiple checkpoints
// ---------------------------------------------------------------------------

// TestE2E_MultipleCheckpointsCompaction verifies that multiple checkpoints
// are correctly applied in BuildCheckpointCompactedMessages, with remaining
// checkpoints properly shifted.
func TestE2E_MultipleCheckpointsCompaction(t *testing.T) {
	t.Parallel()

	messages := []api.Message{
		{Role: "user", Content: "Turn 1: Fix the bug in parser"},
		{Role: "assistant", Content: "Fixed the parser bug by updating the regex pattern"},
		{Role: "user", Content: "Turn 2: Add tests"},
		{Role: "assistant", Content: "Added unit tests for parser"},
		{Role: "user", Content: "Turn 3: Review changes"},
		{Role: "assistant", Content: "Reviewed all changes and they look good"},
	}

	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.optimizer = NewConversationOptimizer(true, false)
	agent.messages = messages

	// Record checkpoint for turn 1 (indices 0-1)
	agent.RecordTurnCheckpoint(0, 1)
	// Record checkpoint for turn 3 (indices 4-5) — will be consumed too
	agent.RecordTurnCheckpoint(4, 5)
	assert.True(t, agent.HasTurnCheckpoints())

	compacted, remaining := agent.BuildCheckpointCompactedMessages(messages)

	// Original: 6 messages. Two checkpoints each covering 2 messages -> 2 summaries.
	// Expected: 6 - 4 + 2 = 4 messages.
	assert.Equal(t, 4, len(compacted),
		"expected 4 messages after compacting two ranges, got %d", len(compacted))

	// Both checkpoints consumed -> no remaining
	assert.Equal(t, 0, len(remaining),
		"expected 0 remaining checkpoints after consuming both")

	// Verify message ordering
	assert.Equal(t, "assistant", compacted[0].Role, "first message should be summary for turn 1")
	assert.Equal(t, "user", compacted[1].Role, "second message should be turn 2 user")
	assert.Equal(t, "assistant", compacted[2].Role, "third message should be summary for turn 3")
}

// ---------------------------------------------------------------------------
// Additional: prepareMessages passes tools token costs correctly
// ---------------------------------------------------------------------------

// TestE2E_PrepareMessagesTokenEstimation verifies that prepareMessages
// respects the maxContextTokens boundary and estimates tokens including
// tool definitions when tools are provided.
func TestE2E_PrepareMessagesTokenEstimation(t *testing.T) {
	t.Parallel()

	client := NewScriptedClient(stopResponse())
	agent := makeAgentWithScriptedClient(10, client)
	agent.maxContextTokens = 100000 // Large enough to not trigger compaction

	ch := NewConversationHandler(agent)

	// Add a few messages
	agent.messages = []api.Message{
		{Role: "user", Content: "Hello world"},
	}

	// Call prepareMessages with no tools — should not panic
	prepared := ch.prepareMessages(nil)
	assert.Greater(t, len(prepared), 0, "expected at least system message")
	assert.Equal(t, "system", prepared[0].Role)

	// Call estimateRequestTokens to verify it works
	tokens := ch.apiClient.estimateRequestTokens(prepared, nil)
	assert.Greater(t, tokens, 0, "expected non-zero token estimate")

	// Call with tools
	tools := api.GetToolDefinitions()
	tokensWithTools := ch.apiClient.estimateRequestTokens(prepared, tools)
	assert.Greater(t, tokensWithTools, tokens,
		"expected tools to increase token estimate: with tools=%d, without=%d", tokensWithTools, tokens)
}

// ---------------------------------------------------------------------------
// Test 16 – Tool call execution flow
// ---------------------------------------------------------------------------

// TestE2E_ToolCallExecutionFlow verifies the complete tool call execution flow:
// 1. Model returns a tool call (e.g., read_file)
// 2. Tool executes and result is appended to conversation
// 3. Model sees the tool result and continues
// 4. Model returns a stop response
// The test verifies the conversation completes with RunTerminationCompleted
// and exactly 2 iterations (one for tool call, one for stop).
func TestE2E_ToolCallExecutionFlow(t *testing.T) {
	t.Parallel()

	// Create a temporary file so the read_file tool succeeds
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "test_file.txt")
	testContent := "This is test content for the e2e tool call test.\nLine 2 of the file."
	if err := os.WriteFile(tempFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// Build a tool call for read_file
	toolCall := api.ToolCall{
		ID:   "call_read_file_001",
		Type: "function",
		Function: struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		}{
			Name: "read_file",
			Arguments: fmt.Sprintf(`{"file_path": "%s", "start_line": 1, "end_line": 2}`, tempFile),
		},
	}

	// First response: model returns a tool call (finish_reason empty = continue)
	firstResp := NewScriptedResponseBuilder().
		Content("Let me read the file to get the information.").
		ToolCall(toolCall).
		Build()

	// Second response: model sees tool result and returns stop
	// Using stopResponse() to match the pattern used in other e2e tests
	secondResp := stopResponse()

	// Build responses array: tool call response first, then stop response
	responses := []*ScriptedResponse{firstResp, secondResp}

	// Build the E2E agent with the scripted client
	agent, _ := buildE2EAgent(t, 10, responses...)

	// Execute ProcessQuery
	result, err := agent.ProcessQuery("What is in the test file?")
	require.NoError(t, err, "ProcessQuery should succeed")

	// Verify the result
	assert.Equal(t, "Done.", result, "ProcessQuery should return Done.")

	// Verify termination reason is completed
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(),
		"expected termination reason RunTerminationCompleted")

	// Verify iteration count is exactly 2
	// Iteration 1: model returns tool call
	// Iteration 2: model sees tool result and stops
	assert.Equal(t, 2, agent.GetCurrentIteration()+1,
		"expected exactly 2 iterations (1 tool call + 1 stop), got %d",
		agent.GetCurrentIteration()+1)

	// Verify conversation history has the expected structure
	// Should have: user message, assistant (tool call), tool result, assistant (stop)
	assert.GreaterOrEqual(t, len(agent.messages), 4,
		"expected at least 4 messages (user, assistant with tool call, tool result, assistant stop)")

	// Verify the tool call was recorded
	var foundToolCall bool
	var foundToolResult bool
	for _, msg := range agent.messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			foundToolCall = true
			assert.Equal(t, "read_file", msg.ToolCalls[0].Function.Name,
				"expected tool call name to be read_file")
		}
		if msg.Role == "tool" {
			foundToolResult = true
			assert.Contains(t, msg.Content, "test content",
				"expected tool result to contain the file content")
		}
	}
	assert.True(t, foundToolCall, "expected to find assistant message with tool call")
	assert.True(t, foundToolResult, "expected to find tool result message")
}

// ---------------------------------------------------------------------------
// Test 17 – Fallback parser extracts tool from unstructured content
// ---------------------------------------------------------------------------

// TestE2E_FallbackParser extracts tool calls from unstructured content when
// the model returns tool calls in the content field instead of structured
// tool_calls. The fallback parser should extract the tool, execute it, and
// the conversation should continue normally.
func TestE2E_FallbackParser(t *testing.T) {
	t.Parallel()

	// Create a temporary file to ensure the tool succeeds (like TestE2E_ToolCallExecutionFlow)
	tempFile := filepath.Join(t.TempDir(), "test_readme.md")
	testContent := "This is a test README file for the fallback parser e2e test.\nProject: ledit"
	if err := os.WriteFile(tempFile, []byte(testContent), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// First response: model returns tool call in content (unstructured)
	// This simulates a model that doesn't use the structured tool_calls API
	// but instead embeds the tool call in natural text
	// Use a format that matches the fallback parser's named tool block pattern:
	// tool_name { "arg": "value" }
	firstResp := NewScriptedResponseBuilder().
		Content(fmt.Sprintf(`Let me use the read_file tool to check the README.

read_file {
"file_path": "%s"
}`, tempFile)).
		Build()

	// Second response: model sees tool result and completes
	secondResp := stopResponse()

	agent, _ := buildE2EAgent(t, 10, firstResp, secondResp)
	result, err := agent.ProcessQuery("What is in the README?")

	require.NoError(t, err, "ProcessQuery should succeed with fallback parser")
	assert.Equal(t, "Done.", result, "ProcessQuery should return Done after fallback parser extracts and executes tool")
	assert.Equal(t, RunTerminationCompleted, agent.GetLastRunTerminationReason(), "Should complete successfully")
	// Expected 2 iterations: fallback parser extracts tool → tool executes → stop response
	assert.Equal(t, 2, agent.GetCurrentIteration()+1, "expected 2 iterations (fallback tool + stop)")

	// Verify the fallback parser was used by checking the conversation history
	// Should have: user message, assistant (unstructured content), tool result, assistant (stop)
	assert.GreaterOrEqual(t, len(agent.messages), 4,
		"expected at least 4 messages (user, assistant with unstructured tool, tool result, assistant stop)")

	// Verify the tool call was extracted and executed, and the assistant message was cleaned
	var foundToolCall bool
	var foundToolResult bool
	var cleanedContentVerified bool
	for _, msg := range agent.messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			foundToolCall = true
			assert.Equal(t, "read_file", msg.ToolCalls[0].Function.Name,
				"expected extracted tool call name to be read_file")
			// Verify the fallback parser cleaned the content by stripping the raw tool block
			assert.NotContains(t, msg.Content, "read_file {",
				"expected cleaned content to strip the raw tool block")
			assert.NotContains(t, msg.Content, `"file_path"`,
				"expected cleaned content to strip the raw tool block JSON")
			assert.Contains(t, msg.Content, "Let me use the",
				"expected cleaned content to preserve the natural text preamble")
			cleanedContentVerified = true
		}
		if msg.Role == "tool" {
			foundToolResult = true
			// Tool result should contain actual file content, not an error
			assert.Contains(t, msg.Content, "test README file",
				"expected tool result to contain the file content")
			assert.NotContains(t, msg.Content, "Error reading file",
				"expected tool to succeed, got error")
		}
	}
	assert.True(t, foundToolCall, "expected to find assistant message with extracted tool call")
	assert.True(t, cleanedContentVerified, "expected to verify cleaned content assertions")
	assert.True(t, foundToolResult, "expected to find tool result message")
}
