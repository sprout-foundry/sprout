package agent

import (
	"strings"
	"unicode"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// isBlankIteration checks if an iteration is considered blank (no meaningful content or tool calls)
func (ch *ConversationHandler) isBlankIteration(content string, toolCalls []api.ToolCall) bool {
	// Check if there are tool calls - if yes, not blank
	if len(toolCalls) > 0 {
		return false
	}

	// Check if content is empty or contains only whitespace
	trimmedContent := strings.TrimSpace(content)
	if len(trimmedContent) == 0 {
		return true
	}

	// Check if content is just a very short response that doesn't seem meaningful
	// Be less aggressive - only consider truly meaningless content as blank
	if len(trimmedContent) <= 1 {
		// Only single characters or empty content should be considered blank
		return true
	}

	// Check if it's just punctuation or whitespace
	if len(trimmedContent) <= 3 {
		for _, char := range trimmedContent {
			if !unicode.IsPunct(rune(char)) && !unicode.IsSpace(rune(char)) {
				return false // Contains a non-punctuation character, not blank
			}
		}
		return true // All punctuation/space, considered blank
	}

	return false
}

// isRepetitiveContent checks if the content is repetitive or indicates a loop
func (ch *ConversationHandler) isRepetitiveContent(content string) bool {
	trimmedContent := strings.TrimSpace(content)

	// Check for common repetitive patterns that indicate the agent is stuck
	// Focus on specific problematic patterns rather than common analysis phrases
	repetitivePatterns := []string{
		"let me check for any simple improvements",
		"let me look for any obvious issues",
		"let me check for any simple improvements by looking at the file more carefully",
		"let me look for any obvious issues:",
		"let me check for any simple improvements by looking at the file more carefully. let me look for any obvious issues:",
		"let me look at the agent creation code more carefully:",
		// Remove overly broad patterns like "let me examine the", "let me analyze the", etc.
		// These are common legitimate analysis phrases that shouldn't trigger repetition detection
	}

	lowerContent := strings.ToLower(trimmedContent)
	for _, pattern := range repetitivePatterns {
		if strings.Contains(lowerContent, pattern) {
			ch.agent.debugLog("🔄 Repetitive content pattern detected: %s\n", pattern)
			return true
		}
	}

	// Check if the content is exactly the same as the previous assistant message
	for idx := len(ch.agent.messages) - 2; idx >= 0; idx-- {
		prevMsg := ch.agent.messages[idx]
		if prevMsg.Role != "assistant" {
			continue
		}
		if strings.TrimSpace(prevMsg.Content) == trimmedContent {
			ch.agent.debugLog("🔄 Exact duplicate content detected\n")
			return true
		}
		break
	}

	// Check for excessive repetition of the same phrase
	words := strings.Fields(lowerContent)
	if len(words) > 10 {
		// Count word frequency
		wordCount := make(map[string]int)
		for _, word := range words {
			wordCount[word]++
		}

		// If any word appears more than 30% of the time, it's likely repetitive
		for word, count := range wordCount {
			if float64(count)/float64(len(words)) > 0.3 && len(word) > 3 {
				ch.agent.debugLog("🔄 High word repetition detected: %s (%d/%d)\n", word, count, len(words))
				return true
			}
		}
	}

	return false
}

// handleIncompleteResponse adds a transient message asking the model to continue
func (ch *ConversationHandler) handleIncompleteResponse() {
	ch.enqueueTransientMessage(api.Message{
		Role:    "user",
		Content: "Please continue with your response. The previous response appears incomplete.",
	})
}

// handleMissingCompletionSignal adds a reminder about the completion signal
func (ch *ConversationHandler) handleMissingCompletionSignal() {
	// Allow multiple reminders but with increasing intervals to avoid infinite loops
	if ch.missingCompletionReminders >= 3 {
		// After 3 reminders, assume the agent is stuck and provide stronger guidance
		ch.agent.debugLog("⚠️ Multiple completion reminders sent, providing stronger guidance\n")
		strongReminder := "You have not provided [[TASK_COMPLETE]] after multiple reminders. Please:\n" +
			"1. If your task is COMPLETE, respond exactly with: [[TASK_COMPLETE]]\n" +
			"2. If your task is NOT complete, take a specific ACTION (use tools) or provide a concrete RESULT\n" +
			"3. Do not provide explanations or plans - take action or confirm completion."
		ch.enqueueTransientMessage(api.Message{Role: "user", Content: strongReminder})
		ch.missingCompletionReminders++
		return
	}

	reminder := "Please confirm completion by responding with [[TASK_COMPLETE]] once all requested work is fully done. If you still have steps remaining, continue with the next action."
	ch.enqueueTransientMessage(api.Message{Role: "user", Content: reminder})
	ch.missingCompletionReminders++
	ch.agent.debugLog("🔔 Added reminder requesting explicit [[TASK_COMPLETE]] signal (reminder #%d)\n", ch.missingCompletionReminders)
}