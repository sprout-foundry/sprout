// Package events provides event system for sprout UI architecture
package events

import (
	"fmt"
	"log"
	"sync"
	"time"
)

// UIEvent represents an event that can be forwarded between CLI and Web UI.
//
// @ts-generated  webui/src/types/generated.ts::UIEvent
// SP-034-5b: the EventType* constants below are mirrored as the
// ServerEventType string-literal union in generated.ts. The outbound
// registry in pkg/webui/websocket_outbound_registry.go covers the
// same surface (a test asserts they stay in sync).
type UIEvent struct {
	ID        string    `json:"id"`
	Type      string    `json:"type"`
	Timestamp time.Time `json:"timestamp"`
	Data      any       `json:"data"`
}

// Common event types
const (
	EventTypeQueryStarted            = "query_started"
	EventTypeQueryProgress           = "query_progress"
	EventTypeQueryCompleted          = "query_completed"
	EventTypeError                   = "error"
	EventTypeToolExecution           = "tool_execution"
	EventTypeToolStart               = "tool_start"
	EventTypeToolEnd                 = "tool_end"
	EventTypeSubagentActivity        = "subagent_activity"
	EventTypeTodoUpdate              = "todo_update"
	EventTypeFileChanged            = "file_changed"
	EventTypeWorkspacePatch         = "workspace_patch"
	EventTypeFileContentChanged     = "file_content_changed"
	EventTypeStreamChunk             = "stream_chunk"
	EventTypeMetricsUpdate           = "metrics_update"
	EventTypeValidation              = "validation"
	EventTypeSecurityApprovalRequest = "security_approval_request"
	EventTypeSecurityPromptRequest  = "security_prompt_request"
	EventTypeAskUserRequest        = "ask_user_request"
	EventTypeAgentMessage            = "agent_message"
	// EventTypeProviderNoCredential is published when a provider change
	// would activate a provider that requires an API key but doesn't
	// have one configured. The frontend surfaces it as a sticky toast
	// pointing at Settings → Credentials, distinct from generic warning
	// messages that get inlined into the active assistant bubble.
	EventTypeProviderNoCredential    = "provider_no_credential"
	EventTypeWorkspaceChanged        = "workspace_changed"
	EventTypeSessionTerminated       = "session_terminated"
	EventTypeDriftDetected           = "drift_detected"
	// EventTypeSessionChanged signals that a chat session's metadata
	// (name, pin state, active state) changed and tabs viewing that chat
	// should reconcile. SP-034-3e.
	EventTypeSessionChanged          = "session_changed"
)

// EventBus manages event distribution between CLI and Web UI
type EventBus struct {
	subscribers map[string]chan UIEvent
	mutex       sync.RWMutex
	nextID      int64

	// drainMu serializes critical event delivery so that concurrent
	// critical events don't race on the drain-then-send sequence.
	drainMu sync.Mutex
}

// NewEventBus creates a new event bus
func NewEventBus() *EventBus {
	return &EventBus{
		subscribers: make(map[string]chan UIEvent),
	}
}

// Subscribe adds a new subscriber to the event bus
func (eb *EventBus) Subscribe(name string) <-chan UIEvent {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	ch := make(chan UIEvent, 100) // Buffered channel
	eb.subscribers[name] = ch
	return ch
}

// Unsubscribe removes a subscriber from the event bus
func (eb *EventBus) Unsubscribe(name string) {
	eb.mutex.Lock()
	defer eb.mutex.Unlock()

	if ch, exists := eb.subscribers[name]; exists {
		delete(eb.subscribers, name)
		close(ch)
	}
}

// Publish broadcasts an event to all subscribers.
// Critical events (security approvals, prompts) are never silently dropped
// — if the channel is full, they replace the oldest event to make room.
func (eb *EventBus) Publish(eventType string, data any) {
	eb.mutex.Lock()
	eb.nextID++
	event := UIEvent{
		ID:        generateEventID(eb.nextID),
		Type:      eventType,
		Timestamp: time.Now(),
		Data:      data,
	}
	subscribers := make([]chan UIEvent, 0, len(eb.subscribers))
	for _, ch := range eb.subscribers {
		subscribers = append(subscribers, ch)
	}
	eb.mutex.Unlock()

	isCritical := eventType == EventTypeSecurityApprovalRequest ||
		eventType == EventTypeSecurityPromptRequest ||
		eventType == EventTypeAskUserRequest

	// Publish to all subscribers without holding the lock
	for _, ch := range subscribers {
		if isCritical {
			// For critical events, serialize the drain-then-send to
			// prevent concurrent critical events from racing on the
			// same subscriber channel (which could lose events).
			eb.drainMu.Lock()
			// Drain one stale event to make room so the security dialog
			// is always delivered to the client.
			select {
			case ch <- event:
				eb.drainMu.Unlock()
			default:
				select {
				case <-ch:
					// Bounded send: a concurrent non-critical publisher
					// can refill the slot between drain and send. Without
					// the timeout, this would block indefinitely on a slow
					// subscriber while holding drainMu — visible as a long
					// pause when a security prompt is pending.
					select {
					case ch <- event:
					case <-time.After(1 * time.Second):
						log.Printf("[EventBus] Dropped critical %s event: subscriber unresponsive for 1s after drain", eventType)
					}
					eb.drainMu.Unlock()
				default:
					// Channel is empty but concurrently closed; give up.
					eb.drainMu.Unlock()
				}
			}
		} else {
			select {
			case ch <- event:
			default:
				// Channel is full — subscriber is slow or disconnected.
				// Log the drop so operators can diagnose missing events.
				log.Printf("[EventBus] Dropped %s event for slow subscriber (channel full, cap=100)", eventType)
			}
		}
	}
}

// generateEventID creates a unique event ID
func generateEventID(id int64) string {
	return fmt.Sprintf("%s-%d", time.Now().Format("20060102-150405"), id)
}

// Helper functions for creating specific event types

// QueryStartedEvent creates a query started event
func QueryStartedEvent(query, provider, model string) map[string]interface{} {
	return map[string]interface{}{
		"query":    query,
		"provider": provider,
		"model":    model,
	}
}

// QueryProgressEvent creates a query progress event
func QueryProgressEvent(message string, iteration int, tokensUsed int) map[string]interface{} {
	return map[string]interface{}{
		"message":     message,
		"iteration":   iteration,
		"tokens_used": tokensUsed,
	}
}

// QueryCompletedEvent creates a query completed event
func QueryCompletedEvent(query, response string, tokensUsed int, cost float64, duration time.Duration) map[string]interface{} {
	return map[string]interface{}{
		"query":       query,
		"response":    response,
		"tokens_used": tokensUsed,
		"cost":        cost,
		"duration_ms": duration.Milliseconds(),
	}
}

// ErrorEvent creates an error event
func ErrorEvent(message string, err error) map[string]interface{} {
	data := map[string]interface{}{
		"message": message,
	}
	if err != nil {
		data["error"] = err.Error()
	}
	return data
}

// ToolExecutionEvent creates a tool execution event
func ToolExecutionEvent(toolName, action string, details map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name": toolName,
		"action":    action,
	}
	for k, v := range details {
		data[k] = v
	}
	return data
}

// FileChangedEvent creates a file changed event
func FileChangedEvent(filePath, action string, content string) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"action":    action, // "created", "modified", "deleted"
		"content":   content,
	}
}

// FileContentChangedEvent creates an event indicating a file's content on disk
// has changed while it was open in the editor
func FileContentChangedEvent(filePath string, modTime int64, size int64) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"mod_time":  modTime,
		"size":      size,
	}
}

// PatchConflictInfo holds optional conflict metadata for a workspace_patch event.
type PatchConflictInfo struct {
	Conflict   bool
	TheirsPath string
}

// WorkspacePatchEvent creates a workspace_patch event payload for real-time
// file content synchronization from the agent to the browser.
// The optional conflictInfo parameter enriches the event with conflict
// metadata when the container patch conflicts with unsynced browser edits.
func WorkspacePatchEvent(filePath, content, action string, seqNum int64, conflictInfo ...PatchConflictInfo) map[string]interface{} {
	payload := map[string]interface{}{
		"file_path": filePath,
		"content":   content,
		"action":    action, // "write", "edit"
		"seq":       seqNum,
	}
	if len(conflictInfo) > 0 && conflictInfo[0].Conflict {
		payload["conflict"] = true
		payload["theirs_path"] = conflictInfo[0].TheirsPath
	}
	return payload
}

// StreamChunkEvent creates a stream chunk event with content type
func StreamChunkEvent(chunk string, contentType string) map[string]interface{} {
	return map[string]interface{}{
		"chunk":        chunk,
		"content_type": contentType,
	}
}

// MetricsUpdateEvent creates a metrics update event
func MetricsUpdateEvent(totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64) map[string]interface{} {
	return map[string]interface{}{
		"total_tokens":       totalTokens,
		"context_tokens":     contextTokens,
		"max_context_tokens": maxContextTokens,
		"iteration":          iteration,
		"total_cost":         totalCost,
	}
}

// ValidationEvent creates a validation event
func ValidationEvent(filePath string, diagnostics []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"file_path":   filePath,
		"diagnostics": diagnostics,
		"timestamp":   time.Now().Format(time.RFC3339),
	}
}

// ToolStartEvent creates a tool start event with rich metadata
func ToolStartEvent(toolName, toolCallID, arguments, displayName, persona string, isSubagent bool, subagentType string, toolIndex int) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name":    toolName,
		"tool_call_id": toolCallID,
		"arguments":    arguments,
		"display_name": displayName,
	}
	if persona != "" {
		data["persona"] = persona
	}
	if isSubagent {
		data["is_subagent"] = true
		if subagentType != "" {
			data["subagent_type"] = subagentType
		}
	}
	data["tool_index"] = toolIndex
	return data
}

// ToolEndEvent creates a tool end event with result and status
func ToolEndEvent(toolCallID, toolName, status, result, errorMessage string, duration time.Duration) map[string]interface{} {
	data := map[string]interface{}{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		"status":       status, // "completed" or "failed"
		"duration_ms":  duration.Milliseconds(),
	}
	if result != "" {
		// Truncate results to 2000 chars for the WebUI - full result stays in the conversation
		if len(result) > 2000 {
			data["result"] = result[:2000] + "\n... (truncated)"
			data["result_truncated"] = true
			data["result_length"] = len(result)
		} else {
			data["result"] = result
			data["result_truncated"] = false
			data["result_length"] = len(result)
		}
	}
	if errorMessage != "" {
		data["error"] = errorMessage
	}
	return data
}

// SecurityApprovalRequestEvent creates a security approval request event for the webui
func SecurityApprovalRequestEvent(requestID, toolName, riskLevel, reasoning string, extras map[string]string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"tool_name":  toolName,
		"risk_level": riskLevel,
		"reasoning":  reasoning,
	}
	for k, v := range extras {
		payload[k] = v
	}
	return payload
}

// TodoUpdateEvent creates a todo update event
func TodoUpdateEvent(todos []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"todos": todos,
	}
}

// ProviderNoCredentialEvent creates an event signalling that the newly
// active provider requires an API key but doesn't have one configured.
// The frontend uses providerID to drive a toast that opens Settings →
// Credentials scoped to this provider.
func ProviderNoCredentialEvent(providerID, message string) map[string]interface{} {
	return map[string]interface{}{
		"provider": providerID,
		"message":  message,
	}
}

// AgentMessageEvent creates an agent system message event.
// category: "info", "warning", "error", "tool_log", "thought"
func AgentMessageEvent(category, message string, extra map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"category": category,
		"message":  message,
	}
	for k, v := range extra {
		data[k] = v
	}
	return data
}

// SubagentActivityEvent creates a structured subagent activity event.
// phase is typically "spawn", "output", or "complete".
func SubagentActivityEvent(toolCallID, toolName, phase, message string, details map[string]interface{}) map[string]interface{} {
	data := map[string]interface{}{
		"tool_call_id": toolCallID,
		"tool_name":    toolName,
		"phase":        phase,
		"message":      message,
	}
	for k, v := range details {
		data[k] = v
	}
	return data
}

// WorkspaceChangedEvent creates a workspace changed event
func WorkspaceChangedEvent(daemonRoot, workspaceRoot, previousWorkspaceRoot string) map[string]interface{} {
	return map[string]interface{}{
		"daemon_root":             daemonRoot,
		"workspace_root":          workspaceRoot,
		"previous_workspace_root": previousWorkspaceRoot,
	}
}

// SecurityPromptRequestEvent creates a security prompt request event for the webui
func SecurityPromptRequestEvent(requestID, prompt string, defaultResponse bool, extras map[string]string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id":      requestID,
		"prompt":          prompt,
		"default_response": defaultResponse,
	}
	for k, v := range extras {
		payload[k] = v
	}
	return payload
}

// SecurityPromptResponseEvent creates a security prompt response event
func SecurityPromptResponseEvent(requestID, response bool) map[string]interface{} {
	return map[string]interface{}{
		"request_id": requestID,
		"response":   response,
	}
}

// AskUserRequestEvent creates an ask_user request event for the webui
func AskUserRequestEvent(requestID, question, clientID string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"question":   question,
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}
	return payload
}

// DriftDetectedEvent creates a drift notification event for the WebUI
func DriftDetectedEvent(similarity float64, threshold float64, sessionID string) map[string]interface{} {
	return map[string]interface{}{
		"similarity":  similarity,
		"threshold":   threshold,
		"sessionId":   sessionID,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
		"options":     []string{"continue", "new_chat"},
	}
}
