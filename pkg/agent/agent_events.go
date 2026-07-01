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
// PublishFileChange emits a file_changed event so the WebUI activity
// feed can reflect ChangeTracker-detected mutations (including
// shell-driven ones, not just direct write_file/edit_file calls).
// Content is the captured original (for deletes/edits) — pass empty
// for creates, where there's no prior content. Action: "created" /
// "modified" / "deleted" — matches events.FileChangedEvent vocabulary.
func (a *Agent) PublishFileChange(filePath, action, content string) {
	a.publishEvent(events.EventTypeFileChanged, events.FileChangedEvent(filePath, action, content))
}

func (a *Agent) PublishAgentMessage(category, message string, extra map[string]interface{}) {
	a.publishEvent(events.EventTypeAgentMessage, events.AgentMessageEvent(category, message, extra))
}

// PublishCompactStarted emits a compact_started event with diagnostic
// fields describing the conversation state at the moment compaction
// begins. source is the path: "manual" (slash command) or
// "auto_llm_summary" (seed structural compaction).
func (a *Agent) PublishCompactStarted(source string, messageCount, checkpointCount int) {
	a.publishEvent(events.EventTypeCompactStarted, events.CompactStartedEvent(source, messageCount, checkpointCount))
}

// PublishCompactCompleted emits a compact_completed event with the
// result of the compaction. Pass nil err on success.
func (a *Agent) PublishCompactCompleted(source string, beforeCount, afterCount, summaryChars int, err error) {
	a.publishEvent(events.EventTypeCompactCompleted, events.CompactCompletedEvent(source, beforeCount, afterCount, summaryChars, err))
}

// PublishContextManagementDiagnostic (SP-066 Phase 1) emits the per-iteration
// context-budget snapshot so the WebUI metrics panel can render the effective
// trigger threshold and verify substitution is doing the heavy lifting.
// cachedTokens/promptTokens/cacheWriteTokens expose provider cache
// effectiveness in the diagnostic payload.
func (a *Agent) PublishContextManagementDiagnostic(currentTokens, maxTokens, iteration, messageCount, cachedTokens, promptTokens, cacheWriteTokens int) {
	a.publishEvent(
		events.EventTypeContextManagementDiagnostic,
		events.ContextManagementDiagnosticEvent(
			currentTokens,
			maxTokens,
			a.computeCompactionTriggerFraction(),
			reservedForResponseFraction,
			reservedForThinkingFraction,
			reservedForToolIOFraction,
			iteration,
			messageCount,
			cachedTokens,
			promptTokens,
			cacheWriteTokens,
		),
	)
}

// PublishRecallDiagnostic (SP-066 Phase 3) emits a single semantic-recall
// pass diagnostic. Called from InjectSemanticRecall after every recall
// query (including no-op queries) so subscribers can see the full
// distribution of recall behavior, not just hits.
func (a *Agent) PublishRecallDiagnostic(diag recallRetrievalDiagnostic) {
	a.publishEvent(
		events.EventTypeRecallDiagnostic,
		events.RecallDiagnosticEvent(
			diag.EmbedDurationMS,
			diag.CandidatesConsidered,
			diag.Injected,
			diag.InjectedChars,
			diag.TopScores,
		),
	)
}

// PublishRateLimited emits a rate_limited event so the WebUI can show
// "rate-limited, retrying…" and gate the input until the backoff elapses.
func (a *Agent) PublishRateLimited(ev *events.RateLimitedEvent) {
	if ev != nil {
		a.publishEvent(events.EventTypeRateLimited, ev)
	}
}

// PublishBudgetUpdate publishes a budget update event for automate sessions.
// This goes through decorateEventPayload to include client_id/chat_id metadata.
func (a *Agent) PublishBudgetUpdate(eventType string, data interface{}) {
	a.publishEvent(eventType, data)
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
