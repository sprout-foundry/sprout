package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/seed/events"
)

// overflowRecoveryProvider is a scripted provider: the first overflowOn chat
// calls fail with a ContextOverflowError, then the provider succeeds. Its
// token estimate reflects the real prepared content so the loop's proactive
// trigger and the recovery compaction agree on pressure.
type overflowRecoveryProvider struct {
	calls      int
	overflowOn int
	info       ProviderInfo
	resp       *ChatResponse
}

func (p *overflowRecoveryProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	p.calls++
	if p.calls <= p.overflowOn {
		return nil, &ContextOverflowError{TokensUsed: p.info.ContextSize + 100, TokensLimit: p.info.ContextSize}
	}
	return p.resp, nil
}

func (p *overflowRecoveryProvider) ChatStream(_ context.Context, _ *ChatRequest, handler StreamHandler) error {
	p.calls++
	if p.calls <= p.overflowOn {
		return &ContextOverflowError{TokensUsed: p.info.ContextSize + 100, TokensLimit: p.info.ContextSize}
	}
	handler.OnContent("recovered")
	handler.OnDone(p.resp)
	return nil
}

func (p *overflowRecoveryProvider) Info() ProviderInfo { return p.info }

func (p *overflowRecoveryProvider) EstimateTokens(req *ChatRequest) int {
	return roughTokens(req.Messages)
}

// overflowSeedMessages builds 60 alternating user/assistant messages (~1000
// chars each) so the prepared prompt lands above the recovery compaction
// target (0.595 × context) but below the proactive trigger (0.85 × context) —
// the window where only provider-side overflow recovery can rescue the turn.
func overflowSeedMessages() []Message {
	msgs := make([]Message, 0, 60)
	for i := 0; i < 30; i++ {
		msgs = append(msgs,
			Message{Role: "user", Content: strings.Repeat("y", 1000)},
			Message{Role: "assistant", Content: strings.Repeat("x", 1000)},
		)
	}
	return msgs
}

// hasOverflowRecoveryCompactionEvent drains the subscription looking for a
// compaction event carrying trigger "context_overflow_recovery".
func hasOverflowRecoveryCompactionEvent(ch <-chan events.UIEvent) bool {
	for {
		select {
		case ev := <-ch:
			if ev.Type != events.EventTypeCompaction {
				continue
			}
			data, ok := ev.Data.(map[string]interface{})
			if !ok {
				continue
			}
			if trigger, _ := data["trigger"].(string); trigger == "context_overflow_recovery" {
				return true
			}
		default:
			return false
		}
	}
}

// overflowTestAgent wires an agent over the scripted provider with a small
// retry delay and an adaptive pruner, seeded with a long conversation.
func overflowTestAgent(t *testing.T, p *overflowRecoveryProvider, bus *events.EventBus) *Agent {
	t.Helper()
	a, err := NewAgent(Options{
		Provider:        p,
		Executor:        &mockExecutor{},
		EventPublisher:  bus,
		RetryConfig:     RetryConfig{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
		Pruner:          NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages: overflowSeedMessages(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return a
}

func TestProcessQuery_ContextOverflowRecoversWithCompaction(t *testing.T) {
	provider := &overflowRecoveryProvider{
		overflowOn: 1,
		info:       ProviderInfo{Model: "test", ContextSize: 20000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "recovered"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		},
	}
	bus := events.NewEventBus()
	sub := bus.Subscribe("test")

	a := overflowTestAgent(t, provider, bus)

	result, err := a.Run(context.Background(), "final query")
	if err != nil {
		t.Fatalf("expected context-overflow recovery to complete the query, got error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("expected result %q, got %q", "recovered", result)
	}
	if provider.calls != 2 {
		t.Errorf("expected exactly 2 provider calls (overflow + recovery retry), got %d", provider.calls)
	}
	if !hasOverflowRecoveryCompactionEvent(sub) {
		t.Error("expected a compaction event with trigger 'context_overflow_recovery'")
	}
}

func TestProcessQueryStream_ContextOverflowRecoversWithCompaction(t *testing.T) {
	provider := &overflowRecoveryProvider{
		overflowOn: 1,
		info:       ProviderInfo{Model: "test", ContextSize: 20000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "recovered"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 20, TotalTokens: 120},
		},
	}
	bus := events.NewEventBus()
	sub := bus.Subscribe("test")

	a := overflowTestAgent(t, provider, bus)

	result, err := a.RunStream(context.Background(), "final query")
	if err != nil {
		t.Fatalf("expected streaming context-overflow recovery to complete the query, got error: %v", err)
	}
	if result != "recovered" {
		t.Errorf("expected result %q, got %q", "recovered", result)
	}
	if provider.calls != 2 {
		t.Errorf("expected exactly 2 provider calls (overflow + recovery retry), got %d", provider.calls)
	}
	if !hasOverflowRecoveryCompactionEvent(sub) {
		t.Error("expected a compaction event with trigger 'context_overflow_recovery'")
	}
}

// partialThenTransientProvider streams a partial chunk then fails with a
// transient error, so the streaming retry path must detect that content was
// already delivered and fail fast instead of retrying — which would append
// attempt-2's content to the same shared buffers and duplicate the text.
type partialThenTransientProvider struct {
	calls     int
	info      ProviderInfo
	streamErr error
}

func (p *partialThenTransientProvider) Chat(_ context.Context, _ *ChatRequest) (*ChatResponse, error) {
	return nil, errors.New("not used")
}

func (p *partialThenTransientProvider) ChatStream(_ context.Context, _ *ChatRequest, handler StreamHandler) error {
	p.calls++
	handler.OnContent("partial")
	handler.OnReasoning("thinking")
	return p.streamErr
}

func (p *partialThenTransientProvider) Info() ProviderInfo { return p.info }

func (p *partialThenTransientProvider) EstimateTokens(_ *ChatRequest) int { return 100 }

func TestProcessQueryStream_PartialOutputThenTransientFailsFast(t *testing.T) {
	provider := &partialThenTransientProvider{
		info:      ProviderInfo{Model: "test", ContextSize: 20000},
		streamErr: fmt.Errorf("timeout waiting for stream"),
	}
	a, err := NewAgent(Options{
		Provider:    provider,
		Executor:    &mockExecutor{},
		RetryConfig: RetryConfig{InitialDelay: time.Millisecond, MaxDelay: time.Millisecond},
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = a.RunStream(context.Background(), "query")
	if err == nil {
		t.Fatal("expected error when the stream fails after delivering partial content")
	}
	if !strings.Contains(err.Error(), "timeout waiting for stream") {
		t.Errorf("expected the transient error surfaced, got %v", err)
	}
	if provider.calls != 1 {
		t.Errorf("expected exactly 1 provider call (no retry after partial delivery), got %d", provider.calls)
	}
	// The partial text must appear exactly once in the streaming buffers — a
	// retry would have appended it a second time.
	if got := a.outputMgr.ContentBuffer().String(); got != "partial" {
		t.Errorf("expected buffer to hold the single partial chunk, got %q", got)
	}
	if got := a.outputMgr.ReasoningBuffer().String(); got != "thinking" {
		t.Errorf("expected reasoning buffer to hold the single partial chunk, got %q", got)
	}
	// No assistant message with the partial text may exist in state: the run
	// failed before an assistant message was recorded.
	for _, m := range a.State().Messages() {
		if m.Role == "assistant" && strings.Contains(m.Content, "partial") {
			t.Errorf("unexpected assistant message with partial content in state: %q", m.Content)
		}
	}
}

func TestProcessQuery_ContextOverflowPersistsFailsFast(t *testing.T) {
	provider := &overflowRecoveryProvider{
		overflowOn: 100, // always overflows
		info:       ProviderInfo{Model: "test", ContextSize: 20000},
		resp:       &ChatResponse{Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "x"}}}},
	}

	a := overflowTestAgent(t, provider, nil)

	_, err := a.Run(context.Background(), "final query")
	if err == nil {
		t.Fatal("expected error when context overflow persists after recovery")
	}
	var c *ContextOverflowError
	if !errors.As(err, &c) {
		t.Fatalf("expected ContextOverflowError, got %T: %v", err, err)
	}
	if provider.calls != 2 {
		t.Errorf("expected exactly 2 provider calls (overflow + recovery retry, then overflow again), got %d", provider.calls)
	}
}
