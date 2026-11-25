package core

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/sprout-foundry/seed/events"
)

// Regression tests for compaction thrash: a proactive compaction pass must
// settle BELOW the loop's trigger, or the loop re-fires compaction every
// iteration. Two mechanisms carry the fix:
//   - PruneCallOptions.TargetTokens + ConversationPruner.settleBelowTarget
//   - CompactInputs.TargetTokens overriding the derived 0.85 × TokenLimit

// settleMessages builds a conversation whose estimate sits above a trigger:
// N filler turns of ~150 tokens each plus a small tail. The pruner's own
// strategy targets (0.85/0.77/0.70 × window by message count) can sit ABOVE
// the trigger for short message counts, reproducing the thrash shape.
func settleMessages(n int) []Message {
	msgs := []Message{{Role: "system", Content: "system prompt"}}
	for i := 0; i < n; i++ {
		u, a := makeTextTurn(i)
		msgs = append(msgs, u, a)
	}
	return msgs
}

// TestPruneSettlesBelowTrigger reproduces the thrash: without TargetTokens,
// the adaptive pruner's keep-set can exceed the loop's trigger; with it, the
// post-prune estimate must land at or below the trigger.
func TestPruneSettlesBelowTrigger(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive})
	ctx := context.Background()

	// 30 turns → 61 messages, ~4.6K roughTokens. Window 8K: trigger 0.70×8K
	// = 5600. The pruner's own target for 61 messages is 0.70×8K = 5600 —
	// equal to the trigger, so any content the greedy keep adds above the
	// always-kept core leaves the estimate at/over the trigger.
	const window = 8000
	msgs := settleMessages(30)
	before := roughTokens(msgs)
	if before <= 5600 {
		t.Fatalf("fixture must start over the trigger: got %d tokens", before)
	}

	target := 4800 // 0.60 × window — below the 0.70 trigger
	pruned := cp.Prune(ctx, msgs, before, window, PruneCallOptions{
		IsAgenticFlow: true,
		TargetTokens:  target,
	})

	after := roughTokens(pruned)
	if after > target {
		t.Errorf("prune must settle at or below TargetTokens %d, got %d", target, after)
	}
	if len(pruned) < cp.MinMessagesToKeep() {
		t.Errorf("prune dropped below min keep %d, got %d messages", cp.MinMessagesToKeep(), len(pruned))
	}
	if pruned[0].Role != "system" {
		t.Errorf("system message must survive settle, got %q first", pruned[0].Role)
	}
}

// TestPruneSettleRespectsRecentWindow verifies the settle pass stops rather
// than eating into the protected recent window — losing the live causal
// chain mid-turn is never worth hitting an arbitrary target.
func TestPruneSettleRespectsRecentWindow(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive})
	ctx := context.Background()

	// 30 filler turns + a recognizable tail. Target absurdly low so the
	// settle pass exhausts everything droppable and must stop at the
	// recent window.
	msgs := settleMessages(30)
	tail := []Message{
		{Role: "user", Content: "live task"},
		{Role: "assistant", ToolCalls: []ToolCall{{ID: "c1", Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"x"}`}}}},
		{Role: "tool", ToolCallID: "c1", Content: "result"},
		{Role: "assistant", Content: "final"},
	}
	msgs = append(msgs, tail...)

	before := roughTokens(msgs)
	pruned := cp.Prune(ctx, msgs, before, 8000, PruneCallOptions{
		IsAgenticFlow: true,
		TargetTokens:  10, // unreachable
	})

	if len(pruned) < cp.MinMessagesToKeep() {
		t.Errorf("min keep violated: %d messages", len(pruned))
	}
	// recent window + system + the pinned opening anchor (2 messages)
	if len(pruned) > cp.recentMessagesToKeep+3 {
		t.Errorf("settle kept more than recent window + system + anchor: %d", len(pruned))
	}
	// The live causal chain must be intact and ordered.
	last := pruned[len(pruned)-1]
	if last.Role != "assistant" || last.Content != "final" {
		t.Errorf("tail message must survive, got role=%q content=%q", last.Role, last.Content)
	}
	if !validToolGroups(pruned) {
		t.Error("settle broke tool-call/result atomicity")
	}
}

// TestSettleBelowTargetDropsWholeToolTurns verifies the settle pass drops
// complete turns atomically: an assistant-with-tool-calls never survives
// without its tool results (and vice versa). Dropping raw index 1 — the
// naive implementation — splits these groups and strict providers
// (MiniMax/DeepSeek) reject the request with a threading error.
//
// The settle pass is tested in isolation (direct call): the plain
// importance strategy that runs before it in Prune does NOT guarantee
// tool-group atomicity for non-strict providers — that's pre-existing,
// tolerated behavior, cleaned by the loop's removeOrphanedToolResults
// safety net before the provider send.
func TestSettleBelowTargetDropsWholeToolTurns(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive})

	// 20 tool-call turns in the droppable region + recent tail. The
	// assistant-with-tool-calls carries the bulk of each turn's tokens so a
	// naive sequential dropper crosses the target immediately after
	// removing the assistant — leaving its tool result orphaned in the
	// final state. The turn-aware dropper removes whole turns and can only
	// stop on turn boundaries.
	msgs := []Message{{Role: "system", Content: "system prompt"}}
	for i := 0; i < 20; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			Message{Role: "user", Content: "Q"},
			Message{Role: "assistant", Content: strings.Repeat("A", 40000), ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"f"}`}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: "R"},
			Message{Role: "assistant", Content: "done"},
		)
	}
	msgs = append(msgs, Message{Role: "user", Content: "live task"})

	before := roughTokens(msgs)
	target := before / 2 // crosses mid-turn for a sequential dropper

	pruned := cp.settleBelowTarget(msgs, target, nil)

	if !validToolGroups(pruned) {
		t.Error("settle orphaned a tool call from its result (or vice versa)")
	}
	if after := roughTokens(pruned); after > target+40000 {
		// Allow at most one turn's overshoot: whole-turn drops can step
		// over the target by less than one turn.
		t.Errorf("settle must converge to ~target %d, got %d", target, after)
	}
	if after := roughTokens(pruned); after > before {
		t.Errorf("settle must not grow the list: %d -> %d", before, after)
	}
}

// TestCompactWithTargetTokensOverride verifies CompactInputs.TargetTokens
// tightens the whole pipeline's stop condition — the early-exit comparison
// must use the override, not the derived 0.85 × TokenLimit.
func TestCompactWithTargetTokensOverride(t *testing.T) {
	msgs := settleMessages(20)
	before := roughTokens(msgs) // ~3.1K

	// tokenLimit chosen so the derived target (0.85 × limit) sits ABOVE the
	// whole conversation — no override, no compaction fires at all.
	limit := before * 2
	noTarget := CompactWith(CompactInputs{Messages: msgs, TokenLimit: limit})
	if noTarget.Strategy != "none" {
		t.Fatalf("no-override control should no-op, got strategy %q", noTarget.Strategy)
	}

	// Same limit, but an explicit target just under the current estimate:
	// the pipeline must now fire and settle below the target.
	target := before - 200
	result := CompactWith(CompactInputs{Messages: msgs, TokenLimit: limit, TargetTokens: target})
	if result.Strategy == "none" {
		t.Fatal("TargetTokens override must make CompactWith fire")
	}
	if after := roughTokens(result.Messages); after > target {
		t.Errorf("pipeline must settle at or below target %d, got %d", target, after)
	}
}

// TestLoopProactivePassSettlesBelowTrigger is the end-to-end guard: run a
// seeded agent whose history is above the trigger; count compaction events
// across a multi-iteration run. Before the fix the loop re-fired compaction
// every iteration (3+ events); after, one pass settles the estimate below
// the trigger and subsequent iterations fire none.
func TestLoopProactivePassSettlesBelowTrigger(t *testing.T) {
	// Scripted provider: always answers with a small final response.
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 8000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		},
	}

	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:       provider,
		Executor:       NoopExecutor,
		EventPublisher: bus,
		Pruner:         NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		// History sized to sit above the 0.70 trigger (5600 of 8000).
		InitialMessages:           settleMessages(38), // ~5.8K tokens
		CompactionTriggerFraction: 0.70,
	})
	if err != nil {
		t.Fatal(err)
	}

	if before := roughTokens(a.State().Messages()); before <= 5600 {
		t.Fatalf("fixture must start over the trigger, got %d tokens", before)
	}

	sub := bus.Subscribe("settle-e2e")
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	compactions := 0
	for {
		select {
		case ev := <-sub:
			if ev.Type == events.EventTypeCompaction {
				compactions++
			}
		default:
			goto done
		}
	}
done:
	// Exactly one settling pass: the first iteration fires, settles below
	// the trigger, and no subsequent iteration re-fires (the reported bug
	// was 3-in-22s).
	if compactions != 1 {
		t.Errorf("expected exactly one settled compaction pass, got %d events", compactions)
	}
	// End-to-end invariant: the last request the provider received sits
	// below the trigger (0.70 × 8000 = 5600) — the settle semantics apply
	// to the prepared request slice, not to state (which keeps raw history).
	if after := roughTokens(provider.lastReqMsgs); after > 5600 {
		t.Errorf("post-settle request estimate %d must sit below the 5600 trigger", after)
	}
}

// A configured settle margin settles a proactive pass that far below the
// trigger, so the conversation grows for longer before the next pass.
func TestLoopProactivePassSettlesByTheConfiguredMargin(t *testing.T) {
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 8000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		},
	}
	a, err := NewAgent(Options{
		Provider:                  provider,
		Executor:                  NoopExecutor,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           settleMessages(38),
		CompactionTriggerFraction: 0.70,
		CompactionSettleMargin:    0.25,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// (0.70 - 0.25) × 8000 = 3600.
	if after := roughTokens(provider.lastReqMsgs); after > 3600 {
		t.Errorf("post-settle request estimate %d must sit at or below 3600", after)
	}
}

// TestLoopExhaustedCornerEmitsNoCompactionEvents covers the exhausted
// corner: a short history whose overage lives entirely inside the protected
// recent window can never settle (see docs/compaction.md "Known corner").
// Before the fix, the pruner branch labeled its strategy unconditionally,
// so the re-firing loop emitted one zero-drop compaction event per
// iteration — UI spam with no signal.
func TestLoopExhaustedCornerEmitsNoCompactionEvents(t *testing.T) {
	provider := &settleTestProvider{
		info: ProviderInfo{Model: "test", ContextSize: 80000},
		resp: &ChatResponse{
			Choices: []ChatChoice{{Message: Message{Role: "assistant", Content: "done"}, FinishReason: "stop"}},
			Usage:   ChatUsage{PromptTokens: 100, CompletionTokens: 5, TotalTokens: 105},
		},
	}

	// 10 messages, ~65K rough tokens on an 80K window: every message sits
	// inside the 24-message recent window, so settleBelowTarget's
	// recentStart < 2 guard no-ops the pass and the estimate stays over
	// the 0.70 trigger (56000) for every iteration. The window is sized
	// so ensureRequiredHeadroom is already satisfied (remaining 15K >= its
	// 12K default) — otherwise that pass drops to the min-keep floor and
	// the event it publishes is a REAL compaction, not the zero-drop spam
	// under test.
	fat := []Message{{Role: "system", Content: "system prompt"}}
	for i := 0; i < 4; i++ {
		u, a := makeTextTurn(i)
		u.Content += strings.Repeat("F", 32000)
		a.Content += strings.Repeat("F", 32000)
		fat = append(fat, u, a)
	}

	bus := events.NewEventBus()
	a, err := NewAgent(Options{
		Provider:                  provider,
		Executor:                  NoopExecutor,
		EventPublisher:            bus,
		Pruner:                    NewConversationPruner(PrunerOptions{Strategy: PruneStrategyAdaptive}),
		InitialMessages:           fat,
		CompactionTriggerFraction: 0.70,
	})
	if err != nil {
		t.Fatal(err)
	}
	if before := roughTokens(fat); before <= 56000 {
		t.Fatalf("fixture must start over the trigger, got %d tokens", before)
	}

	sub := bus.Subscribe("exhausted-corner")
	if _, err := a.Run(context.Background(), "go"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	compactions := 0
	for {
		select {
		case ev := <-sub:
			if ev.Type == events.EventTypeCompaction {
				compactions++
			}
		default:
			goto drained
		}
	}
drained:
	if compactions != 0 {
		t.Errorf("exhausted corner must emit zero zero-drop compaction events, got %d", compactions)
	}
}

// trigger math matches the fixtures' arithmetic, and records the last
// request so tests can assert on what the provider actually received
// (the loop settles the prepared request slice — state keeps raw history).
// settleTestProvider reports roughTokens-based estimates so the loop's
// settleTestProvider reports roughTokens-based estimates so the loop's
// trigger math matches the fixtures' arithmetic, and records the last
// request so tests can assert on what the provider actually received
// (the loop settles the prepared request slice — state keeps raw history).
type settleTestProvider struct {
	info        ProviderInfo
	resp        *ChatResponse
	lastReqMsgs []Message

	// Optional hooks for tests that need multi-request turns.
	flipToFinal func() // called after the first request completes
	record      func([]Message)
}

func (p *settleTestProvider) Chat(_ context.Context, req *ChatRequest) (*ChatResponse, error) {
	// Capture the scripted response BEFORE the hooks run — hooks advance
	// the script for the NEXT request; this request must return the head
	// it came in with.
	resp := p.resp
	p.lastReqMsgs = req.Messages
	if p.record != nil {
		p.record(req.Messages)
	}
	if p.flipToFinal != nil {
		flip := p.flipToFinal
		p.flipToFinal = nil
		flip()
	}
	return resp, nil
}
func (p *settleTestProvider) ChatStream(_ context.Context, req *ChatRequest, h StreamHandler) error {
	p.lastReqMsgs = req.Messages
	h.OnContent("done")
	h.OnDone(p.resp)
	return nil
}
func (p *settleTestProvider) Info() ProviderInfo { return p.info }
func (p *settleTestProvider) EstimateTokens(req *ChatRequest) int {
	return roughTokens(req.Messages)
}
