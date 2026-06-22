package agent

import (
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// buildSubagentPrefix returns the terminal prefix for a subagent based on persona and taskID.
// For single subagents (taskID starting with "subagent-"), returns "[{persona}]".
// For parallel subagents (other taskIDs), returns "[{persona}:{taskID}]".
func buildSubagentPrefix(persona, taskID string) string {
	if taskID != "" && !strings.HasPrefix(taskID, "subagent-") {
		return fmt.Sprintf("[%s:%s]", persona, taskID)
	}
	return fmt.Sprintf("[%s]", persona)
}

// activeSubagentCount is the process-wide count of currently-running
// subagents. The CLI status footer reads it via GetActiveSubagents() to
// render " · N sub" while delegation is in flight. SP-051-2d.
var activeSubagentCount atomic.Int64

// IncrementActiveSubagents bumps the active-subagent counter; paired with
// DecrementActiveSubagents under a defer in the spawner.
func IncrementActiveSubagents() { activeSubagentCount.Add(1) }

// DecrementActiveSubagents lowers the active-subagent counter when a
// subagent finishes (success, error, cancel — any terminal state).
func DecrementActiveSubagents() { activeSubagentCount.Add(-1) }

// GetActiveSubagents returns the current number of running subagents.
func GetActiveSubagents() int { return int(activeSubagentCount.Load()) }

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
func (r *SubagentRunner) publishLifecycleEvent(taskID, persona, status, reason string, tokensUsed int, elapsedMs int64) {
	r.publishLifecycleEventWithCost(taskID, persona, status, reason, tokensUsed, elapsedMs, 0)
}

// publishLifecycleEventWithCost is the extended form used when the runner
// has the per-subagent cost in hand (typically at "completed"/"cancelled").
// Kept as a separate entry point so the existing call sites that only have
// the lifecycle transition remain a one-liner; the original signature is
// preserved.
func (r *SubagentRunner) publishLifecycleEventWithCost(taskID, persona, status, reason string, tokensUsed int, elapsedMs int64, cost float64) {
	if r.shared == nil || r.shared.EventBus == nil {
		return
	}
	data := map[string]any{
		"task_id": taskID,
		"persona": persona,
		"status":  status, // "queued", "started", "completed", "cancelled"
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
