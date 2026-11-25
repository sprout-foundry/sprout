package core

import (
	"time"
)

// finalize returns the final response content.
func (ch *ConversationHandler) finalize(query string) (string, error) {
	messages := ch.agent.state.Messages()
	var finalContent string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			finalContent = messages[i].Content
			break
		}
	}

	// Record a turn checkpoint asynchronously so it doesn't block the response.
	if ch.turnCompleted && ch.queryStartIndex >= 0 && ch.turnEndIndex >= ch.queryStartIndex {
		if ch.turnEndIndex < len(messages) {
			turnMessages := messages[ch.queryStartIndex : ch.turnEndIndex+1]
			RecordTurnCheckpointAsync(
				ch.agent.state,
				turnMessages,
				ch.queryStartIndex,
				ch.turnEndIndex,
				5*time.Second,
				ch.agent.onCheckpoint,
			)
		} else {
			ch.agent.debugLog("[checkpoint] Skipping checkpoint: turnEndIndex %d >= messages len %d\n",
				ch.turnEndIndex, len(messages))
		}
	} else if !ch.turnCompleted {
		ch.agent.debugLog("[checkpoint] Skipping checkpoint: turn not completed\n")
	} else {
		ch.agent.debugLog("[checkpoint] Skipping checkpoint: invalid indices (queryStart=%d, turnEnd=%d)\n",
			ch.queryStartIndex, ch.turnEndIndex)
	}

	// Publish query completed event
	if ch.agent.eventPublisher != nil {
		duration := time.Since(ch.conversationStart)
		ch.agent.eventPublisher.Publish(EventTypeQueryCompleted, map[string]interface{}{
			"query":       query,
			"response":    finalContent,
			"tokens":      ch.agent.state.TotalTokens(),
			"cost":        ch.agent.state.TotalCost(),
			"duration_ms": duration.Milliseconds(),
		})

		// Publish agent_message event with final response content
		if finalContent != "" {
			ch.agent.eventPublisher.Publish("agent_message",
				map[string]interface{}{"category": "info", "message": finalContent})
		}
	}

	return finalContent, nil
}
