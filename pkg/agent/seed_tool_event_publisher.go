// Package agent: richEventPublisher type and its Publish method for
// enriching seed tool events with display_name, persona, subagent metadata
// and emitting CLI tool_log output. (split from seed_tool_registry.go)
package agent

import (
	core "github.com/sprout-foundry/seed/core"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// ---------------------------------------------------------------------------
// richEventPublisher — enriches seed's lightweight tool_start/tool_end events
// with rich metadata (display_name, persona, is_subagent, subagent_type)
// and emits CLI tool_log output for tool execution.
// ---------------------------------------------------------------------------

// richEventPublisher wraps an *events.EventBus and enriches ALL events with
// agent event metadata (client_id, chat_id, user_id) so that the WebSocket
// event router can deliver them to the correct browser tab.  For tool_start
// and tool_end events it also adds display_name, persona, is_subagent, and
// subagent_type fields that the webui expects, and emits CLI tool_log output.
type richEventPublisher struct {
	bus   *events.EventBus
	agent *Agent
}

func newRichEventPublisher(bus *events.EventBus, agent *Agent) *richEventPublisher {
	return &richEventPublisher{bus: bus, agent: agent}
}

// Publish implements core.EventPublisher. All events are decorated with the
// agent's event metadata (client_id, chat_id, user_id) so that the WebSocket
// handler's shouldForwardEventToConnection can route them to the correct
// browser connection.  Tool events receive additional enrichment (display_name,
// persona, is_subagent, subagent_type) and emit CLI tool_log output.
func (r *richEventPublisher) Publish(eventType string, data any) {
	// Decorate with agent metadata for WebSocket routing.
	data = r.decorateWithMetadata(data)

	switch eventType {
	case core.EventTypeToolStart, core.EventTypeToolEnd:
		// Tool execution now happens entirely inside seed's ToolRegistry, which
		// doesn't go through Agent.callTool — so the only place we can keep
		// sprout's TotalToolCalls counter in sync is here, on tool_end.
		// SubagentResult.ToolCalls reads this counter; without the increment,
		// every subagent reports 0 tool calls and the orchestrator thinks
		// nothing happened.
		if eventType == core.EventTypeToolEnd && r.agent != nil && r.agent.state != nil {
			r.agent.state.IncrementTotalToolCalls()
		}
		enriched := r.enrichEventData(data, eventType)
		r.bus.Publish(eventType, enriched)
	case "agent_message":
		// The seed core's finalize() publishes the full final response as an
		// agent_message event. When streaming is active, the response was
		// already displayed to the terminal via the streaming callback, so
		// forwarding this event to the bus causes a duplicate terminal print
		// (the terminal subscriber renders agent_message events with a ⓘ
		// glyph). Suppress the event when streaming content was produced.
		if r.agent != nil && r.agent.output.IsStreamingEnabled() && r.agent.output.GetStreamingBuffer().Len() > 0 {
			if payload, ok := data.(map[string]interface{}); ok {
				if cat, _ := payload["category"].(string); cat == "info" {
					return
				}
			}
		}
		r.bus.Publish(eventType, data)

	case core.EventTypeQueryStarted:
		// Sprout's prepareQueryRun (seed_query.go) publishes query_started
		// with provider/model/chat metadata before seed's run starts. Seed's
		// own publication ~1s later (conversation.go runLoop) would render a
		// duplicate "Query:" line in the sidebar log and a second user
		// bubble. Suppress seed's copy, mirroring the query_completed
		// suppression below.
		return

	case core.EventTypeMetricsUpdate:
		// Seed's metrics publishes (retry.go, conversation.go, streaming.go)
		// carry token/cost counters but not provider/model. The WebUI footer
		// falls back to previous state, but the sidebar log renders missing
		// keys as "Model: ? | Provider: ?". Fill them from the agent.
		if payload, ok := data.(map[string]interface{}); ok {
			if r.agent != nil {
				if _, has := payload["provider"]; !has {
					payload["provider"] = r.agent.GetProvider()
				}
				if _, has := payload["model"]; !has {
					payload["model"] = r.agent.GetModel()
				}
			}
		}
		r.bus.Publish(eventType, data)

	case core.EventTypeError:
		// Seed publishes an error event with message "chat failed" both
		// per retry attempt and when a chat call finally fails. Sprout
		// already surfaces strictly better signals for the same failures:
		// publishRetryEvent emits a metrics_update (with error category,
		// provider, model) plus a visible provider_retry agent_message per
		// attempt, and the webui's runChatQuery publishes the terminal
		// "Query failed" with the underlying cause. Forwarding seed's copy
		// makes the WebUI's fatal error handler fire mid-retry (spinner
		// drops, "[FAIL] Error: chat failed" bubbles) while the backend is
		// still retrying. Suppress it; other seed error events pass through.
		if payload, ok := data.(map[string]interface{}); ok {
			if msg, _ := payload["message"].(string); msg == "chat failed" {
				return
			}
		}
		r.bus.Publish(eventType, data)

	case core.EventTypeQueryCompleted:
		// Seed's finalize() publishes QueryCompleted, but sprout's
		// finalizeConversationPostHooks also publishes it (on ALL paths
		// including errors where seed's finalize() doesn't run).
		// Suppress seed's publication to prevent the duplicate
		// "✓ turn complete" line in the terminal. Sprout's version
		// carries the same fields plus a "status" field.
		return
	default:
		r.bus.Publish(eventType, data)
	}
}

// decorateWithMetadata merges the agent's event metadata (client_id, chat_id,
// user_id) into the event payload. This ensures that events published by seed's
// core agent through the EventPublisher are properly routed to the originating
// browser tab via shouldForwardEventToConnection. Without this decoration,
// events without client_id/chat_id are silently dropped by the WebSocket
// forwarding logic.
func (r *richEventPublisher) decorateWithMetadata(data any) any {
	if r.agent == nil {
		return data
	}
	return r.agent.decorateEventPayload(data)
}

// enrichEventData adds rich metadata fields to a tool event payload.
// For tool_end, it also emits a CLI tool_log when streaming is disabled.
func (r *richEventPublisher) enrichEventData(data any, eventType string) any {
	if data == nil {
		return data
	}

	payload, ok := data.(map[string]interface{})
	if !ok {
		return data
	}

	// Extract tool_name (both event types have this)
	toolName, _ := payload["tool_name"].(string)

	if toolName == "" {
		return data
	}

	displayName := buildDisplayName(toolName, argsFromPayload(payload))
	isSubagent := isSubagentTool(toolName)
	var subagentType string
	if isSubagent {
		subagentType = func() string {
			if toolName == "run_subagent" {
				return "single"
			}
			if toolName == "run_parallel_subagents" {
				return "parallel"
			}
			return ""
		}()
	}
	persona := extractPersona(payload)

	// Enrich with rich fields
	payload["display_name"] = displayName
	if persona != "" {
		payload["persona"] = persona
	}
	if isSubagent {
		payload["is_subagent"] = true
		payload["subagent_type"] = subagentType
	}

	// Emit CLI tool_log for tool execution progress.
	//
	// tool_start: a single dim "→ <displayName>" line at the moment the
	//   tool begins. Uses the cleaner buildDisplayName format (no
	//   nested brackets / quoted JSON) — the structured payload still
	//   carries the raw args for WebUI consumers.
	// tool_end:   an indented duration / outcome chip ("  ✓ 124ms") that
	//   visually pairs with the line above. Emitted unconditionally so
	//   the user always sees how long a tool took; streaming or not.
	if r.agent != nil {
		if eventType == core.EventTypeToolStart {
			r.agent.ToolLog("executing tool", displayName)
		} else if eventType == core.EventTypeToolEnd {
			if router := r.agent.OutputRouter(); router != nil {
				duration := extractDurationMs(payload)
				status, _ := payload["status"].(string)
				errMsg, _ := payload["error"].(string)
				router.RouteToolCompletion(status != "failed", duration, errMsg)
			}
		}
	}
	return payload
}
