package agent

import (
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/validation"
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

	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadata()
	if len(eventMetadata) == 0 {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	cloned := make(map[string]interface{}, len(payload)+len(eventMetadata))
	for k, v := range payload {
		cloned[k] = v
	}
	for k, v := range eventMetadata {
		if _, exists := cloned[k]; !exists {
			// Resolve function values to their return value
			if fn, ok := v.(func() string); ok {
				resolved := fn()
				cloned[k] = resolved
			} else {
				cloned[k] = v
			}
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
	if a.OutputRouter() != nil {
		a.OutputRouter().SetEventBus(eventBus)
	} else {
		a.output.SetOutputRouter(NewOutputRouter(a, eventBus))
	}
	// Initialize validator for syntax checking and async diagnostics
	a.validator = validation.NewValidator(eventBus)
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	em := a.output.GetEventMetadata()
	if len(em) > 0 {
		a.validator.SetEventMetadata(em)
	}
	mu.RUnlock()
}

// GetEventBus returns the current event bus
func (a *Agent) GetEventBus() *events.EventBus {
	return a.eventBus
}

// SetEventMetadata attaches metadata that should be merged into all emitted UI events.
func (a *Agent) SetEventMetadata(metadata map[string]interface{}) {
	mu := a.output.GetEventMetadataMutex()
	mu.Lock()
	defer mu.Unlock()
	if len(metadata) == 0 {
		a.output.SetEventMetadataUnlocked(nil)
		if a.validator != nil {
			a.validator.SetEventMetadata(nil)
		}
		return
	}
	cloned := make(map[string]interface{}, len(metadata))
	for k, v := range metadata {
		cloned[k] = v
	}
	a.output.SetEventMetadataUnlocked(cloned)
	if a.validator != nil {
		a.validator.SetEventMetadata(cloned)
	}
}

// MergeEventMetadata adds extras to the current event metadata without
// discarding existing keys. Unlike SetEventMetadata, which replaces the
// map wholesale, this is the right call when a subagent needs to layer
// per-spawn fields (e.g. subagent_depth, active_persona) on top of
// already-set chat/client routing keys inherited from its parent.
func (a *Agent) MergeEventMetadata(extras map[string]interface{}) {
	if len(extras) == 0 {
		return
	}
	existing := a.output.GetEventMetadata()
	merged := make(map[string]interface{}, len(existing)+len(extras))
	for k, v := range existing {
		merged[k] = v
	}
	for k, v := range extras {
		merged[k] = v
	}
	a.SetEventMetadata(merged)
}

// GetEventClientID returns the bound client_id from event metadata, if present.
func (a *Agent) GetEventClientID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadata()
	if len(eventMetadata) == 0 {
		return ""
	}
	if clientID, ok := eventMetadata["client_id"].(string); ok {
		return strings.TrimSpace(clientID)
	}
	return ""
}

// GetEventChatID returns the bound chat_id from event metadata, if present.
func (a *Agent) GetEventChatID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadata()
	if len(eventMetadata) == 0 {
		return ""
	}
	if chatID, ok := eventMetadata["chat_id"].(string); ok {
		return strings.TrimSpace(chatID)
	}
	return ""
}

// GetEventUserID returns the bound user_id from event metadata, if present.
func (a *Agent) GetEventUserID() string {
	mu := a.output.GetEventMetadataMutex()
	mu.RLock()
	defer mu.RUnlock()
	eventMetadata := a.output.GetEventMetadata()
	if len(eventMetadata) == 0 {
		return ""
	}
	if userID, ok := eventMetadata["user_id"].(string); ok {
		return strings.TrimSpace(userID)
	}
	return ""
}
