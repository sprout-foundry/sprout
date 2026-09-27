package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/changes"
)

// PublishRawEvent publishes eventType/data on the event bus WITHOUT the
// event-metadata decoration that publishEvent adds. The change-tracking
// shell-bulk rollup (pkg/agent/changes) uses it so the seam preserves the
// pre-seam publish semantics exactly. Nil-safe.
func (a *Agent) PublishRawEvent(eventType string, data interface{}) {
	if a == nil || a.eventBus == nil {
		return
	}
	a.eventBus.Publish(eventType, data)
}

// DebugLogger exposes the agent's logger as the narrow changes.DebugLogger
// interface so *Agent structurally satisfies changes.ChangeAgent without
// pkg/agent/changes importing pkg/agent (a concrete *AgentLogger return
// type in the interface would create an import cycle). Nil-safe.
func (a *Agent) DebugLogger() changes.DebugLogger {
	if a == nil {
		return nil
	}
	return a.Logger()
}
