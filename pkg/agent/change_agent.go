package agent

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// ChangeAgent is the narrow surface ChangeTracker needs from its owning
// agent (SP-141 phase 2, step A). *Agent satisfies it structurally, so
// existing call sites and white-box tests pass *Agent unchanged. The
// physical move of the cluster into pkg/agent/changes (step B) moves
// this interface along with it.
type ChangeAgent interface {
	GetSessionID() string
	GetModel() string
	GetWorkspaceRoot() string
	GenerateResponse([]api.Message) (string, error)
	PublishFileChange(filePath, action, content string)
	PublishRawEvent(eventType string, data interface{})
	Logger() *AgentLogger
}

// PublishRawEvent publishes eventType/data on the event bus WITHOUT the
// event-metadata decoration that publishEvent adds. The change-tracking
// shell-bulk rollup uses it so the seam preserves the pre-seam publish
// semantics exactly. Nil-safe.
func (a *Agent) PublishRawEvent(eventType string, data interface{}) {
	if a == nil || a.eventBus == nil {
		return
	}
	a.eventBus.Publish(eventType, data)
}
