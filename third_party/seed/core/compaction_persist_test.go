package core

import (
	"context"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// Regression tests for compaction persistence: the loop's compaction
// cascade must PERSIST its result into state. Before the fix, every
// over-trigger iteration re-derived the cascade from raw (growing) state:
// the pruner re-scored against a longer tail (keep boundary drifting —
// oscillating request sizes), the LLM-summary path regenerated different
// summary text each pass (new mid-history bytes — zero provider
// prompt-cache hits), and each iteration paid the full cascade again.

// TestLoopCompactionPersistsToState runs one seeded turn over the trigger
// and asserts the compaction survived into state: after the run, state's
// message list must equal the compacted request view (minus the prepended
// system prompt), not the original raw history.
func TestLoopCompactionPersistsToState(t *testing.T) {
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 8000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		},
	}

	// 38 turns ≈ 5.8K tokens on an 8K window: over the 0.70 trigger (5600).
	fixture := settleMessages(38)
	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:                  provider,
		Executor:                  NoopExecutor,
		EventPublisher:            bus,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           fixture,
		CompactionTriggerFraction: 0.70,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stateMsgs := a.State().Messages()

	// State must have shrunk relative to the raw fixture + the new turn's
	// messages (user query + assistant response).
	if len(stateMsgs) >= len(fixture)+2 {
		t.Fatalf("compaction did not persist: state holds %d messages, raw fixture was %d (+2 for the new turn)", len(stateMsgs), len(fixture))
	}

	// The compacted request view and state must agree: the provider's last
	// request is [agent system prompt, ...persisted non-system messages];
	// state additionally carries the fixture's own leading system message
	// and the trailing assistant response. Wire tail == state middle.
	lastReq := provider.lastReqMsgs
	if len(lastReq) == 0 || lastReq[0].Role != "system" {
		t.Fatalf("last request must lead with the system prompt, got %+v", firstOrNil(lastReq))
	}
	if len(stateMsgs) == 0 || stateMsgs[0].Role != "system" {
		t.Fatalf("state must retain its leading (fixture) system message, got %+v", firstOrNil(stateMsgs))
	}
	if len(lastReq)-1 != len(stateMsgs)-2 {
		t.Fatalf("request tail (%d msgs) must equal state minus its system head and assistant tail (state=%d)", len(lastReq)-1, len(stateMsgs))
	}
	for i, sm := range stateMsgs[1 : len(stateMsgs)-1] {
		if sm.Content != lastReq[i+1].Content || sm.Role != lastReq[i+1].Role {
			t.Fatalf("state[%d] diverged from the compacted request view: state=%q/%q wire=%q/%q",
				i+1, sm.Role, sm.Content, lastReq[i+1].Role, lastReq[i+1].Content)
		}
	}

	// The rebase map must be exposed for consumers that mirror state into
	// richer structures (sprout's checkpoints).
	if a.State().LastCompactionRebase() == nil {
		t.Fatal("LastCompactionRebase must be set after a persisted compaction")
	}
}

// TestLoopCompactionNoRefire asserts the second request of the SAME run
// replays the compacted prefix byte-identically — with the request-only
// cascade, iteration 2 rebuilt a different keep-set from grown raw state
// and the prefix bytes changed between iterations 1 and 2 (cache miss on
// every iteration, not just every turn).
func TestLoopCompactionNoRefire(t *testing.T) {
	// Provider answers with a tool call once, then a final text response,
	// forcing two requests in one turn.
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 8000},
	}
	provider.resp = &ChatResponse{
		Choices: []ChatChoice{{Message: Message{
			Role: "assistant",
			ToolCalls: []ToolCall{{
				ID: "c1",
				Function: ToolCallFunction{
					Name:      "read_file",
					Arguments: `{"path":"x"}`,
				},
			}},
		}, FinishReason: "tool_calls"}},
		Usage: ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
	}

	fixture := settleMessages(38)
	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       &resultExecutor{},
		EventPublisher: bus,
		Pruner:         NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		// History sized to sit above the 0.70 trigger (5600 of 8000).
		InitialMessages:           fixture, // ~5.8K tokens
		CompactionTriggerFraction: 0.70,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Flip the scripted response to a final answer after the first request.
	provider.flipToFinal = func() {
		provider.resp = &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		}
	}

	var requests [][]Message
	provider.record = func(msgs []Message) {
		requests = append(requests, append([]Message(nil), msgs...))
	}

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if len(requests) < 2 {
		t.Fatalf("expected >= 2 requests in the turn, got %d", len(requests))
	}
	first, second := requests[0], requests[1]
	if len(second) < len(first) {
		t.Fatalf("second request shrank: %d -> %d messages", len(first), len(second))
	}
	for i := range first {
		if first[i].Content != second[i].Content || first[i].Role != second[i].Role {
			t.Fatalf("request 2 prefix diverged at message %d — iteration 2 rebuilt a different compaction:\n  req1=%q/%q\n  req2=%q/%q",
				i, first[i].Role, first[i].Content, second[i].Role, second[i].Content)
		}
	}
}

// TestPersistCompactedStatePreservesFrontSystemMessages: state-only system
// messages (session-name markers) must survive a persist — they never
// appear on the wire but state consumers (session naming) read them.
func TestPersistCompactedStatePreservesFrontSystemMessages(t *testing.T) {
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 8000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		},
	}

	fixture := settleMessages(38)
	// Prepend a state-only session-name system message.
	stateFixture := append([]Message{{Role: "system", Content: "[SESSION_NAME:]my session"}}, fixture...)

	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:                  provider,
		Executor:                  NoopExecutor,
		EventPublisher:            bus,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           stateFixture,
		CompactionTriggerFraction: 0.70,
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	stateMsgs := a.State().Messages()
	if len(stateMsgs) == 0 || stateMsgs[0].Role != "system" || stateMsgs[0].Content != "[SESSION_NAME:]my session" {
		t.Fatalf("state-only system message must survive a compaction persist at the front, got %+v", firstOrNil(stateMsgs))
	}
}

// TestRebaseCheckpoints: survivor map semantics — ranges that survive map
// through; ranges with a dropped endpoint are removed.
func TestRebaseCheckpoints(t *testing.T) {
	s := NewState()
	s.SetCheckpoints([]TurnCheckpoint{
		{StartIndex: 2, EndIndex: 5, Summary: "survives"},
		{StartIndex: 7, EndIndex: 9, Summary: "dropped (endpoint gone)"},
	})

	survivorOf := map[int]int{0: 0, 1: 1, 2: 2, 3: 3, 4: 4, 5: 5, 10: 7, 11: 8}
	RebaseCheckpoints(s, survivorOf)

	cps := s.GetCheckpoints()
	if len(cps) != 1 {
		t.Fatalf("want 1 surviving checkpoint, got %d", len(cps))
	}
	if cps[0].Summary != "survives" || cps[0].StartIndex != 2 || cps[0].EndIndex != 5 {
		t.Fatalf("surviving checkpoint mis-rebased: %+v", cps[0])
	}
}

// resultExecutor is a minimal ToolExecutor exposing one tool (read_file)
// whose execution returns a small tool result — enough for the loop to run
// a tool-call iteration followed by a final-response iteration.
type resultExecutor struct{}

func (e *resultExecutor) GetTools() []Tool {
	return []Tool{{
		Type: "function",
		Function: ToolFunction{
			Name:        "read_file",
			Description: "test tool",
			Parameters:  map[string]interface{}{"type": "object"},
		},
	}}
}

func (e *resultExecutor) Execute(_ context.Context, calls []ToolCall) []Message {
	out := make([]Message, 0, len(calls))
	for _, c := range calls {
		out = append(out, Message{Role: "tool", ToolCallID: c.ID, Content: "ok"})
	}
	return out
}

// firstOrNil is a nil-safe viewer for failure messages.
func firstOrNil(msgs []Message) *Message {
	if len(msgs) == 0 {
		return nil
	}
	return &msgs[0]
}
