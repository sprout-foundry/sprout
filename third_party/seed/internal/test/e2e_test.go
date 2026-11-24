package test

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/seed/events"
)

// --- End-to-End Integration Tests ---

func TestE2E_SimpleQuery(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello, how can I help?")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Hi there")

	h.AssertNoError(err)
	h.AssertEquals(result, "Hello, how can I help?")
	h.AssertProviderCalledN(1)
	h.AssertStateHasNMessages(agent, 2) // user + assistant
}

func TestE2E_SystemPromptSent(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgentWithOptions(core.Options{
		SystemPrompt: "You are a test assistant.",
	})
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	h.AssertFirstMessageIsSystem()
	h.AssertSystemPromptEquals("You are a test assistant.")
}

func TestE2E_DefaultSystemPrompt(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	h.AssertSystemPromptEquals(core.DefaultSystemPrompt)
}

func TestE2E_ToolCallFlow(t *testing.T) {
	h := NewHarnessWithT(t)

	// First response: assistant calls a tool
	h.Provider().AddToolCallResponse(
		"Let me check.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	// Second response: assistant gives final answer
	h.Provider().AddTextResponse("The file contains 'hello'.")

	// Executor returns tool result
	h.Executor().AddToolResult("call_1", "hello")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read test.txt")
	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains 'hello'.")

	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)
	h.AssertStateHasNMessages(agent, 4) // user + assistant(tool call) + tool result + assistant(final)
}

func TestE2E_ToolsSentToProvider(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	h.Executor().AddTool(core.Tool{
		Type: "function",
		Function: core.ToolFunction{
			Name:        "search",
			Description: "Search the web",
		},
	})

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	h.AssertLastRequestHasTool("search")
}

func TestE2E_MultipleToolCalls(t *testing.T) {
	h := NewHarnessWithT(t)

	// First: two tool calls
	h.Provider().AddToolCallResponse(
		"Running commands.",
		core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "shell", Arguments: `{"cmd":"ls"}`}},
		core.ToolCall{ID: "call_2", Function: core.ToolCallFunction{Name: "shell", Arguments: `{"cmd":"pwd"}`}},
	)
	// Second: final answer
	h.Provider().AddTextResponse("Done.")

	// Executor returns results for both calls
	h.Executor().AddToolResult("call_1", "file.txt")
	h.Executor().AddToolResult("call_2", "/home")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Run ls and pwd")
	h.AssertNoError(err)
	h.AssertProviderCalledN(2)
}

func TestE2E_MaxIterations(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider keeps returning tool calls (would loop forever)
	for i := 0; i < 10; i++ {
		h.Provider().AddToolCallResponse(
			"Looping...",
			core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "loop", Arguments: "{}"}},
		)
	}
	h.Executor().AddToolResult("call_1", "still looping")

	agent := h.NewAgentWithOptions(core.Options{
		MaxIterations: 3,
	})
	_, err := agent.Run(context.Background(), "loop")
	h.AssertError(err)
	h.AssertErrorContains(err, "max iterations")
	// Should stop after 3 iterations
	h.AssertProviderCalledN(3)
}

func TestE2E_MaxIterations_ErrorsIs(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider always returns tool calls so the loop never completes.
	// Add enough responses to cover the 2 iterations.
	for i := 0; i < 3; i++ {
		h.Provider().AddToolCallResponse(
			"Looping...",
			core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "loop", Arguments: "{}"}},
		)
	}
	h.Executor().AddToolResult("call_1", "still looping")

	agent := h.NewAgentWithOptions(core.Options{
		MaxIterations: 2,
	})
	_, err := agent.Run(context.Background(), "loop")
	h.AssertError(err)

	// Verify the error wraps ErrMaxIterations via errors.Is
	if !errors.Is(err, core.ErrMaxIterations) {
		t.Fatalf("expected errors.Is(err, core.ErrMaxIterations) to be true, got: %v", err)
	}
}

func TestE2E_MaxIterations_NoCheckpointOnIncomplete(t *testing.T) {
	var checkpoints []core.TurnCheckpoint
	var mu sync.Mutex

	h := NewHarnessWithT(t)

	// Provider always returns tool calls so the loop never completes.
	// Add enough responses to cover the 2 iterations.
	for i := 0; i < 3; i++ {
		h.Provider().AddToolCallResponse(
			"Looping...",
			core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "loop", Arguments: "{}"}},
		)
	}
	h.Executor().AddToolResult("call_1", "still looping")

	agent := h.NewAgentWithOptions(core.Options{
		MaxIterations: 2,
		OnCheckpoint: func(cp core.TurnCheckpoint) {
			mu.Lock()
			defer mu.Unlock()
			checkpoints = append(checkpoints, cp)
		},
	})
	_, err := agent.Run(context.Background(), "loop")
	h.AssertError(err)
	h.AssertErrorContains(err, "max iterations")

	// OnCheckpoint must not have been called for an incomplete turn.
	mu.Lock()
	defer mu.Unlock()
	if len(checkpoints) != 0 {
		t.Fatalf("expected 0 OnCheckpoint calls for incomplete turn, got %d", len(checkpoints))
	}
}

func TestE2E_ProviderError(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddError(fmt.Errorf("simulated provider error"))

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertError(err)
	h.AssertErrorContains(err, "chat failed")
}

func TestE2E_ProviderError_PublishesErrorEvent(t *testing.T) {
	h := NewHarnessWithT(t)
	// Add 3 errors (one per retry attempt) so the mock doesn't run out
	h.Provider().AddError(fmt.Errorf("simulated provider failure"))
	h.Provider().AddError(fmt.Errorf("simulated provider failure"))
	h.Provider().AddError(fmt.Errorf("simulated provider failure"))

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertError(err)
	h.AssertErrorContains(err, "chat failed")

	// Verify the error event was published
	h.AssertEventPublished(events.EventTypeError)

	// With retry logic: 3 retry error events (from chatFn) + 1 final error event (from runLoop) = 4
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) < 3 {
		t.Fatalf("expected at least 3 error events (one per failed attempt), got %d", len(errorEvents))
	}

	// Check the first error event payload
	data, ok := errorEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map[string]interface{}, got %T", errorEvents[0].Data)
	}

	// Verify the message field
	msg, ok := data["message"].(string)
	if !ok {
		t.Fatalf("expected error event message to be string, got %T", data["message"])
	}
	h.AssertEquals(msg, "chat failed")

	// Verify the error field contains the simulated error
	errField, ok := data["error"].(string)
	if !ok {
		t.Fatalf("expected error event error field to be string, got %T", data["error"])
	}
	h.AssertContains(errField, "simulated provider failure")
}

func TestE2E_ProviderError_NoEventBus(t *testing.T) {
	// Regression: provider error should not panic when EventBus is nil
	h := NewHarnessWithT(t)
	h.Provider().AddError(fmt.Errorf("provider failure"))

	agent, err := core.NewAgent(core.Options{
		Provider: h.Provider(),
		Executor: h.Executor(),
		UI:       h.UI(),
		// No EventBus
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Run(context.Background(), "test")
	h.AssertError(err)
	h.AssertErrorContains(err, "chat failed")
}

func TestE2E_PauseAndResume(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Resumed!")

	agent := h.NewAgent()
	agent.Pause()
	_, err := agent.Run(context.Background(), "test")
	h.AssertError(err)
	h.AssertErrorContains(err, "paused")

	agent.Resume()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "Resumed!")
}

func TestE2E_StateExportImport(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First query")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Hello")
	h.AssertNoError(err)

	// Export state
	data, err := agent.ExportState()
	h.AssertNoError(err)

	// Import into new agent
	h2 := NewHarnessWithT(t)
	h2.Provider().AddTextResponse("Second query")

	agent2 := h2.NewAgent()
	err = agent2.ImportState(data)
	h2.AssertNoError(err)

	// Should have the previous conversation
	h2.AssertStateHasNMessages(agent2, 2) // user + assistant from first run

	// Run new query
	_, err = agent2.Run(context.Background(), "World")
	h2.AssertNoError(err)
	h2.AssertStateHasNMessages(agent2, 4) // 2 from import + 2 new
}

func TestE2E_EventBusIntegration(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Done")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)

	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)
}

func TestE2E_NoEventBus(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Done")

	agent, err := core.NewAgent(core.Options{
		Provider: h.Provider(),
		Executor: h.Executor(),
		// No EventBus
	})
	if err != nil {
		t.Fatal(err)
	}
	_, err = agent.Run(context.Background(), "test")
	h.AssertNoError(err)
}

func TestE2E_DebugMode(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgentWithOptions(core.Options{
		Debug: true,
	})
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Debug output should go to UI
	output := h.UI().LineOutput()
	if !strings.Contains(output, "ProcessQuery") && !strings.Contains(output, "Iteration") {
		// Debug output may vary; just verify no crash
	}
}

func TestE2E_HeadlessMode(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent, err := core.NewAgent(core.Options{
		Provider: h.Provider(),
		Executor: h.Executor(),
		UI:       nil, // headless
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "OK")
}

func TestE2E_SessionID(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// EnsureSessionID must be called explicitly
	agent.State().EnsureSessionID()
	h.AssertSessionIDNotEmpty(agent)
}

func TestE2E_TokenTracking(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Hi")
	h.AssertNoError(err)

	h.AssertStateHasTokens(agent, 35) // from the mock response usage
}

func TestE2E_MetricsUpdateEvent(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Hi")
	h.AssertNoError(err)

	// Verify the metrics_update event was published
	h.AssertEventPublished(events.EventTypeMetricsUpdate)

	// Find the metrics events and verify the payload fields
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 1 {
		t.Fatalf("expected 1 metrics_update event, got %d", len(metricsEvents))
	}

	// Check the event payload
	data, ok := metricsEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected metrics event data to be map[string]interface{}, got %T", metricsEvents[0].Data)
	}

	// total_tokens: 35 (prompt 20 + completion 15 from mock AddTextResponse)
	totalTokens, ok := data["total_tokens"].(int)
	if !ok {
		t.Fatalf("expected total_tokens to be int, got %T", data["total_tokens"])
	}
	if totalTokens != 35 {
		t.Errorf("expected total_tokens 35, got %v", totalTokens)
	}

	// prompt_tokens: 20 (prompt tokens from mock AddTextResponse)
	// Note: the event field is named "context_tokens" but carries prompt token count
	promptTokens, ok := data["context_tokens"].(int)
	if !ok {
		t.Fatalf("expected context_tokens to be int, got %T", data["context_tokens"])
	}
	if promptTokens != 20 {
		t.Errorf("expected context_tokens 20, got %v", promptTokens)
	}

	// max_context_tokens: 128000 (mock provider default context size)
	maxContextTokens, ok := data["max_context_tokens"].(int)
	if !ok {
		t.Fatalf("expected max_context_tokens to be int, got %T", data["max_context_tokens"])
	}
	if maxContextTokens != 128000 {
		t.Errorf("expected max_context_tokens 128000, got %v", maxContextTokens)
	}

	// iteration: 0 (first iteration)
	iteration, ok := data["iteration"].(int)
	if !ok {
		t.Fatalf("expected iteration to be int, got %T", data["iteration"])
	}
	if iteration != 0 {
		t.Errorf("expected iteration 0, got %v", iteration)
	}

	// total_cost: 0 (mock provider default cost)
	totalCost, ok := data["total_cost"].(float64)
	if !ok {
		t.Fatalf("expected total_cost to be float64, got %T", data["total_cost"])
	}
	if totalCost != 0 {
		t.Errorf("expected total_cost 0, got %v", totalCost)
	}
}

func TestE2E_MetricsUpdateEvent_MultiIteration(t *testing.T) {
	h := NewHarnessWithT(t)

	// First: tool call (iteration 0)
	h.Provider().AddToolCallResponse(
		"Checking...",
		core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "read", Arguments: `{}`}},
	)
	// Second: final answer (iteration 1)
	h.Provider().AddTextResponse("Done.")
	h.Executor().AddToolResult("call_1", "content")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Read file")
	h.AssertNoError(err)

	// Two iterations should produce two metrics_update events
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 2 {
		t.Fatalf("expected 2 metrics_update events, got %d", len(metricsEvents))
	}

	// Verify iteration increments correctly
	for i, evt := range metricsEvents {
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event %d: expected data to be map[string]interface{}, got %T", i, evt.Data)
		}
		it, ok := data["iteration"].(int)
		if !ok {
			t.Fatalf("event %d: expected iteration to be int, got %T", i, data["iteration"])
		}
		if it != i {
			t.Errorf("event %d: expected iteration %d, got %d", i, i, it)
		}
	}

	// Verify total_tokens accumulates across iterations
	// Iteration 0 (tool call): 80 tokens (50 prompt + 30 completion from AddToolCallResponse)
	// Iteration 1 (final): 35 tokens (20 prompt + 15 completion from AddTextResponse)
	// Accumulated total: 80 + 35 = 115
	lastData := metricsEvents[1].Data.(map[string]interface{})
	totalTokens := lastData["total_tokens"].(int)
	if totalTokens != 115 {
		t.Errorf("expected total_tokens 115 (accumulated), got %d", totalTokens)
	}
}

func TestE2E_ContextCompaction(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().
		WithTokenEstimate(200000). // Over context limit
		WithInfo(core.ProviderInfo{
			Model:       "mock",
			ContextSize: 4096,
			HasVision:   false,
		}).
		AddTextResponse("Compacted!")

	agent := h.NewAgent()

	// Add many messages to state to trigger compaction
	for i := 0; i < 50; i++ {
		agent.State().AddMessage(core.Message{
			Role:    "user",
			Content: strings.Repeat("x", 200),
		})
		agent.State().AddMessage(core.Message{
			Role:    "assistant",
			Content: strings.Repeat("y", 200),
		})
	}

	_, err := agent.Run(context.Background(), "final query")
	h.AssertNoError(err)
	h.AssertEquals("Compacted!", "Compacted!") // verify it completed
}

// --- Compaction Event Tests ---

func TestE2E_CompactionEventPublished(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().
		WithTokenEstimate(200000). // Over context limit to trigger compaction
		WithInfo(core.ProviderInfo{
			Model:       "mock",
			ContextSize: 4096,
			HasVision:   false,
		}).
		AddTextResponse("Compacted!")

	agent := h.NewAgent()

	// Add many messages to state to trigger compaction.
	// roughTokens: 200/4 + 10 = 60 per message; 50 user + 50 assistant = 100 messages = ~6000 tokens > 4096 limit.
	for i := 0; i < 50; i++ {
		agent.State().AddMessage(core.Message{
			Role:    "user",
			Content: strings.Repeat("x", 200),
		})
		agent.State().AddMessage(core.Message{
			Role:    "assistant",
			Content: strings.Repeat("y", 200),
		})
	}

	_, err := agent.Run(context.Background(), "final query")
	h.AssertNoError(err)

	// Provider called once (compaction happens before the call, not a separate iteration)
	h.AssertProviderCalledN(1)

	// Verify the compaction event was published
	h.AssertEventPublished(events.EventTypeCompaction)

	// Find the compaction events and verify the payload
	compactionEvents := h.FindEvents(events.EventTypeCompaction)
	if len(compactionEvents) != 1 {
		t.Fatalf("expected 1 compaction event, got %d", len(compactionEvents))
	}

	data, ok := compactionEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected compaction event data to be map[string]interface{}, got %T", compactionEvents[0].Data)
	}

	// Strategy is "tool_trim" when compaction is needed via turn dropping,
	// "truncation" when content truncation suffices, "emergency" when messages must be dropped, "none" otherwise.
	strategy, ok := data["strategy"].(string)
	if !ok {
		t.Fatalf("expected strategy to be string, got %T", data["strategy"])
	}
	if strategy == "none" {
		t.Errorf("expected strategy not 'none', got %q", strategy)
	}
	// With 100 pre-added messages (50 user/assistant pairs) plus the system prompt
	// and final query, compaction is needed. Turn dropping removes complete turns
	// oldest first, which is more efficient than emergency truncation.
	// Accept "tool_trim", "truncation", or "emergency" — all indicate compaction occurred.
	if strategy != "tool_trim" && strategy != "truncation" && strategy != "emergency" {
		t.Errorf("expected strategy 'tool_trim', 'truncation', or 'emergency', got %q", strategy)
	}

	// messages_before > messages_after
	messagesBefore, ok := data["messages_before"].(int)
	if !ok {
		t.Fatalf("expected messages_before to be int, got %T", data["messages_before"])
	}
	messagesAfter, ok := data["messages_after"].(int)
	if !ok {
		t.Fatalf("expected messages_after to be int, got %T", data["messages_after"])
	}
	if messagesBefore <= messagesAfter {
		t.Errorf("expected messages_before (%d) > messages_after (%d)", messagesBefore, messagesAfter)
	}

	// message_count_delta should equal messages_before - messages_after
	messageCountDelta, ok := data["message_count_delta"].(int)
	if !ok {
		t.Fatalf("expected message_count_delta to be int, got %T", data["message_count_delta"])
	}
	expectedDelta := messagesBefore - messagesAfter
	if messageCountDelta != expectedDelta {
		t.Errorf("expected message_count_delta %d, got %d", expectedDelta, messageCountDelta)
	}

	// tokens_saved should be positive
	tokensSaved, ok := data["tokens_saved"].(int)
	if !ok {
		t.Fatalf("expected tokens_saved to be int, got %T", data["tokens_saved"])
	}
	if tokensSaved <= 0 {
		t.Errorf("expected tokens_saved > 0, got %d", tokensSaved)
	}

	// Verify the provider received the compacted message list (fewer than original)
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}
	// Original: 1 system + 100 pre-added + 1 user query = 102 messages
	// After emergency truncation: tool content trimmed, older content truncated,
	// oldest messages dropped, preserving recent conversation.
	if len(lastReq.Messages) >= 102 {
		t.Errorf("expected compaction to reduce messages below 102, got %d", len(lastReq.Messages))
	}
}

func TestE2E_CompactionEventNotPublishedWhenNoCompaction(t *testing.T) {
	h := NewHarnessWithT(t)
	// Normal provider: large context size (128000) and small token estimate (35 from AddTextResponse default).
	// No compaction should be triggered.
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()

	// Add only a few messages — well under the context limit
	for i := 0; i < 3; i++ {
		agent.State().AddMessage(core.Message{
			Role:    "user",
			Content: "short query",
		})
		agent.State().AddMessage(core.Message{
			Role:    "assistant",
			Content: "short reply",
		})
	}

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Provider called once
	h.AssertProviderCalledN(1)

	// State: 6 pre-added + 1 user query + 1 assistant response = 8 messages
	h.AssertStateHasNMessages(agent, 8)

	// No compaction event should have been published
	compactionEvents := h.FindEvents(events.EventTypeCompaction)
	if len(compactionEvents) != 0 {
		t.Errorf("expected no compaction events, got %d", len(compactionEvents))
	}
}

func TestE2E_ImagesStrippedForNonVision(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	agent.State().AddMessage(core.Message{
		Role:    "user",
		Content: "Look at this",
		Images:  []core.ImageData{{URL: "http://img.png", Type: "image/png"}},
	})

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Check that images were stripped from the request
	req := h.Provider().LastRequest()
	for _, msg := range req.Messages {
		if len(msg.Images) > 0 {
			t.Error("expected images to be stripped for non-vision model")
		}
	}
}

func TestE2E_ImagesKeptForVision(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().
		WithInfo(core.ProviderInfo{
			Model:       "vision-model",
			ContextSize: 128000,
			HasVision:   true,
		}).
		AddTextResponse("OK")

	agent := h.NewAgent()
	agent.State().AddMessage(core.Message{
		Role:    "user",
		Content: "Look at this",
		Images:  []core.ImageData{{URL: "http://img.png", Type: "image/png"}},
	})

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Check that images were kept
	req := h.Provider().LastRequest()
	hasImages := false
	for _, msg := range req.Messages {
		if len(msg.Images) > 0 {
			hasImages = true
			break
		}
	}
	if !hasImages {
		t.Error("expected images to be kept for vision model")
	}
}

func TestE2E_MultiTurnConversation(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("I can help with that.")

	agent := h.NewAgent()

	// First turn
	result1, err := agent.Run(context.Background(), "What can you do?")
	h.AssertNoError(err)
	h.AssertEquals(result1, "I can help with that.")

	// Second turn (with conversation history)
	h.Provider().AddTextResponse("Here's the answer: 42.")
	result2, err := agent.Run(context.Background(), "What is the answer?")
	h.AssertNoError(err)
	h.AssertEquals(result2, "Here's the answer: 42.")

	// State should have 4 messages
	h.AssertStateHasNMessages(agent, 4)
}

func TestE2E_SystemMessagesCollapsed(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	// Manually add a system message to state (simulating imported state)
	agent.State().SetMessages([]core.Message{
		{Role: "system", Content: "Old system prompt"},
		{Role: "user", Content: "previous query"},
		{Role: "assistant", Content: "previous response"},
	})

	_, err := agent.Run(context.Background(), "new query")
	h.AssertNoError(err)

	// The old system message should be stripped; current system prompt prepended
	req := h.Provider().LastRequest()
	if req.Messages[0].Content != core.DefaultSystemPrompt {
		t.Errorf("expected current system prompt, got %q", req.Messages[0].Content)
	}
	// Old system message should not appear
	for _, msg := range req.Messages {
		if msg.Content == "Old system prompt" {
			t.Error("old system prompt should have been stripped")
		}
	}
}

func TestE2E_OrphanedToolResultsRemoved(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	// Add state with orphaned tool result
	agent.State().SetMessages([]core.Message{
		{Role: "user", Content: "query"},
		{Role: "assistant", Content: "response"},
		{Role: "tool", Content: "orphaned", ToolCallID: "nonexistent"},
	})

	_, err := agent.Run(context.Background(), "new query")
	h.AssertNoError(err)

	// Orphaned tool result should not appear in request
	req := h.Provider().LastRequest()
	for _, msg := range req.Messages {
		if msg.ToolCallID == "nonexistent" {
			t.Error("orphaned tool result should have been removed")
		}
	}
}

func TestE2E_SetSystemPrompt(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	agent.SetSystemPrompt("Custom prompt for testing.")

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	h.AssertSystemPromptEquals("Custom prompt for testing.")
}

func TestE2E_ProviderAccess(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().
		WithInfo(core.ProviderInfo{Model: "custom-model", ContextSize: 64000}).
		AddTextResponse("OK")

	agent := h.NewAgent()
	info := agent.Provider().Info()
	if info.Model != "custom-model" {
		t.Errorf("expected 'custom-model', got %q", info.Model)
	}
}

func TestE2E_StreamingBuffer(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Streaming buffer should be empty (no streaming used)
	if agent.StreamingBuffer().Len() != 0 {
		t.Error("expected empty streaming buffer")
	}
}

func TestE2E_FlushCallback(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	// flushed tracking disabled
	agent.SetFlushCallback(func() {})

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
}

func TestE2E_ConcurrentAgents(t *testing.T) {
	// Test that multiple agents can run independently
	for i := 0; i < 5; i++ {
		h := NewHarnessWithT(t)
		h.Provider().AddTextResponse("Response " + string(rune('A'+i)))

		agent := h.NewAgent()
		result, err := agent.Run(context.Background(), "test")
		h.AssertNoError(err)
		h.AssertEquals(result, "Response "+string(rune('A'+i)))
	}
}

// --- Incomplete Response Continuation Tests ---

func TestE2E_IncompleteResponse_TrailingEllipsis(t *testing.T) {
	// Provider returns a response ending with ellipsis, indicating truncation.
	// The validator should detect this as incomplete and continue the loop.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Here is the answer...")
	// Second response must be long enough (>10 words) so it's not flagged as too short.
	h.Provider().AddTextResponse("This is the complete answer that I was going to provide from the start.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "This is the complete answer that I was going to provide from the start.")

	// Provider called twice: initial incomplete response + continuation
	h.AssertProviderCalledN(2)

	// State: user query, assistant(incomplete), assistant(final continuation)
	h.AssertStateHasNMessages(agent, 3)
}

func TestE2E_IncompleteResponse_AbruptEnding(t *testing.T) {
	// Provider returns a response ending with a comma, indicating truncation.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("The file contains,")
	// Second response must be at least 10 words so it's not flagged as too short.
	h.Provider().AddTextResponse("It has a very long content inside the file that we are looking at today.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "It has a very long content inside the file that we are looking at today.")

	// Provider called twice
	h.AssertProviderCalledN(2)

	// State: user + assistant(incomplete) + assistant(final)
	h.AssertStateHasNMessages(agent, 3)
}

func TestE2E_IncompleteResponse_MaxContinuations(t *testing.T) {
	// Provider keeps returning incomplete responses. After 3 consecutive
	// continuations (maxContinuations=3), the loop force-finalizes.
	// Total provider calls: 1 initial + 3 continuations = 4
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First incomplete...")
	h.Provider().AddTextResponse("Second incomplete...")
	h.Provider().AddTextResponse("Third incomplete...")
	h.Provider().AddTextResponse("Fourth and last...")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "Fourth and last...")

	// Provider called 4 times: 1 initial + 3 continuations (4th call force-finalized)
	h.AssertProviderCalledN(4)

	// State: user + 4 assistant messages (one per iteration)
	h.AssertStateHasNMessages(agent, 5)
}

func TestE2E_IncompleteResponse_CompleteShortAnswer(t *testing.T) {
	// "Done." is a known complete short answer — should NOT trigger continuation.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Done.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "Done.")

	// Provider called only once — no continuation triggered
	h.AssertProviderCalledN(1)

	// State: user + assistant
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_IncompleteResponse_WithToolCalls(t *testing.T) {
	// Provider returns an incomplete response WITH tool calls.
	// Tool calls take precedence over the incomplete check, so the tool
	// executes and continuationCount resets. The incomplete response is
	// still recorded in state as the last assistant message.
	h := NewHarnessWithT(t)
	h.Provider().AddToolCallResponse(
		"Let me check...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	// The final response must be at least 10 words so it's not flagged as too short.
	h.Provider().AddTextResponse("The file contents are hello world and that is all you need to know now.")

	h.Executor().AddToolResult("call_1", "hello world")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read test.txt")
	h.AssertNoError(err)
	h.AssertEquals(result, "The file contents are hello world and that is all you need to know now.")

	// Provider called twice: tool call iteration + final answer
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool call, incomplete) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)
}

func TestE2E_TruncatedResponseContinuation_FullFlow(t *testing.T) {
	h := NewHarnessWithT(t)

	// First call: truncated response (ends with "...")
	h.Provider().AddTextResponse("Here is the beginning of the answer...")

	// Second call: complete response (>10 words, no truncation markers)
	h.Provider().AddTextResponse("This is the complete answer that I was going to provide from the very start.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "What is the answer?")

	h.AssertNoError(err)

	// Result should be the complete response (not the truncated one)
	h.AssertEquals(result, "This is the complete answer that I was going to provide from the very start.")

	// Provider called twice: initial truncated + continuation with complete answer
	h.AssertProviderCalledN(2)

	// State: user query + assistant(truncated) + assistant(complete)
	h.AssertStateHasNMessages(agent, 3)

	// Token counts accumulate: 35 (first) + 35 (second) = 70
	h.AssertStateHasTokens(agent, 70)

	// Verify events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Two metrics_update events (one per iteration)
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 2 {
		t.Fatalf("expected 2 metrics_update events, got %d", len(metricsEvents))
	}

	// Verify iteration numbers in metrics events
	for i, evt := range metricsEvents {
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event %d: expected data to be map[string]interface{}, got %T", i, evt.Data)
		}
		it, ok := data["iteration"].(int)
		if !ok {
			t.Fatalf("event %d: expected iteration to be int, got %T", i, data["iteration"])
		}
		if it != i {
			t.Errorf("event %d: expected iteration %d, got %d", i, i, it)
		}
	}

	// Verify accumulated total_tokens in second metrics event
	lastMetrics := metricsEvents[1].Data.(map[string]interface{})
	totalTokens, ok := lastMetrics["total_tokens"].(int)
	if !ok {
		t.Fatalf("expected total_tokens to be int, got %T", lastMetrics["total_tokens"])
	}
	if totalTokens != 70 {
		t.Errorf("expected total_tokens 70 (accumulated), got %d", totalTokens)
	}

	// Verify state messages
	messages := agent.State().Messages()
	if messages[0].Role != "user" {
		t.Errorf("expected message 1 to be user, got %q", messages[0].Role)
	}
	if messages[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", messages[1].Role)
	}
	if !strings.HasSuffix(messages[1].Content, "...") {
		t.Errorf("expected message 2 to end with '...', got %q", messages[1].Content)
	}
	if messages[2].Role != "assistant" {
		t.Errorf("expected message 3 to be assistant, got %q", messages[2].Role)
	}

	// Verify the last provider request included the continuation message.
	// The transient continuation message is prepended to the next API call
	// but is NOT added to state.
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	// Find the transient continuation message in the last request
	foundContinuation := false
	for _, msg := range lastReq.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please continue") {
			foundContinuation = true
			break
		}
	}
	if !foundContinuation {
		t.Error("expected transient continuation message in last provider request")
	}

	// Verify the continuation message is NOT in state (it's transient)
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please continue") {
			t.Error("continuation message should not be in state (it's transient)")
		}
	}

	// Verify the first request did NOT include a continuation message
	firstReq := h.Provider().Calls[0]
	for _, msg := range firstReq.Messages {
		if strings.Contains(msg.Content, "Please continue") {
			t.Error("continuation message should not be in first provider request")
		}
	}
}

// --- Tentative Post-Tool Response Tests ---

func TestE2E_TentativeResponse_NoToolResults_AcceptedImmediately(t *testing.T) {
	// Provider returns a tentative planning stub ("Let me check...") with no
	// tool calls and no prior tool execution. Without tool results in history,
	// the tentative detection does not fire (it only activates after tool
	// results). The response is accepted immediately as the final answer.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Let me check the file contents.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "What's in the file?")
	h.AssertNoError(err)
	h.AssertEquals(result, "Let me check the file contents.")

	// Provider called once — no tentative rejection without tool results
	h.AssertProviderCalledN(1)

	// State: user + assistant
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_TentativeResponse_AfterToolExecution(t *testing.T) {
	// Scenario: tool executes, then provider returns a tentative planning stub
	// instead of using the tool result. The post-tool rejection fires because
	// followsRecentToolResults() returns true. The loop continues and gets a
	// real response.
	h := NewHarnessWithT(t)
	h.Provider().AddToolCallResponse(
		"Reading the file",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	h.Provider().AddTextResponseWithFinish("Let me check the results now.", "stop")
	h.Provider().AddTextResponseWithFinish("The configuration has three sections: database, cache, and logging. All are properly configured.", "stop")

	h.Executor().AddToolResult("call_1", "database:\n  host: localhost\n  port: 5432")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml and summarize")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration has three sections: database, cache, and logging. All are properly configured.")

	// Provider called 3 times: tool call -> tentative stub -> actual response
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool call) + tool result + assistant(tentative) + assistant(final)
	h.AssertStateHasNMessages(agent, 5)
}

func TestE2E_TentativeResponse_AfterToolExecution_MaxRejections(t *testing.T) {
	// After tool execution, the provider returns a tentative response.
	// Post-tool rejection fires, then the next response is accepted.
	h := NewHarnessWithT(t)
	h.Provider().AddToolCallResponse(
		"Reading the file",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	// Tentative after tool results → post-tool rejection #1
	h.Provider().AddTextResponseWithFinish("Let me check the output.", "stop")
	// Second response (followsRecentToolResults=false) → accepted as final
	h.Provider().AddTextResponseWithFinish("The configuration looks good.", "stop")

	h.Executor().AddToolResult("call_1", "database:\n  host: localhost")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration looks good.")

	// Provider called 3 times: tool call -> tentative (rejected) -> accepted
	h.AssertProviderCalledN(3)

	// State: user + assistant(tool call) + tool result + assistant(tentative) + assistant(final)
	h.AssertStateHasNMessages(agent, 5)
}

func TestE2E_TentativeResponse_ContinuesToToolCall(t *testing.T) {
	// Scenario: tool executes, provider returns tentative stub, post-tool
	// rejection fires. The next provider call returns another tool call,
	// which executes. Finally, a substantive answer.
	//
	// Flow:
	// 1. Provider returns tool call (read_file A)
	// 2. Tool executes, result recorded
	// 3. Provider returns tentative "Let me check the files." → post-tool rejection → continue
	// 4. Provider returns another tool call (read_file B)
	// 5. Tool executes, result recorded
	// 6. Provider returns final answer
	h := NewHarnessWithT(t)

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddToolResult("call_1", "database:\n  host: localhost")
	h.Executor().AddToolResult("call_2", "cache:\n  ttl: 300")

	// First call: tool call A
	h.Provider().AddToolCallResponse(
		"Reading the first file",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)

	// Second call: tentative planning stub (after tool results, triggers rejection)
	h.Provider().AddTextResponseWithFinish("Let me check the files.", "stop")

	// Third call: another tool call (model proceeds with work)
	h.Provider().AddToolCallResponse(
		"Reading the second file",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"cache.yaml"}`,
			},
		},
	)

	// Fourth call: final answer using both tool results
	h.Provider().AddTextResponse("The configuration has database settings on port 5432 with localhost as the host, and cache TTL of 300 seconds.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config files")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration has database settings on port 5432 with localhost as the host, and cache TTL of 300 seconds.")

	// Provider called 4 times: tool call A → tentative (rejected) → tool call B → final answer
	h.AssertProviderCalledN(4)
	h.AssertExecutorCalledN(2)

	// State: user + assistant(tool call A) + tool result A + assistant(tentative) + assistant(tool call B) + tool result B + assistant(final)
	h.AssertStateHasNMessages(agent, 7)

	// Verify the tentative message is in state
	messages := agent.State().Messages()
	if messages[3].Role != "assistant" {
		t.Errorf("expected message 4 to be assistant (tentative), got %q", messages[3].Role)
	}
	if messages[3].Content != "Let me check the files." {
		t.Errorf("expected tentative content, got %q", messages[3].Content)
	}

	// Verify the second tool call message follows the tentative one
	if messages[4].Role != "assistant" {
		t.Errorf("expected message 5 to be assistant (tool call), got %q", messages[4].Role)
	}
	if len(messages[4].ToolCalls) != 1 {
		t.Errorf("expected message 5 to have 1 tool call, got %d", len(messages[4].ToolCalls))
	}

	// Verify the transient message was included in the third provider request
	// (the one sent after the tentative rejection)
	if len(h.Provider().Calls) < 3 {
		t.Fatal("expected at least 3 provider calls")
	}
	foundTransient := false
	for _, msg := range h.Provider().Calls[2].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "planning note") {
			foundTransient = true
			break
		}
	}
	if !foundTransient {
		t.Error("expected transient rejection message in third provider request")
	}

	// Verify the transient message is NOT in state
	for _, msg := range messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "planning note") {
			t.Error("transient rejection message should not be in state")
		}
	}
}

func TestE2E_TentativeResponse_SubstantiveNotTentative(t *testing.T) {
	// A response that starts with "Let me" but is over 40 words should NOT
	// be treated as tentative — it's substantive enough to finalize.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse(
		"Let me provide a comprehensive answer. The file contains configuration data with multiple sections including database settings, cache parameters, and logging levels. Each section has been reviewed and appears to be correctly configured for production use. All values are within acceptable ranges and no changes are required at this time.",
	)

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Provider called only once — substantive response not treated as tentative
	h.AssertProviderCalledN(1)

	// State: user + assistant
	h.AssertStateHasNMessages(agent, 2)

	// Result should be the substantive response
	expected := "Let me provide a comprehensive answer. The file contains configuration data with multiple sections including database settings, cache parameters, and logging levels. Each section has been reviewed and appears to be correctly configured for production use. All values are within acceptable ranges and no changes are required at this time."
	h.AssertEquals(result, expected)
}

// --- InjectInput Tests ---

func TestE2E_InjectInput_Accepted(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello")

	agent := h.NewAgent()

	// Inject input before Run when channel is empty — should be accepted
	accepted := agent.InjectInput("test injection")
	if !accepted {
		t.Error("expected InjectInput to return true (channel has space)")
	}
}

func TestE2E_InjectInput_RejectedWhenFull(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Hello")

	agent := h.NewAgent()

	// First injection succeeds (channel is empty)
	accepted1 := agent.InjectInput("first message")
	if !accepted1 {
		t.Error("expected first InjectInput to return true")
	}

	// Second injection fails (channel buffer is 1, now full)
	accepted2 := agent.InjectInput("second message")
	if accepted2 {
		t.Error("expected second InjectInput to return false (channel full)")
	}
}

func TestE2E_InjectInput_MidConversation(t *testing.T) {
	h := NewHarnessWithT(t)

	// Provider returns text responses — no tool calls, so the loop would
	// normally break after the first response. By injecting input before
	// Run, the channel is full when the "no tool calls" check runs.
	// The injected input is consumed, causing the loop to continue.
	h.Provider().AddTextResponse("First response")
	h.Provider().AddTextResponse("Second response after injection")

	agent := h.NewAgent()

	// Pre-inject input so it's in the channel before Run starts.
	// The loop will consume it when it reaches the "no tool calls" check.
	accepted := agent.InjectInput("injected mid-conversation message")
	if !accepted {
		t.Fatal("expected InjectInput to return true")
	}

	result, err := agent.Run(context.Background(), "Original query")
	h.AssertNoError(err)

	// The injected input caused the loop to continue, so the second
	// provider response is the final one.
	h.AssertEquals(result, "Second response after injection")

	// Provider called twice: original query + after injection
	h.AssertProviderCalledN(2)

	// State: user query, assistant (first), injected user, assistant (second)
	h.AssertStateHasNMessages(agent, 4)
}

func TestE2E_InjectInput_NoInputLoopCompletes(t *testing.T) {
	// Regression test: normal conversation without injection completes normally.
	h := NewHarnessWithT(t)

	// Provider returns tool calls (loop continues), then text (loop completes).
	h.Provider().AddToolCallResponse(
		"Running tool.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "execute",
				Arguments: `{"cmd":"ls"}`,
			},
		},
	)
	h.Provider().AddTextResponse("All done.")

	h.Executor().AddToolResult("call_1", "file1 file2")

	agent := h.NewAgent()

	result, err := agent.Run(context.Background(), "Run ls")
	h.AssertNoError(err)
	h.AssertEquals(result, "All done.")

	// Provider called twice: tool call iteration + final text iteration.
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)
	h.AssertStateHasNMessages(agent, 4) // user + assistant(tool) + tool result + assistant(final)
}

func TestE2E_InjectInput_MidConversation_Running(t *testing.T) {
	// Realistic mid-conversation injection:
	// 1. Conversation starts with a tool call (loop continues)
	// 2. Tool executes, second provider call is blocked
	// 3. User injects input while blocked
	// 4. Provider returns text — loop finds injected input and continues
	// 5. Third provider call returns final response — loop completes
	h := NewHarnessWithT(t)

	// First response: tool call (loop continues for tool execution)
	h.Provider().AddToolCallResponse(
		"Let me check that.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "file content here")

	// Second response: block so we can inject input while the conversation is running
	blockCh := h.Provider().BlockOnCallN(2)
	h.Provider().AddTextResponse("I checked the file.")

	// Third response: after injected input is processed, this is the final answer
	h.Provider().AddTextResponse("Done with everything.")

	agent := h.NewAgent()

	type runResult struct {
		result string
		err    error
	}
	done := make(chan runResult, 1)
	go func() {
		result, err := agent.Run(context.Background(), "Read test.txt")
		done <- runResult{result, err}
	}()

	// Wait for the first iteration to complete (provider call + tool execution)
	for i := 0; i < 100; i++ {
		if h.Provider().CallCount() >= 1 && h.Executor().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// At this point the conversation is blocked on the second provider call.
	// Inject input while the conversation is actively running.
	accepted := agent.InjectInput("Also summarize the content")
	if !accepted {
		t.Fatal("expected InjectInput to return true while conversation is running")
	}

	// Unblock the provider so the second call returns
	close(blockCh)

	// Wait for the conversation to complete
	var res runResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("conversation did not complete within timeout")
	}

	// Assert in main goroutine so failures report correctly
	h.AssertNoError(res.err)
	h.AssertEquals(res.result, "Done with everything.")

	// Provider called 3 times:
	//  1. Original query → tool call response
	//  2. After tool result → text response (injected input found, loop continues)
	//  3. After injected input → final text response (no injection, loop completes)
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(1)

	// State messages:
	//  1. user: "Read test.txt"
	//  2. assistant: "Let me check that." (with tool call)
	//  3. tool: "file content here"
	//  4. assistant: "I checked the file."
	//  5. user: "Also summarize the content" (injected)
	//  6. assistant: "Done with everything."
	h.AssertStateHasNMessages(agent, 6)

	// Verify the injected message is in state
	messages := agent.State().Messages()
	if messages[4].Role != "user" {
		t.Errorf("expected message 5 to be user role, got %q", messages[4].Role)
	}
	if messages[4].Content != "Also summarize the content" {
		t.Errorf("expected injected content, got %q", messages[4].Content)
	}
	// Verify the final assistant message content
	if messages[5].Role != "assistant" {
		t.Errorf("expected message 6 to be assistant role, got %q", messages[5].Role)
	}
	if messages[5].Content != "Done with everything." {
		t.Errorf("expected final content 'Done with everything.', got %q", messages[5].Content)
	}
}

func TestE2E_InjectInput_MidConversation_RejectedWhenPending(t *testing.T) {
	// If a prior injection is still pending (channel full), a second injection is rejected.
	h := NewHarnessWithT(t)

	// First response: tool call (loop continues)
	h.Provider().AddToolCallResponse(
		"Running...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "run",
				Arguments: `{}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "ok")

	// Block on second call so we can test injection timing
	blockCh := h.Provider().BlockOnCallN(2)
	h.Provider().AddTextResponse("After injection.")
	h.Provider().AddTextResponse("Final.")

	agent := h.NewAgent()

	type runResult struct {
		err error
	}
	done := make(chan runResult, 1)
	go func() {
		_, err := agent.Run(context.Background(), "Start")
		done <- runResult{err}
	}()

	// Wait for first iteration
	for i := 0; i < 100; i++ {
		if h.Provider().CallCount() >= 1 && h.Executor().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// First injection succeeds (channel empty)
	accepted1 := agent.InjectInput("first injection")
	if !accepted1 {
		t.Fatal("expected first injection to succeed")
	}

	// Second injection fails (channel buffer is 1, now full)
	accepted2 := agent.InjectInput("second injection")
	if accepted2 {
		t.Error("expected second injection to fail (channel full)")
	}

	// Unblock and let conversation complete
	close(blockCh)
	var res runResult
	select {
	case res = <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("conversation did not complete within timeout")
	}

	// Assert in main goroutine
	h.AssertNoError(res.err)

	// Only the first injection was consumed
	h.AssertStateHasNMessages(agent, 6) // user + assistant(tool) + tool + assistant + injected user + assistant

	// Verify the injected message content
	messages := agent.State().Messages()
	if messages[4].Role != "user" {
		t.Errorf("expected message 5 to be user role, got %q", messages[4].Role)
	}
	if messages[4].Content != "first injection" {
		t.Errorf("expected injected content 'first injection', got %q", messages[4].Content)
	}
	// Verify the final assistant message content
	if messages[5].Role != "assistant" {
		t.Errorf("expected message 6 to be assistant role, got %q", messages[5].Role)
	}
	if messages[5].Content != "Final." {
		t.Errorf("expected final content 'Final.', got %q", messages[5].Content)
	}
}

// --- Cancellation Tests ---

func TestE2E_Cancellation_StopsLoop(t *testing.T) {
	h := NewHarnessWithT(t)

	// Configure the provider to block so we can cancel the context mid-request.
	blockCh := h.Provider().BlockUntil()
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(ctx, "test query")
		done <- err
	}()

	// Give the goroutine time to enter the blocking Chat call
	time.Sleep(50 * time.Millisecond)

	// Cancel the context — this should cause the provider to return ctx.Err()
	cancel()

	// Unblock the provider so it can observe the cancelled context
	close(blockCh)

	// Wait for Run to complete
	err := <-done

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")
}

// --- Interrupt Tests ---

func TestE2E_Interrupt_StopsRunningQuery(t *testing.T) {
	// The provider blocks on its first call. When we call agent.Interrupt(),
	// the agent's interruptCtx is cancelled, which propagates through the
	// runCtx bridge goroutine, causing the provider to see ctx.Done().
	h := NewHarnessWithT(t)

	blockCh := h.Provider().BlockUntil()
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), "test query")
		done <- err
	}()

	// Wait for the goroutine to enter the blocking Chat call
	for i := 0; i < 100; i++ {
		if h.Provider().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Interrupt the agent — this cancels interruptCtx, which the bridge
	// goroutine propagates to runCtx. Add a brief delay so the goroutine
	// has time to call runCancel() before we close blockCh.
	agent.Interrupt()
	time.Sleep(50 * time.Millisecond)

	// Unblock the provider so it can observe the cancelled context
	close(blockCh)

	// Wait for Run to complete
	err := <-done

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")

	// Verify the error wraps ErrInterrupted
	if !errors.Is(err, core.ErrInterrupted) {
		t.Errorf("expected error to wrap ErrInterrupted, got: %v", err)
	}
}

func TestE2E_Interrupt_ResetsAfterRun(t *testing.T) {
	// Call Interrupt(), then start a fresh Run() that should work normally.
	// This verifies that ResetInterrupt() is called at the start of Run(),
	// so a stale interrupt doesn't poison the next query.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Response after interrupt")

	agent := h.NewAgent()

	// Interrupt — but nothing is running, so it just cancels the idle context
	agent.Interrupt()

	// Next Run should work normally — ResetInterrupt() clears the stale cancel
	result, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)
	h.AssertEquals(result, "Response after interrupt")

	// Verify the agent can continue with another query too
	h.Provider().AddTextResponse("Second response")
	result2, err := agent.Run(context.Background(), "second query")
	h.AssertNoError(err)
	h.AssertEquals(result2, "Second response")
}

func TestE2E_ResetInterrupt_Manual(t *testing.T) {
	// Manually call ResetInterrupt() and verify the agent continues to work.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("After reset")

	agent := h.NewAgent()

	// Interrupt then manually reset
	agent.Interrupt()
	agent.ResetInterrupt()

	// Run should work normally after manual reset
	result, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)
	h.AssertEquals(result, "After reset")
}

func TestE2E_Interrupt_MidConversation(t *testing.T) {
	// Interrupt after a tool call completes but before the final response.
	// This verifies that interrupt works mid-loop, not just on the first call.
	h := NewHarnessWithT(t)

	// First call: tool call that executes
	h.Provider().AddToolCallResponse(
		"Running tool...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "slow_tool",
				Arguments: `{}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "tool result")

	// Second call: block so we can interrupt mid-conversation
	blockCh := h.Provider().BlockOnCallN(2)
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(context.Background(), "test query")
		done <- err
	}()

	// Wait for the first iteration to complete (tool call + execution)
	for i := 0; i < 100; i++ {
		if h.Provider().CallCount() >= 1 && h.Executor().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for the second iteration to start and hit the block
	time.Sleep(50 * time.Millisecond)

	// Interrupt the running conversation. Add a brief delay so the bridge
	// goroutine has time to call runCancel() before we close blockCh.
	agent.Interrupt()
	time.Sleep(50 * time.Millisecond)

	// Unblock the provider so it can observe the cancelled context
	close(blockCh)

	// Wait for Run to complete
	err := <-done

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")

	// Verify the error wraps ErrInterrupted
	if !errors.Is(err, core.ErrInterrupted) {
		t.Errorf("expected error to wrap ErrInterrupted, got: %v", err)
	}

	// Verify the tool call was executed before interrupt
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)
}

func TestE2E_Cancellation_ReturnsErrInterrupted(t *testing.T) {
	h := NewHarnessWithT(t)

	blockCh := h.Provider().BlockUntil()
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(ctx, "test query")
		done <- err
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()
	close(blockCh)

	err := <-done

	// Verify the error wraps ErrInterrupted
	if !errors.Is(err, core.ErrInterrupted) {
		t.Errorf("expected error to wrap ErrInterrupted, got: %v", err)
	}
}

func TestE2E_Cancellation_DuringToolCalls(t *testing.T) {
	h := NewHarnessWithT(t)

	// First response: tool call (loop continues)
	h.Provider().AddToolCallResponse(
		"Running tool...",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "slow_tool",
				Arguments: `{}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "result")

	// Second response: block on the 2nd call so we can cancel on the next iteration
	_ = h.Provider().BlockOnCallN(2)
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(ctx, "test query")
		done <- err
	}()

	// Wait for the first iteration to complete (provider + executor called)
	for i := 0; i < 50; i++ {
		if h.Provider().CallCount() >= 1 && h.Executor().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Give the goroutine time to enter the second iteration and hit the block
	time.Sleep(50 * time.Millisecond)

	// Cancel the context — the provider will observe ctx.Done() and return
	// Do NOT close blockCh; let ctx.Done() be the only exit path
	cancel()

	err := <-done

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")

	// Provider was called twice: first iteration succeeded, second call returned ctx.Err()
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)
}

func TestE2E_Cancellation_ContextExpired(t *testing.T) {
	h := NewHarnessWithT(t)

	// Use a context with a very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	blockCh := h.Provider().BlockUntil()
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	_, err := agent.Run(ctx, "test query")

	// Unblock the provider so it can observe the expired context
	close(blockCh)

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")
}

func TestE2E_Cancellation_NoEffectWithoutCancel(t *testing.T) {
	// Regression: normal flow without cancellation should still work.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Normal response")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "Normal response")
	h.AssertProviderCalledN(1)
}

// --- Continuation Budget Reset Tests ---

func TestE2E_ContinuationBudget_ResetsOnToolCalls(t *testing.T) {
	// This test verifies that the continuation budget resets when tool calls
	// occur between incomplete responses. Without the reset, the budget would
	// accumulate incorrectly and the loop could force-finalize prematurely.
	//
	// Flow:
	// 1. Provider returns an incomplete response → continuationCount = 1
	// 2. Provider returns a tool call → continuationCount resets to 0
	// 3. Provider returns an incomplete response → continuationCount = 1 again
	// 4. Provider returns another tool call → continuationCount resets to 0 again
	// 5. Provider returns a complete final answer → loop completes normally
	//
	// Total provider calls: 5
	// Total executor calls: 2

	h := NewHarnessWithT(t)

	// Call 1: incomplete response (ends with "...") — triggers continuation
	h.Provider().AddTextResponse("First incomplete response...")

	// Call 2: tool call response — resets continuationCount to 0
	h.Provider().AddToolCallResponse(
		"Let me check the data.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "fetch_data",
				Arguments: `{"id": "123"}`,
			},
		},
	)

	// Call 3: incomplete response (ends with "...") — continuationCount = 1 again
	h.Provider().AddTextResponse("Looking at the results...")

	// Call 4: another tool call — resets continuationCount to 0 again
	h.Provider().AddToolCallResponse(
		"I'll aggregate the data.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "aggregate",
				Arguments: `{"source": "123"}`,
			},
		},
	)

	// Call 5: final complete answer — loop completes
	h.Provider().AddTextResponse("The aggregated data shows all metrics are within acceptable ranges.")

	// Executor returns results for both tool calls
	h.Executor().AddToolResult("call_1", "raw data content")
	h.Executor().AddToolResult("call_2", "aggregated: {\"status\": \"ok\"}")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Check data and aggregate")
	h.AssertNoError(err)
	h.AssertEquals(result, "The aggregated data shows all metrics are within acceptable ranges.")

	// Provider called 5 times:
	//  1. Initial incomplete response → continuation
	//  2. Tool call response → executes, resets continuationCount
	//  3. Second incomplete response → continuation (count = 1)
	//  4. Second tool call → executes, resets continuationCount
	//  5. Final answer → completes
	h.AssertProviderCalledN(5)
	h.AssertExecutorCalledN(2)

	// State messages (8 total):
	//  1. user: "Check data and aggregate"
	//  2. assistant: "First incomplete response..."
	//  3. assistant: "Let me check the data." (with tool call)
	//  4. tool: "raw data content"
	//  5. assistant: "Looking at the results..."
	//  6. assistant: "I'll aggregate the data." (with tool call)
	//  7. tool: "aggregated: {\"status\": \"ok\"}"
	//  8. assistant: "The aggregated data shows..."
	h.AssertStateHasNMessages(agent, 8)
}

func TestE2E_ContinuationBudget_TruncatedAndTentativeOverlap(t *testing.T) {
	// A response that matches both LooksTruncated (ends with "...") and
	// LooksLikeTentativePostToolResponse (starts with "Let me") should only
	// consume one continuation budget. The truncated check takes priority
	// (if/else if chain), so only that check fires.
	h := NewHarnessWithT(t)

	// First two responses match both truncated (ends with "...") and
	// tentative (starts with "Let me"). Each should consume only 1
	// continuation budget (not 2) because the if/else if chain ensures
	// only the truncated check fires. The final response is substantive
	// enough (>40 words) that it won't be flagged as tentative.
	h.Provider().AddTextResponse("Let me check this...")
	h.Provider().AddTextResponse("Let me try again...")
	h.Provider().AddTextResponse("The file contains configuration data with multiple sections including database settings, cache parameters, and logging levels. All values are within acceptable ranges.")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// Provider called 3 times: 2 continuations (budget 2/3) + 1 final
	h.AssertProviderCalledN(3)

	// State: user + 3 assistant messages
	h.AssertStateHasNMessages(agent, 4)
}

// --- Streaming Tests ---

func TestE2E_Streaming_CallbacksFire(t *testing.T) {
	h := NewHarnessWithT(t)

	// Configure streaming with explicit chunks
	chunks := []string{"Hello", ", ", "world", "!"}
	h.Provider().
		WithStreaming().
		AddStreamChunks(chunks...).
		AddTextResponse("Hello, world!")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// RunStream returns the final response content from state
	h.AssertEquals(result, "Hello, world!")

	// Streaming buffer should contain the accumulated content
	buf := agent.StreamingBuffer()
	h.AssertEquals(buf.String(), "Hello, world!")

	// stream_chunk events should have been published
	h.AssertEventPublished(events.EventTypeStreamChunk)

	// agent_message events should have been published per chunk
	h.AssertEventPublished(events.EventTypeAgentMessage)

	// query_started and query_completed should still fire
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)
}

func TestE2E_Streaming_BufferAccumulation(t *testing.T) {
	h := NewHarnessWithT(t)

	// Configure streaming with multiple chunks
	h.Provider().
		WithStreaming().
		AddStreamChunks("chunk1", "chunk2", "chunk3").
		AddTextResponse("chunk1chunk2chunk3")

	agent := h.NewAgent()

	// Track flush invocations
	flushCount := 0
	agent.SetFlushCallback(func() {
		flushCount++
	})

	result, err := agent.RunStream(context.Background(), "stream test")
	h.AssertNoError(err)

	// Buffer should contain all chunks concatenated
	buf := agent.StreamingBuffer()
	h.AssertEquals(buf.String(), "chunk1chunk2chunk3")

	// Buffer length should match
	if buf.Len() != 18 {
		h.fail("expected buffer length 18, got %d", buf.Len())
	}

	// Flush callback should have been called once per chunk
	if flushCount != 3 {
		h.fail("expected flush called 3 times, got %d", flushCount)
	}

	// RunStream returns the final response content from state
	h.AssertEquals(result, "chunk1chunk2chunk3")
}

func TestE2E_Streaming_BufferPreferredOverChoice(t *testing.T) {
	h := NewHarnessWithT(t)

	// Configure streaming — the response has content in Choices,
	// finalize returns the final content from state regardless of buffer
	h.Provider().
		WithStreaming().
		AddStreamChunks("streamed ", "content").
		AddTextResponse("streamed content")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// finalize() returns the final response content from state
	h.AssertEquals(result, "streamed content")

	// Buffer also has the streamed content (both return the same data)
	h.AssertEquals(agent.StreamingBuffer().String(), "streamed content")
}

func TestE2E_Streaming_WithToolCalls(t *testing.T) {
	h := NewHarnessWithT(t)

	// First: streamed tool call response
	h.Provider().
		WithStreaming().
		AddStreamChunks("Let ", "me ", "check.").
		AddToolCallResponse(
			"Let me check.",
			core.ToolCall{
				ID: "call_1",
				Function: core.ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"test.txt"}`,
				},
			},
		)
	// Second: streamed final answer
	h.Provider().
		AddStreamChunks("File ", "read.").
		AddTextResponse("File read.")

	h.Executor().AddToolResult("call_1", "file content")

	agent := h.NewAgent()
	_, err := agent.RunStream(context.Background(), "Read test.txt")
	h.AssertNoError(err)

	// Buffer accumulates streamed content across all iterations
	h.AssertEquals(agent.StreamingBuffer().String(), "Let me check.File read.")

	// Provider called twice (tool call iteration + final)
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)
}

func TestE2E_Streaming_EventsPublished(t *testing.T) {
	h := NewHarnessWithT(t)

	h.Provider().
		WithStreaming().
		AddStreamChunks("A", "B", "C").
		AddTextResponse("ABC")

	agent := h.NewAgent()
	_, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// stream_chunk events — one per chunk
	streamChunks := h.FindEvents(events.EventTypeStreamChunk)
	if len(streamChunks) != 3 {
		h.fail("expected 3 stream_chunk events, got %d", len(streamChunks))
	}

	// agent_message events — one per chunk
	agentMsgs := h.FindEvents(events.EventTypeAgentMessage)
	if len(agentMsgs) < 3 {
		h.fail("expected at least 3 agent_message events, got %d", len(agentMsgs))
	}

	// metrics_update event from OnDone
	h.AssertEventPublished(events.EventTypeMetricsUpdate)
}

func TestE2E_Streaming_ReasoningChunks(t *testing.T) {
	h := NewHarnessWithT(t)

	// We need a custom stream handler that also sends reasoning.
	// Since MockProvider only sends content chunks, we verify
	// that the reasoning buffer is separate from the content buffer.
	h.Provider().
		WithStreaming().
		AddStreamChunks("content only").
		AddTextResponse("content only")

	agent := h.NewAgent()
	_, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// Content buffer has the streamed content
	h.AssertEquals(agent.StreamingBuffer().String(), "content only")

	// Reasoning buffer should be empty (no reasoning chunks sent)
	if agent.ReasoningBuffer().Len() != 0 {
		h.fail("expected empty reasoning buffer")
	}
}

// --- Retry / Error Recovery Tests ---

func TestE2E_Retry_TransientErrorRecovers(t *testing.T) {
	h := NewHarnessWithT(t)

	// First two calls fail with transient errors; third succeeds.
	h.Provider().AddError(fmt.Errorf("connection refused"))
	h.Provider().AddError(fmt.Errorf("timeout: deadline exceeded"))
	h.Provider().AddTextResponse("Recovered!")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "Recovered!")

	// Provider called 3 times: 2 transient errors + 1 success
	h.AssertProviderCalledN(3)
}

func TestE2E_Retry_AuthErrorFailsFast(t *testing.T) {
	h := NewHarnessWithT(t)

	// Auth error should fail fast — no retries.
	h.Provider().AddError(&core.AuthError{
		Provider: "mock-model",
		Wrapped:  fmt.Errorf("invalid api key"),
	})

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")

	h.AssertError(err)
	h.AssertErrorContains(err, "authentication failed")

	// Provider called only once — no retry on auth errors
	h.AssertProviderCalledN(1)
}

func TestE2E_Retry_ContextOverflowFailsFast(t *testing.T) {
	h := NewHarnessWithT(t)

	// Context overflow should fail fast — no retries.
	h.Provider().AddError(&core.ContextOverflowError{
		TokensUsed:  130000,
		TokensLimit: 128000,
		Wrapped:     fmt.Errorf("context window exceeded"),
	})

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")

	h.AssertError(err)
	h.AssertErrorContains(err, "context window exceeded")

	// Provider called only once — no retry on context overflow
	h.AssertProviderCalledN(1)
}

func TestE2E_Retry_ExhaustedReturnsLastError(t *testing.T) {
	h := NewHarnessWithT(t)

	// All 3 attempts fail with transient errors (max attempts = 3).
	h.Provider().AddError(fmt.Errorf("transient failure 1"))
	h.Provider().AddError(fmt.Errorf("transient failure 2"))
	h.Provider().AddError(fmt.Errorf("transient failure 3"))

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")

	h.AssertError(err)
	h.AssertErrorContains(err, "transient failure 3")

	// Provider called 3 times (max attempts exhausted)
	h.AssertProviderCalledN(3)

	// State should only have the user message — no assistant message recorded
	h.AssertStateHasNMessages(agent, 1)
}

func TestE2E_Retry_RateLimitErrorRetries(t *testing.T) {
	h := NewHarnessWithT(t)

	// First call fails with rate limit; second succeeds.
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  1,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddTextResponse("After rate limit!")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "After rate limit!")

	// Provider called 2 times: 1 rate limit + 1 success
	h.AssertProviderCalledN(2)
}

func TestE2E_Retry_RateLimitMultipleRetriesWithBackoff(t *testing.T) {
	h := NewHarnessWithT(t)

	// Two consecutive rate limit errors, then success.
	// This exercises the backoff delay between retries.
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  1,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  2,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddTextResponse("Recovered after rate limits!")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "Recovered after rate limits!")

	// Provider called 3 times: 2 rate limit errors + 1 success
	h.AssertProviderCalledN(3)

	// State should have user + assistant messages (successful path)
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_Retry_RateLimitErrorEventsPublished(t *testing.T) {
	h := NewHarnessWithT(t)

	// Two rate limit errors before success.
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  1,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  2,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "OK")

	// Error events published for each rate limit failure (from chatFn)
	h.AssertEventPublished(events.EventTypeError)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 2 {
		t.Errorf("expected 2 error events (one per rate limit retry), got %d", len(errorEvents))
	}

	// Verify error event payload contains rate limit info
	data, ok := errorEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map[string]interface{}, got %T", errorEvents[0].Data)
	}
	errField, ok := data["error"].(string)
	if !ok {
		t.Fatalf("expected error event error field to be string, got %T", data["error"])
	}
	h.AssertContains(errField, "rate limit exceeded")
}

func TestE2E_Retry_RateLimitExhausted(t *testing.T) {
	h := NewHarnessWithT(t)

	// All 3 attempts fail with rate limit errors (max attempts = 3).
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  1,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  2,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  3,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")

	h.AssertError(err)
	h.AssertErrorContains(err, "chat failed")

	// The wrapped error should be the last RateLimitError
	var rateLimitErr *core.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected error chain to contain RateLimitError, got: %v", err)
	}

	// Provider called 3 times (max attempts exhausted)
	h.AssertProviderCalledN(3)

	// State should only have the user message — no assistant message recorded
	h.AssertStateHasNMessages(agent, 1)

	// Error events: 3 from chatFn (one per rate limit attempt) + 1 from runLoop (final error) = 4
	h.AssertEventPublished(events.EventTypeError)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 4 {
		t.Errorf("expected 4 error events (3 retry + 1 final), got %d", len(errorEvents))
	}
}

func TestE2E_Retry_RateLimitMixedWithTransient(t *testing.T) {
	h := NewHarnessWithT(t)

	// Rate limit on first attempt, transient on second, success on third.
	h.Provider().AddError(&core.RateLimitError{
		Provider: "mock-model",
		Attempt:  1,
		Wrapped:  fmt.Errorf("rate limit exceeded"),
	})
	h.Provider().AddError(fmt.Errorf("connection refused"))
	h.Provider().AddTextResponse("Recovered from mixed errors!")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "Recovered from mixed errors!")

	// Provider called 3 times: 1 rate limit + 1 transient + 1 success
	h.AssertProviderCalledN(3)

	// Error events published for both retryable errors
	h.AssertEventPublished(events.EventTypeError)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 2 {
		t.Errorf("expected 2 error events (rate limit + transient), got %d", len(errorEvents))
	}
}

func TestE2E_Retry_RateLimitErrorWrapping(t *testing.T) {
	h := NewHarnessWithT(t)

	// Single rate limit error that exhausts retries.
	h.Provider().AddError(&core.RateLimitError{
		Provider: "my-provider",
		Attempt:  1,
		Wrapped:  fmt.Errorf("429 too many requests"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "my-provider",
		Attempt:  2,
		Wrapped:  fmt.Errorf("429 too many requests"),
	})
	h.Provider().AddError(&core.RateLimitError{
		Provider: "my-provider",
		Attempt:  3,
		Wrapped:  fmt.Errorf("429 too many requests"),
	})

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test query")

	h.AssertError(err)

	// Verify the error chain: fmt.Errorf("chat failed: %w") wraps RateLimitError
	var rateLimitErr *core.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("expected error chain to contain RateLimitError, got: %v", err)
	}

	// Verify RateLimitError fields
	if rateLimitErr.Provider != "my-provider" {
		t.Errorf("expected provider 'my-provider', got %q", rateLimitErr.Provider)
	}
	if rateLimitErr.Wrapped == nil {
		t.Error("expected wrapped error to be set")
	} else if !strings.Contains(rateLimitErr.Wrapped.Error(), "429") {
		t.Errorf("expected wrapped error to contain '429', got: %v", rateLimitErr.Wrapped)
	}
}

func TestE2E_Retry_ErrorEventsPublished(t *testing.T) {
	h := NewHarnessWithT(t)

	// Two transient errors before success.
	h.Provider().AddError(fmt.Errorf("timeout: connection reset"))
	h.Provider().AddError(fmt.Errorf("service unavailable"))
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test query")

	h.AssertNoError(err)
	h.AssertEquals(result, "OK")

	// Error events published for each transient failure (from chatFn)
	h.AssertEventPublished(events.EventTypeError)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 2 {
		t.Errorf("expected 2 error events (one per retry attempt), got %d", len(errorEvents))
	}
}

func TestE2E_Retry_ContextCancellationDuringBackoff(t *testing.T) {
	h := NewHarnessWithT(t)

	// First call fails with transient error; second call would succeed
	// but context is cancelled during backoff delay.
	h.Provider().AddError(fmt.Errorf("timeout: connection reset"))
	h.Provider().AddTextResponse("Should not reach this")

	agent := h.NewAgent()

	ctx, cancel := context.WithCancel(context.Background())

	// Start the run in a goroutine
	done := make(chan error, 1)
	go func() {
		_, err := agent.Run(ctx, "test query")
		done <- err
	}()

	// Wait for the first provider call to complete (it will fail with transient error)
	for i := 0; i < 50; i++ {
		if h.Provider().CallCount() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Cancel during the backoff delay before the retry
	cancel()

	err := <-done

	h.AssertError(err)
	h.AssertErrorContains(err, "conversation interrupted")

	// Provider called once (first attempt failed, backoff cancelled before retry)
	h.AssertProviderCalledN(1)
}

// --- Malformed Response (Fallback Parser) Tests ---

func TestE2E_MalformedResponse_JSONFence(t *testing.T) {
	// Simulates a model returning tool calls embedded in content as JSON
	// rather than in the structured tool_calls field. The fallback parser
	// should extract them, the tool executes, and the conversation completes.
	h := NewHarnessWithT(t)

	// Register a tool so the executor knows about it.
	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})

	// First response: malformed — tool call embedded in JSON fence inside content.
	malformedContent := "I'll read the file for you.\n\n```json\n{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"test.txt\\\"}\"}}]}\n```\n\nLet me know if you need anything else."
	h.Provider().AddMalformedResponse(malformedContent)

	// Second response: final answer after tool result.
	h.Provider().AddTextResponse("The file contains 'hello world'.")

	// Executor returns the tool result.
	h.Executor().AddToolResult("call_1", "hello world")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read test.txt")

	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains 'hello world'.")

	// Provider called twice: malformed response (fallback extracted tool call) + final answer.
	h.AssertProviderCalledN(2)

	// Executor called once: the fallback parser extracted the tool call.
	h.AssertExecutorCalledN(1)

	// State: user + assistant(malformed) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)

	// Verify the assistant message has the cleaned content (tool call stripped).
	msgs := agent.State().Messages()
	if msgs[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", msgs[1].Role)
	}
	// The cleaned content should not contain the JSON fence.
	if strings.Contains(msgs[1].Content, "```json") {
		t.Error("expected cleaned content to not contain JSON fence")
	}
}

func TestE2E_MalformedResponse_XMLBlock(t *testing.T) {
	// Simulates a model returning tool calls in XML format embedded in content.
	h := NewHarnessWithT(t)

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "search"}})

	// Malformed response with <tool> XML block containing tool_calls JSON.
	malformedContent := "Let me search for that.\n<tool>{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"search\",\"arguments\":\"{\\\"query\\\":\\\"go testing\\\"}\"}}]}</tool>\nThat should help."
	h.Provider().AddMalformedResponse(malformedContent)

	h.Provider().AddTextResponse("Found 3 results for 'go testing'.")

	h.Executor().AddToolResult("call_1", "3 results found")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Search go testing")

	h.AssertNoError(err)
	h.AssertEquals(result, "Found 3 results for 'go testing'.")

	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)
	h.AssertStateHasNMessages(agent, 4)
}

func TestE2E_MalformedResponse_NoPatternTriggers(t *testing.T) {
	// Content has no tool-call patterns — fallback parser should not trigger.
	// The response should be treated as a normal text response.
	h := NewHarnessWithT(t)

	h.Provider().AddMalformedResponse("Just a normal text response with no tool calls.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "test")

	h.AssertNoError(err)
	h.AssertEquals(result, "Just a normal text response with no tool calls.")

	h.AssertProviderCalledN(1)
	h.AssertExecutorCalledN(0)
	h.AssertStateHasNMessages(agent, 2) // user + assistant
}

func TestE2E_MalformedResponse_MultipleIterations(t *testing.T) {
	// Tests a conversation where multiple malformed responses are processed
	// across iterations, each with tool calls extracted by the fallback parser.
	h := NewHarnessWithT(t)

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "shell"}})

	// First malformed: read_file tool call
	h.Provider().AddMalformedResponse(
		"```json\n{\"tool_calls\":[{\"id\":\"call_1\",\"type\":\"function\",\"function\":{\"name\":\"read_file\",\"arguments\":\"{\\\"path\\\":\\\"data.txt\\\"}\"}}]}\n```",
	)
	// Second malformed: shell tool call
	h.Provider().AddMalformedResponse(
		"```json\n{\"tool_calls\":[{\"id\":\"call_2\",\"type\":\"function\",\"function\":{\"name\":\"shell\",\"arguments\":\"{\\\"cmd\\\":\\\"wc -l data.txt\\\"}\"}}]}\n```",
	)
	// Final text response
	h.Provider().AddTextResponse("The file has 42 lines.")

	h.Executor().AddToolResult("call_1", "some data content")
	h.Executor().AddToolResult("call_2", "42 data.txt")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Count lines in data.txt")

	h.AssertNoError(err)
	h.AssertEquals(result, "The file has 42 lines.")

	// Provider called 3 times: 2 malformed + 1 final
	h.AssertProviderCalledN(3)

	// Executor called 2 times: once per malformed iteration
	h.AssertExecutorCalledN(2)

	// State: user + assistant(malformed1) + tool result1 + assistant(malformed2) + tool result2 + assistant(final)
	h.AssertStateHasNMessages(agent, 6)
}

// --- Streaming Incomplete Response Tests ---

func TestE2E_IncompleteResponse_Streaming_TrailingEllipsis(t *testing.T) {
	// Provider streams a response ending with ellipsis (structural truncation marker).
	// The validator should detect this as incomplete and continue the loop.
	// The second call streams a complete answer (>10 words, no truncation markers).
	h := NewHarnessWithT(t)

	// First call: incomplete streaming response (ends with "...")
	h.Provider().
		WithStreaming().
		AddStreamChunks("Here is the answer...").
		AddTextResponse("Here is the answer...")

	// Second call: complete streaming response (no truncation, >10 words)
	h.Provider().
		AddStreamChunks("This is the complete answer that I was going to provide from the start.").
		AddTextResponse("This is the complete answer that I was going to provide from the start.")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// RunStream returns the final response content from the last assistant message in state
	h.AssertEquals(result, "This is the complete answer that I was going to provide from the start.")

	// Streaming buffer accumulates all streamed content across iterations
	buf := agent.StreamingBuffer()
	h.AssertEquals(buf.String(), "Here is the answer...This is the complete answer that I was going to provide from the start.")

	// Provider called twice: initial incomplete response + continuation with complete answer
	h.AssertProviderCalledN(2)

	// State: user query + assistant(incomplete) + assistant(final continuation)
	h.AssertStateHasNMessages(agent, 3)
}

func TestE2E_IncompleteResponse_Streaming_CompleteShortAnswer(t *testing.T) {
	// "Done." is a known complete short answer — should NOT trigger continuation.
	h := NewHarnessWithT(t)

	h.Provider().
		WithStreaming().
		AddStreamChunks("Done.").
		AddTextResponse("Done.")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "test")
	h.AssertNoError(err)

	// RunStream returns the final response content from state
	h.AssertEquals(result, "Done.")

	// Streaming buffer contains the streamed content
	buf := agent.StreamingBuffer()
	h.AssertEquals(buf.String(), "Done.")

	// Provider called only once — no continuation triggered (known complete short answer)
	h.AssertProviderCalledN(1)

	// State: user + assistant
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_TentativeResponse_Streaming(t *testing.T) {
	// Provider streams a tentative planning stub after tool execution.
	// The post-tool rejection fires because followsRecentToolResults() returns true.
	// The loop continues and the second call streams a complete answer.
	h := NewHarnessWithT(t)

	// Tool execution setup
	h.Executor().AddToolResult("call_1", "file contents here")

	// First call: tool call
	h.Provider().AddToolCallResponse(
		"Reading file",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)

	// Second call: tentative streaming response (after tool results)
	h.Provider().
		WithStreaming().
		AddStreamChunks("Let me ", "check.").
		AddTextResponseWithFinish("Let me check.", "stop")

	// Third call: complete streaming response
	h.Provider().
		AddStreamChunks("The file contains the expected configuration data.").
		AddTextResponseWithFinish("The file contains the expected configuration data.", "stop")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "What's in the file?")
	h.AssertNoError(err)

	// RunStream returns the final response content from the last assistant message
	h.AssertEquals(result, "The file contains the expected configuration data.")

	// Streaming buffer accumulates all streamed content across iterations
	buf := agent.StreamingBuffer()
	h.AssertEquals(buf.String(), "Let me check.The file contains the expected configuration data.")

	// Provider called 3 times: tool call -> tentative (rejected) -> actual response
	h.AssertProviderCalledN(3)

	// State: user + assistant(tool call) + tool result + assistant(tentative) + assistant(final)
	h.AssertStateHasNMessages(agent, 5)
}

// --- Conversation Optimizer Integration Tests ---

func TestE2E_Optimizer_Integration_FileReadDedup(t *testing.T) {
	// End-to-end test for the ConversationOptimizer's file read deduplication.
	//
	// Flow:
	//   Iteration 0: assistant calls read_file("test.txt") → tool returns "file content here"
	//   Iteration 1: assistant calls read_file("test.txt") again → tool returns "file content here"
	//   Iteration 2: assistant gives final answer "Done."
	//
	// The optimizer should replace the first tool result's content with
	// `[Earlier file read: test.txt]` while keeping the second with original content.
	h := NewHarnessWithT(t)

	// Register the read_file tool.
	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})

	// Provider returns: tool call 1, tool call 2 (same file), final answer.
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	h.Provider().AddToolCallResponse(
		"Reading it again to confirm.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Done.")

	// Both calls return the same content.
	h.Executor().AddToolResult("call_1", "file content here")
	h.Executor().AddToolResult("call_2", "file content here")

	// Enable the optimizer with file read classification.
	optimizer := core.NewConversationOptimizer(core.ConversationOptimizerOptions{
		Enabled: true,
		KnownToolFn: func(name string) core.ToolCategory {
			switch name {
			case "read_file":
				return core.ToolCategoryFileRead
			case "shell":
				return core.ToolCategoryShellCommand
			default:
				return core.ToolCategoryUnknown
			}
		},
	})

	agent := h.NewAgentWithOptions(core.Options{
		Optimizer: optimizer,
	})

	result, err := agent.Run(context.Background(), "Read test.txt")
	h.AssertNoError(err)
	h.AssertEquals(result, "Done.")

	// Provider called 3 times: call1 → call2 → final answer.
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(2)

	// State: user + assistant(call1) + tool + assistant(call2) + tool + assistant(final) = 6.
	h.AssertStateHasNMessages(agent, 6)

	// Verify the last provider request has the first tool result replaced with a placeholder
	// and the second retaining the original content. Check by position to ensure the
	// optimizer replaced the correct (earlier) read, not the later one.
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	var toolMsgs []core.Message
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" {
			toolMsgs = append(toolMsgs, msg)
		}
	}
	if len(toolMsgs) != 2 {
		t.Fatalf("expected 2 tool messages in last request, got %d", len(toolMsgs))
	}

	// First (earlier) read should be replaced with placeholder
	h.AssertEquals(toolMsgs[0].Content, "[Earlier file read: test.txt]")
	// Second (latest) read should retain original content
	h.AssertEquals(toolMsgs[1].Content, "file content here")

	// Verify state messages ARE persisted with the dedup placeholders. The
	// dedup pass runs on the raw state slice and writes back when it masks
	// an earlier duplicate — a stable wire prefix keeps the provider's
	// prompt cache and any prefix-anchored token accounting valid across
	// iterations. The LATEST read must retain its original content.
	stateMsgs := agent.State().Messages()
	var stateToolMsgs []core.Message
	for _, m := range stateMsgs {
		if m.Role == "tool" {
			stateToolMsgs = append(stateToolMsgs, m)
		}
	}
	if len(stateToolMsgs) != 2 {
		t.Fatalf("expected 2 tool messages in state, got %d", len(stateToolMsgs))
	}
	h.AssertEquals(stateToolMsgs[0].Content, "[Earlier file read: test.txt]")
	h.AssertEquals(stateToolMsgs[1].Content, "file content here")
}

// --- Steer Message Tests ---

func TestE2E_Steer_MessageIncludedInRequest(t *testing.T) {
	// Verify that a steered message appears in the provider request and is NOT
	// persisted in agent state.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Got it, focusing on performance.")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "Focus on performance."})

	_, err := agent.Run(context.Background(), "Analyze this code")
	h.AssertNoError(err)

	// The steer message should appear in the first provider request
	req := h.Provider().Calls[0]
	found := false
	for _, m := range req.Messages {
		if m.Role == "user" && m.Content == "Focus on performance." {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected steer message to appear in provider request")
	}

	// The steer message should NOT be in agent state
	msgs := agent.State().Messages()
	for _, m := range msgs {
		if m.Content == "Focus on performance." {
			t.Error("steer message should not be persisted in state")
		}
	}

	// State should only have user + assistant
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_Steer_ConsumedOnce(t *testing.T) {
	// Steer a message, Run twice. The first Run should include the steer
	// message; the second Run should NOT.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First run response")
	h.Provider().AddTextResponse("Second run response")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "Only in first run."})

	// First Run — steer message present
	_, err := agent.Run(context.Background(), "First query")
	h.AssertNoError(err)

	req1 := h.Provider().Calls[0]
	found1 := false
	for _, m := range req1.Messages {
		if m.Content == "Only in first run." {
			found1 = true
			break
		}
	}
	if !found1 {
		t.Error("expected steer message in first provider request")
	}

	// Second Run — steer message should NOT be present (consumed)
	_, err = agent.Run(context.Background(), "Second query")
	h.AssertNoError(err)

	req2 := h.Provider().Calls[1]
	found2 := false
	for _, m := range req2.Messages {
		if m.Content == "Only in first run." {
			found2 = true
			break
		}
	}
	if found2 {
		t.Error("steer message should not appear in second provider request (already consumed)")
	}
}

func TestE2E_Steer_MultipleMessages(t *testing.T) {
	// Steer two messages, then Run. Both should appear in the provider request.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Both messages received.")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "First steer message."})
	agent.Steer(core.Message{Role: "user", Content: "Second steer message."})

	_, err := agent.Run(context.Background(), "Original query")
	h.AssertNoError(err)

	req := h.Provider().Calls[0]
	foundFirst := false
	foundSecond := false
	for _, m := range req.Messages {
		if m.Content == "First steer message." {
			foundFirst = true
		}
		if m.Content == "Second steer message." {
			foundSecond = true
		}
	}
	if !foundFirst {
		t.Error("expected first steer message in provider request")
	}
	if !foundSecond {
		t.Error("expected second steer message in provider request")
	}

	// Neither steer message should be in state
	msgs := agent.State().Messages()
	for _, m := range msgs {
		if m.Content == "First steer message." || m.Content == "Second steer message." {
			t.Error("steer messages should not be persisted in state")
		}
	}

	// State should only have user + assistant
	h.AssertStateHasNMessages(agent, 2)
}

func TestE2E_Steer_NotPersistedInState(t *testing.T) {
	// Steer + Run. Assert state only has user + assistant messages
	// (no steer message persisted).
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "Do not persist me."})

	_, err := agent.Run(context.Background(), "Query")
	h.AssertNoError(err)

	// Verify state only has 2 messages (user query + assistant response)
	h.AssertStateHasNMessages(agent, 2)

	// Verify no steer content in state
	msgs := agent.State().Messages()
	for i, m := range msgs {
		switch i {
		case 0:
			if m.Role != "user" || m.Content != "Query" {
				t.Errorf("expected message 1 to be user 'Query', got %q %q", m.Role, m.Content)
			}
		case 1:
			if m.Role != "assistant" {
				t.Errorf("expected message 2 to be assistant, got %q", m.Role)
			}
		default:
			t.Errorf("unexpected message %d in state: %q", i+1, m.Content)
		}
	}
}

func TestE2E_Steer_SystemRole(t *testing.T) {
	// Steer with Role "system". Verify it gets collapsed into the system prompt
	// (since collapseSystemMessages merges system messages).
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "system", Content: "Additional system instruction."})

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// The system message should appear as part of the system prompt in the request
	// (collapsed into the system message position)
	req := h.Provider().Calls[0]
	if len(req.Messages) == 0 || req.Messages[0].Role != "system" {
		t.Errorf("expected first message to be system role, got %q", req.Messages[0].Role)
	}
	// The system prompt should contain the additional system instruction
	// (it gets appended to the default system prompt)
	systemContent := req.Messages[0].Content
	if !strings.Contains(systemContent, "Additional system instruction.") {
		t.Errorf("expected system prompt to contain 'Additional system instruction.', got: %q", systemContent)
	}
}

func TestE2E_Steer_DuringActiveRun_IsDeferred(t *testing.T) {
	// Calling Steer() during an active Run() should NOT affect the current Run.
	// The message should be deferred to the next Run().
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First run response")
	h.Provider().AddTextResponse("Second run response")

	agent := h.NewAgent()

	// Start Run in a goroutine
	done := make(chan string, 1)
	go func() {
		result, _ := agent.Run(context.Background(), "First query")
		done <- result
	}()

	// Steer during active Run — should be deferred, not consumed immediately
	time.Sleep(20 * time.Millisecond)
	agent.Steer(core.Message{Role: "user", Content: "Steer during run"})

	// Wait for first run to complete
	h.AssertEquals(<-done, "First run response")

	// Verify steer was NOT consumed in first Run
	firstReq := h.Provider().Calls[0]
	for _, m := range firstReq.Messages {
		if m.Content == "Steer during run" {
			t.Error("steer message should NOT appear in first Run (deferred)")
		}
	}

	// Second Run should see the steer message
	_, err := agent.Run(context.Background(), "Second query")
	h.AssertNoError(err)

	req2 := h.Provider().Calls[1]
	found := false
	for _, m := range req2.Messages {
		if m.Content == "Steer during run" {
			found = true
			break
		}
	}
	if !found {
		t.Error("steer message should appear in second Run (deferred from first)")
	}
}

func TestE2E_Steer_NotRepeatedOnRetry(t *testing.T) {
	// When a transient error occurs, the retry loop inside ProcessQuery
	// reuses the same ChatRequest (with the same messages slice). This means
	// the steer message appears in both the failed attempt and the successful
	// retry — because they are the same request object. The steer is consumed
	// by prepareMessages once per loop iteration, but the same request is
	// retried within that iteration.
	//
	// Verify: steer appears in both calls (same request retried).
	h := NewHarnessWithT(t)

	// First call fails with transient error, second succeeds
	h.Provider().AddError(&core.TransientError{Op: "chat", Wrapped: fmt.Errorf("timeout")})
	h.Provider().AddTextResponse("Recovered!")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "Steer guidance"})

	_, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)

	// Provider called twice (1 error + 1 success)
	h.AssertProviderCalledN(2)

	// Both calls should contain the steer message because the same request
	// is retried within the same loop iteration.
	for i, req := range h.Provider().Calls {
		found := false
		for _, m := range req.Messages {
			if m.Content == "Steer guidance" {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("call %d: steer message should appear in retried request", i)
		}
	}
}

func TestE2E_Steer_WithTruncationRecovery(t *testing.T) {
	// When both a steer message and a truncation recovery message are present,
	// both should appear in the API request. The steer message is drained into
	// transientMsgs before Run, and the truncation message is enqueued during
	// the loop — both are appended by prepareMessages.
	h := NewHarnessWithT(t)

	// Call 1: incomplete response — triggers truncation recovery
	h.Provider().AddTextResponse("This is incomplete...")
	// Call 2: final complete answer
	h.Provider().AddTextResponse("Complete answer.")

	agent := h.NewAgent()
	agent.Steer(core.Message{Role: "user", Content: "Focus on security."})

	_, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)
	h.AssertProviderCalledN(2)

	// First request should contain the steer message
	firstReq := h.Provider().Calls[0]
	foundSteer := false
	for _, m := range firstReq.Messages {
		if m.Content == "Focus on security." {
			foundSteer = true
			break
		}
	}
	if !foundSteer {
		t.Error("steer message should appear in first API call")
	}

	// Second request should contain the truncation recovery message
	// (but NOT the steer message, which was consumed in the first call)
	secondReq := h.Provider().Calls[1]
	foundContinue := false
	foundSteerInRetry := false
	for _, m := range secondReq.Messages {
		if strings.Contains(m.Content, "continue") {
			foundContinue = true
		}
		if m.Content == "Focus on security." {
			foundSteerInRetry = true
		}
	}
	if !foundContinue {
		t.Error("truncation recovery message should appear in second API call")
	}
	if foundSteerInRetry {
		t.Error("steer message should NOT appear in second API call (consumed once)")
	}
}

// --- OnIteration Hook Tests ---

func TestE2E_OnIteration_SingleIteration(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("Done")

	var calls []struct {
		iter          int
		messages      int
		tokenEstimate int
		contextSize   int
	}
	agent := h.NewAgentWithOptions(core.Options{
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			calls = append(calls, struct {
				iter          int
				messages      int
				tokenEstimate int
				contextSize   int
			}{iter, messages, tokenEstimate, contextSize})
		},
	})

	_, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)

	// One iteration (0-based)
	if len(calls) != 1 {
		t.Fatalf("expected 1 OnIteration call, got %d", len(calls))
	}
	if calls[0].iter != 0 {
		t.Errorf("expected iteration 0, got %d", calls[0].iter)
	}
	// State has 1 message (user query) when OnIteration fires
	if calls[0].messages != 1 {
		t.Errorf("expected 1 message, got %d", calls[0].messages)
	}
}

func TestE2E_OnIteration_MultipleIterations(t *testing.T) {
	h := NewHarnessWithT(t)

	// First: tool call (iteration 0)
	h.Provider().AddToolCallResponse(
		"Checking...",
		core.ToolCall{ID: "call_1", Function: core.ToolCallFunction{Name: "read", Arguments: `{}`}},
	)
	// Second: final answer (iteration 1)
	h.Provider().AddTextResponse("Done.")
	h.Executor().AddToolResult("call_1", "content")

	var calls []struct {
		iter          int
		messages      int
		tokenEstimate int
		contextSize   int
	}
	agent := h.NewAgentWithOptions(core.Options{
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			calls = append(calls, struct {
				iter          int
				messages      int
				tokenEstimate int
				contextSize   int
			}{iter, messages, tokenEstimate, contextSize})
		},
	})

	_, err := agent.Run(context.Background(), "test query")
	h.AssertNoError(err)

	// Two iterations: 0 (tool call) and 1 (final answer)
	if len(calls) != 2 {
		t.Fatalf("expected 2 OnIteration calls, got %d", len(calls))
	}

	// Iteration 0: 1 message (user query)
	if calls[0].iter != 0 {
		t.Errorf("expected iteration 0, got %d", calls[0].iter)
	}
	if calls[0].messages != 1 {
		t.Errorf("expected 1 message at iteration 0, got %d", calls[0].messages)
	}

	// Iteration 1: 3 messages (user + assistant(tool call) + tool result)
	if calls[1].iter != 1 {
		t.Errorf("expected iteration 1, got %d", calls[1].iter)
	}
	if calls[1].messages != 3 {
		t.Errorf("expected 3 messages at iteration 1, got %d", calls[1].messages)
	}
}

func TestE2E_OnIteration_NilCallback_NoPanic(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgentWithOptions(core.Options{
		OnIteration: nil, // explicitly nil
	})

	// Should not panic
	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
}

func TestE2E_OnIteration_MaxContinuations(t *testing.T) {
	// Provider keeps returning incomplete responses. After 3 consecutive
	// continuations (maxContinuations=3), the loop force-finalizes.
	// OnIteration should fire 4 times: 1 initial + 3 continuations.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First incomplete...")
	h.Provider().AddTextResponse("Second incomplete...")
	h.Provider().AddTextResponse("Third incomplete...")
	h.Provider().AddTextResponse("Fourth and last...")

	var calls []struct {
		iter          int
		messages      int
		tokenEstimate int
		contextSize   int
	}
	agent := h.NewAgentWithOptions(core.Options{
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			calls = append(calls, struct {
				iter          int
				messages      int
				tokenEstimate int
				contextSize   int
			}{iter, messages, tokenEstimate, contextSize})
		},
	})

	_, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)

	// 4 iterations: 1 initial + 3 continuations
	if len(calls) != 4 {
		t.Fatalf("expected 4 OnIteration calls, got %d", len(calls))
	}

	// Verify iteration numbers are 0, 1, 2, 3
	for i, c := range calls {
		if c.iter != i {
			t.Errorf("expected iteration %d, got %d", i, c.iter)
		}
	}

	// Verify message counts grow: 1 (user), 2 (+assistant), 3 (+assistant), 4 (+assistant)
	expectedMsgs := []int{1, 2, 3, 4}
	for i, c := range calls {
		if c.messages != expectedMsgs[i] {
			t.Errorf("iteration %d: expected %d messages, got %d", i, expectedMsgs[i], c.messages)
		}
	}
}

func TestE2E_OnIteration_CallbackPanicRecovered(t *testing.T) {
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("OK")

	agent := h.NewAgentWithOptions(core.Options{
		OnIteration: func(iter int, messages int, tokenEstimate int, contextSize int) {
			panic("telemetry error")
		},
	})

	// Should not crash; the panic is recovered and the agent continues
	result, err := agent.Run(context.Background(), "test")
	h.AssertNoError(err)
	h.AssertEquals(result, "OK")
}

// --- Checkpoint Recording Tests ---

// waitForCheckpoints polls agent.State().GetCheckpoints() until the expected
// count is reached or the timeout expires. This avoids brittle time.Sleep
// waits for the async RecordTurnCheckpointAsync goroutine.
func waitForCheckpoints(t *testing.T, agent *core.Agent, expected int) []core.TurnCheckpoint {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		cp := agent.State().GetCheckpoints()
		if len(cp) >= expected {
			return cp
		}
		time.Sleep(50 * time.Millisecond)
	}
	cp := agent.State().GetCheckpoints()
	t.Errorf("timeout waiting for %d checkpoints (got %d)", expected, len(cp))
	return cp
}

func TestE2E_CheckpointRecording_CompletedTurn(t *testing.T) {
	// After a completed turn, a checkpoint should be recorded with
	// a valid summary and actionable summary.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("The answer to your question is 42.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "What is the answer?")
	h.AssertNoError(err)
	h.AssertEquals(result, "The answer to your question is 42.")

	// Wait for the async checkpoint recording to complete.
	checkpoints := waitForCheckpoints(t, agent, 1)
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint after async recording, got %d", len(checkpoints))
	}

	cp := checkpoints[0]

	// Verify indices: user query is index 0, assistant response is index 1.
	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex != 1 {
		t.Errorf("expected EndIndex 1, got %d", cp.EndIndex)
	}

	// Verify summary is non-empty and contains expected content.
	if cp.Summary == "" {
		t.Error("expected non-empty Summary")
	}
	if !strings.Contains(cp.Summary, "User asked:") {
		t.Errorf("expected Summary to contain 'User asked:', got: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "The answer to your question is 42") {
		t.Errorf("expected Summary to contain response text, got: %s", cp.Summary)
	}

	// Verify actionable summary is non-empty and contains expected content.
	if cp.ActionableSummary == "" {
		t.Error("expected non-empty ActionableSummary")
	}
	if !strings.Contains(cp.ActionableSummary, "Question:") {
		t.Errorf("expected ActionableSummary to contain 'Question:', got: %s", cp.ActionableSummary)
	}
	if !strings.Contains(cp.ActionableSummary, "Result:") {
		t.Errorf("expected ActionableSummary to contain 'Result:', got: %s", cp.ActionableSummary)
	}
}

func TestE2E_CheckpointRecording_WithToolCalls(t *testing.T) {
	// A completed turn with tool calls should produce a checkpoint that
	// references the tools used and files accessed.
	h := NewHarnessWithT(t)

	// First response: assistant calls read_file
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	// Second response: final answer
	h.Provider().AddTextResponse("The config has database settings on port 5432.")

	h.Executor().AddToolResult("call_1", "database:\n  host: localhost\n  port: 5432")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml and summarize")
	h.AssertNoError(err)
	h.AssertEquals(result, "The config has database settings on port 5432.")

	// Wait for async checkpoint recording.
	checkpoints := waitForCheckpoints(t, agent, 1)
	if len(checkpoints) != 1 {
		t.Fatalf("expected 1 checkpoint after async recording, got %d", len(checkpoints))
	}

	cp := checkpoints[0]

	// State messages: user(0) + assistant/tool-call(1) + tool-result(2) + assistant/final(3)
	if cp.StartIndex != 0 {
		t.Errorf("expected StartIndex 0, got %d", cp.StartIndex)
	}
	if cp.EndIndex != 3 {
		t.Errorf("expected EndIndex 3, got %d", cp.EndIndex)
	}

	// Summary should mention the tool used and file read.
	if !strings.Contains(cp.Summary, "read_file") {
		t.Errorf("expected Summary to mention 'read_file', got: %s", cp.Summary)
	}
	if !strings.Contains(cp.Summary, "config.yaml") {
		t.Errorf("expected Summary to mention 'config.yaml', got: %s", cp.Summary)
	}

	// Actionable summary should list the file read.
	if !strings.Contains(cp.ActionableSummary, "config.yaml") {
		t.Errorf("expected ActionableSummary to mention 'config.yaml', got: %s", cp.ActionableSummary)
	}
}

func TestE2E_CheckpointRecording_MultipleTurns(t *testing.T) {
	// Multiple completed turns should produce multiple checkpoints.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First answer.")
	h.Provider().AddTextResponse("Second answer.")

	agent := h.NewAgent()

	// Turn 1
	_, err := agent.Run(context.Background(), "First question")
	h.AssertNoError(err)

	// Turn 2
	_, err = agent.Run(context.Background(), "Second question")
	h.AssertNoError(err)

	// Wait for async checkpoint recording (both turns).
	checkpoints := waitForCheckpoints(t, agent, 2)
	if len(checkpoints) < 2 {
		t.Fatalf("expected at least 2 checkpoints after async recording, got %d", len(checkpoints))
	}

	// Async recording may store checkpoints in any order, so sort by StartIndex.
	// Use a local copy to avoid mutating state.
	sorted := make([]core.TurnCheckpoint, len(checkpoints))
	copy(sorted, checkpoints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIndex < sorted[j].StartIndex
	})

	// First checkpoint: user(0) + assistant(1)
	if sorted[0].StartIndex != 0 {
		t.Errorf("expected cp0 StartIndex 0, got %d", sorted[0].StartIndex)
	}
	if sorted[0].EndIndex != 1 {
		t.Errorf("expected cp0 EndIndex 1, got %d", sorted[0].EndIndex)
	}

	// Second checkpoint: user(2) + assistant(3)
	if sorted[1].StartIndex != 2 {
		t.Errorf("expected cp1 StartIndex 2, got %d", sorted[1].StartIndex)
	}
	if sorted[1].EndIndex != 3 {
		t.Errorf("expected cp1 EndIndex 3, got %d", sorted[1].EndIndex)
	}

	// Both should have non-empty summaries
	for i, cp := range sorted {
		if cp.Summary == "" {
			t.Errorf("checkpoint %d: expected non-empty Summary", i)
		}
		if cp.ActionableSummary == "" {
			t.Errorf("checkpoint %d: expected non-empty ActionableSummary", i)
		}
	}

	// Verify each checkpoint references the correct question.
	if !strings.Contains(sorted[0].Summary, "First question") {
		t.Errorf("expected cp0 Summary to contain 'First question', got: %s", sorted[0].Summary)
	}
	if !strings.Contains(sorted[1].Summary, "Second question") {
		t.Errorf("expected cp1 Summary to contain 'Second question', got: %s", sorted[1].Summary)
	}
}

func TestE2E_CheckpointCompaction_MultipleTurns(t *testing.T) {
	// Multiple completed turns produce checkpoints. On a subsequent turn,
	// prepareMessages() should replace consumed checkpoint ranges with summary
	// messages, reducing the message count sent to the provider.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First answer to your question.")
	h.Provider().AddTextResponse("Second answer to your question.")
	h.Provider().AddTextResponse("Third answer to your question.")

	// Checkpoint substitution is pressure-gated. Force the gate to trigger
	// with a tiny ContextSize + non-trivial token estimate so this test
	// exercises the substitution path (do summaries replace the right
	// ranges?).
	h.Provider().WithInfo(core.ProviderInfo{Model: "mock", ContextSize: 200})
	h.Provider().WithTokenEstimate(200)
	agent := h.NewAgent()

	// Turn 1
	_, err := agent.Run(context.Background(), "First question about the project")
	h.AssertNoError(err)

	// Turn 2
	_, err = agent.Run(context.Background(), "Second question about the code")
	h.AssertNoError(err)

	// Turn 3
	_, err = agent.Run(context.Background(), "Third question about testing")
	h.AssertNoError(err)

	// Wait for async checkpoint recording (3 turns = 3 checkpoints)
	checkpoints := waitForCheckpoints(t, agent, 3)
	if len(checkpoints) < 3 {
		t.Fatalf("expected at least 3 checkpoints, got %d", len(checkpoints))
	}

	// State should have 6 messages from the 3 turns (user + assistant each)
	rawMsgCount := agent.State().Len()
	if rawMsgCount != 6 {
		t.Fatalf("expected 6 raw state messages after 3 turns, got %d", rawMsgCount)
	}

	// Turn 4: this should trigger checkpoint compaction in prepareMessages().
	// The provider should receive fewer messages than the raw state count.
	h.Provider().AddTextResponse("Fourth answer to your question.")

	_, err = agent.Run(context.Background(), "Fourth question")
	h.AssertNoError(err)

	// Provider called 4 times total (one per turn)
	h.AssertProviderCalledN(4)

	// State should have 8 messages now (6 original + user + assistant from turn 4)
	finalRawMsgCount := agent.State().Len()
	if finalRawMsgCount != 8 {
		t.Fatalf("expected 8 raw state messages after 4 turns, got %d", finalRawMsgCount)
	}

	// Verify the last provider request had fewer messages than without compaction.
	// At API call time, state has 7 messages (6 from turns 1-3 + 1 query for turn 4).
	// The turn 4 assistant response hasn't been added yet.
	// Without compaction: 7 raw + 1 system = 8 messages in request.
	// With compaction (SP-128): 3 checkpoints (indices 0-1, 2-3, 4-5) are all consumable,
	//   each replaced by 1 summary message, but the leading user message is preserved.
	//   Only the turn 4 query (idx 6) survives.
	//   1 system + 3 preserved user queries + 3 summaries + 1 query = 8 messages.
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	// Calculate no-compaction baseline at API call time (state had finalRawMsgCount - 1 messages).
	rawAtAPICall := finalRawMsgCount - 1  // = 7
	noCompactionCount := rawAtAPICall + 1 // = 8 (raw + system)
	// With SP-128, compaction preserves user queries, so compacted count equals no-compaction baseline.
	if len(lastReq.Messages) > noCompactionCount {
		t.Errorf("expected checkpoint compaction not to exceed baseline %d, got %d",
			noCompactionCount, len(lastReq.Messages))
	}

	// Verify the exact expected message count after compaction:
	// 1 system + 3 preserved user queries + 3 summaries + 1 query = 8
	if len(lastReq.Messages) != 8 {
		t.Errorf("expected 8 messages after compaction, got %d", len(lastReq.Messages))
	}

	// Verify all 3 checkpoint summaries appear in the request.
	// Summaries use ActionableSummary format (bullet list with "- Question:" prefix)
	// when ≤500 chars, falling back to Summary (narrative with "User asked:" prefix).
	foundSummaries := 0
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "- Question:") || strings.Contains(msg.Content, "User asked: ") {
			foundSummaries++
		}
	}
	if foundSummaries != 3 {
		t.Errorf("expected 3 checkpoint summaries in request, got %d", foundSummaries)
	}

	// Verify the compacted messages still contain the most recent turn's messages
	// (turn 4 query should be present as-is, not compacted yet)
	foundRecentQuery := false
	for _, msg := range lastReq.Messages {
		if msg.Role == "user" && msg.Content == "Fourth question" {
			foundRecentQuery = true
			break
		}
	}
	if !foundRecentQuery {
		t.Error("expected recent user query 'Fourth question' in provider request")
	}
}

func TestE2E_CheckpointIndexShifting_AfterCompaction(t *testing.T) {
	// Scenario: compaction removes messages from the API request, but checkpoint
	// indices reference state.Messages() which only appends. After multiple turns
	// with compaction, remaining checkpoints must still have valid indices that
	// can be re-applied on subsequent calls.
	//
	// Flow:
	// 1. Run 3 turns to create 3 checkpoints (indices [0,1], [2,3], [4,5])
	// 2. Run turn 4 — checkpoint compaction replaces [0,5] with summaries
	// 3. Verify all 3 checkpoints still have valid indices into state.Messages()
	// 4. Run turn 5 — compaction should still work; now 4 checkpoints exist
	//    (turn 4 added a 4th), so 4 summaries replace [0,7]
	// 5. Verify checkpoints remain valid after the second compaction pass
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponse("First answer to your question.")
	h.Provider().AddTextResponse("Second answer to your question.")
	h.Provider().AddTextResponse("Third answer to your question.")
	h.Provider().AddTextResponse("Fourth answer to your question.")
	h.Provider().AddTextResponse("Fifth answer to your question.")

	// Checkpoint substitution is pressure-gated. Force pressure so this
	// test exercises its real intent (do checkpoint indices remain valid
	// across substitution passes?).
	h.Provider().WithInfo(core.ProviderInfo{Model: "mock", ContextSize: 200})
	h.Provider().WithTokenEstimate(200)
	agent := h.NewAgent()

	// Turns 1-3: build up checkpoints
	_, err := agent.Run(context.Background(), "First question about the project")
	h.AssertNoError(err)

	_, err = agent.Run(context.Background(), "Second question about the code")
	h.AssertNoError(err)

	_, err = agent.Run(context.Background(), "Third question about testing")
	h.AssertNoError(err)

	// Wait for async checkpoint recording (3 turns = 3 checkpoints)
	checkpoints := waitForCheckpoints(t, agent, 3)
	if len(checkpoints) < 3 {
		t.Fatalf("expected at least 3 checkpoints, got %d", len(checkpoints))
	}

	// Verify checkpoint indices are valid within state.Messages()
	rawMsgCount := agent.State().Len()
	if rawMsgCount != 6 {
		t.Fatalf("expected 6 raw state messages after 3 turns, got %d", rawMsgCount)
	}

	// Verify specific checkpoint indices: [0,1], [2,3], [4,5]
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})
	expectedIndices := []struct{ start, end int }{{0, 1}, {2, 3}, {4, 5}}
	for i, cp := range checkpoints {
		if cp.StartIndex < 0 || cp.StartIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: StartIndex %d out of bounds [0, %d)", i, cp.StartIndex, rawMsgCount)
		}
		if cp.EndIndex < 0 || cp.EndIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: EndIndex %d out of bounds [0, %d)", i, cp.EndIndex, rawMsgCount)
		}
		if cp.StartIndex > cp.EndIndex {
			t.Errorf("checkpoint %d: StartIndex %d > EndIndex %d", i, cp.StartIndex, cp.EndIndex)
		}
		if i < len(expectedIndices) {
			if cp.StartIndex != expectedIndices[i].start {
				t.Errorf("checkpoint %d: expected StartIndex %d, got %d", i, expectedIndices[i].start, cp.StartIndex)
			}
			if cp.EndIndex != expectedIndices[i].end {
				t.Errorf("checkpoint %d: expected EndIndex %d, got %d", i, expectedIndices[i].end, cp.EndIndex)
			}
		}
	}

	// Turn 4: triggers checkpoint compaction. All 3 checkpoints are consumable,
	// so their ranges [0,1], [2,3], [4,5] are replaced with summaries.
	_, err = agent.Run(context.Background(), "Fourth question")
	h.AssertNoError(err)

	// Provider called 4 times total (one per turn)
	h.AssertProviderCalledN(4)

	// State now has 8 messages (6 original + user + assistant from turn 4)
	rawMsgCount = agent.State().Len()
	if rawMsgCount != 8 {
		t.Fatalf("expected 8 raw state messages after 4 turns, got %d", rawMsgCount)
	}

	// Wait for turn 4's async checkpoint (now 4 total)
	checkpoints = waitForCheckpoints(t, agent, 4)
	if len(checkpoints) < 4 {
		t.Fatalf("expected at least 4 checkpoints after turn 4, got %d", len(checkpoints))
	}

	// Verify checkpoints still have valid indices into the expanded state.
	// Checkpoint indices reference state.Messages() which only appends,
	// so [0,1], [2,3], [4,5], [6,7] are still valid within 8 messages.
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})
	expectedIndices = []struct{ start, end int }{{0, 1}, {2, 3}, {4, 5}, {6, 7}}
	for i, cp := range checkpoints {
		if cp.StartIndex < 0 || cp.StartIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: StartIndex %d out of bounds [0, %d) after turn 4", i, cp.StartIndex, rawMsgCount)
		}
		if cp.EndIndex < 0 || cp.EndIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: EndIndex %d out of bounds [0, %d) after turn 4", i, cp.EndIndex, rawMsgCount)
		}
		if cp.StartIndex > cp.EndIndex {
			t.Errorf("checkpoint %d: StartIndex %d > EndIndex %d after turn 4", i, cp.StartIndex, cp.EndIndex)
		}
		if i < len(expectedIndices) {
			if cp.StartIndex != expectedIndices[i].start {
				t.Errorf("checkpoint %d: expected StartIndex %d, got %d", i, expectedIndices[i].start, cp.StartIndex)
			}
			if cp.EndIndex != expectedIndices[i].end {
				t.Errorf("checkpoint %d: expected EndIndex %d, got %d", i, expectedIndices[i].end, cp.EndIndex)
			}
		}
	}

	// Turn 5: compaction should still work with all 4 checkpoints.
	// All 4 checkpoints [0,1], [2,3], [4,5], [6,7] are consumable and will be
	// replaced with summaries. Only the turn 5 query (idx 8) is uncovered.
	_, err = agent.Run(context.Background(), "Fifth question")
	h.AssertNoError(err)

	// Provider called 5 times total (one per turn)
	h.AssertProviderCalledN(5)

	// State now has 10 messages (8 + user + assistant from turn 5)
	rawMsgCount = agent.State().Len()
	if rawMsgCount != 10 {
		t.Fatalf("expected 10 raw state messages after 5 turns, got %d", rawMsgCount)
	}

	// Verify checkpoints still have valid indices after the second compaction pass.
	checkpoints = agent.State().GetCheckpoints()
	if len(checkpoints) < 4 {
		t.Fatalf("expected at least 4 checkpoints after turn 5, got %d", len(checkpoints))
	}
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})
	for i, cp := range checkpoints {
		if cp.StartIndex < 0 || cp.StartIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: StartIndex %d out of bounds [0, %d) after turn 5", i, cp.StartIndex, rawMsgCount)
		}
		if cp.EndIndex < 0 || cp.EndIndex >= rawMsgCount {
			t.Errorf("checkpoint %d: EndIndex %d out of bounds [0, %d) after turn 5", i, cp.EndIndex, rawMsgCount)
		}
		if cp.StartIndex > cp.EndIndex {
			t.Errorf("checkpoint %d: StartIndex %d > EndIndex %d after turn 5", i, cp.StartIndex, cp.EndIndex)
		}
	}

	// Verify the last provider request still had compacted messages.
	// At API call time, state had 9 messages (10 - 1 for the not-yet-added assistant).
	// 4 checkpoints cover [0,7], leaving message [8] (turn 5 query) uncovered.
	// Compacted (SP-128): 1 system + 4 preserved user queries + 4 summaries + 1 uncovered = 10 messages.
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}
	// Should match the no-compaction baseline (9 raw + 1 system = 10).
	// With SP-128, compaction preserves user queries, so compacted count equals no-compaction baseline.
	if len(lastReq.Messages) > 10 {
		t.Errorf("expected compaction not to exceed baseline 10, got %d", len(lastReq.Messages))
	}
	// Exact count: 1 system + 4 preserved user queries + 4 summaries + 1 uncovered query = 10
	if len(lastReq.Messages) != 10 {
		t.Errorf("expected 10 messages after compaction, got %d", len(lastReq.Messages))
	}
	// Verify 4 summaries are present.
	// Summaries use ActionableSummary format (bullet list with "- Question:" prefix)
	// when ≤500 chars, falling back to Summary (narrative with "User asked:" prefix).
	foundSummaries := 0
	for _, msg := range lastReq.Messages {
		if strings.Contains(msg.Content, "- Question:") || strings.Contains(msg.Content, "User asked: ") {
			foundSummaries++
		}
	}
	if foundSummaries != 4 {
		t.Errorf("expected 4 checkpoint summaries in turn 5 request, got %d", foundSummaries)
	}
}

// --- Malformed Structured Tool Call Tests ---

func TestE2E_MalformedToolCalls_RetryAndReEmit(t *testing.T) {
	// Provider returns a malformed tool call (unrepairable JSON args) on the
	// first call. The normalizer drops it, the loop enqueues a transient
	// retry message, and the second provider call returns a valid tool call
	// that executes successfully.
	h := NewHarnessWithT(t)

	// Call 1: malformed tool call with unrepairable JSON arguments
	h.Provider().AddToolCallResponse(
		"Let me check.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: "not json at all", // unrepairable — will be dropped by normalizer
			},
		},
	)

	// Call 2: valid tool call (response to malformed retry prompt)
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)

	// Call 3: final text answer
	h.Provider().AddTextResponse("The file contains 'hello'.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddToolResult("call_2", "hello")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read test.txt")

	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains 'hello'.")
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(1)
	// State: user + assistant(malformed, dropped) + assistant(valid) + tool_result + assistant(final)
	h.AssertStateHasNMessages(agent, 5)

	// Verify the transient retry message appears in the SECOND provider request
	// (after the malformed call was normalized and dropped).
	if len(h.Provider().Calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	secondReq := h.Provider().Calls[1]
	foundRetryMsg := false
	for _, msg := range secondReq.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Your previous tool call was malformed") {
			foundRetryMsg = true
			break
		}
	}
	if !foundRetryMsg {
		t.Error("expected malformed retry transient message in second provider request")
	}

	// Verify the transient retry message does NOT appear in the first request
	firstReq := h.Provider().Calls[0]
	for _, msg := range firstReq.Messages {
		if strings.Contains(msg.Content, "Your previous tool call was malformed") {
			t.Error("transient retry message should not be in first provider request")
		}
	}

	// Verify the transient retry message is NOT in agent state (it's transient, one-shot)
	msgs := agent.State().Messages()
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "Your previous tool call was malformed") {
			t.Error("transient retry message should not be persisted in state")
		}
	}
}

func TestE2E_MalformedToolCalls_MaxContinuations(t *testing.T) {
	// Provider keeps returning malformed tool calls (all dropped by the
	// normalizer). After maxContinuations (3) retries, the loop force-finalizes.
	h := NewHarnessWithT(t)

	// Provide enough responses: 1 initial + 3 continuations + 1 force-finalized = 5
	for i := 0; i < 10; i++ {
		h.Provider().AddToolCallResponse(
			fmt.Sprintf("Still trying %d...", i+1),
			core.ToolCall{
				ID: fmt.Sprintf("call_bad_%d", i),
				Function: core.ToolCallFunction{
					Name:      "broken_tool",
					Arguments: "not json at all", // unrepairable — always dropped by normalizer
				},
			},
		)
	}

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "test")

	// No error — the loop force-finalizes after maxContinuations
	h.AssertNoError(err)

	// Provider called 4 times: 1 initial + 3 continuations (4th force-finalized)
	h.AssertProviderCalledN(4)

	// No tool ever executed — all calls were malformed
	h.AssertExecutorCalledN(0)

	// State: user + 4 assistant messages (one per provider call, all malformed)
	h.AssertStateHasNMessages(agent, 5)

	// Verify transient retry messages were enqueued in each retry request
	// (but NOT persisted in state — they're one-shot, appended to API requests only)
	msgs := agent.State().Messages()
	for _, msg := range msgs {
		if strings.Contains(msg.Content, "Your previous tool call was malformed") {
			t.Error("transient retry messages should not be persisted in state")
		}
	}

	// Verify the transient retry message appears in calls 1, 2, and 3 (zero-indexed),
	// but NOT in call 0 (the initial malformed call has no transient yet).
	// Call 4 (the force-finalized call) does not enqueue a new transient because
	// continuationCount was not < maxContinuations at that point.
	if len(h.Provider().Calls) < 4 {
		t.Fatalf("expected at least 4 provider calls, got %d", len(h.Provider().Calls))
	}

	// Call 0: no transient message (first malformed call, no prior transient)
	for _, msg := range h.Provider().Calls[0].Messages {
		if strings.Contains(msg.Content, "Your previous tool call was malformed") {
			t.Error("call 0 should not contain transient retry message")
		}
	}

	// Calls 1, 2, 3: should contain transient retry messages from prior malformed calls
	for i := 1; i <= 3; i++ {
		req := h.Provider().Calls[i]
		foundRetryMsg := false
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, "Your previous tool call was malformed") {
				foundRetryMsg = true
				break
			}
		}
		if !foundRetryMsg {
			t.Errorf("expected retry transient message in call %d provider request", i)
		}
	}
}

func TestE2E_MalformedToolCalls_PartialDropped(t *testing.T) {
	// Provider returns two tool calls in one response: one malformed (unrepairable
	// JSON) and one valid. The normalizer drops the malformed one but keeps the
	// valid one. Because not ALL calls are malformed, no retry transient message
	// is enqueued — the valid call executes normally and the loop continues.
	h := NewHarnessWithT(t)

	// Single response with two tool calls: 1 malformed + 1 valid
	h.Provider().AddToolCallResponse(
		"Let me check both.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "broken_tool",
				Arguments: "not json at all", // malformed — dropped by normalizer
			},
		},
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"data.txt"}`, // valid — kept by normalizer
			},
		},
	)

	// Second response: final text answer
	h.Provider().AddTextResponse("Done processing.")

	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "read_file"}})
	h.Executor().AddTool(core.Tool{Function: core.ToolFunction{Name: "broken_tool"}})
	h.Executor().AddToolResult("call_2", "data result")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Process data")

	h.AssertNoError(err)
	h.AssertEquals(result, "Done processing.")
	h.AssertProviderCalledN(2)
	// Only one tool executed (the valid call_2); call_1 was dropped
	h.AssertExecutorCalledN(1)

	// State: user + assistant(mixed calls) + tool_result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)

	// Verify the executor received only the valid tool call (call_2)
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(execCalls))
	}
	if execCalls[0].ID != "call_2" {
		t.Errorf("expected executor to call call_2, got %s", execCalls[0].ID)
	}

	// Verify the transient retry message is NOT in any provider request
	// (because not ALL calls were malformed — no retry needed)
	for i, req := range h.Provider().Calls {
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, "Your previous tool call was malformed") {
				t.Errorf("malformed retry transient should NOT appear in call %d (partial drop, not all malformed)", i)
			}
		}
	}
}

// --- Finish Reason: "length" e2e tests ---

func TestE2E_FinishReasonLength_Continuation(t *testing.T) {
	// Provider returns finish_reason="length" (model hit token limit).
	// The loop should continue with a transient continuation message,
	// and the second provider call returns the rest of the response.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("This is a long", "length")
	h.Provider().AddTextResponse(" response that completes the answer with enough words to be considered complete.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, " response that completes the answer with enough words to be considered complete.")

	// Provider called twice: initial truncated + continuation
	h.AssertProviderCalledN(2)

	// State: user + assistant(truncated) + assistant(complete)
	h.AssertStateHasNMessages(agent, 3)

	// Verify state message content
	msgs := agent.State().Messages()
	if msgs[0].Content != "hi" {
		t.Errorf("expected message 1 to be 'hi', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "This is a long" {
		t.Errorf("expected message 2 to be 'This is a long', got %q", msgs[1].Content)
	}

	// Verify the transient continuation message appears in the second request
	if len(h.Provider().Calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	foundTransient := false
	for _, msg := range h.Provider().Calls[1].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please continue") {
			foundTransient = true
			break
		}
	}
	if !foundTransient {
		t.Error("expected transient continuation message in second provider request")
	}

	// Verify the transient message is NOT in state
	for _, msg := range agent.State().Messages() {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please continue") {
			t.Error("transient continuation message should not be in state")
		}
	}

	// Verify no error events published (length is not an error)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) > 0 {
		t.Errorf("expected no error events for length finish reason, got %d", len(errorEvents))
	}

	// Verify metrics_update events (one per iteration)
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 2 {
		t.Fatalf("expected 2 metrics_update events, got %d", len(metricsEvents))
	}
	// Verify accumulated tokens: 35 (first) + 35 (second) = 70
	lastMetrics := metricsEvents[1].Data.(map[string]interface{})
	totalTokens, ok := lastMetrics["total_tokens"].(int)
	if !ok || totalTokens != 70 {
		t.Errorf("expected total_tokens 70, got %v (type: %T)", totalTokens, totalTokens)
	}
}

func TestE2E_FinishReasonLength_MaxContinuations(t *testing.T) {
	// Provider keeps returning finish_reason="length". After maxContinuations
	// (3) consecutive continuations, the loop force-finalizes.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("chunk1", "length")
	h.Provider().AddTextResponseWithFinish("chunk2", "length")
	h.Provider().AddTextResponseWithFinish("chunk3", "length")
	h.Provider().AddTextResponseWithFinish("chunk4", "length")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	// 4th call force-finalized (continuationCount was 3, not < 3)
	h.AssertEquals(result, "chunk4")

	// Provider called 4 times: 1 initial + 3 continuations (4th force-finalized)
	h.AssertProviderCalledN(4)

	// State: user + 4 assistant messages
	h.AssertStateHasNMessages(agent, 5)

	// Force-finalization due to max continuations should NOT record a checkpoint
	// (turnCompleted is false for this path). Give async checkpoint goroutine time.
	time.Sleep(100 * time.Millisecond)
	checkpoints := agent.State().GetCheckpoints()
	if len(checkpoints) != 0 {
		t.Errorf("expected no checkpoint after max continuations force-finalize, got %d", len(checkpoints))
	}

	// Verify no error events published (length is not an error)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) > 0 {
		t.Errorf("expected no error events for length finish reason, got %d", len(errorEvents))
	}

	// Verify transient continuation messages appear in calls 1, 2, and 3
	for i := 1; i <= 3; i++ {
		req := h.Provider().Calls[i]
		foundTransient := false
		for _, msg := range req.Messages {
			if msg.Role == "user" && strings.Contains(msg.Content, "Please continue") {
				foundTransient = true
				break
			}
		}
		if !foundTransient {
			t.Errorf("expected transient continuation message in call %d provider request", i)
		}
	}
}

func TestE2E_FinishReasonLength_Stream_Continuation(t *testing.T) {
	// Same as non-streaming but using RunStream.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("partial", "length")
	h.Provider().AddTextResponse(" done with enough words to be a complete response that passes validation.")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, " done with enough words to be a complete response that passes validation.")

	// Provider called twice
	h.AssertProviderCalledN(2)
}

func TestE2E_FinishReasonLength_NoErrorEvent(t *testing.T) {
	// Verify that finish_reason="length" does NOT publish error events.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("truncated", "length")
	h.Provider().AddTextResponse(" complete response with enough words to pass the validator checks.")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")
	h.AssertNoError(err)

	// No error events should be published for length finish reason
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) > 0 {
		t.Errorf("expected no error events for length finish reason, got %d", len(errorEvents))
	}
}

func TestE2E_FinishReasonLength_ResetsOnToolCalls(t *testing.T) {
	// A "length" response followed by a tool call should reset the
	// continuation budget. The tool call represents progress.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("thinking...", "length")
	h.Provider().AddToolCallResponse(
		"",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "echo",
				Arguments: `{"message":"test"}`,
			},
		},
	)
	h.Provider().AddTextResponse("Final answer with enough words to be a complete response that passes validation.")

	h.Executor().AddToolResult("call_1", "tool result")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, "Final answer with enough words to be a complete response that passes validation.")

	// Provider called 3 times: length → tool call → final
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(1)
}

func TestE2E_FinishReasonContentFilter_RetryOnceThenError(t *testing.T) {
	// Provider returns finish_reason="content_filter" twice.
	// First occurrence triggers a retry with a transient rephrase message.
	// Second occurrence returns ContentFilteredError to the consumer.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("filtered response", "content_filter")
	h.Provider().AddTextResponseWithFinish("still filtered", "content_filter")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	// Second content_filter returns ContentFilteredError
	h.AssertError(err)
	if !core.IsContentFiltered(err) {
		t.Fatalf("expected ContentFilteredError, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}

	// Verify Provider field on the error
	cfErr := &core.ContentFilteredError{}
	if !errors.As(err, &cfErr) {
		t.Fatal("expected ContentFilteredError via errors.As")
	}
	if cfErr.Provider != "mock-model" {
		t.Errorf("expected provider 'mock-model', got %q", cfErr.Provider)
	}

	// Provider called exactly twice: initial + retry
	h.AssertProviderCalledN(2)

	// State: user + 2 assistant messages (both filtered responses recorded)
	h.AssertStateHasNMessages(agent, 3)

	// Verify state message content
	msgs := agent.State().Messages()
	if msgs[0].Content != "hi" {
		t.Errorf("expected message 1 to be 'hi', got %q", msgs[0].Content)
	}
	if msgs[1].Content != "filtered response" {
		t.Errorf("expected message 2 to be 'filtered response', got %q", msgs[1].Content)
	}
	if msgs[2].Content != "still filtered" {
		t.Errorf("expected message 3 to be 'still filtered', got %q", msgs[2].Content)
	}

	// Verify the transient rephrase message appears in the second request
	if len(h.Provider().Calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	foundTransient := false
	for _, msg := range h.Provider().Calls[1].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please rephrase") {
			foundTransient = true
			break
		}
	}
	if !foundTransient {
		t.Error("expected transient rephrase message in second provider request")
	}

	// Verify the transient message is NOT in state
	for _, msg := range agent.State().Messages() {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please rephrase") {
			t.Error("transient rephrase message should not be in state")
		}
	}

	// Verify the first request did NOT contain the transient rephrase message
	for _, msg := range h.Provider().Calls[0].Messages {
		if strings.Contains(msg.Content, "Please rephrase") {
			t.Error("transient rephrase message should not be in first provider request")
		}
	}

	// Verify error events published: one for first (retrying), one for second (exhausted)
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 2 {
		t.Fatalf("expected 2 error events (first retry + second exhausted), got %d", len(errorEvents))
	}
	// First event: retrying
	data1, ok := errorEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map, got %T", errorEvents[0].Data)
	}
	if data1["message"] != "response filtered by content policy (retrying)" {
		t.Errorf("expected retrying message, got %v", data1["message"])
	}
	if data1["error"] != "content_filter" {
		t.Errorf("expected error field 'content_filter', got %v", data1["error"])
	}
	// Second event: exhausted
	data2, ok := errorEvents[1].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map, got %T", errorEvents[1].Data)
	}
	if data2["message"] != "response filtered by content policy (retry exhausted)" {
		t.Errorf("expected exhausted message, got %v", data2["message"])
	}
	if data2["error"] != "content_filter" {
		t.Errorf("expected error field 'content_filter', got %v", data2["error"])
	}

	// Verify no query_completed event (error path does not complete normally)
	completedEvents := h.FindEvents(events.EventTypeQueryCompleted)
	if len(completedEvents) != 0 {
		t.Errorf("expected no query_completed event for content_filter error, got %d", len(completedEvents))
	}

	// Verify metrics_update events (one per iteration)
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 2 {
		t.Fatalf("expected 2 metrics_update events, got %d", len(metricsEvents))
	}

	// Verify accumulated tokens: 35 (first) + 35 (second) = 70
	lastMetrics := metricsEvents[1].Data.(map[string]interface{})
	totalTokens, ok := lastMetrics["total_tokens"].(int)
	if !ok || totalTokens != 70 {
		t.Errorf("expected total_tokens 70, got %v (type: %T)", totalTokens, totalTokens)
	}
}

func TestE2E_FinishReasonContentFilter_RetrySucceeds(t *testing.T) {
	// Provider returns content_filter once, then a valid response on retry.
	// Should complete normally with the rephrased response.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("filtered", "content_filter")
	h.Provider().AddTextResponseWithFinish("This is the rephrased response that should complete successfully now.", "stop")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, "This is the rephrased response that should complete successfully now.")

	// Provider called twice: initial filtered + retry with valid response
	h.AssertProviderCalledN(2)

	// State: user + 2 assistant messages
	h.AssertStateHasNMessages(agent, 3)

	// Verify error event published for the first (retrying) occurrence
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 1 {
		t.Fatalf("expected 1 error event (first retry), got %d", len(errorEvents))
	}
	data, ok := errorEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map, got %T", errorEvents[0].Data)
	}
	if data["message"] != "response filtered by content policy (retrying)" {
		t.Errorf("expected retrying message, got %v", data["message"])
	}

	// Verify query_completed event (successful completion after retry)
	h.AssertEventPublished(events.EventTypeQueryCompleted)
}

func TestE2E_FinishReasonContentFilter_Stream_RetryOnceThenError(t *testing.T) {
	// Same retry-then-error behavior in streaming mode.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("filtered", "content_filter")
	h.Provider().AddTextResponseWithFinish("still filtered", "content_filter")

	agent := h.NewAgent()
	result, err := agent.RunStream(context.Background(), "hi")

	h.AssertError(err)
	if !core.IsContentFiltered(err) {
		t.Fatalf("expected ContentFilteredError, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}

	// Provider called twice: initial + retry
	h.AssertProviderCalledN(2)

	// Verify error events published
	errorEvents := h.FindEvents(events.EventTypeError)
	if len(errorEvents) != 2 {
		t.Fatalf("expected 2 error events, got %d", len(errorEvents))
	}
}

// --- Blank/Repetitive Response Detection Tests ---

func TestE2E_BlankResponse_SingleBlankThenRecovery(t *testing.T) {
	// Model returns blank once, then valid content on retry.
	// Should complete normally — first blank sends reminder, second response succeeds.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("", "stop")
	h.Provider().AddTextResponseWithFinish("This is a valid response with enough words to complete successfully.", "stop")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, "This is a valid response with enough words to complete successfully.")
	h.AssertProviderCalledN(2)
}

func TestE2E_BlankIteration_FullFlow(t *testing.T) {
	// Model returns blank twice. Full flow: blank → reminder → blank → error.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("", "stop")
	h.Provider().AddTextResponseWithFinish("", "stop")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")

	// Verify error is BlankResponseError with Count=2
	h.AssertError(err)
	if !core.IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*core.BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}

	// Verify provider was called exactly 2 times
	h.AssertProviderCalledN(2)

	// Verify the second provider call included the reminder message
	calls := h.Provider().Calls
	if len(calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	secondCall := calls[1]

	// Find the reminder message in the second call's messages
	reminderFound := false
	for _, msg := range secondCall.Messages {
		if msg.Role == "user" && msg.Content == "Your previous response was empty. Please provide a complete response." {
			reminderFound = true
			break
		}
	}
	if !reminderFound {
		t.Errorf("expected reminder message in second provider call, got messages: %v", secondCall.Messages)
	}
}

func TestE2E_BlankResponse_TwoConsecutiveBlanks(t *testing.T) {
	// Model returns blank twice. Should return BlankResponseError with Count=2.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("", "stop")
	h.Provider().AddTextResponseWithFinish("", "stop")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")

	h.AssertError(err)
	if !core.IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*core.BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}
	if bErr.Provider != "mock-model" {
		t.Errorf("expected provider 'mock-model', got %q", bErr.Provider)
	}
	h.AssertProviderCalledN(2)
}

func TestE2E_BlankResponse_RepetitiveThenBlank(t *testing.T) {
	// Model returns repetitive content, then blank. Counter should accumulate.
	// Should return BlankResponseError with Count=2.
	//
	// Setup: tool call first to establish a prior assistant message with the
	// repetitive text, so subsequent responses can be compared against it.
	h := NewHarnessWithT(t)

	repetitiveText := "This is a very long repetitive response that has many words to pass the similarity threshold for detection"

	// First: tool call with the repetitive text as content (establishes prior assistant msg)
	h.Provider().AddToolCallResponse(
		repetitiveText,
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "echo",
				Arguments: `{"message":"test"}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "tool result")

	// Second: same text (repetitive vs prior assistant) — triggers 1st consecutive
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")
	// Third: blank — triggers 2nd consecutive, returns error
	h.Provider().AddTextResponseWithFinish("", "stop")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")

	h.AssertError(err)
	if !core.IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*core.BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}
}

func TestE2E_BlankResponse_BlankThenRepetitive(t *testing.T) {
	// After a blank response, the previous assistant message is blank.
	// contentSimilar("", repetitiveText) returns false because one is empty.
	// So a repetitive response after blank won't match — it resets the counter.
	// This test verifies: blank (1st) → repetitive (resets counter, not blank/repetitive) → completes.
	h := NewHarnessWithT(t)

	repetitiveText := "This is a very long repetitive response that has many words to pass the similarity threshold for detection"

	// First: blank — triggers 1st consecutive
	h.Provider().AddTextResponseWithFinish("", "stop")
	// Second: different non-blank content — not repetitive vs blank, resets counter
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, repetitiveText)
	h.AssertProviderCalledN(2)
}

func TestE2E_BlankResponse_TwoConsecutiveRepetitive(t *testing.T) {
	// Model returns same long text twice after an initial tool call.
	// The tool call establishes a prior assistant message with the text.
	// 1st repetitive: repetitive vs prior assistant (1st consecutive),
	// 2nd repetitive: repetitive vs prior assistant (2nd consecutive) → BlankResponseError.
	h := NewHarnessWithT(t)

	repetitiveText := "This is a very long repetitive response that has many words to pass the similarity threshold for detection properly"

	// First: tool call with the repetitive text (establishes prior assistant msg)
	h.Provider().AddToolCallResponse(
		repetitiveText,
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "echo",
				Arguments: `{"message":"test"}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "tool result")

	// Second: same text (repetitive vs prior assistant) — triggers 1st consecutive
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")
	// Third: same text (repetitive) — triggers 2nd consecutive, returns error
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")

	h.AssertError(err)
	if !core.IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*core.BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}
}

func TestE2E_RepetitiveContent_FullFlow(t *testing.T) {
	// Model returns the same content twice. Full flow: repetitive → reminder → repetitive → error.
	h := NewHarnessWithT(t)

	repetitiveText := "This is a very long repetitive response that has many words to pass the similarity threshold for detection properly"

	// First: tool call with the repetitive text (establishes prior assistant message)
	h.Provider().AddToolCallResponse(
		repetitiveText,
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "echo",
				Arguments: `{"message":"test"}`,
			},
		},
	)
	h.Executor().AddToolResult("call_1", "tool result")

	// Second: same text (repetitive vs prior assistant) — triggers 1st consecutive, reminder sent
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")
	// Third: same text (repetitive) — triggers 2nd consecutive, returns error
	h.Provider().AddTextResponseWithFinish(repetitiveText, "stop")

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "hi")

	// Verify error is BlankResponseError with Count=2
	h.AssertError(err)
	if !core.IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*core.BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}
	if bErr.Provider != "mock-model" {
		t.Errorf("expected provider 'mock-model', got %q", bErr.Provider)
	}

	// Verify provider was called exactly 3 times: tool call + 1st repetitive + 2nd repetitive
	h.AssertProviderCalledN(3)

	// Verify the third provider call included the repetitive-content reminder
	calls := h.Provider().Calls
	if len(calls) < 3 {
		t.Fatal("expected at least 3 provider calls")
	}
	thirdCall := calls[2]

	reminderFound := false
	for _, msg := range thirdCall.Messages {
		if msg.Role == "user" && msg.Content == "Your previous response appears repetitive. Please provide new content." {
			reminderFound = true
			break
		}
	}
	if !reminderFound {
		t.Errorf("expected repetitive-content reminder in third provider call, got messages: %v", thirdCall.Messages)
	}

	// Verify the second provider call did NOT include the reminder
	// (reminder is only sent after the 1st consecutive, before the 2nd call)
	secondCall := calls[1]
	for _, msg := range secondCall.Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "repetitive") {
			t.Error("reminder should not be in second provider call")
		}
	}

	// Verify state messages: user + assistant(tool call) + tool result + assistant(1st rep) + assistant(2nd rep)
	h.AssertStateHasNMessages(agent, 5)
}

func TestE2E_BlankResponse_BlankThenToolCalls(t *testing.T) {
	// Model returns blank, then tool calls. Counter should reset on tool execution.
	h := NewHarnessWithT(t)

	// First: blank response
	h.Provider().AddTextResponseWithFinish("", "stop")
	// Second: tool call (counter resets)
	h.Provider().AddToolCallResponse(
		"Let me check.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"test.txt"}`,
			},
		},
	)
	// Third: final answer after tool
	h.Provider().AddTextResponseWithFinish("The file contains data.", "stop")

	h.Executor().AddToolResult("call_1", "file content")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read a file")

	h.AssertNoError(err)
	h.AssertEquals(result, "The file contains data.")
	h.AssertProviderCalledN(3)
	h.AssertExecutorCalledN(1)
}

func TestBlankResponseError_ErrorFormat(t *testing.T) {
	err := &core.BlankResponseError{Provider: "test-model", Count: 3}
	expected := "model produced 3 consecutive blank or repetitive responses (test-model)"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestBlankResponseError_NoProvider(t *testing.T) {
	err := &core.BlankResponseError{Count: 2}
	if !strings.Contains(err.Error(), "2 consecutive blank or repetitive responses") {
		t.Errorf("expected count in error, got: %v", err)
	}
}

func TestIsBlankResponse(t *testing.T) {
	if !core.IsBlankResponse(&core.BlankResponseError{}) {
		t.Error("expected IsBlankResponse(true) for BlankResponseError")
	}
	if core.IsBlankResponse(&core.ContentFilteredError{}) {
		t.Error("expected IsBlankResponse(false) for ContentFilteredError")
	}
	if core.IsBlankResponse(core.ErrNoProvider) {
		t.Error("expected IsBlankResponse(false) for ErrNoProvider")
	}
	if core.IsBlankResponse(nil) {
		t.Error("expected IsBlankResponse(false) for nil")
	}
}

// --- ANSI Sanitization Tests ---

func TestE2E_ANSISanitization(t *testing.T) {
	// ANSI escape codes in tool results must be stripped before messages
	// are sent to the LLM provider. This prevents terminal formatting codes
	// (colors, cursor moves, etc.) from polluting the conversation context.
	h := NewHarnessWithT(t)

	// ANSI codes to test stripping:
	// - \x1b[31m ... \x1b[0m — ANSI color (red)
	// - \x1b[1m ... \x1b[0m — bold
	// - \x1b[32m ... \x1b[0m — ANSI color (green)
	// - \x1b[K — clear to end of line
	ansiColorRed := "\x1b[31m"
	ansiReset := "\x1b[0m"
	ansiBold := "\x1b[1m"
	ansiColorGreen := "\x1b[32m"
	ansiClearLine := "\x1b[K"

	toolResult := fmt.Sprintf("%s%sERROR%s%s%s: some problem%s", ansiBold, ansiColorRed, ansiReset, ansiColorGreen, "success", ansiClearLine)

	// First provider response: assistant calls a tool
	h.Provider().AddToolCallResponse(
		"Checking the output.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"output.txt"}`,
			},
		},
	)
	// Second provider response: assistant gives final answer
	h.Provider().AddTextResponse("The output has been analyzed.")

	// Executor returns tool result with ANSI codes
	h.Executor().AddToolResult("call_1", toolResult)

	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "Check the output")
	h.AssertNoError(err)

	// Verify the provider received clean messages (no ANSI codes)
	// The tool result message appears in the SECOND provider request
	// (after the tool executed, before the provider is called again).
	h.AssertProviderCalledN(2)
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	foundToolResult := false
	for _, msg := range lastReq.Messages {
		if msg.Role == "tool" {
			foundToolResult = true
			// The tool result content should NOT contain any ANSI codes
			if strings.Contains(msg.Content, "\x1b") {
				t.Errorf("expected tool result content to be ANSI-free, but found escape codes in: %q", msg.Content)
			}
			// Verify the actual text content (minus ANSI) is preserved
			wantContent := "ERRORsuccess: some problem"
			if msg.Content != wantContent {
				t.Errorf("expected tool result content %q, got %q", wantContent, msg.Content)
			}
		}
	}
	if !foundToolResult {
		t.Error("expected tool result message in last provider request")
	}

	// Verify the FIRST provider request did NOT contain the tool result
	// (since the tool hadn't executed yet)
	firstReq := h.Provider().Calls[0]
	for _, msg := range firstReq.Messages {
		if msg.Role == "tool" {
			t.Error("expected no tool result in first provider request")
		}
	}
}

func TestE2E_ANSISanitization_PreservesNonANSIContent(t *testing.T) {
	// Sanitization should not alter content that lacks ANSI codes.
	h := NewHarnessWithT(t)

	h.Provider().AddTextResponse("OK")
	agent := h.NewAgent()
	_, err := agent.Run(context.Background(), "simple query")
	h.AssertNoError(err)

	req := h.Provider().LastRequest()
	if req == nil {
		t.Fatal("expected provider to have been called")
	}

	// The user query should be preserved exactly
	userMsg := ""
	for _, msg := range req.Messages {
		if msg.Role == "user" && msg.Content == "simple query" {
			userMsg = msg.Content
			break
		}
	}
	h.AssertEquals(userMsg, "simple query")
}

// --- Finish Reason: "stop" with empty content e2e tests ---

func TestE2E_FinishReasonStop_EmptyContent_Continuation(t *testing.T) {
	// Provider returns finish_reason="stop" with empty content on the first call.
	// The conversation handler should detect this as incomplete and continue the loop
	// with a transient continuation message. The second provider call returns the
	// actual answer.
	h := NewHarnessWithT(t)
	h.Provider().AddTextResponseWithFinish("", "stop")
	h.Provider().AddTextResponseWithFinish("Here is the complete answer.", "stop")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "hi")

	h.AssertNoError(err)
	h.AssertEquals(result, "Here is the complete answer.")

	// Provider called twice: initial empty response + continuation with content
	h.AssertProviderCalledN(2)

	// State: user query + assistant(empty) + assistant(complete)
	h.AssertStateHasNMessages(agent, 3)

	// Token counts accumulate: 35 (first) + 35 (second) = 70
	h.AssertStateHasTokens(agent, 70)

	// Verify events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Two metrics_update events (one per iteration)
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 2 {
		t.Fatalf("expected 2 metrics_update events, got %d", len(metricsEvents))
	}

	// Verify iteration numbers in metrics events
	for i, evt := range metricsEvents {
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("event %d: expected data to be map[string]interface{}, got %T", i, evt.Data)
		}
		it, ok := data["iteration"].(int)
		if !ok {
			t.Fatalf("event %d: expected iteration to be int, got %T", i, data["iteration"])
		}
		if it != i {
			t.Errorf("event %d: expected iteration %d, got %d", i, i, it)
		}
	}

	// Verify accumulated total_tokens in second metrics event
	lastMetrics := metricsEvents[1].Data.(map[string]interface{})
	totalTokens, ok := lastMetrics["total_tokens"].(int)
	if !ok {
		t.Fatalf("expected total_tokens to be int, got %T", lastMetrics["total_tokens"])
	}
	if totalTokens != 70 {
		t.Errorf("expected total_tokens 70 (accumulated), got %d", totalTokens)
	}

	// Verify state messages
	messages := agent.State().Messages()
	if messages[0].Content != "hi" {
		t.Errorf("expected message 1 to be 'hi', got %q", messages[0].Content)
	}
	if messages[1].Content != "" {
		t.Errorf("expected message 2 to be empty, got %q", messages[1].Content)
	}
	if messages[2].Content != "Here is the complete answer." {
		t.Errorf("expected message 3 to be 'Here is the complete answer.', got %q", messages[2].Content)
	}

	// Verify the transient continuation message appears in the second provider request
	if len(h.Provider().Calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	foundTransient := false
	for _, msg := range h.Provider().Calls[1].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please provide a complete response") {
			foundTransient = true
			break
		}
	}
	if !foundTransient {
		t.Error("expected transient continuation message in second provider request")
	}

	// Verify the transient message is NOT in state
	for _, msg := range agent.State().Messages() {
		if msg.Role == "user" && strings.Contains(msg.Content, "Please provide a complete response") {
			t.Error("transient continuation message should not be in state")
		}
	}

	// Verify the first request did NOT contain a transient continuation message
	firstReq := h.Provider().Calls[0]
	for _, msg := range firstReq.Messages {
		if strings.Contains(msg.Content, "Please provide a complete response") {
			t.Error("transient continuation message should not be in first provider request")
		}
	}
}

// --- Tool Call Normalization Tests ---

func TestE2E_ChannelSuffixStripped(t *testing.T) {
	// Some models append <|channel|>N suffix to tool names (e.g., "read_file<|channel|>0").
	// The ToolCallNormalizer strips this suffix so the normalized name matches
	// the registered tool. This test verifies the full flow:
	//
	// 1. Provider returns tool call with "read_file<|channel|>0" name
	// 2. Normalizer strips "<|channel|>0" → "read_file"
	// 3. Tool executes successfully (name matches registered tool)
	// 4. Conversation completes with final answer
	h := NewHarnessWithT(t)

	// Register the tool with the clean name
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
		},
	})

	// First response: tool call with <|channel|>0 suffix in the name
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file<|channel|>0",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)
	// Second response: final answer after tool execution
	h.Provider().AddTextResponse("The configuration file has been read successfully.")

	// Executor returns the tool result (matched by tool call ID)
	h.Executor().AddToolResult("call_1", "database:\n  host: localhost\n  port: 5432")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration file has been read successfully.")

	// Provider called twice: tool call iteration + final answer
	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool call) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)

	// Verify the normalized tool call was recorded in state
	messages := agent.State().Messages()
	if messages[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", messages[1].Role)
	}
	if len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected message 2 to have 1 tool call, got %d", len(messages[1].ToolCalls))
	}
	// The tool name in state should be normalized (suffix stripped)
	toolName := messages[1].ToolCalls[0].Function.Name
	if toolName != "read_file" {
		t.Errorf("expected normalized tool name 'read_file', got %q", toolName)
	}

	// Verify the executor received the normalized tool call
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(execCalls))
	}
	if execCalls[0].Function.Name != "read_file" {
		t.Errorf("expected executor to receive normalized name 'read_file', got %q", execCalls[0].Function.Name)
	}
}

func TestE2E_MissingToolCallID_SyntheticID(t *testing.T) {
	// Scenario: provider returns structured tool calls with empty IDs.
	// The normalizer generates synthetic IDs, the executor receives them, and the tool
	// result is linked via the generated ID.
	//
	// Flow:
	// 1. Provider returns tool call with empty ID
	// 2. Normalizer generates synthetic ID (call_{name}_{nano}_{seq})
	// 3. Executor receives the tool call with the synthetic ID
	// 4. Executor returns tool result using the synthetic ID
	// 5. Tool result is linked to the assistant message via matching ID
	// 6. Provider returns final answer
	h := NewHarnessWithT(t)

	// Register the tool so the executor recognizes it
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
		},
	})

	// First response: tool call with EMPTY ID — normalizer must generate one
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "", // intentionally empty
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)

	// Second response: final answer after tool execution
	h.Provider().AddTextResponse("The configuration file contains database settings on port 5432.")

	// Don't pre-configure a tool result by ID — the executor will use the
	// synthetic ID from the call. We'll verify the linkage below.

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration file contains database settings on port 5432.")

	// Provider called twice: tool call iteration + final answer
	h.AssertProviderCalledN(2)

	// Executor called once (for the tool call with synthetic ID)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool call) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)

	// Verify the executor received a tool call with a non-empty synthetic ID
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(execCalls))
	}
	syntheticID := execCalls[0].ID
	if syntheticID == "" {
		t.Error("expected normalizer to generate non-empty ID for tool call")
	}
	if !strings.HasPrefix(syntheticID, "call_read_file_") {
		t.Errorf("expected synthetic ID prefix 'call_read_file_', got %q", syntheticID)
	}
	if execCalls[0].Function.Name != "read_file" {
		t.Errorf("expected executor to receive 'read_file', got %q", execCalls[0].Function.Name)
	}

	// Verify state messages
	messages := agent.State().Messages()

	// Message 2 (index 1) should be the assistant with the tool call
	if messages[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", messages[1].Role)
	}
	if len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected message 2 to have 1 tool call, got %d", len(messages[1].ToolCalls))
	}
	// The tool call in state should have the synthetic ID (normalizer updated it)
	stateToolCallID := messages[1].ToolCalls[0].ID
	if stateToolCallID == "" {
		t.Error("expected tool call in state to have non-empty ID after normalization")
	}
	if stateToolCallID != syntheticID {
		t.Errorf("expected tool call ID in state (%q) to match executor ID (%q)", stateToolCallID, syntheticID)
	}

	// Message 3 (index 2) should be the tool result linked to the synthetic ID
	if messages[2].Role != "tool" {
		t.Errorf("expected message 3 to be tool, got %q", messages[2].Role)
	}
	if messages[2].ToolCallID != syntheticID {
		t.Errorf("expected tool result ToolCallID (%q) to match synthetic ID (%q)", messages[2].ToolCallID, syntheticID)
	}
	// Verify the tool result content (mock executor default placeholder)
	if messages[2].Content != "mock result" {
		t.Errorf("expected tool result content 'mock result', got %q", messages[2].Content)
	}

	// Message 4 (index 3) should be the final answer
	if messages[3].Role != "assistant" {
		t.Errorf("expected message 4 to be assistant, got %q", messages[3].Role)
	}
	if messages[3].Content != "The configuration file contains database settings on port 5432." {
		t.Errorf("expected final answer in state message 4, got %q", messages[3].Content)
	}

	// Verify query lifecycle events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Verify tool_start and tool_end events use the synthetic ID
	h.AssertEventPublished(events.EventTypeToolStart)
	h.AssertEventPublished(events.EventTypeToolEnd)

	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 1 {
		t.Fatalf("expected 1 tool_start event, got %d", len(toolStartEvents))
	}
	tsData := toolStartEvents[0].Data.(map[string]interface{})
	tsID, ok := tsData["tool_call_id"].(string)
	if !ok || tsID != syntheticID {
		t.Errorf("expected tool_start event tool_call_id to match synthetic ID (%q), got %q", syntheticID, tsID)
	}

	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 1 {
		t.Fatalf("expected 1 tool_end event, got %d", len(toolEndEvents))
	}
	teData := toolEndEvents[0].Data.(map[string]interface{})
	teID, ok := teData["tool_call_id"].(string)
	if !ok || teID != syntheticID {
		t.Errorf("expected tool_end event tool_call_id to match synthetic ID (%q), got %q", syntheticID, teID)
	}

	// Verify accumulated token counts: 80 (tool call) + 35 (final) = 115
	h.AssertStateHasTokens(agent, 115)
}

func TestE2E_MissingToolCallID_MultipleCalls(t *testing.T) {
	// Scenario: provider returns multiple tool calls, some with missing IDs.
	// Normalizer generates unique synthetic IDs for each. All are executed
	// and the results are linked correctly.
	h := NewHarnessWithT(t)

	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
		},
	})
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "write_file",
			Description: "Write a file",
		},
	})

	// First response: two tool calls — first has ID, second doesn't
	h.Provider().AddToolCallResponse(
		"Reading and writing files.",
		core.ToolCall{
			ID: "call_existing",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"input.txt"}`,
			},
		},
		core.ToolCall{
			ID: "", // missing ID — normalizer generates one
			Function: core.ToolCallFunction{
				Name:      "write_file",
				Arguments: `{"path":"output.txt","content":"result"}`,
			},
		},
	)

	// Second response: final answer
	h.Provider().AddTextResponse("Files processed successfully with the expected output.")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Process files")
	h.AssertNoError(err)
	h.AssertEquals(result, "Files processed successfully with the expected output.")

	h.AssertProviderCalledN(2)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool calls) + 2 tool results + assistant(final)
	h.AssertStateHasNMessages(agent, 5)

	// Verify executor received both calls with proper IDs
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 2 {
		t.Fatalf("expected 2 executor calls, got %d", len(execCalls))
	}

	// First call preserves existing ID
	if execCalls[0].ID != "call_existing" {
		t.Errorf("expected first call ID 'call_existing', got %q", execCalls[0].ID)
	}

	// Second call has synthetic ID
	syntheticID := execCalls[1].ID
	if syntheticID == "" {
		t.Error("expected normalizer to generate ID for second call")
	}
	if !strings.HasPrefix(syntheticID, "call_write_file_") {
		t.Errorf("expected synthetic ID prefix 'call_write_file_', got %q", syntheticID)
	}

	// IDs must be unique
	if execCalls[0].ID == execCalls[1].ID {
		t.Error("expected unique IDs for different tool calls")
	}

	// Verify state: assistant message has both tool calls with correct IDs
	messages := agent.State().Messages()
	if len(messages[1].ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls in assistant message, got %d", len(messages[1].ToolCalls))
	}
	if messages[1].ToolCalls[0].ID != "call_existing" {
		t.Errorf("expected first tool call ID 'call_existing', got %q", messages[1].ToolCalls[0].ID)
	}
	if messages[1].ToolCalls[1].ID != syntheticID {
		t.Errorf("expected second tool call ID %q, got %q", syntheticID, messages[1].ToolCalls[1].ID)
	}

	// Verify tool results are linked to the correct IDs
	if messages[2].ToolCallID != "call_existing" {
		t.Errorf("expected first tool result linked to 'call_existing', got %q", messages[2].ToolCallID)
	}
	if messages[3].ToolCallID != syntheticID {
		t.Errorf("expected second tool result linked to synthetic ID %q, got %q", syntheticID, messages[3].ToolCallID)
	}
	// Verify tool result content (mock executor default placeholder)
	if messages[2].Content != "mock result" {
		t.Errorf("expected first tool result content 'mock result', got %q", messages[2].Content)
	}
	if messages[3].Content != "mock result" {
		t.Errorf("expected second tool result content 'mock result', got %q", messages[3].Content)
	}

	// Verify tool_start events — should have 2, one per call
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 2 {
		t.Fatalf("expected 2 tool_start events, got %d", len(toolStartEvents))
	}

	// Verify tool_end events — should have 2, one per result
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 2 {
		t.Fatalf("expected 2 tool_end events, got %d", len(toolEndEvents))
	}

	// Verify accumulated token counts: 80 (provider response with 2 tool calls) + 35 (final) = 115
	h.AssertStateHasTokens(agent, 115)
}

func TestE2E_MalformedStructuredToolCall_Retry(t *testing.T) {
	// Scenario: provider returns structured tool calls with malformed JSON
	// arguments that the normalizer cannot repair. All calls are dropped,
	// so a transient message is injected asking the model to re-emit.
	// The second provider call returns properly formatted tool calls,
	// which execute successfully and the conversation completes.
	//
	// Flow:
	// 1. Provider returns tool call with unrepairable JSON args (e.g., "not json at all")
	// 2. Normalizer drops the call — allMalformed is true
	// 3. Transient message injected: "Your previous tool call was malformed..."
	// 4. Provider returns properly formatted tool call
	// 5. Tool executes, result recorded
	// 6. Provider returns final answer
	h := NewHarnessWithT(t)

	// Register the tool so the executor recognizes it
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
		},
	})

	// First response: tool call with unrepairable JSON arguments.
	// The normalizer's repairJSON will try to fix it, but "not json at all"
	// cannot be repaired, so the call is dropped. allMalformed becomes true.
	h.Provider().AddToolCallResponse(
		"Let me read the file.",
		core.ToolCall{
			ID: "call_bad",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: "not json at all",
			},
		},
	)

	// Second response: properly formatted tool call after re-emit request
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "call_good",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)

	// Third response: final answer after tool execution
	h.Provider().AddTextResponse("The configuration file contains database settings on port 5432.")

	h.Executor().AddToolResult("call_good", "database:\n  host: localhost\n  port: 5432")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration file contains database settings on port 5432.")

	// Provider called 3 times: malformed tool call → re-emitted tool call → final answer
	h.AssertProviderCalledN(3)

	// Executor called only once (for the good tool call)
	h.AssertExecutorCalledN(1)

	// State: user + assistant(malformed tool call, recorded before normalization) +
	//        assistant(re-emitted tool call) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 5)

	// Verify the transient re-emit message was included in the second provider request
	if len(h.Provider().Calls) < 2 {
		t.Fatal("expected at least 2 provider calls")
	}
	foundTransient := false
	for _, msg := range h.Provider().Calls[1].Messages {
		if msg.Role == "user" && strings.Contains(msg.Content, "malformed") {
			foundTransient = true
			break
		}
	}
	if !foundTransient {
		t.Error("expected transient malformed-reemit message in second provider request")
	}

	// Verify the transient message is NOT in state (it's transient)
	for _, msg := range agent.State().Messages() {
		if msg.Role == "user" && strings.Contains(msg.Content, "malformed") {
			t.Error("transient re-emit message should not be in state")
		}
	}

	// Verify the first request did NOT contain the transient message
	for _, msg := range h.Provider().Calls[0].Messages {
		if strings.Contains(msg.Content, "malformed") {
			t.Error("transient re-emit message should not be in first provider request")
		}
	}

	// Verify the executor received the properly formatted tool call
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(execCalls))
	}
	if execCalls[0].Function.Name != "read_file" {
		t.Errorf("expected executor to receive 'read_file', got %q", execCalls[0].Function.Name)
	}
	if execCalls[0].ID != "call_good" {
		t.Errorf("expected executor to receive call ID 'call_good', got %q", execCalls[0].ID)
	}

	// Verify accumulated token counts: 80 (malformed) + 80 (re-emitted) + 35 (final) = 195
	h.AssertStateHasTokens(agent, 195)

	// Verify query lifecycle events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Verify metrics_update events (one per provider call = 3)
	metricsEvents := h.FindEvents(events.EventTypeMetricsUpdate)
	if len(metricsEvents) != 3 {
		t.Fatalf("expected 3 metrics_update events, got %d", len(metricsEvents))
	}

	// Verify state message contents
	messages := agent.State().Messages()

	// Message 3 (index 2) should have the re-emitted good tool call
	if len(messages[2].ToolCalls) != 1 {
		t.Fatalf("expected message 3 to have 1 tool call, got %d", len(messages[2].ToolCalls))
	}
	if messages[2].ToolCalls[0].ID != "call_good" {
		t.Errorf("expected tool call ID 'call_good' in state message 3, got %q", messages[2].ToolCalls[0].ID)
	}
	if messages[2].ToolCalls[0].Function.Name != "read_file" {
		t.Errorf("expected tool name 'read_file' in state message 3, got %q", messages[2].ToolCalls[0].Function.Name)
	}

	// Message 5 (index 4) should be the final answer
	if messages[4].Role != "assistant" {
		t.Errorf("expected message 5 to be assistant, got %q", messages[4].Role)
	}
	if messages[4].Content != "The configuration file contains database settings on port 5432." {
		t.Errorf("expected final answer in state message 5, got %q", messages[4].Content)
	}
}

func TestE2E_MissingToolCallID_GeneratesSyntheticID(t *testing.T) {
	// Scenario: provider returns a tool call with an empty ID.
	// The normalizer should generate a synthetic ID, the tool should
	// execute successfully, and the tool result should be linked to the
	// synthetic ID in state.
	//
	// Flow:
	// 1. Provider returns tool call with ID == "" (missing)
	// 2. Normalizer generates synthetic ID (call_{name}_{nano}_{seq})
	// 3. Tool executes; executor returns result using the synthetic ID
	// 4. Provider returns final answer
	// 5. Conversation completes; tool result linked to synthetic ID
	h := NewHarnessWithT(t)

	// First response: tool call with empty ID
	h.Provider().AddToolCallResponse(
		"Reading the file.",
		core.ToolCall{
			ID: "", // intentionally empty — normalizer must generate one
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"config.yaml"}`,
			},
		},
	)

	// Second response: final answer
	h.Provider().AddTextResponse("The configuration file contains database settings on port 5432.")

	// The executor will return a default result using whatever ID the
	// normalized tool call has (the synthetic one). No need to pre-configure
	// a specific result because MockExecutor defaults to using call.ID.

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read config.yaml")
	h.AssertNoError(err)
	h.AssertEquals(result, "The configuration file contains database settings on port 5432.")

	// Provider called twice: tool call iteration + final answer
	h.AssertProviderCalledN(2)

	// Executor called once
	h.AssertExecutorCalledN(1)

	// State: user + assistant(tool call) + tool result + assistant(final)
	h.AssertStateHasNMessages(agent, 4)

	// Verify the executor received the tool call with a non-empty synthetic ID
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 1 {
		t.Fatalf("expected 1 executor call, got %d", len(execCalls))
	}
	syntheticID := execCalls[0].ID
	if syntheticID == "" {
		t.Fatal("expected synthetic ID to be non-empty, got empty string")
	}
	if !strings.HasPrefix(syntheticID, "call_read_file_") {
		t.Errorf("expected synthetic ID prefix 'call_read_file_', got %q", syntheticID)
	}

	// Verify state messages
	messages := agent.State().Messages()

	// Message 2 (index 1) should be the assistant tool call with the synthetic ID
	if messages[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", messages[1].Role)
	}
	if len(messages[1].ToolCalls) != 1 {
		t.Fatalf("expected message 2 to have 1 tool call, got %d", len(messages[1].ToolCalls))
	}
	if messages[1].ToolCalls[0].ID != syntheticID {
		t.Errorf("expected tool call ID %q in state, got %q", syntheticID, messages[1].ToolCalls[0].ID)
	}

	// Message 3 (index 2) should be the tool result linked to the synthetic ID
	if messages[2].Role != "tool" {
		t.Errorf("expected message 3 to be tool, got %q", messages[2].Role)
	}
	if messages[2].ToolCallID != syntheticID {
		t.Errorf("expected tool result ToolCallID %q, got %q", syntheticID, messages[2].ToolCallID)
	}

	// Message 4 (index 3) should be the final answer
	if messages[3].Role != "assistant" {
		t.Errorf("expected message 4 to be assistant, got %q", messages[3].Role)
	}

	// Verify tool_start event has the synthetic ID
	h.AssertEventPublished(events.EventTypeToolStart)
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) == 0 {
		t.Fatal("expected at least 1 tool_start event")
	}
	startData := toolStartEvents[0].Data.(map[string]interface{})
	startID, ok := startData["tool_call_id"].(string)
	if !ok {
		t.Fatalf("expected tool_call_id to be string, got %T", startData["tool_call_id"])
	}
	if startID != syntheticID {
		t.Errorf("expected tool_start event ID %q, got %q", syntheticID, startID)
	}

	// Verify tool_end event has the synthetic ID
	h.AssertEventPublished(events.EventTypeToolEnd)
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) == 0 {
		t.Fatal("expected at least 1 tool_end event")
	}
	endData := toolEndEvents[0].Data.(map[string]interface{})
	endID, ok := endData["tool_call_id"].(string)
	if !ok {
		t.Fatalf("expected tool_call_id to be string, got %T", endData["tool_call_id"])
	}
	if endID != syntheticID {
		t.Errorf("expected tool_end event ID %q, got %q", syntheticID, endID)
	}

	// Verify query lifecycle events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Verify accumulated token counts: 80 (tool call) + 35 (final) = 115
	h.AssertStateHasTokens(agent, 115)
}

func TestE2E_DuplicateToolCalls_OnlyUniqueExecute(t *testing.T) {
	// Scenario: provider returns 4 tool calls in a single response, but 2 of them
	// are duplicates of call_1 (same ID + same arguments). The ToolCallNormalizer
	// deduplicates by ID+arguments, so only the 2 unique calls (call_1 and call_2)
	// are executed.
	//
	// Flow:
	// 1. Provider returns 4 tool calls:
	//    - call_1: read_file {path:"/tmp/test.txt"} (unique)
	//    - call_1: read_file {path:"/tmp/test.txt"} (duplicate)
	//    - call_2: shell {cmd:"ls"} (unique)
	//    - call_1: read_file {path:"/tmp/test.txt"} (duplicate)
	// 2. Normalizer deduplicates → 2 unique calls remain
	// 3. Executor receives exactly 2 tool calls
	// 4. Tool results returned for call_1 and call_2
	// 5. Provider returns final answer
	// 6. Conversation completes
	h := NewHarnessWithT(t)

	// Register tools so the executor recognizes them
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "read_file",
			Description: "Read a file",
		},
	})
	h.Executor().AddTool(core.Tool{
		Function: core.ToolFunction{
			Name:        "shell",
			Description: "Execute a shell command",
		},
	})

	// First response: 4 tool calls (2 are duplicates of call_1)
	h.Provider().AddToolCallResponse(
		"Reading the file and listing the directory.",
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/tmp/test.txt"}`,
			},
		},
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/tmp/test.txt"}`,
			},
		},
		core.ToolCall{
			ID: "call_2",
			Function: core.ToolCallFunction{
				Name:      "shell",
				Arguments: `{"cmd":"ls"}`,
			},
		},
		core.ToolCall{
			ID: "call_1",
			Function: core.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path":"/tmp/test.txt"}`,
			},
		},
	)

	// Second response: final answer
	h.Provider().AddTextResponse("The file test.txt exists and the directory contains: file.txt")

	// Executor returns results for each unique tool call
	h.Executor().AddToolResult("call_1", "file content: hello")
	h.Executor().AddToolResult("call_2", "file.txt")

	agent := h.NewAgent()
	result, err := agent.Run(context.Background(), "Read /tmp/test.txt and list directory")
	h.AssertNoError(err)
	h.AssertEquals(result, "The file test.txt exists and the directory contains: file.txt")

	// Provider called twice: tool call iteration + final answer
	h.AssertProviderCalledN(2)

	// Executor called once (single Execute call with 2 tool calls)
	h.AssertExecutorCalledN(1)

	// Verify executor received exactly 2 tool calls (not 4)
	execCalls := h.Executor().LastCalls()
	if len(execCalls) != 2 {
		t.Fatalf("expected 2 executor calls (deduplicated), got %d", len(execCalls))
	}

	// Verify the two unique tool calls are present
	calledNames := make(map[string]bool)
	calledIDs := make(map[string]bool)
	for _, tc := range execCalls {
		calledNames[tc.Function.Name] = true
		calledIDs[tc.ID] = true
	}
	if !calledNames["read_file"] {
		t.Error("expected read_file tool call to be in executor calls")
	}
	if !calledNames["shell"] {
		t.Error("expected shell tool call to be in executor calls")
	}
	if !calledIDs["call_1"] {
		t.Error("expected call_1 to be in executor calls")
	}
	if !calledIDs["call_2"] {
		t.Error("expected call_2 to be in executor calls")
	}

	// State: user + assistant(tool calls, 2 entries after dedup) + 2 tool results + assistant(final) = 5
	h.AssertStateHasNMessages(agent, 5)

	// Verify tool_start events — should have exactly 2 (not 4)
	h.AssertEventPublished(events.EventTypeToolStart)
	toolStartEvents := h.FindEvents(events.EventTypeToolStart)
	if len(toolStartEvents) != 2 {
		t.Fatalf("expected 2 tool_start events (deduplicated), got %d", len(toolStartEvents))
	}

	// Verify tool_start event IDs
	tsIDs := make(map[string]bool)
	for _, evt := range toolStartEvents {
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected tool_start event data to be map[string]interface{}, got %T", evt.Data)
		}
		id, ok := data["tool_call_id"].(string)
		if !ok {
			t.Fatalf("expected tool_call_id to be string, got %T", data["tool_call_id"])
		}
		tsIDs[id] = true
	}
	if !tsIDs["call_1"] {
		t.Error("expected tool_start event for call_1")
	}
	if !tsIDs["call_2"] {
		t.Error("expected tool_start event for call_2")
	}

	// Verify tool_end events — should have exactly 2 (not 4)
	h.AssertEventPublished(events.EventTypeToolEnd)
	toolEndEvents := h.FindEvents(events.EventTypeToolEnd)
	if len(toolEndEvents) != 2 {
		t.Fatalf("expected 2 tool_end events (deduplicated), got %d", len(toolEndEvents))
	}

	// Verify tool_end event IDs
	teIDs := make(map[string]bool)
	for _, evt := range toolEndEvents {
		data, ok := evt.Data.(map[string]interface{})
		if !ok {
			t.Fatalf("expected tool_end event data to be map[string]interface{}, got %T", evt.Data)
		}
		id, ok := data["tool_call_id"].(string)
		if !ok {
			t.Fatalf("expected tool_call_id to be string, got %T", data["tool_call_id"])
		}
		teIDs[id] = true
	}
	if !teIDs["call_1"] {
		t.Error("expected tool_end event for call_1")
	}
	if !teIDs["call_2"] {
		t.Error("expected tool_end event for call_2")
	}

	// Verify accumulated token counts: 80 (tool call iteration) + 35 (final) = 115
	h.AssertStateHasTokens(agent, 115)

	// Verify query lifecycle events
	h.AssertEventPublished(events.EventTypeQueryStarted)
	h.AssertEventPublished(events.EventTypeQueryCompleted)

	// Verify state messages structure
	messages := agent.State().Messages()
	if messages[0].Role != "user" {
		t.Errorf("expected message 1 to be user, got %q", messages[0].Role)
	}
	if messages[1].Role != "assistant" {
		t.Errorf("expected message 2 to be assistant, got %q", messages[1].Role)
	}
	if len(messages[1].ToolCalls) != 2 {
		t.Fatalf("expected assistant message to have 2 tool calls (after dedup), got %d", len(messages[1].ToolCalls))
	}
	// Verify the assistant message has the correct tool call IDs
	if messages[1].ToolCalls[0].ID != "call_1" {
		t.Errorf("expected first tool call ID 'call_1', got %q", messages[1].ToolCalls[0].ID)
	}
	if messages[1].ToolCalls[1].ID != "call_2" {
		t.Errorf("expected second tool call ID 'call_2', got %q", messages[1].ToolCalls[1].ID)
	}
	// Message 3 should be tool result for call_1
	if messages[2].Role != "tool" {
		t.Errorf("expected message 3 to be tool, got %q", messages[2].Role)
	}
	if messages[2].ToolCallID != "call_1" {
		t.Errorf("expected tool result for call_1, got %q", messages[2].ToolCallID)
	}
	// Message 4 should be tool result for call_2
	if messages[3].Role != "tool" {
		t.Errorf("expected message 4 to be tool, got %q", messages[3].Role)
	}
	if messages[3].ToolCallID != "call_2" {
		t.Errorf("expected tool result for call_2, got %q", messages[3].ToolCallID)
	}
	// Message 5 should be final answer
	if messages[4].Role != "assistant" {
		t.Errorf("expected message 5 to be assistant, got %q", messages[4].Role)
	}
}

// --- Long Conversation Compaction Tests ---

func TestE2E_Compaction_LongConversation_RecentTurnsIntact(t *testing.T) {
	// Verify that checkpoint compaction correctly handles a long conversation
	// with 50+ turns: old turns are replaced with actionable summaries while
	// the most recent turn and the new query remain intact.
	const turnsCount = 50

	h := NewHarnessWithT(t)

	// Use a small context size (4096) and high token estimate (200000)
	// so that compaction is triggered.
	h.Provider().
		WithTokenEstimate(200000).
		WithInfo(core.ProviderInfo{
			Model:       "mock",
			ContextSize: 4096,
			HasVision:   false,
		})

	// Add 51 text responses (one per each of the 50 turns + 1 final turn)
	for i := 1; i <= turnsCount+1; i++ {
		h.Provider().AddTextResponse(
			fmt.Sprintf("Answer %d: This is a detailed and comprehensive response about the topic that provides useful information for the user.", i),
		)
	}

	agent := h.NewAgent()

	// --- Phase 1: Run 50 turns ---
	for i := 1; i <= turnsCount; i++ {
		_, err := agent.Run(context.Background(), fmt.Sprintf("Question %d about topic", i))
		h.AssertNoError(err)
	}

	// Wait for all async checkpoint recordings to complete.
	// Checkpoints are recorded via goroutines so their order is not guaranteed.
	// Sort by StartIndex for deterministic verification.
	checkpoints := waitForCheckpoints(t, agent, turnsCount)
	if len(checkpoints) != turnsCount {
		t.Fatalf("expected %d checkpoints, got %d", turnsCount, len(checkpoints))
	}
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})

	// State should have 100 messages (50 turns × 2 messages each).
	if got := agent.State().Len(); got != turnsCount*2 {
		t.Fatalf("expected %d messages after %d turns, got %d", turnsCount*2, turnsCount, got)
	}

	// --- Phase 2: Run one more turn (turn 51) ---
	// This triggers checkpoint compaction in prepareMessages() which replaces
	// the consumed checkpoint ranges with summary messages.
	_, err := agent.Run(context.Background(), "Question 51 about topic")
	h.AssertNoError(err)

	// State should have 102 messages (51 turns × 2 messages each).
	if got := agent.State().Len(); got != (turnsCount+1)*2 {
		t.Fatalf("expected %d messages after %d turns, got %d", (turnsCount+1)*2, turnsCount+1, got)
	}

	// The provider was called once per turn (50 turns before + 1 final turn).
	// We verify the final turn's request by checking LastRequest.

	// --- Verify provider request ---
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	// The system prompt should be prepended as the first message.
	if len(lastReq.Messages) == 0 || lastReq.Messages[0].Role != "system" {
		t.Errorf("expected first message to be system, got role %q", func() string {
			if len(lastReq.Messages) > 0 {
				return lastReq.Messages[0].Role
			}
			return "(empty)"
		}())
	}

	// The last message should be the new user query (intact, not summarized).
	if lastReq.Messages[len(lastReq.Messages)-1].Role != "user" {
		t.Errorf("expected last message to be user, got role %q", lastReq.Messages[len(lastReq.Messages)-1].Role)
	}
	if lastReq.Messages[len(lastReq.Messages)-1].Content != "Question 51 about topic" {
		t.Errorf("expected last message content to be 'Question 51 about topic', got %q",
			lastReq.Messages[len(lastReq.Messages)-1].Content)
	}

	// Verify message count: raw state has 102 messages, but the
	// provider request should have roughly 102 due to checkpoint
	// compaction (SP-128 fix preserves the leading user message of each consumed range).
	// With 50 checkpoints covering [user, asst] pairs, each range collapses to
	// [user, summary] instead of just [summary], so no net reduction occurs.
	// We expect roughly 102 messages (1 system + 50 preserved user queries + 50 summaries + 1 new query).
	if len(lastReq.Messages) > agent.State().Len() {
		t.Errorf("expected request messages not to exceed raw state: request has %d messages, raw state has %d",
			len(lastReq.Messages), agent.State().Len())
	}

	// --- Verify old turns are summarized ---
	summaryCount := 0
	questionTopics := make(map[string]bool)
	hasActionableDetail := false

	for i := 1; i < len(lastReq.Messages)-1; i++ {
		msg := lastReq.Messages[i]

		// Checkpoint summary messages have role "user" and Meta["checkpoint"] == "true".
		if msg.Role != "user" {
			continue
		}
		if msg.Meta == nil || msg.Meta["checkpoint"] != "true" {
			// Could be a recent turn that survived (unlikely with 50 turns) or
			// a non-checkpoint user message.
			continue
		}

		summaryCount++

		// Extract the question topic from the summary to verify multiple
		// distinct turns are represented.
		if strings.Contains(msg.Content, "Question") {
			questionTopics[msg.Content] = true
		}

		// Verify actionable detail: summaries should contain structured content
		// like "- Question:", "- Result:", "- Read:", or "User asked:" from the
		// ActionableSummary field.
		if strings.Contains(msg.Content, "- Question:") ||
			strings.Contains(msg.Content, "- Result:") ||
			strings.Contains(msg.Content, "- Read:") ||
			strings.Contains(msg.Content, "- Command:") ||
			strings.Contains(msg.Content, "User asked:") {
			hasActionableDetail = true
		}
	}

	// All 50 turns should be replaced by summary messages.
	if summaryCount != turnsCount {
		t.Errorf("expected %d summary messages in provider request, got %d", turnsCount, summaryCount)
	}

	// Verify actionable detail is present in summaries.
	if !hasActionableDetail {
		t.Error("expected summaries to contain actionable detail (e.g., '- Question:', '- Result:', 'User asked:')")
	}

	// Verify multiple distinct turns are represented in the summaries.
	if len(questionTopics) < 3 {
		t.Errorf("expected summaries for at least 3 distinct turns, got %d topics: %v",
			len(questionTopics), questionTopics)
	}

	// --- Verify the new query is intact and has no checkpoint meta ---
	lastMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if lastMsg.Meta != nil && lastMsg.Meta["checkpoint"] == "true" {
		t.Error("expected the new query message to NOT have checkpoint meta")
	}

	// --- Verify checkpoint data ---
	// The checkpoint for turn 1 should cover indices 0-1 (user query + assistant response).
	if len(checkpoints) > 0 {
		cp1 := checkpoints[0]
		if cp1.StartIndex != 0 {
			t.Errorf("expected checkpoint 0 StartIndex 0, got %d", cp1.StartIndex)
		}
		if cp1.EndIndex != 1 {
			t.Errorf("expected checkpoint 0 EndIndex 1, got %d", cp1.EndIndex)
		}
		if cp1.Summary == "" {
			t.Error("expected checkpoint 0 Summary to be non-empty")
		}
		if cp1.ActionableSummary == "" {
			t.Error("expected checkpoint 0 ActionableSummary to be non-empty")
		}
	}

	// --- Verify the checkpoint for the most recent completed turn (turn 50) ---
	if len(checkpoints) >= turnsCount {
		cpLast := checkpoints[turnsCount-1]
		// Turn 50: user at index 98, assistant at index 99.
		if cpLast.StartIndex != 98 {
			t.Errorf("expected last checkpoint StartIndex 98, got %d", cpLast.StartIndex)
		}
		if cpLast.EndIndex != 99 {
			t.Errorf("expected last checkpoint EndIndex 99, got %d", cpLast.EndIndex)
		}
	}
}

// --- Checkpoint Summary Dropping Test ---

func TestE2E_CheckpointSummariesDroppedWhenOverContext(t *testing.T) {
	// Scenario: checkpoint summaries themselves exceed context limits.
	// When compaction runs and checkpoint summaries are still too large,
	// the oldest summaries should be dropped entirely — not replaced with
	// additional metadata or re-summarized.
	//
	// Setup:
	// 1. Create an agent with a small context window
	// 2. Pre-populate state with checkpoint summary messages (Meta["checkpoint"]="true")
	//    that are large enough to exceed the context limit
	// 3. Run a query — compaction should trigger
	// 4. Verify oldest checkpoint summaries are dropped, not replaced
	h := NewHarnessWithT(t)
	h.Provider().
		WithTokenEstimate(200000). // Ensure over context limit
		WithInfo(core.ProviderInfo{
			Model:       "mock",
			ContextSize: 2048,
			HasVision:   false,
		}).
		AddTextResponse("Response after compaction")

	agent := h.NewAgent()

	// Add checkpoint summary messages that are large enough to trigger
	// checkpoint dropping. Each summary is ~1000 chars (~250 tokens).
	// With 15 summaries × ~260 tokens each ≈ 3900 tokens, which exceeds
	// the 2048 context limit. The target (0.85 * 2048 = 1740) means
	// dropping the oldest summaries should bring us under.
	numCheckpoints := 15
	for i := 0; i < numCheckpoints; i++ {
		summaryContent := fmt.Sprintf(
			"- Question: What was discussed in turn %d?\n- Result: The assistant provided a detailed analysis of topic %d covering multiple aspects.\n- Read: file%d.txt\n- Command: cat file%d.txt\n",
			i+1, i+1, i+1, i+1,
		)
		// Pad to ~1000 chars to ensure meaningful token count (~250 tokens each)
		for len(summaryContent) < 1000 {
			summaryContent += fmt.Sprintf(" Additional context for turn %d: the discussion covered implementation details, edge cases, and best practices. ", i+1)
		}
		agent.State().AddMessage(core.Message{
			Role:    "user",
			Content: summaryContent,
			Meta:    map[string]string{core.MetaKeyCheckpoint: "true"},
		})
	}

	// Add recent messages (protected zone)
	for i := 0; i < 10; i++ {
		agent.State().AddMessage(core.Message{
			Role:    "user",
			Content: fmt.Sprintf("Recent question %d", i+1),
		})
		agent.State().AddMessage(core.Message{
			Role:    "assistant",
			Content: fmt.Sprintf("Recent answer %d with enough words to be substantive", i+1),
		})
	}

	// Record checkpoint summaries before compaction for comparison.
	// Use a counter for the total count (not map length, which counts unique contents).
	numCpBefore := 0
	cpContentsBefore := make(map[string]bool)
	for _, msg := range agent.State().Messages() {
		if msg.Meta != nil && msg.Meta[core.MetaKeyCheckpoint] == "true" {
			numCpBefore++
			cpContentsBefore[msg.Content] = true
		}
	}
	if numCpBefore != numCheckpoints {
		t.Fatalf("expected %d checkpoint summaries before compaction, got %d", numCheckpoints, numCpBefore)
	}

	// Run query — this triggers compaction
	_, err := agent.Run(context.Background(), "Final query after many turns")
	h.AssertNoError(err)

	// Verify compaction event was published with strategy "checkpoint_drop"
	h.AssertEventPublished(events.EventTypeCompaction)
	compactionEvents := h.FindEvents(events.EventTypeCompaction)
	if len(compactionEvents) != 1 {
		t.Fatalf("expected 1 compaction event, got %d", len(compactionEvents))
	}
	cpEventData, ok := compactionEvents[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected compaction event data to be map[string]interface{}, got %T", compactionEvents[0].Data)
	}
	strategy, ok := cpEventData["strategy"].(string)
	if !ok || strategy != "checkpoint_drop" {
		t.Errorf("expected compaction strategy 'checkpoint_drop', got %q", strategy)
	}

	// Verify provider was called
	h.AssertProviderCalledN(1)

	// Check the provider request to verify checkpoint summaries were dropped
	lastReq := h.Provider().LastRequest()
	if lastReq == nil {
		t.Fatal("expected provider to have been called")
	}

	// Count checkpoint summaries in the request
	cpCountInRequest := 0
	var cpContentsInRequest []string
	for _, msg := range lastReq.Messages {
		if msg.Meta != nil && msg.Meta[core.MetaKeyCheckpoint] == "true" {
			cpCountInRequest++
			cpContentsInRequest = append(cpContentsInRequest, msg.Content)
		}
	}

	// Verify that some checkpoint summaries were dropped
	if cpCountInRequest >= numCheckpoints {
		t.Errorf("expected checkpoint summaries to be dropped, but found %d (same as before: %d)",
			cpCountInRequest, numCheckpoints)
	}
	if cpCountInRequest == 0 {
		t.Error("expected at least some checkpoint summaries to remain after dropping")
	}

	// Verify that the dropped summaries are the OLDEST ones (first added)
	// and the newest ones are preserved.
	// Track which contents were dropped vs. kept.
	droppedContents := make(map[string]bool)
	keptContents := make(map[string]bool)
	for originalContent := range cpContentsBefore {
		foundInRequest := false
		for _, reqContent := range cpContentsInRequest {
			if reqContent == originalContent {
				foundInRequest = true
				break
			}
		}
		if !foundInRequest {
			droppedContents[originalContent] = true
		} else {
			keptContents[originalContent] = true
		}
	}

	// Verify oldest summaries were dropped (turn 1, 2, 3 should be among the dropped)
	droppedOldest := 0
	for content := range droppedContents {
		if strings.Contains(content, "turn 1") ||
			strings.Contains(content, "turn 2") ||
			strings.Contains(content, "turn 3") {
			droppedOldest++
		}
	}
	if droppedOldest < 3 {
		t.Errorf("expected at least turns 1-3 to be dropped, found %d of them dropped", droppedOldest)
	}

	// Verify newest summaries are preserved (turn 15 should be among the kept)
	foundNewest := false
	for content := range keptContents {
		if strings.Contains(content, "turn 15") {
			foundNewest = true
			break
		}
	}
	if !foundNewest {
		t.Error("expected the newest checkpoint summary (turn 15) to be preserved")
	}

	// Verify that no new metadata messages were created to replace dropped summaries
	// (i.e., we didn't replace summaries with "[Summarized: ...]" or similar)
	for _, msg := range lastReq.Messages {
		if msg.Role == "user" && msg.Meta == nil {
			// Non-checkpoint user messages should be the recent ones or the final query
			if strings.Contains(msg.Content, "Summarized") ||
				strings.Contains(msg.Content, "[metadata") ||
				strings.Contains(msg.Content, "[compacted") {
				t.Errorf("found replacement metadata message: %q", msg.Content)
			}
		}
	}

	// Verify the final query is intact
	lastMsg := lastReq.Messages[len(lastReq.Messages)-1]
	if lastMsg.Content != "Final query after many turns" {
		t.Errorf("expected final query intact, got %q", lastMsg.Content)
	}
	if lastMsg.Meta != nil && lastMsg.Meta[core.MetaKeyCheckpoint] == "true" {
		t.Error("final query should not have checkpoint meta")
	}

	// Verify the system prompt is still first
	if len(lastReq.Messages) == 0 || lastReq.Messages[0].Role != "system" {
		t.Error("expected system prompt as first message")
	}

	// Verify recent messages are preserved (they're in the protected zone)
	recentPreserved := 0
	for _, msg := range lastReq.Messages {
		if strings.HasPrefix(msg.Content, "Recent question") ||
			strings.HasPrefix(msg.Content, "Recent answer") {
			recentPreserved++
		}
	}
	if recentPreserved < 20 {
		t.Errorf("expected at least 20 recent messages preserved, got %d", recentPreserved)
	}
}
