package agent

import (
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// handleFinishReason processes the model's finish reason and returns whether to stop
func (ch *ConversationHandler) handleFinishReason(finishReason, content string) (bool, string) {
	if finishReason == "" {
		return false, ""
	}

	ch.agent.debugLog("[GO] Model finish reason: %s\n", finishReason)

	switch finishReason {
	case "tool_calls":
		return false, "model tool_calls finish"
	case "stop":
		if strings.TrimSpace(content) == "" {
			ch.agent.debugLog("[WARN] WARNING: Model returned finish_reason='stop' with empty content\n")
			ch.agent.debugLog("[WARN] Treating as incomplete and asking model to continue\n")
			ch.handleIncompleteResponse()
			return false, "empty stop response"
		}
		if ch.responseValidator != nil && ch.responseValidator.IsIncomplete(content) {
			ch.agent.debugLog("[WARN] Model returned finish_reason='stop' with incomplete content\n")
			ch.enqueueTransientMessage(api.Message{
				Role: "user",
				Content: "You indicated completion, but your answer appears incomplete. " +
					"Provide a concise final answer to the original user request now.",
			})
			return false, "incomplete stop response"
		}
		if ch.responseValidator != nil && ch.followsRecentToolResults() &&
			ch.responseValidator.LooksLikeTentativePostToolResponse(content) {
			ch.tentativeRejectionCount++
			if ch.tentativeRejectionCount >= 2 {
				// Accept the response after 2 rejections to avoid loops
				ch.tentativeRejectionCount = 0
				ch.agent.debugLog("[WARN] Tentative post-tool rejection limit reached, accepting response\n")
				ch.displayFinalResponse(content)
				return true, "completion"
			}
			ch.agent.debugLog("[WARN] Model returned finish_reason='stop' immediately after tool results with tentative content (rejection %d/2)\n", ch.tentativeRejectionCount)
			ch.enqueueTransientMessage(api.Message{
				Role: "user",
				Content: "You just received tool results. Do not stop with a planning note. " +
					"Either take the next concrete action or provide the actual final answer now.",
			})
			return false, "tentative post-tool stop response"
		}
		// Model explicitly signaled it's done with non-empty content - accept completion.
		ch.agent.debugLog("[GO] Model signaled 'stop' - accepting response as complete\n")
		ch.displayFinalResponse(content)
		return true, "completion"
	case "length":
		ch.agent.debugLog("[WARN] Model hit length limit, asking to continue\n")
		ch.handleIncompleteResponse()
		return false, "model length limit"
	case "content_filter":
		ch.agent.debugLog("[NO] Model response was filtered\n")
		return false, "content filtered"
	default:
		ch.agent.debugLog("[?] Unknown finish reason: %s\n", finishReason)
		return false, "unknown finish reason: " + finishReason
	}
}

func (ch *ConversationHandler) followsRecentToolResults() bool {
	if ch == nil || ch.agent == nil || len(ch.agent.messages) == 0 {
		return false
	}

	i := len(ch.agent.messages) - 1
	if ch.agent.messages[i].Role == "assistant" {
		i--
	}
	if i < 0 {
		return false
	}

	foundTool := false
	for ; i >= 0 && ch.agent.messages[i].Role == "tool"; i-- {
		foundTool = true
	}
	return foundTool
}
