package agent

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// sendMessage handles the API communication with retry logic
func (ch *ConversationHandler) sendMessage() (*api.ChatResponse, error) {
	messages := ch.prepareMessages()
	tools := ch.prepareTools()
	reasoning := ch.determineReasoningEffort()

	return ch.apiClient.SendWithRetry(messages, tools, reasoning)
}

// prepareTools gets the optimized tool definitions for the current context
func (ch *ConversationHandler) prepareTools() []api.Tool {
	return ch.agent.getOptimizedToolDefinitions(ch.agent.messages)
}

// determineReasoningEffort determines the reasoning effort level for the current context
func (ch *ConversationHandler) determineReasoningEffort() string {
	return ch.agent.determineReasoningEffort(ch.agent.messages)
}

// sanitizeToolMessages removes orphaned or duplicate tool messages
func (ch *ConversationHandler) sanitizeToolMessages(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Debug logging for DeepSeek and Minimax
	if ch.agent != nil {
		provider := ch.agent.GetProvider()
		if strings.EqualFold(provider, "deepseek") || strings.EqualFold(provider, "minimax") {
			ch.agent.debugLog("🔍 %s sanitizing %d messages\n", strings.ToUpper(provider), len(messages))
		}
	}

	sanitized := make([]api.Message, 0, len(messages))
	seenToolCalls := make(map[string]struct{})

	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			sanitized = append(sanitized, msg)
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" {
						seenToolCalls[tc.ID] = struct{}{}
					}
				}
			}
		case "tool":
			if msg.ToolCallId == "" {
				ch.logDroppedToolMessage("missing tool_call_id", msg)
				continue
			}

			if _, ok := seenToolCalls[msg.ToolCallId]; ok {
				sanitized = append(sanitized, msg)
				delete(seenToolCalls, msg.ToolCallId)
			} else {
				ch.logDroppedToolMessage(fmt.Sprintf("no matching assistant for %s", msg.ToolCallId), msg)
			}
		default:
			sanitized = append(sanitized, msg)
		}
	}

	// Minimax-specific: After sanitization, make absolutely sure there are no orphaned tool results
	if ch.agent != nil && strings.EqualFold(ch.agent.GetProvider(), "minimax") {
		// Second pass: verify each tool result has a matching assistant message
		finalSanitized := make([]api.Message, 0, len(sanitized))
		validToolCallIds := make(map[string]struct{})

		for _, msg := range sanitized {
			if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" {
						validToolCallIds[tc.ID] = struct{}{}
					}
				}
				finalSanitized = append(finalSanitized, msg)
			} else if msg.Role == "tool" {
				// Only keep tool result if we've seen a matching tool call ID
				if _, ok := validToolCallIds[msg.ToolCallId]; ok {
					finalSanitized = append(finalSanitized, msg)
					delete(validToolCallIds, msg.ToolCallId)
				} else {
					ch.agent.debugLog("🚨 Minimax: DROPPING orphaned tool result with tool_call_id=%s\n", msg.ToolCallId)
				}
			} else {
				finalSanitized = append(finalSanitized, msg)
			}
		}

		if len(finalSanitized) != len(sanitized) {
			ch.agent.debugLog("✅ Minimax: Sanitization removed %d orphaned tool result(s)\n", len(sanitized)-len(finalSanitized))
		}

		return finalSanitized
	}

	return sanitized
}

// logDroppedToolMessage logs information about dropped tool messages for debugging
func (ch *ConversationHandler) logDroppedToolMessage(reason string, msg api.Message) {
	if ch.agent == nil || !ch.agent.debug {
		return
	}

	snippet := strings.TrimSpace(msg.Content)
	if len(snippet) > 80 {
		snippet = snippet[:77] + "..."
	}

	// Enhanced logging for DeepSeek and Minimax
	provider := ch.agent.GetProvider()
	if strings.EqualFold(provider, "deepseek") {
		ch.agent.debugLog("🚨 DeepSeek: ⚠️ Dropping tool message (%s). tool_call_id=%s snippet=%q\n", reason, msg.ToolCallId, snippet)
	} else if strings.EqualFold(provider, "minimax") {
		ch.agent.debugLog("🚨 Minimax: ⚠️ Dropping tool message (%s). tool_call_id=%s snippet=%q\n", reason, msg.ToolCallId, snippet)
	} else {
		ch.agent.debugLog("⚠️ Dropping tool message (%s). tool_call_id=%s snippet=%q\n", reason, msg.ToolCallId, snippet)
	}
}
