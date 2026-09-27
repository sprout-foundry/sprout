// SP-141 phase 2 wiring: the seam between pkg/agent and the extracted
// pkg/agent/changes package. The tracker's logic moved to changes; the
// Agent-facing construction half stays here (the unexported changeTracker
// field, the unexported config-stamping path, and Agent identity make it
// agent-internal), mirroring phase 1's workflow.LoopAgent shape:
// pkg/agent imports changes, never the reverse.
package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ---------------------------------------------------------------------------
// Type aliases — the exported tracker surface stays in pkg/agent
// ---------------------------------------------------------------------------

// ChangeTracker manages change tracking for the agent workflow. SP-141
// phase 2 moved the implementation to pkg/agent/changes; this alias
// keeps every pkg/agent (and external) reference to agent.ChangeTracker
// valid with no behavior change.
type ChangeTracker = changes.ChangeTracker

// TrackedFileChange represents a file change made during agent execution.
type TrackedFileChange = changes.TrackedFileChange

// TrackedBulkItem is the per-file payload packed inside a bulk TrackedFileChange.
type TrackedBulkItem = changes.TrackedBulkItem

// RedactedContentMarker aliases history.RedactedContentMarker (kept
// stable across the phase-2 move; the canonical definition lives in
// pkg/agent/changes next to its primary consumer).
const RedactedContentMarker = changes.RedactedContentMarker

// NewChangeTracker creates a new change tracker for an agent session.
// Thin forwarder to changes.NewChangeTracker through the AgentView seam.
func NewChangeTracker(view changes.AgentView, instructions string) *ChangeTracker {
	return changes.NewChangeTracker(view, instructions)
}

// ---------------------------------------------------------------------------
// changesAgentView — the *Agent → changes.AgentView adapter
// ---------------------------------------------------------------------------

// changesAgentView adapts *Agent to changes.AgentView. Accessor names
// match the seam one-for-one except where the tracker needs a facet
// (logger, raw event publish) the Agent expresses differently.
type changesAgentView struct{ a *Agent }

func (v changesAgentView) GetSessionID() string     { return v.a.GetSessionID() }
func (v changesAgentView) GetModel() string         { return v.a.GetModel() }
func (v changesAgentView) GetWorkspaceRoot() string { return v.a.GetWorkspaceRoot() }
func (v changesAgentView) GenerateResponse(messages []api.Message) (string, error) {
	return v.a.GenerateResponse(messages)
}

func (v changesAgentView) DebugLogger() changes.DebugLogger {
	if v.a == nil {
		return nil
	}
	if l := v.a.Logger(); l != nil {
		return l
	}
	return nil
}

// PublishFileChange mirrors Agent.PublishFileChange including its
// nil-safe publishEvent behavior (drops the event when no bus/output
// is wired, and decorates the payload with event metadata).
func (v changesAgentView) PublishFileChange(filePath, action, content string) {
	if v.a == nil {
		return
	}
	v.a.PublishFileChange(filePath, action, content)
}

// PublishRawFileChanged publishes a file_changed event on the bus
// WITHOUT event-metadata decoration — the behavior the old
// ChangeTracker code had via direct eventBus.Publish for bulk rollups
// (their payload "path" is a directory label or command name, not a
// real file, so chat metadata decoration never applied).
func (v changesAgentView) PublishRawFileChanged(eventType string, payload interface{}) {
	if v.a == nil || v.a.eventBus == nil {
		return
	}
	v.a.eventBus.Publish(eventType, payload)
}

// changesView returns the seam view of an Agent.
func (a *Agent) changesView() changes.AgentView { return changesAgentView{a} }
