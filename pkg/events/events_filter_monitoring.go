// Event factory functions for the diagnostics / compact / automate /
// monitoring events: the compact-started / context-management-diagnostics /
// recall-diagnostics / compact-completed factories, the drift-detected
// event, the automate session / budget / output-chunk / ended factories, and
// the OOM-watchdog + allowed-path-hit monitoring events. Split out of
// events_filter.go.

package events

import (
	"time"
)

// Helper functions for creating specific event types
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
