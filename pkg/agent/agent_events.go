package agent

import (
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/validation"
)

// publishEvent publishes an event to the event bus if available
func (a *Agent) publishEvent(eventType string, data interface{}) {
	if a.eventBus != nil {
		a.eventBus.Publish(eventType, a.decorateEventPayload(data))
	}
}

// decorateEventPayload merges event metadata into the payload if present.
func (a *Agent) decorateEventPayload(data interface{}) interface{} {
	if data == nil {
		return data
	}

	a.eventMetadataMu.RLock()
	defer a.eventMetadataMu.RUnlock()
	if len(a.eventMetadata) == 0 {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	cloned := make(map[string]interface{}, len(payload)+len(a.eventMetadata))
	for k, v := range payload {
		cloned[k] = v
	}
	for k, v := range a.eventMetadata {
		if _, exists := cloned[k]; !exists {
			cloned[k] = v
		}
	}
	return cloned
}

// PublishQueryProgress publishes query progress for real-time updates
func (a *Agent) PublishQueryProgress(message string, iteration int, tokensUsed int) {
	a.publishEvent(events.EventTypeQueryProgress, events.QueryProgressEvent(message, iteration, tokensUsed))
}

// PublishToolExecution publishes tool execution events for real-time updates
func (a *Agent) PublishToolExecution(toolName, action string, details map[string]interface{}) {
	a.publishEvent(events.EventTypeToolExecution, events.ToolExecutionEvent(toolName, action, details))
}

// PublishToolStart publishes a rich tool start event
func (a *Agent) PublishToolStart(toolName, toolCallID, arguments, displayName, persona string, isSubagent bool, subagentType string, toolIndex int) {
	a.publishEvent(events.EventTypeToolStart, events.ToolStartEvent(toolName, toolCallID, arguments, displayName, persona, isSubagent, subagentType, toolIndex))
}

// PublishToolEnd publishes a rich tool end event
func (a *Agent) PublishToolEnd(toolCallID, toolName, status, result, errorMessage string, duration time.Duration) {
	a.publishEvent(events.EventTypeToolEnd, events.ToolEndEvent(toolCallID, toolName, status, result, errorMessage, duration))
}

// PublishTodoUpdate publishes a structured todo update event
func (a *Agent) PublishTodoUpdate(todos []map[string]interface{}) {
	a.publishEvent(events.EventTypeTodoUpdate, events.TodoUpdateEvent(todos))
}

// PublishAgentMessage publishes a structured agent system message event.
// This is the single unified routing point for all agent output.
// Safe to call even when eventBus is nil (CLI-only mode) — the
// internal publishEvent method checks for nil before publishing.
func (a *Agent) PublishAgentMessage(category, message string, extra map[string]interface{}) {
	a.publishEvent(events.EventTypeAgentMessage, events.AgentMessageEvent(category, message, extra))
}

// SetEventBus sets the event bus for real-time UI updates and initializes the validator
func (a *Agent) SetEventBus(eventBus *events.EventBus) {
	a.eventBus = eventBus
	if a.outputRouter != nil {
		a.outputRouter.SetEventBus(eventBus)
	} else {
		a.outputRouter = NewOutputRouter(a, eventBus)
	}
	// Initialize validator for syntax checking and async diagnostics
	a.validator = validation.NewValidator(eventBus)
	a.eventMetadataMu.RLock()
	if len(a.eventMetadata) > 0 {
		a.validator.SetEventMetadata(a.eventMetadata)
	}
	a.eventMetadataMu.RUnlock()
	a.enablePreWriteValidation = true
}

// GetEventBus returns the current event bus
func (a *Agent) GetEventBus() *events.EventBus {
	return a.eventBus
}

// SetEventMetadata attaches metadata that should be merged into all emitted UI events.
func (a *Agent) SetEventMetadata(metadata map[string]interface{}) {
	a.eventMetadataMu.Lock()
	defer a.eventMetadataMu.Unlock()
	if len(metadata) == 0 {
		a.eventMetadata = nil
		if a.validator != nil {
			a.validator.SetEventMetadata(nil)
		}
		return
	}
	cloned := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		cloned[k] = v
	}
	a.eventMetadata = cloned
	if a.validator != nil {
		a.validator.SetEventMetadata(cloned)
	}
}

// GetEventClientID returns the bound client_id from event metadata, if present.
func (a *Agent) GetEventClientID() string {
	a.eventMetadataMu.RLock()
	defer a.eventMetadataMu.RUnlock()
	if len(a.eventMetadata) == 0 {
		return ""
	}
	if clientID, ok := a.eventMetadata["client_id"].(string); ok {
		return strings.TrimSpace(clientID)
	}
	return ""
}
