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

// flushToolLogsToOutput flushes any accumulated tool logs to the output
func (ch *ConversationHandler) flushToolLogsToOutput() []string {
	logs := ch.agent.drainToolLogs()
	if len(logs) == 0 {
		return nil
	}
	if ch.agent.streamingEnabled && ch.agent.streamingCallback != nil {
		for _, log := range logs {
			ch.agent.streamingCallback(log)
		}
	}
	return logs
}

// sanitizeToolMessages removes orphaned or duplicate tool messages
func (ch *ConversationHandler) sanitizeToolMessages(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
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

	ch.agent.debugLog("⚠️ Dropping tool message (%s). tool_call_id=%s snippet=%q\n", reason, msg.ToolCallId, snippet)
}
