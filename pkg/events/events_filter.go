// Event factory functions
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

// CompactStartedEvent creates the payload for a compact_started event.
// source is one of "manual" (slash command) or "auto_llm_summary" (seed
// structural compaction / context-limit recovery). messageCount and
// checkpointCount capture the pre-compact state for diagnostics.
func CompactStartedEvent(source string, messageCount, checkpointCount int) map[string]interface{} {
	return map[string]interface{}{
		"source":           source,
		"message_count":    messageCount,
		"checkpoint_count": checkpointCount,
		"timestamp":        time.Now().UTC().Format(time.RFC3339),
	}
}

// ContextManagementDiagnosticEvent (SP-066 Phase 1, SP-126) reports the
// model-aware context-budget math at a single iteration. Subscribers (WebUI
// metrics panel, telemetry pipelines) use it to verify substitution is doing
// the heavy lifting and the LLM fall-through stays approximately never.
//
// Fields:
//   - current_tokens: tokenizer-estimated size of the prompt going to the model.
//   - max_tokens: the EFFECTIVE max — the smaller of the model's native window
//     and the user's MaxContextTokens cap (SP-126). This is the value seed's
//     budget math operates against. Renamed semantically from the SP-066
//     "hard context-window limit" wording because SP-126 makes the cap a
//     first-class concept; pre-SP-126 the two were identical (no cap).
//   - native_max_tokens: the model's UNCAPPED native window. Equal to
//     max_tokens when no user cap is set; larger than max_tokens when a
//     cap is active. Lets subscribers render "X / 300K of 1M tokens"
//     (effective vs native) distinctly in the metrics panel.
//   - effective_max: max_tokens minus reservation budget; substitution
//     triggers when current_tokens exceeds trigger_fraction × max_tokens.
//   - trigger_fraction: share of max_tokens at which seed triggers compaction
//     (1 − total_reserved_fraction).
//   - reserved_response / reserved_thinking / reserved_tool_io: the three
//     reservation slices as fractions of max_tokens.
//   - iteration: current iteration number from seed's OnIteration callback.
//   - message_count: messages in the prepared prompt list.
//   - cached_tokens: cumulative prompt tokens served from the provider's
//     prompt cache so far this session.
//   - prompt_tokens: cumulative prompt tokens charged so far this session.
//   - cache_write_tokens: cumulative tokens written to the provider's cache
//     (Anthropic cache_create_input_tokens). May be 0 if not tracked.
//   - cache_hit_rate: cached_tokens / prompt_tokens, or 0 when prompt_tokens
//     is 0. Lets the UI render cache effectiveness at a glance.
func ContextManagementDiagnosticEvent(currentTokens, maxTokens, nativeMaxTokens int, triggerFraction, reservedResponse, reservedThinking, reservedToolIO float64, iteration, messageCount int, cachedTokens, promptTokens, cacheWriteTokens int) map[string]interface{} {
	effectiveMax := 0
	if maxTokens > 0 {
		effectiveMax = int(float64(maxTokens) * triggerFraction)
	}
	cacheHitRate := 0.0
	if promptTokens > 0 {
		cacheHitRate = float64(cachedTokens) / float64(promptTokens)
	}
	return map[string]interface{}{
		"current_tokens":     currentTokens,
		"max_tokens":         maxTokens,
		"native_max_tokens":  nativeMaxTokens,
		"effective_max":      effectiveMax,
		"trigger_fraction":   triggerFraction,
		"reserved_response":  reservedResponse,
		"reserved_thinking":  reservedThinking,
		"reserved_tool_io":   reservedToolIO,
		"iteration":          iteration,
		"message_count":      messageCount,
		"cached_tokens":      cachedTokens,
		"prompt_tokens":      promptTokens,
		"cache_write_tokens": cacheWriteTokens,
		"cache_hit_rate":     cacheHitRate,
		"timestamp":          time.Now().UTC().Format(time.RFC3339),
	}
}

// RecallDiagnosticEvent (SP-066 Phase 3) reports a single semantic-recall
// pass. embedDurationMS measures the embed call (the recall query's
// latency on the user's critical path). candidatesConsidered is what the
// store returned before recency rerank + filter. injected/injectedChars
// is what actually landed in the prompt supplement. topScores is the
// raw cosine similarities for the candidates so subscribers can spot
// near-miss patterns and tune the threshold.
func RecallDiagnosticEvent(embedDurationMS float64, candidatesConsidered, injected, injectedChars int, topScores []float32) map[string]interface{} {
	scores := make([]float64, len(topScores))
	for i, s := range topScores {
		scores[i] = float64(s)
	}
	return map[string]interface{}{
		"embed_duration_ms":     embedDurationMS,
		"candidates_considered": candidatesConsidered,
		"injected":              injected,
		"injected_chars":        injectedChars,
		"top_scores":            scores,
		"timestamp":             time.Now().UTC().Format(time.RFC3339),
	}
}

// CompactCompletedEvent creates the payload for a compact_completed event.
// On success, err should be nil and after/summary fields describe the new
// state. On failure, err carries the reason and counts reflect the
// unchanged pre-compact totals.
func CompactCompletedEvent(source string, beforeCount, afterCount int, summaryChars int, err error) map[string]interface{} {
	data := map[string]interface{}{
		"source":               source,
		"before_message_count": beforeCount,
		"after_message_count":  afterCount,
		"summary_chars":        summaryChars,
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
	}
	if err != nil {
		data["error"] = err.Error()
		data["success"] = false
	} else {
		data["success"] = true
	}
	return data
}

// DriftDetectedEvent creates a drift notification event for the WebUI
func DriftDetectedEvent(similarity float64, threshold float64, sessionID string) map[string]interface{} {
	return map[string]interface{}{
		"similarity": similarity,
		"threshold":  threshold,
		"sessionId":  sessionID,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
		"options":    []string{"continue", "new_chat"},
	}
}

// AutomateSessionStartedEvent creates a session_started event payload.
func AutomateSessionStartedEvent(sessionID, workflow, kind string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"workflow":   workflow,
		"kind":       kind,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateBudgetUpdateEvent creates a budget_update event payload.
func AutomateBudgetUpdateEvent(sessionID string, spentUSD, budgetUSD float64, fraction float64, iteration int) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"spent_usd":  spentUSD,
		"budget_usd": budgetUSD,
		"fraction":   fraction,
		"iteration":  iteration,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateOutputChunkEvent creates an output_chunk event payload.
// Note: we send chunk_len instead of the full chunk to avoid bloating WS frames.
func AutomateOutputChunkEvent(sessionID string, offset int, chunk string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"offset":     offset,
		"chunk_len":  len(chunk),
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// AutomateSessionEndedEvent creates a session_ended event payload.
func AutomateSessionEndedEvent(sessionID, workflow, status string, totalCost float64) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"workflow":   workflow,
		"status":     status,
		"total_cost": totalCost,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}

// OOMWatchdogAlertEvent creates an oom_watchdog_alert event payload.
func OOMWatchdogAlertEvent(nodeCount int, totalRSSBytes uint64, thresholdNodeCount int, thresholdRSSBytes uint64, triggerReason string) map[string]interface{} {
	return map[string]interface{}{
		"node_count":           nodeCount,
		"total_rss_bytes":      totalRSSBytes,
		"threshold_node_count": thresholdNodeCount,
		"threshold_rss_bytes":  thresholdRSSBytes,
		"trigger_reason":       triggerReason,
		"timestamp":            time.Now().UTC().Format(time.RFC3339),
	}
}

// AllowedPathHitEvent creates an allowed_path_hit event payload.
// SP-127 Phase 2.7: emitted when a file operation lands under a
// session-allowlisted folder so the WebUI automations panel can count
// per-run folder grants.
func AllowedPathHitEvent(sessionID, workflow, path, mode string) map[string]interface{} {
	return map[string]interface{}{
		"session_id": sessionID,
		"workflow":   workflow,
		"path":       path,
		"mode":       mode,
		"timestamp":  time.Now().UTC().Format(time.RFC3339),
	}
}
