package agent

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// convertToHistoryMessages converts api.Message to history.APIMessage format.
// Returns nil for nil input — the receiving side handles nil gracefully.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func convertToHistoryMessages(messages []api.Message) []history.APIMessage {
	if messages == nil {
		return nil
	}

	result := make([]history.APIMessage, len(messages))
	for i, msg := range messages {
		result[i] = history.APIMessage{
			Role:             msg.Role,
			Content:          msg.Content,
			ReasoningContent: msg.ReasoningContent,
			ToolCallID:       msg.ToolCallID,
		}

		// Convert tool calls if present
		if len(msg.ToolCalls) > 0 {
			result[i].ToolCalls = make([]history.APIToolCall, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				result[i].ToolCalls[j] = history.APIToolCall{
					ID:   tc.ID,
					Type: tc.Type,
					Function: struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					}{
						Name:      tc.Function.Name,
						Arguments: tc.Function.Arguments,
					},
				}
			}
		}
	}

	return result
}
