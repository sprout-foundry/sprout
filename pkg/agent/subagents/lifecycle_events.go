// Package subagents: subagent lifecycle-event emission (SP-141 phase 4,
// increment 2). PublishLifecycleEventWithCost emits the subagent_activity
// event (queued/started/completed/cancelled) on the shared EventBus and
// mirrors it to the runlog. It is pure over the EventBus + runlog — no
// *Agent state — so it lives here; pkg/agent keeps the thin
// *SubagentRunner method wrappers that resolve the shared EventBus.
package subagents

import (
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// PublishLifecycleEvent emits a subagent_activity event with a status field
// describing the lifecycle transition. The event is only published when
// the EventBus is available.
func PublishLifecycleEvent(bus *events.EventBus, parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64) {
	PublishLifecycleEventWithCost(bus, parentCallID, taskID, persona, status, reason, tokensUsed, elapsedMs, 0)
}

// PublishLifecycleEventWithCost is the extended form used when the runner
// has the per-subagent cost in hand (typically at "completed"/"cancelled").
// Kept as a separate entry point so the existing call sites that only have
// the lifecycle transition remain a one-liner; the original signature is
// preserved.
func PublishLifecycleEventWithCost(bus *events.EventBus, parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64, cost float64) {
	if bus == nil {
		return
	}
	data := map[string]any{
		"task_id": taskID,
		"persona": persona,
		"status":  status, // "queued", "started", "completed", "cancelled"
	}
	// Parent tool-call correlation: lets the WebUI attach this event to the
	// run_subagent tool call that spawned it (seed v1.4.0 provides the ID
	// in handler contexts).
	if parentCallID != "" {
		data["tool_call_id"] = parentCallID
	}
	if reason != "" {
		data["reason"] = reason
	}
	if tokensUsed > 0 {
		data["tokens_used"] = tokensUsed
	}
	if elapsedMs > 0 {
		data["elapsed_ms"] = elapsedMs
	}
	if cost > 0 {
		data["cost"] = cost
	}
	bus.Publish(events.EventTypeSubagentActivity, data)

	// Also write to the runlog for persistent structured logging.
	logger := utils.GetRunLogger()
	if logger != nil {
		logger.LogEvent("subagent_activity", data)
	}
}
