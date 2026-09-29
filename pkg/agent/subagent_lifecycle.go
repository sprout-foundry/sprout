// Package agent: SubagentRunner construction, metrics, and lifecycle-event
// publishing (SP-141 phase 4, increment 1). The prefix builder and the
// process-wide active counter moved to pkg/agent/subagents (forwarded via
// subagent_forwarders.go).
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// NewSubagentRunner creates a new SubagentRunner
func NewSubagentRunner(parent *Agent, shared *SharedState) *SubagentRunner {
	return &SubagentRunner{
		parentAgent: parent,
		shared:      shared,
	}
}

// Metrics returns a snapshot of the subagent runner's operational metrics.
func (r *SubagentRunner) Metrics() SubagentMetrics {
	return SubagentMetrics{
		Active:            r.metricActive.Load(),
		Queued:            r.metricQueued.Load(),
		Completed:         r.metricCompleted.Load(),
		Failed:            r.metricFailed.Load(),
		Cancelled:         r.metricCancelled.Load(),
		TotalQueuedWaitMS: r.metricQueuedWaitMS.Load(),
	}
}

// publishLifecycleEvent emits a subagent_activity event with a status field
// describing the lifecycle transition. The event is only published when
// the shared EventBus is available.
func (r *SubagentRunner) publishLifecycleEvent(parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64) {
	r.publishLifecycleEventWithCost(parentCallID, taskID, persona, status, reason, tokensUsed, elapsedMs, 0)
}

// publishLifecycleEventWithCost is the extended form used when the runner
// has the per-subagent cost in hand (typically at "completed"/"cancelled").
// Kept as a separate entry point so the existing call sites that only have
// the lifecycle transition remain a one-liner; the original signature is
// preserved.
func (r *SubagentRunner) publishLifecycleEventWithCost(parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64, cost float64) {
	if r.shared == nil || r.shared.EventBus == nil {
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
	r.shared.EventBus.Publish(events.EventTypeSubagentActivity, data)

	// Also write to the runlog for persistent structured logging.
	logger := utils.GetRunLogger()
	if logger != nil {
		logger.LogEvent("subagent_activity", data)
	}
}
