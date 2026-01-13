package agent

import (
	"fmt"
	"os"
	"strings"
)

// shouldLogTurnSummaries determines if turn summaries should be logged
func (ch *ConversationHandler) shouldLogTurnSummaries() bool {
	if ch.agent == nil {
		return false
	}
	if ch.agent.debug {
		return true
	}
	return os.Getenv("LEDIT_LOG_TURNS") == "1"
}

// logTurnSummary logs a summary of the turn for debugging
func (ch *ConversationHandler) logTurnSummary(turn TurnEvaluation) {
	if !ch.shouldLogTurnSummaries() {
		return
	}
	var builder strings.Builder
	builder.WriteString(fmt.Sprintf("\n🔎 Turn %d summary (%s)\n", turn.Iteration, turn.Timestamp.Format("15:04:05")))
	builder.WriteString(fmt.Sprintf("  User: %s\n", abbreviate(turn.UserInput, 120)))
	builder.WriteString(fmt.Sprintf("  Assistant: %s\n", abbreviate(turn.AssistantContent, 240)))
	if turn.ReasoningSnippet != "" {
		builder.WriteString(fmt.Sprintf("  Reasoning: %s\n", abbreviate(turn.ReasoningSnippet, 240)))
	}
	if len(turn.ToolCalls) > 0 {
		builder.WriteString(fmt.Sprintf("  Tool calls: %d\n", len(turn.ToolCalls)))
	}
	if len(turn.ToolResults) > 0 {
		builder.WriteString(fmt.Sprintf("  Tool results: %d entries\n", len(turn.ToolResults)))
	}
	if turn.FinishReason != "" {
		builder.WriteString(fmt.Sprintf("  Finish reason: %s\n", turn.FinishReason))
	}
	tokens := turn.TokenUsage
	builder.WriteString(fmt.Sprintf("  Tokens: prompt=%d completion=%d total=%d\n", tokens.PromptTokens, tokens.CompletionTokens, tokens.TotalTokens))
	if turn.CompletionReached {
		builder.WriteString("  Completion: reached\n")
	}
	if turn.GuardrailTrigger != "" {
		builder.WriteString(fmt.Sprintf("  Guardrail: %s\n", turn.GuardrailTrigger))
	}
	ch.agent.PrintLineAsync(builder.String())
}

// abbreviate truncates text to the specified limit with an ellipsis if needed
func abbreviate(text string, limit int) string {
	clean := strings.TrimSpace(text)
	if len(clean) <= limit || limit <= 0 {
		return clean
	}
	if limit > 1 {
		return clean[:limit-1] + "…"
	}
	return clean[:limit]
}

// finalizeTurn finalizes a turn by recording it and logging if needed
func (ch *ConversationHandler) finalizeTurn(turn TurnEvaluation, shouldStop bool) bool {
	if shouldStop {
		turn.CompletionReached = true
	}
	ch.turnHistory = append(ch.turnHistory, turn)
	ch.logTurnSummary(turn)
	ch.appendTurnLogFile(turn)
	return shouldStop
}
