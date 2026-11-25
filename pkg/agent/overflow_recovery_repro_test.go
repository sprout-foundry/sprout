package agent

import (
	"testing"

	core "github.com/sprout-foundry/seed/core"
)

// This file reproduces finding #2 from the compaction/token-management review:
// a near-window prompt that only fits after content-truncating a single giant
// recent tool result is treated by seed's overflow-recovery guard as
// "bottomed out" and the turn is killed, even though a truncated request WOULD
// have fit.
//
// The recovery guard (seed core/conversation.go, tryContextOverflowRecovery)
// decides progress by MESSAGE COUNT alone:
//
//	if result.Strategy == "none" || len(result.Messages) >= before {
//		return false // "compaction has bottomed out"
//	}
//
// The drop+truncate pipeline (core.CompactWith) CAN reduce a prompt with no
// message drops — its emergency phase content-truncates tool results to 1500
// chars and old messages to head/tail stubs. So a request that fits only via
// content truncation passes the pipeline under target yet is discarded by the
// count-based guard. The tests below pin that gap.
//
// The mitigation for this is the trigger-clamp fix in context_budget.go: a
// conservative (underestimate-safe) proactive trigger fires compaction BEFORE
// the prompt overflows, so the turn no longer depends on the recovery path.

// TestRecoveryGuardDiscardsContentTruncation builds a near-window prompt that
// fits ONLY by content-truncating one giant recent tool result, runs seed's
// drop+truncate pipeline, and asserts the exact shape the recovery guard
// misreads: the pipeline reduces the prompt under target (TokensAfter <
// target) with NO message drops (len unchanged), so the guard
// `len(result.Messages) >= before` fires and recovery reports "bottomed out".
func TestRecoveryGuardDiscardsContentTruncation(t *testing.T) {
	const (
		window = 100_000
		target = 85_000 // 0.85 * window, the pipeline's derived target
	)

	// A short history (all inside the 24-message recent tail, so nothing is
	// droppable) with ONE giant recent tool result. Pre-compaction the prompt
	// is over target; after the pipeline content-truncates the tool result to
	// 1500 chars it is far under target, with the same message count.
	// The history has >5 messages so it clears CompactWith's min-messages
	// early exit and reaches the truncate phase.
	giant := make([]byte, 400_000) // 400K chars ~ 100K tokens of tool output
	for i := range giant {
		giant[i] = 'x'
	}
	messages := []core.Message{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "old question"},
		{Role: "assistant", Content: "old answer"},
		{Role: "user", Content: "do the thing"},
		{Role: "assistant", Content: "running it", ToolCalls: []core.ToolCall{
			{ID: "c1", Function: core.ToolCallFunction{Name: "shell_command", Arguments: `{}`}},
		}},
		{Role: "tool", ToolCallID: "c1", Content: string(giant)}, // the giant recent tool result
	}

	// Sanity: the pre-compaction prompt is over target, so compaction is needed.
	if roughMessagesTokens(messages) < target {
		t.Fatalf("fixture does not exceed the target; no compaction would fire (pre=%d)", roughMessagesTokens(messages))
	}

	r := core.CompactWith(core.CompactInputs{
		Messages:     messages,
		TokenLimit:   window,
		TargetTokens: target,
	})

	// The pipeline must have reduced the prompt under target…
	if r.TokensAfter >= target {
		t.Fatalf("pipeline did not reduce under target: TokensAfter=%d target=%d strategy=%s",
			r.TokensAfter, target, r.Strategy)
	}
	// …with NO message drops — the reduction is content truncation only.
	if len(r.Messages) != len(messages) {
		t.Fatalf("expected content-truncation-only pass (no drops), but %d -> %d messages (strategy=%s)",
			len(messages), len(r.Messages), r.Strategy)
	}

	// This is the exact shape the recovery guard misreads: it sees
	// `len(result.Messages) >= before` (no drop) and concludes "bottomed out",
	// discarding a request that now fits. Reproduce the guard's decision here.
	before := len(messages)
	guardSaysBottomedOut := r.Strategy == "none" || len(r.Messages) >= before
	if !guardSaysBottomedOut {
		t.Fatalf("expected the count-based recovery guard to misfire (bottomed out) for a pure content-truncation, but it did not")
	}
	t.Logf("pipeline fit the prompt via content truncation (TokensAfter=%d < target=%d, no drops), yet the recovery guard %q discards it — the turn dies even though a truncated request would fit",
		r.TokensAfter, target, "len(messages) >= before")
}

// roughMessagesTokens is a local copy of seed's unexported roughTokens so the
// fixture's pre-compaction size is asserted without reaching into seed's
// internals. It must stay in step with seed's 4-chars-per-token estimate.
func roughMessagesTokens(messages []core.Message) int {
	total := 0
	for _, m := range messages {
		total += len(m.Content) / 4
		if m.Role == "assistant" {
			total += len(m.ToolCalls) * 20
			for _, tc := range m.ToolCalls {
				total += len(tc.Function.Arguments) / 4
			}
		}
		total += 10 // per-message overhead
	}
	return total
}
