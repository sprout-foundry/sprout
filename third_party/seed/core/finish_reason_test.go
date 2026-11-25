package core

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// --- Multi-response mock provider for finish reason tests ---

type finishReasonProvider struct {
	responses []*ChatResponse
	idx       int
	info      ProviderInfo
}

func (m *finishReasonProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	if m.idx >= len(m.responses) {
		return nil, fmt.Errorf("provider exhausted: %w", ErrNoProvider)
	}
	resp := m.responses[m.idx]
	m.idx++
	return resp, nil
}

func (m *finishReasonProvider) ChatStream(_ context.Context, _ *ChatRequest, h StreamHandler) error {
	if m.idx >= len(m.responses) {
		return fmt.Errorf("provider exhausted: %w", ErrNoProvider)
	}
	resp := m.responses[m.idx]
	m.idx++
	h.OnDone(resp)
	return nil
}

func (m *finishReasonProvider) Info() ProviderInfo {
	return m.info
}

func (m *finishReasonProvider) EstimateTokens(_ *ChatRequest) int {
	return 10
}

func newFRProvider(responses ...*ChatResponse) *finishReasonProvider {
	return &finishReasonProvider{
		responses: responses,
		info: ProviderInfo{
			Model:       "test-model",
			ContextSize: 128000,
		},
	}
}

func frTextResponse(content string, finishReason string) *ChatResponse {
	return &ChatResponse{
		Choices: []ChatChoice{{
			Message:      Message{Role: "assistant", Content: content},
			FinishReason: finishReason,
		}},
		Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
	}
}

// findEvent returns the first event of the given type, or zero value if not found.
func findEvent(evts []events.UIEvent, eventType string) events.UIEvent {
	for _, e := range evts {
		if e.Type == eventType {
			return e
		}
	}
	return events.UIEvent{}
}

// --- Tests ---

func TestFinishReasonStop(t *testing.T) {
	provider := newFRProvider(frTextResponse("Done.", "stop"))
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Errorf("expected %q, got %q", "Done.", result)
	}
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

func TestFinishReasonEmpty(t *testing.T) {
	provider := newFRProvider(frTextResponse("Hello.", ""))
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Hello." {
		t.Errorf("expected %q, got %q", "Hello.", result)
	}
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

func TestFinishReasonLength_Continues(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("This is a long", "length"),
		frTextResponse(" response.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != " response." {
		t.Errorf("expected %q, got %q", " response.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonLength_MaxContinuations(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("chunk", "length"),
		frTextResponse("chunk", "length"),
		frTextResponse("chunk", "length"),
		frTextResponse("chunk", "length"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.idx != 4 {
		t.Errorf("expected 4 provider calls, got %d", provider.idx)
	}
	if result != "chunk" {
		t.Errorf("expected %q, got %q", "chunk", result)
	}
}

func TestFinishReasonContentFilter_RetriesOnce(t *testing.T) {
	// First content_filter triggers a retry. Second content_filter returns error.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError on second content_filter")
	}
	if !IsContentFiltered(err) {
		t.Fatalf("expected ContentFilteredError, got: %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}

	// Verify Provider field is set
	cfErr := &ContentFilteredError{}
	if !errors.As(err, &cfErr) {
		t.Fatal("expected ContentFilteredError via errors.As")
	}
	if cfErr.Provider != "test-model" {
		t.Errorf("expected Provider 'test-model', got %q", cfErr.Provider)
	}
}

func TestFinishReasonContentFilter_RetrySucceeds(t *testing.T) {
	// First content_filter triggers a retry. Second response succeeds.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("rephrased response", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "rephrased response" {
		t.Errorf("expected %q, got %q", "rephrased response", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}
}

func TestFinishReasonContentFilter_PublishesErrorOnSecond(t *testing.T) {
	// Error events published on both first (retrying) and second (exhausted) content_filter.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_cf_err__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError")
	}

	evts := drainEvents(sub)
	// Should have 2 error events: one for first retry, one for second (exhausted)
	var errorEvts []events.UIEvent
	for _, e := range evts {
		if e.Type == EventTypeError {
			errorEvts = append(errorEvts, e)
		}
	}
	if len(errorEvts) != 2 {
		t.Fatalf("expected 2 error events (first retry + second exhausted), got %d", len(errorEvts))
	}
	// First event: retrying
	data1, ok := errorEvts[0].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", errorEvts[0].Data)
	}
	if data1["message"] != "response filtered by content policy (retrying)" {
		t.Errorf("expected retrying message, got %v", data1["message"])
	}
	// Second event: exhausted
	data2, ok := errorEvts[1].Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected map data, got %T", errorEvts[1].Data)
	}
	if data2["message"] != "response filtered by content policy (retry exhausted)" {
		t.Errorf("expected exhausted message, got %v", data2["message"])
	}
}

func TestFinishReasonContentFilter_RetriesOnFirst(t *testing.T) {
	// First content_filter triggers a retry — the second response is called.
	provider := newFRProvider(
		frTextResponse("This was cut off...", "content_filter"),
		frTextResponse("rephrased answer", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}
	if result != "rephrased answer" {
		t.Errorf("expected %q, got %q", "rephrased answer", result)
	}
}

func TestFinishReasonUnknown_Continues(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("partial", "some_unknown_reason"),
		frTextResponse(" done", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != " done" {
		t.Errorf("expected %q, got %q", " done", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonUnknown_MaxContinuations(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("x", "weird"),
		frTextResponse("x", "weird"),
		frTextResponse("x", "weird"),
		frTextResponse("x", "weird"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.idx != 4 {
		t.Errorf("expected 4 provider calls, got %d", provider.idx)
	}
	if result != "x" {
		t.Errorf("expected %q, got %q", "x", result)
	}
}

func TestFinishReasonToolCalls(t *testing.T) {
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"hello"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("Got it.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "echo result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Got it." {
		t.Errorf("expected %q, got %q", "Got it.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReason_NoChoices(t *testing.T) {
	provider := newFRProvider(&ChatResponse{
		Choices: []ChatChoice{},
		Usage:   ChatUsage{PromptTokens: 10, CompletionTokens: 0, TotalTokens: 10},
	})
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected error for zero choices, got nil")
	}
	if !errors.Is(err, ErrZeroChoices) {
		t.Fatalf("expected ErrZeroChoices, got %v", err)
	}
	if result != "" {
		t.Errorf("expected empty result, got %q", result)
	}
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

func TestFinishReasonLength_NoErrorEvent(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("truncated", "length"),
		frTextResponse(" done", "stop"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test2__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	evts := drainEvents(sub)
	if findEventType(evts, EventTypeError) {
		t.Errorf("expected no error events for length finish reason")
	}
}

func TestFinishReasonContentFilter_EventDetails(t *testing.T) {
	// Error events published on both first (retrying) and second (exhausted) content_filter.
	provider := newFRProvider(
		frTextResponse("filtered content", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test3__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError on second content_filter")
	}

	evts := drainEvents(sub)
	// Find the second error event (exhausted)
	var errorEvts []events.UIEvent
	for _, e := range evts {
		if e.Type == EventTypeError {
			errorEvts = append(errorEvts, e)
		}
	}
	if len(errorEvts) < 2 {
		t.Fatalf("expected at least 2 error events, got %d", len(errorEvts))
	}
	errEvt := errorEvts[1] // second event (exhausted)
	data, ok := errEvt.Data.(map[string]interface{})
	if !ok {
		t.Fatalf("expected error event data to be map, got %T", errEvt.Data)
	}
	if data["error"] != "content_filter" {
		t.Errorf("expected error field 'content_filter', got %v", data["error"])
	}
}

func TestFinishReasonLength_ResetsOnToolCalls(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("thinking...", "length"),
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("final", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "final" {
		t.Errorf("expected %q, got %q", "final", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonContentFilter_NoTurnCompletedOnError(t *testing.T) {
	// When content_filter returns an error (second occurrence), no checkpoint
	// is recorded because the turn didn't complete normally.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test4__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError")
	}

	evts := drainEvents(sub)
	// No query_completed because the turn errored out
	if findEventType(evts, EventTypeQueryCompleted) {
		t.Error("expected no query_completed event for content_filter error")
	}
}

func TestFinishReasonContentFilter_WithEventBus(t *testing.T) {
	// Error events published on both first (retrying) and second (exhausted) content_filter.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test5__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError")
	}

	evts := drainEvents(sub)
	if !findEventType(evts, EventTypeError) {
		t.Error("expected error event for second content_filter")
	}
}

func TestFinishReasonLength_Stream(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("partial", "length"),
		frTextResponse(" done", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != " done" {
		t.Errorf("expected %q, got %q", " done", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonContentFilter_Stream(t *testing.T) {
	// Same retry-then-error behavior in streaming mode.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("still filtered", "content_filter"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test6__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError on second content_filter")
	}
	if result != "" {
		t.Errorf("expected empty result on error, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}

	evts := drainEvents(sub)
	if !findEventType(evts, EventTypeError) {
		t.Error("expected error event for content_filter in streaming mode")
	}
}

func TestFinishReasonContentFilter_Stream_RetrySucceeds(t *testing.T) {
	// First content_filter triggers retry; second response succeeds in streaming mode.
	provider := newFRProvider(
		frTextResponse("filtered", "content_filter"),
		frTextResponse("rephrased", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "rephrased" {
		t.Errorf("expected %q, got %q", "rephrased", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}
}

func TestFinishReasonContentFilter_CaseSensitive(t *testing.T) {
	// "CONTENT_FILTER" (uppercase) should NOT match "content_filter".
	provider := newFRProvider(
		frTextResponse("content", "CONTENT_FILTER"),
		frTextResponse("second", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// CONTENT_FILTER hits default case → continues → second call returns "second"
	if result != "second" {
		t.Errorf("expected %q (default case continued), got %q", "second", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (default continued), got %d", provider.idx)
	}
}

func TestFinishReasonLength_DrainsEventChannel(t *testing.T) {
	provider := newFRProvider(
		frTextResponse("a", "length"),
		frTextResponse("b", "length"),
		frTextResponse("c", "stop"),
	)
	bus := events.NewEventBus()
	_ = bus.Subscribe("__fr_test7__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "c" {
		t.Errorf("expected %q, got %q", "c", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonContentFilter_WithTools(t *testing.T) {
	// content_filter with tool calls: first occurrence retries, second returns error.
	// Tools should never execute when content_filter is returned.
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "filtered",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "dangerous_tool",
							Arguments: `{}`,
						},
					}},
				},
				FinishReason: "content_filter",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "still filtered",
					ToolCalls: []ToolCall{{
						ID:   "call_2",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "dangerous_tool",
							Arguments: `{}`,
						},
					}},
				},
				FinishReason: "content_filter",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "should not execute", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected ContentFilteredError on second content_filter")
	}
	if !IsContentFiltered(err) {
		t.Fatalf("expected ContentFilteredError, got: %v", err)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls (retry once), got %d", provider.idx)
	}
}

func TestFinishReasonStop_EmptyContent_Continues(t *testing.T) {
	// "stop" with empty content should be treated as incomplete and continue.
	provider := newFRProvider(
		frTextResponse("", "stop"),
		frTextResponse("Here is the answer.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Here is the answer." {
		t.Errorf("expected %q, got %q", "Here is the answer.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_WhitespaceOnly_Continues(t *testing.T) {
	// "stop" with whitespace-only content should also continue.
	provider := newFRProvider(
		frTextResponse("   \n\t  ", "stop"),
		frTextResponse("Real content.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Real content." {
		t.Errorf("expected %q, got %q", "Real content.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_EmptyContent_MaxContinuations(t *testing.T) {
	// After 2 consecutive empty "stop" responses, force-finalize with BlankResponseError.
	provider := newFRProvider(
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected BlankResponseError, got nil")
	}
	if !IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}
	bErr := err.(*BlankResponseError)
	if bErr.Count != 2 {
		t.Errorf("expected count 2, got %d", bErr.Count)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_EmptyContent_Stream_Continues(t *testing.T) {
	// "stop" with empty content should also continue in streaming mode.
	provider := newFRProvider(
		frTextResponse("", "stop"),
		frTextResponse("Streaming answer.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Streaming answer." {
		t.Errorf("expected %q, got %q", "Streaming answer.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_WithToolCalls_DoesNotContinue(t *testing.T) {
	// "stop" with empty content but tool calls present should NOT continue —
	// tool calls represent progress and the existing logic handles them.
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"hello"}`,
						},
					}},
				},
				FinishReason: "stop",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("After tool.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "echo result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have executed tool, then continued to second response
	if result != "After tool." {
		t.Errorf("expected %q, got %q", "After tool.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_EmptyContent_NoErrorEvent(t *testing.T) {
	// Force-finalize after 2 consecutive blanks returns BlankResponseError
	// but should NOT publish error events (the error is returned to the caller).
	provider := newFRProvider(
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
		frTextResponse("", "stop"),
	)
	bus := events.NewEventBus()
	sub := bus.Subscribe("__fr_test_empty__")
	agent, _ := NewAgent(Options{
		Provider:       provider,
		Executor:       &mockExecutor{},
		EventPublisher: bus,
	})

	_, err := agent.Run(context.Background(), "hi")
	if err == nil {
		t.Fatal("expected BlankResponseError, got nil")
	}
	if !IsBlankResponse(err) {
		t.Fatalf("expected BlankResponseError, got: %v", err)
	}

	evts := drainEvents(sub)
	if findEventType(evts, EventTypeError) {
		t.Errorf("expected no error events for stop/empty force-finalize")
	}
}

func TestFinishReasonStop_EmptyContent_NilToolCalls(t *testing.T) {
	// "stop" with empty content and nil ToolCalls (not just empty slice) should continue.
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message:      Message{Role: "assistant", Content: "", ToolCalls: nil},
				FinishReason: "stop",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("Recovery content.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Recovery content." {
		t.Errorf("expected %q, got %q", "Recovery content.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

// --- "stop" with incomplete content tests ---

func TestFinishReasonStop_IncompleteContent_TrailingEllipsis(t *testing.T) {
	// "stop" with content ending in "..." should be treated as incomplete
	// and continue the loop, asking for the final answer.
	provider := newFRProvider(
		frTextResponse("Here is the answer...", "stop"),
		// Second response must be long enough (>10 words) so it's not flagged as too short.
		frTextResponse("This is the complete answer that I was going to provide from the start.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "This is the complete answer that I was going to provide from the start." {
		t.Errorf("expected complete answer, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_IncompleteContent_AbruptEnding(t *testing.T) {
	// "stop" with content ending in comma should be treated as incomplete.
	provider := newFRProvider(
		frTextResponse("The file contains,", "stop"),
		frTextResponse("It has a very long content inside the file that we are looking at today.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "It has a very long content inside the file that we are looking at today." {
		t.Errorf("expected complete answer, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_IncompleteContent_UnclosedCodeBlock(t *testing.T) {
	// "stop" with unclosed code block (odd number of ``` markers) should continue.
	provider := newFRProvider(
		frTextResponse("Here's the code:\n```go\nfunc main() {}", "stop"),
		frTextResponse("Here is the complete code example that demonstrates the pattern we discussed.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Here is the complete code example that demonstrates the pattern we discussed." {
		t.Errorf("expected complete answer, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_IncompleteContent_MaxContinuations(t *testing.T) {
	// After maxContinuations incomplete "stop" responses, force-finalize.
	provider := newFRProvider(
		frTextResponse("First incomplete...", "stop"),
		frTextResponse("Second incomplete...", "stop"),
		frTextResponse("Third incomplete...", "stop"),
		frTextResponse("Fourth incomplete...", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider.idx != 4 {
		t.Errorf("expected 4 provider calls, got %d", provider.idx)
	}
	if result != "Fourth incomplete..." {
		t.Errorf("expected %q, got %q", "Fourth incomplete...", result)
	}
}

func TestFinishReasonStop_IncompleteContent_Stream_Continues(t *testing.T) {
	// "stop" with incomplete content should also continue in streaming mode.
	provider := newFRProvider(
		frTextResponse("Here is the answer...", "stop"),
		frTextResponse("This is the complete answer that I was going to provide from the start.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "This is the complete answer that I was going to provide from the start." {
		t.Errorf("expected complete answer, got %q", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_IncompleteContent_CompleteShortAnswer(t *testing.T) {
	// "stop" with a short but complete answer (e.g., "Done.") should NOT
	// trigger continuation — the validator recognizes it as complete.
	provider := newFRProvider(frTextResponse("Done.", "stop"))
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Done." {
		t.Errorf("expected %q, got %q", "Done.", result)
	}
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

func TestFinishReasonStop_IncompleteContent_WithToolCalls_DoesNotContinue(t *testing.T) {
	// "stop" with incomplete content but tool calls present should NOT
	// trigger the incomplete check — tool calls represent progress and
	// the existing logic handles them.
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "Let me check...",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"hello"}`,
						},
					}},
				},
				FinishReason: "stop",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("After tool.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "echo result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "After tool." {
		t.Errorf("expected %q, got %q", "After tool.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

// --- Tentative post-tool "stop" response tests ---

func TestFinishReasonStop_TentativePostTool_Rejected(t *testing.T) {
	// After tool results, a "stop" with tentative planning content should be rejected
	// with the post-tool rejection message. After rejection, the loop continues and
	// the model should provide a concrete answer.
	provider := newFRProvider(
		// 1. Model makes a tool call
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 2. Tentative content after tool results → rejected (rejection #1)
		frTextResponse("Let me check the results now.", "stop"),
		// 3. Concrete final answer → accepted
		frTextResponse("The answer is 42.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The answer is 42." {
		t.Errorf("expected %q, got %q", "The answer is 42.", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePostTool_FirstRejectionThenConcrete(t *testing.T) {
	// After tool results, a tentative "stop" triggers post-tool rejection #1.
	// On the next iteration, followsRecentToolResults() returns false (the
	// rejected assistant message sits between the tool result and the current
	// message, breaking the scan). A concrete final answer is then accepted.
	//
	// Flow:
	//  1. Tool call → result
	//  2. "Let me check the results." → post-tool rejection #1 (tentativeRejectionCount=1) → continue
	//  3. "The answer is 42." → accepted (non-tentative, no tool results immediately before)
	provider := newFRProvider(
		// 1. Model makes a tool call
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 2. Tentative → post-tool rejection #1 (tentativeRejectionCount=1)
		frTextResponse("Let me check the results.", "stop"),
		// 3. Concrete final answer → accepted
		frTextResponse("The answer is 42.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Post-tool rejection + concrete answer → 3rd call wins
	if result != "The answer is 42." {
		t.Errorf("expected %q, got %q", "The answer is 42.", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePostTool_ConcreteAnswer_Accepted(t *testing.T) {
	// After tool results, a concrete (non-tentative) response should be accepted
	// immediately without any rejection — the post-tool rejection path is
	// bypassed because the content is not tentative.
	provider := newFRProvider(
		// 1. Model makes a tool call
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 2. Concrete answer (doesn't match tentative prefixes)
		frTextResponse("The file contents are: error, success, failed.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The file contents are: error, success, failed." {
		t.Errorf("expected %q, got %q", "The file contents are: error, success, failed.", result)
	}
	if provider.idx != 2 {
		t.Errorf("expected 2 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePrefix_NoToolResults_AcceptedImmediately(t *testing.T) {
	// Without tool results in the message history, the post-tool rejection path
	// should NOT trigger (followsRecentToolResults returns false). The response
	// is accepted immediately even if it starts with a planning prefix, because
	// tentative detection only fires after tool results.
	provider := newFRProvider(
		frTextResponse("Let me think about this.", "stop"),
	)
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: &mockExecutor{},
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Let me think about this." {
		t.Errorf("expected %q, got %q", "Let me think about this.", result)
	}
	// Only 1 provider call — no tentative rejection without tool results
	if provider.idx != 1 {
		t.Errorf("expected 1 provider call, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePostTool_Stream_Rejected(t *testing.T) {
	// Same as TestFinishReasonStop_TentativePostTool_Rejected but using RunStream.
	provider := newFRProvider(
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		frTextResponse("Let me check the results now.", "stop"),
		frTextResponse("Streaming answer.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.RunStream(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "Streaming answer." {
		t.Errorf("expected %q, got %q", "Streaming answer.", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePostTool_RejectionThenAccepted(t *testing.T) {
	// After tool results, a tentative "stop" triggers post-tool rejection #1.
	// The next response has followsRecentToolResults()=false (the rejected
	// assistant message sits between the tool result and the current message),
	// so it is accepted as the final answer.
	//
	// Flow:
	//  1. Tool call → result
	//  2. "Let me check." → post-tool rejection #1 (tentativeRejectionCount=1) → continue
	//  3. "The answer is 42." → accepted
	provider := newFRProvider(
		// 1. Tool call
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 2. Post-tool rejection #1
		frTextResponse("Let me check.", "stop"),
		// 3. Concrete answer → accepted
		frTextResponse("The answer is 42.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{{Role: "tool", Content: "tool result", ToolCallID: "call_1"}},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The answer is 42." {
		t.Errorf("expected %q, got %q", "The answer is 42.", result)
	}
	if provider.idx != 3 {
		t.Errorf("expected 3 provider calls, got %d", provider.idx)
	}
}

func TestFinishReasonStop_TentativePostTool_MultipleToolCycles(t *testing.T) {
	// Verifies that post-tool rejection works across multiple tool cycles.
	// After a new tool call is executed, the tentativeRejectionCount resets
	// (because the new tool result establishes a fresh "recent tool result"
	// baseline), so post-tool rejection fires again in the next cycle.
	//
	// Flow:
	//  1. Tool call A → result
	//  2. "Let me review." → post-tool rejection #1 (tentativeRejectionCount=1) → continue
	//  3. Tool call B → result (counter resets to 0, new tool result baseline)
	//  4. "I'll analyze it." → post-tool rejection #1 again (tentativeRejectionCount=1) → continue
	//  5. "The answer is 42." → accepted (non-tentative)
	//
	// Note: the >= 2 tentativeRejectionCount path is unreachable in practice
	// because followsRecentToolResults() only skips one assistant message.
	// After rejection #1, the rejected assistant sits between the tool result
	// and the current message, blocking the scan. So post-tool rejection can
	// only fire once per tool-result cycle. This test demonstrates that the
	// mechanism correctly re-fires after a new tool call resets the state.
	provider := newFRProvider(
		// 1. Tool call A
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_1",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 2. Post-tool rejection #1 (tentativeRejectionCount=1)
		frTextResponse("Let me check.", "stop"),
		// 3. Tool call B (counter resets to 0, new tool result baseline)
		&ChatResponse{
			Choices: []ChatChoice{{
				Message: Message{
					Role:    "assistant",
					Content: "",
					ToolCalls: []ToolCall{{
						ID:   "call_2",
						Type: "function",
						Function: ToolCallFunction{
							Name:      "echo",
							Arguments: `{"message":"test2"}`,
						},
					}},
				},
				FinishReason: "tool_calls",
			}},
			Usage: ChatUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		},
		// 4. Post-tool rejection #1 again (tentativeRejectionCount=1, counter reset)
		frTextResponse("Let me look at the data.", "stop"),
		// 5. Concrete answer → accepted
		frTextResponse("The answer is 42.", "stop"),
	)
	executor := &mockExecutor{
		results: []Message{
			{Role: "tool", Content: "result1", ToolCallID: "call_1"},
			{Role: "tool", Content: "result2", ToolCallID: "call_2"},
		},
	}
	agent, _ := NewAgent(Options{
		Provider: provider,
		Executor: executor,
	})

	result, err := agent.Run(context.Background(), "hi")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "The answer is 42." {
		t.Errorf("expected %q, got %q", "The answer is 42.", result)
	}
	// 5 provider calls: tool A, tentative (rejected), tool B, tentative (rejected), answer
	if provider.idx != 5 {
		t.Errorf("expected 5 provider calls, got %d", provider.idx)
	}
}
