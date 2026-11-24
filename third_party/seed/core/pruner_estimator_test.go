package core

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// inflatedEstimateFn mirrors the default 4-chars-per-token estimate but
// inflated 4x: a message of N chars counts as ~N tokens instead of N/4.
func inflatedEstimateFn(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += len(m.Content) / charsPerToken * 4
	}
	return total
}

// defaultLikeEstimateFn replicates the pruner's internal estimateTokens math
// exactly, for proving nil EstimateFn falls back to the internal estimator.
func defaultLikeEstimateFn(msgs []Message) int {
	total := 0
	for _, m := range msgs {
		total += estimateTextTokens(m.Content)
		if m.ReasoningContent != "" {
			total += estimateTextTokens(m.ReasoningContent)
		}
	}
	return total
}

func messagesEquivalent(a, b []Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Role != b[i].Role || a[i].Content != b[i].Content {
			return false
		}
	}
	return true
}

// importanceMessages builds a 40-message conversation: system, 15 old
// (user/assistant) turns with large content, then 24 recent small messages.
// The recent window + first user/assistant are always kept; the old middle is
// the greedy candidate pool. Assistant messages in the old middle score
// exactly the keep-threshold (0.5) and are always dropped; the user messages
// (score 0.6) are budget candidates.
func importanceMessages() []Message {
	msgs := []Message{{Role: "system", Content: "system prompt"}}
	for i := 1; i < 16; i++ {
		role := "user"
		if i%2 == 0 {
			role = "assistant"
		}
		msgs = append(msgs, Message{Role: role, Content: strings.Repeat("L", 12000)})
	}
	for i := 16; i < 40; i++ {
		msgs = append(msgs, Message{Role: "user", Content: strings.Repeat("s", 500)})
	}
	return msgs
}

func TestPruneImportance_UsesEstimateFnBudget(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})
	msgs := importanceMessages()
	ctx := context.Background()
	const maxTokens = 100000

	pruned := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{EstimateFn: inflatedEstimateFn})

	// The budget must bind: under the 4x-inflated estimator, not all candidate
	// user messages fit, so the pruner drops more than the always-kept set.
	if len(pruned) >= len(msgs)-6 {
		t.Fatalf("expected inflated estimator to drop budget-exceeding candidates, got %d of %d", len(pruned), len(msgs))
	}

	// The kept set must respect the fn-based target.
	target := cp.getTargetTokens(len(msgs), maxTokens)
	if kept := inflatedEstimateFn(pruned); kept > target {
		t.Errorf("kept fn-tokens %d exceed fn-based target %d", kept, target)
	}

	// System, first user, first assistant, and the most recent message survive.
	if pruned[0].Role != "system" {
		t.Errorf("expected system message preserved, got role %q", pruned[0].Role)
	}
	if pruned[1].Role != "user" || pruned[2].Role != "assistant" {
		t.Errorf("expected first user/assistant preserved, got roles %q/%q", pruned[1].Role, pruned[2].Role)
	}
	if pruned[len(pruned)-1].Content != msgs[len(msgs)-1].Content {
		t.Error("expected most recent message preserved")
	}
}

func TestPruneImportance_NilEstimateFnMatchesInternalEstimator(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})
	msgs := importanceMessages()
	ctx := context.Background()
	const maxTokens = 100000

	nilResult := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{})
	defaultLikeResult := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{EstimateFn: defaultLikeEstimateFn})
	fnResult := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{EstimateFn: inflatedEstimateFn})

	// nil must be byte-for-byte identical to an EstimateFn that replicates the
	// internal estimator — proving the nil fallback is unchanged.
	if !messagesEquivalent(nilResult, defaultLikeResult) {
		t.Error("nil EstimateFn must match the internal estimator exactly")
	}
	// The default (non-inflated) estimator does not bind the budget, so nil
	// keeps strictly more than the inflated estimator.
	if len(nilResult) <= len(fnResult) {
		t.Errorf("expected nil estimator to keep more than the inflated one, got %d vs %d", len(nilResult), len(fnResult))
	}
}

// validToolGroups checks that every tool result has its assistant parent and
// every assistant tool call has its result — the atomicity guarantee of the
// tool-call-aware importance path.
func validToolGroups(msgs []Message) bool {
	callIDs := make(map[string]bool)
	toolIDs := make(map[string]bool)
	for _, m := range msgs {
		if m.Role == "assistant" {
			for _, tc := range m.ToolCalls {
				callIDs[tc.ID] = true
			}
		}
		if m.Role == "tool" {
			toolIDs[m.ToolCallID] = true
		}
	}
	for id := range toolIDs {
		if !callIDs[id] {
			return false
		}
	}
	for id := range callIDs {
		if !toolIDs[id] {
			return false
		}
	}
	return true
}

func TestPruneImportance_EstimateFnFixedOverheadNotMultiplied(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})
	msgs := importanceMessages()
	ctx := context.Background()
	const maxTokens = 100000
	const fixedOverhead = 10000

	// An estimator that charges a large fixed per-call overhead on top of the
	// rough tokens. Summing per-message calls would multiply that overhead by
	// the message count; the pruner must treat it as a single whole-list cost.
	fixedFn := func(msgs []Message) int {
		return roughTokens(msgs) + fixedOverhead
	}

	pruned := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{EstimateFn: fixedFn})

	target := cp.getTargetTokens(len(msgs), maxTokens)

	// The fixed overhead must be counted once, not once per kept message.
	if kept := fixedFn(pruned); kept > target+fixedOverhead {
		t.Errorf("kept fn-tokens %d exceed target %d + one fixed overhead %d", kept, target, fixedOverhead)
	}
	// The overhead is a single shared cost, so most messages survive: the old
	// per-message-sum behavior kept only 31 of 40 here (4 of the 7 budget
	// candidates), while distributed weights keep all 7 (34).
	if len(pruned) < 33 {
		t.Errorf("expected distributed weights to keep materially more than per-message sums, got %d of %d", len(pruned), len(msgs))
	}
}

func TestPruneImportanceToolCallAware_EstimateFnFixedOverheadNotMultiplied(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})

	// 10 large tool-call groups + a final assistant message.
	msgs := []Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: strings.Repeat("T", 12000)},
		)
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "done"})

	ctx := context.Background()
	const maxTokens = 50000
	const fixedOverhead = 10000
	fixedFn := func(msgs []Message) int {
		return roughTokens(msgs) + fixedOverhead
	}

	pruned := cp.Prune(ctx, msgs, 48000, maxTokens, PruneCallOptions{
		Provider:   "minimax", // strict tool-call/result pairing
		EstimateFn: fixedFn,
	})

	target := cp.getTargetTokens(len(msgs), maxTokens)

	if !validToolGroups(pruned) {
		t.Error("expected tool groups to stay atomic (no orphaned tool calls/results)")
	}
	// The fixed overhead is one shared cost, not one per message in a group:
	// per-message sums would bill 10000 per group and keep only the 6
	// always-kept groups (13 messages); distributed weights keep 9 groups (20).
	if kept := fixedFn(pruned); kept > target+fixedOverhead {
		t.Errorf("kept fn-tokens %d exceed target %d + one fixed overhead %d", kept, target, fixedOverhead)
	}
	if len(pruned) < 18 {
		t.Errorf("expected distributed weights to keep most groups, got %d of %d", len(pruned), len(msgs))
	}
}

func TestPruneImportanceToolCallAware_EstimateFnBudget(t *testing.T) {
	cp := NewConversationPruner(PrunerOptions{Strategy: PruneStrategyImportance})

	// 10 large tool-call groups + a final assistant message. Each group is
	// ~12000 fn-tokens, so the budget binds and whole groups are dropped.
	msgs := []Message{{Role: "system", Content: "sys"}}
	for i := 0; i < 10; i++ {
		id := fmt.Sprintf("call-%d", i)
		msgs = append(msgs,
			Message{Role: "assistant", ToolCalls: []ToolCall{
				{ID: id, Function: ToolCallFunction{Name: "read_file", Arguments: `{"path":"a"}`}},
			}},
			Message{Role: "tool", ToolCallID: id, Content: strings.Repeat("T", 12000)},
		)
	}
	msgs = append(msgs, Message{Role: "assistant", Content: "done"})

	ctx := context.Background()
	const maxTokens = 100000

	pruned := cp.Prune(ctx, msgs, 90000, maxTokens, PruneCallOptions{
		Provider:   "minimax", // strict tool-call/result pairing
		EstimateFn: inflatedEstimateFn,
	})

	if len(pruned) >= len(msgs) {
		t.Fatalf("expected budget to drop tool groups, got %d of %d", len(pruned), len(msgs))
	}
	if !validToolGroups(pruned) {
		t.Error("expected tool groups to stay atomic (no orphaned tool calls/results)")
	}
	target := cp.getTargetTokens(len(msgs), maxTokens)
	if kept := inflatedEstimateFn(pruned); kept > target {
		t.Errorf("kept fn-tokens %d exceed fn-based target %d", kept, target)
	}
}
