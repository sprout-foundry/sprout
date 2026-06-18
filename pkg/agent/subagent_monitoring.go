package agent

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// monitorBudget watches token usage and cancels if budget exceeded
func (r *SubagentRunner) monitorBudget(ctx context.Context, agent *Agent, maxTokens int, budgetExceeded *atomic.Bool) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			tokens := agent.state.GetTotalTokens()
			if tokens >= maxTokens {
				budgetExceeded.Store(true)
				agent.interruptCancel()
				return
			}
		}
	}
}

// progressTickInterval is the period between subagent progress events.
// Picked to balance visible liveness (the CLI footer can show a moving
// "12.3k/128k ctx" pill within 2s of any new tokens) against event-bus
// pressure when parallel fleets are spawning a dozen subagents at once.
const progressTickInterval = 2 * time.Second

// monitorProgress emits periodic subagent_activity events with the
// running subagent's context-tokens / max-context / cost / iteration
// snapshot. The event lets the parent CLI render a live "↳ persona ·
// 12.3k/128k ctx · $0.03" badge that updates as the subagent burns
// context — previously the parent only learned the final numbers in the
// "completed" event after the subagent had already exited.
//
// Cardinality: one event per progressTickInterval per active subagent.
// A 10-subagent fleet emits ~5 events/sec total at the default tick;
// well below the bus's slow-subscriber threshold.
//
// The function suppresses the very first tick when no tokens have
// accumulated yet, so the first event the subscriber sees is already
// informative rather than "0/0".
func (r *SubagentRunner) monitorProgress(ctx context.Context, agent *Agent, taskID, persona string) {
	if r == nil || r.shared == nil || r.shared.EventBus == nil || agent == nil {
		return
	}

	// Emit one event immediately so the CLI has the subagent's max
	// context budget on hand for the spawn line. Without this, the
	// first ~progressTickInterval seconds of the run render with no
	// "/128k ctx" suffix even though the value was known at creation.
	publishOnce := func() {
		ctxTokens, ctxLimit := agent.GetContextTokens()
		totalTokens := agent.state.GetTotalTokens()
		cost := agent.state.GetTotalCost()
		iteration := agent.state.GetCurrentIteration()
		// Always publish even when totals are zero — the consumer uses
		// max_context_tokens alone to render the spawn-line budget.
		// Without an early emit the CLI has no way to know the limit
		// until the first periodic tick (~2s) later.
		data := map[string]any{
			"task_id":            taskID,
			"persona":            persona,
			"status":             "progress",
			"tokens_used":        totalTokens,
			"context_used":       ctxTokens,
			"max_context_tokens": ctxLimit,
			"iteration":          iteration,
		}
		if cost > 0 {
			data["cost"] = cost
		}
		r.shared.EventBus.Publish(events.EventTypeSubagentActivity, data)
	}
	publishOnce()

	ticker := time.NewTicker(progressTickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			publishOnce()
		}
	}
}
