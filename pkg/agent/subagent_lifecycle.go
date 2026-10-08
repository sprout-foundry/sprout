// Package agent: SubagentRunner construction and metrics (SP-141 phase 4,
// increment 2). The prefix builder and the process-wide active counter
// moved to pkg/agent/subagents in increment 1 (forwarded via
// subagent_forwarders.go); the lifecycle-event emission moved in increment
// 2. These thin wrappers resolve the runner's shared EventBus and delegate.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
	agent_api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// NewSubagentRunner creates a new SubagentRunner
func NewSubagentRunner(parent *Agent, shared *SharedState) *SubagentRunner {
	return &SubagentRunner{
		parentAgent: parent,
		shared:      shared,
	}
}

// SetSubagentClientFactoryForTest installs a client factory the runner uses
// instead of factory.CreateProviderClient when building a subagent. It is the
// exported form of the unexported testClientFactory field so tests outside
// pkg/agent (e.g. pkg/benchmark) can drive a scripted subagent without a real
// provider. Never called in production.
func (r *SubagentRunner) SetSubagentClientFactoryForTest(factory func(clientType agent_api.ClientType, model string) (agent_api.ClientInterface, error)) {
	r.testClientFactory = factory
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

// sharedEventBus returns the runner's shared EventBus, or nil when there is
// no shared state (the defensive nil path the emitter must handle).
func (r *SubagentRunner) sharedEventBus() *events.EventBus {
	if r.shared == nil {
		return nil
	}
	return r.shared.EventBus
}

// publishLifecycleEvent emits a subagent_activity event with a status field
// describing the lifecycle transition (implementation in
// pkg/agent/subagents).
func (r *SubagentRunner) publishLifecycleEvent(parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64) {
	r.publishLifecycleEventWithCost(parentCallID, taskID, persona, status, reason, tokensUsed, elapsedMs, 0)
}

// publishLifecycleEventWithCost is the extended form used when the runner
// has the per-subagent cost in hand (implementation in
// pkg/agent/subagents).
func (r *SubagentRunner) publishLifecycleEventWithCost(parentCallID, taskID, persona, status, reason string, tokensUsed int, elapsedMs int64, cost float64) {
	subagents.PublishLifecycleEventWithCost(r.sharedEventBus(), parentCallID, taskID, persona, status, reason, tokensUsed, elapsedMs, cost)
}
