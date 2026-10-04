// Event factory functions for the agent query / tool / approval /
// user-interaction events: the Query / Progress / Completed / Error /
// File / Workspace / Stream / Metrics / Validation factories, the tool
// start / end + args-truncation helpers, the approval / password /
// security-prompt events, and the todo / provider / agent-message /
// subagent / ask-user / input-required events. The diagnostics / compact /
// automate / monitoring factories live in events_filter_monitoring.go.

package events

import (
	"time"
)

// Helper functions for creating specific event types
// QueryStartedEvent creates a query started event
func QueryStartedEvent(query, provider, model string) map[string]interface{} {
	return QueryStartedEventWithDisplay(query, "", "", provider, model)
}

// QueryStartedEventWithDisplay creates a query started event with a separate
// user-facing bubble text. display is what the WebUI renders in the chat;
// query is the raw prompt sent to the model. Empty display falls back to
// query. source names the caller (agent package QuerySource* constants);
// "auto-resume" turns render as wakeup bubbles rather than user messages.
func QueryStartedEventWithDisplay(query, display, source, provider, model string) map[string]interface{} {
	data := map[string]interface{}{
		"query":    query,
		"provider": provider,
		"model":    model,
	}
	if display != "" && display != query {
		data["display"] = display
	}
	if source != "" {
		data["source"] = source
	}
	return data
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

// FileChangedEvent creates a file changed event.
//
// The full file content is deliberately NOT transmitted. No consumer reads it —
// the WebUI's handler only uses file_path/action, and the editor refetches a
// file's bytes on demand (and gets disk-change notifications via the lean
// FileContentChangedEvent). Shipping whole-file content here made each event
// large, so a burst (bulk shell edits, many writes) filled the per-subscriber
// channel and the replay ring buffer fast — dropping file_changed events and
// spamming "[EventBus] Dropped file_changed event" logs. The `content` arg is
// retained for call-site compatibility but only its length is surfaced.
func FileChangedEvent(filePath, action string, content string) map[string]interface{} {
	return map[string]interface{}{
		"file_path": filePath,
		"action":    action, // "created", "modified", "deleted", "write", "edit", "git_*", …
		"size":      len(content),
		// Server-side timestamp (RFC3339Nano). Consumers that need to order
		// or floor against tracker-recorded timestamps must use server time
		// — browser clocks are not comparable across deployments (cloud
		// mode especially). Emitted at publish time, i.e. microseconds AFTER
		// the tracker append, so revert-since consumers should subtract a
		// safety margin (the tracker's .Before() comparison would otherwise
		// skip the change that produced this event).
		"ts": time.Now().UTC().Format(time.RFC3339Nano),
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

// WorkspacePatchEvent creates a workspace_patch event payload for real-time
// WorkspacePatchEvent creates a workspace_patch event payload for real-time
// file change notification from the agent to the browser.
//
// The payload intentionally carries NO file content. The only in-tree consumer
// (the WebUI's handleWorkspacePatch) logs path/action/seq, and shipping whole
// files on every write/edit flooded every connected tab's main thread with
// multi-hundred-KB JSON.parse work. Consumers needing content must fetch the
// file via /api/files. size is computed from content before it is dropped so
// the UI can still show change magnitude.
func WorkspacePatchEvent(filePath, content, action string, seqNum int64, conflictInfo ...PatchConflictInfo) map[string]interface{} {
	payload := map[string]interface{}{
		"file_path": filePath,
		"size":      len(content),
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

// MetricsUpdateEventWithCategory is the SP-094-6 variant that
// includes the most-recent error category label so the cost/status
// footer can render "rate-limited, retrying…" distinct from generic
// provider errors. The default MetricsUpdateEvent still exists for
// callers that don't have an error context.
//
// provider and model are carried on every metrics payload (the footer
// and the sidebar log read them; without the keys a retry-time update
// renders as "Model: ? | Provider: ?" even though the agent knows
// both).
func MetricsUpdateEventWithCategory(providerID, model string, totalTokens, contextTokens, maxContextTokens, iteration int, totalCost float64, errorCategory string) map[string]interface{} {
	return map[string]interface{}{
		"provider":           providerID,
		"model":              model,
		"total_tokens":       totalTokens,
		"context_tokens":     contextTokens,
		"max_context_tokens": maxContextTokens,
		"iteration":          iteration,
		"total_cost":         totalCost,
		"error_category":     errorCategory,
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
// MaxToolEventArgsLength bounds the arguments string shipped in tool_start
// events. write_file / edit_file arguments embed the entire file payload,
// which otherwise fans out to every connected tab (and sits in the reattach
// replay buffer) only to be re-JSON.parsed and discarded by the UI. The head
// of the string is kept so leading keys like {"path": "..."} survive for
// consumers that extract the target path.
const MaxToolEventArgsLength = 8192

// TruncateEventString returns s unchanged when within limit, otherwise the
// first limit bytes plus a truncation marker.
func TruncateEventString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit] + "\n... (truncated)"
}

func ToolStartEvent(toolName, toolCallID, arguments, displayName, persona string, isSubagent bool, subagentType string, toolIndex int) map[string]interface{} {
	data := map[string]interface{}{
		"tool_name":    toolName,
		"tool_call_id": toolCallID,
		"arguments":    TruncateEventString(arguments, MaxToolEventArgsLength),
		"display_name": displayName,
	}
	if len(arguments) > MaxToolEventArgsLength {
		// write_file / edit_file carry whole file contents in arguments; the
		// UI only renders them (and extracts .path for stats). Mark truncation
		// the same way tool_end marks results.
		data["arguments_truncated"] = true
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

// EditApprovalRequestEvent (SP-072-3) creates an edit_approval_request
// event payload for the per-hunk diff approval gate. requestID uniquely
// identifies the approval so the WebUI can POST a decision back to
// /api/edits/{requestID}/decision. path is the file being edited.
// hunks is a JSON-serializable representation of each diff hunk with
// its line-level change type (context/add/remove). unifiedDiff is the
// raw unified-diff string for display.
func EditApprovalRequestEvent(requestID, path, unifiedDiff string, hunks []map[string]interface{}) map[string]interface{} {
	return map[string]interface{}{
		"request_id":   requestID,
		"file_path":    path,
		"unified_diff": unifiedDiff,
		"hunks":        hunks,
		"timestamp":    time.Now().UTC().Format(time.RFC3339),
	}
}

// PasswordRequestEvent (SP-089-3) creates a password_request event payload.
// requestID uniquely identifies the request so the WebUI can POST a
// response to /api/password/{requestID}/respond. command is the shell
// command that triggered the prompt. prompt is the raw prompt text
// detected on the child's stdout/stderr (e.g., "[sudo] password for user:").
func PasswordRequestEvent(requestID, command, prompt string) map[string]interface{} {
	return map[string]interface{}{
		"request_id": requestID,
		"command":    command,
		"prompt":     prompt,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
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

// SubagentClarificationRequestedEvent creates a delegate_clarification_requested event payload.
func SubagentClarificationRequestedEvent(subagentID, requestID, question string) map[string]interface{} {
	return map[string]interface{}{
		"subagent_id": subagentID,
		"request_id":  requestID,
		"question":    question,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
}

// SubagentClarificationRespondedEvent creates a delegate_clarification_responded event payload.
func SubagentClarificationRespondedEvent(subagentID, requestID, response string) map[string]interface{} {
	return map[string]interface{}{
		"subagent_id": subagentID,
		"request_id":  requestID,
		"response":    response,
		"timestamp":   time.Now().UTC().Format(time.RFC3339),
	}
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
		"request_id":       requestID,
		"prompt":           prompt,
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

// AskUserRequestEvent creates an ask_user request event for the webui.
// Accepts any struct whose JSON shape matches AskUserRequest (the
// agent_tools package supplies one). Falls through fields onto the
// flat event payload so existing frontend consumers that only read
// "question" continue to work.
func AskUserRequestEvent(requestID string, req AskUserRequest, clientID string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"question":   req.Question,
	}
	if req.Header != "" {
		payload["header"] = req.Header
	}
	if len(req.Options) > 0 {
		opts := make([]map[string]string, len(req.Options))
		for i, opt := range req.Options {
			entry := map[string]string{"label": opt.Label}
			if opt.Value != "" {
				entry["value"] = opt.Value
			}
			if opt.Description != "" {
				entry["description"] = opt.Description
			}
			opts[i] = entry
		}
		payload["options"] = opts
	}
	if req.MultiSelect {
		payload["multi_select"] = true
	}
	if req.Default != "" {
		payload["default"] = req.Default
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}
	return payload
}

// AskUserCancelledEvent creates a payload for cancelling an in-flight
// ask_user request. The frontend uses the status field to dismiss the
// dialog without losing context — the same status pattern as "responded".
func AskUserCancelledEvent(requestID, clientID string) map[string]interface{} {
	payload := map[string]interface{}{
		"request_id": requestID,
		"status":     "cancelled",
	}
	if clientID != "" {
		payload["client_id"] = clientID
	}
	return payload
}

// InputRequiredEvent creates an input_required event payload.
// reason is a human-readable description of why input is needed
// (e.g., "security_approval", "ask_user", "blocking_prompt").
// requestID optionally links to the specific request event.
func InputRequiredEvent(reason, requestID string) map[string]interface{} {
	payload := map[string]interface{}{
		"reason":    reason,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	if requestID != "" {
		payload["request_id"] = requestID
	}
	return payload
}

// LanguageGuardReplacementEvent creates a language_guard_replacement event
// payload (SP-152 §152c, item 152.7). It is published when a streamed reply
// that was RELEASED by the streaming hold-back (its start passed the language
// check) is re-checked at completion and found to have switched language
// mid-stream. The reply was already streamed to the client and cannot be
// un-streamed, so the client is told to replace the already-streamed
// assistant message: `replacement` is what it shows (the localized §152b-style
// notice), `original` is the full switched content (for "view original"), and
// `reason` names the trigger (e.g. "mid_stream_switch").
func LanguageGuardReplacementEvent(chatID, replacement, original, reason string) map[string]interface{} {
	payload := map[string]interface{}{
		"replacement": replacement,
		"original":    original,
		"reason":      reason,
	}
	if chatID != "" {
		payload["chat_id"] = chatID
	}
	return payload
}
