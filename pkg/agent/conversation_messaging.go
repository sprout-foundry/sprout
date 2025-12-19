package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// prepareMessages prepares and optimizes the message list for API submission
func (ch *ConversationHandler) prepareMessages() []api.Message {
	var optimizedMessages []api.Message

	// Use conversation optimizer if enabled
	if ch.agent.optimizer != nil && ch.agent.optimizer.IsEnabled() {
		optimizedMessages = ch.agent.optimizer.OptimizeConversation(ch.agent.messages)
	} else {
		optimizedMessages = ch.agent.messages
	}

	// Belt-and-suspenders: ensure we don't carry a duplicate system prompt in history.
	// If any system message matches the current systemPrompt verbatim, drop it from history here
	// because we always prepend the active systemPrompt below.
	filtered := make([]api.Message, 0, len(optimizedMessages))
	for _, m := range optimizedMessages {
		if m.Role == "system" && strings.TrimSpace(m.Content) == strings.TrimSpace(ch.agent.systemPrompt) {
			continue
		}
		filtered = append(filtered, m)
	}
	optimizedMessages = filtered

	// Always include system prompt at the beginning
	allMessages := []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
	allMessages = append(allMessages, optimizedMessages...)

	// Check context limits and apply pruning if needed
	currentTokens := ch.estimateTokens(allMessages)
	if ch.agent.maxContextTokens > 0 {
		// Create pruner if needed and check if we should prune
		if ch.agent.conversationPruner == nil {
			ch.agent.conversationPruner = NewConversationPruner(ch.agent.debug)
		}

		if ch.agent.conversationPruner.ShouldPrune(currentTokens, ch.agent.maxContextTokens, ch.agent.GetProvider()) {
			if ch.agent.debug {
				contextUsage := float64(currentTokens) / float64(ch.agent.maxContextTokens)
				ch.agent.PrintLineAsync(fmt.Sprintf("🔄 Context pruning triggered: %d/%d tokens (%.1f%%)",
					currentTokens, ch.agent.maxContextTokens, contextUsage*100))
			}

			// Apply pruning to optimized messages (excluding system prompt)
			prunedMessages := ch.agent.conversationPruner.PruneConversation(optimizedMessages, currentTokens, ch.agent.maxContextTokens, ch.agent.optimizer, ch.agent.GetProvider())

			// Rebuild with system prompt
			allMessages = []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
			allMessages = append(allMessages, prunedMessages...)
			// Persist pruned history so future iterations don't re-trigger on stale counts
			ch.agent.messages = prunedMessages

			if ch.agent.debug {
				newTokens := ch.estimateTokens(allMessages)
				ch.agent.PrintLineAsync(fmt.Sprintf("✅ Context after pruning: %d tokens (%.1f%%)",
					newTokens, float64(newTokens)/float64(ch.agent.maxContextTokens)*100))
			}
		}
	}

	allMessages = ch.appendTransientMessages(allMessages)
	allMessages = ch.sanitizeToolMessages(allMessages)

	// DeepSeek-specific validation
	if strings.EqualFold(ch.agent.GetProvider(), "deepseek") {
		ch.validateDeepSeekToolCalls(allMessages)
	}

	return allMessages
}

// validateDeepSeekToolCalls performs additional validation for DeepSeek tool call format
func (ch *ConversationHandler) validateDeepSeekToolCalls(messages []api.Message) {
	ch.agent.debugLog("🔍 DeepSeek: Validating tool call format for %d messages\n", len(messages))

	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Check if this assistant message is properly followed by tool messages
			expectedToolCalls := len(msg.ToolCalls)
			foundToolResponses := 0

			// Look ahead for tool messages
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "tool" {
					foundToolResponses++
				} else if messages[j].Role == "assistant" || messages[j].Role == "user" {
					// We've reached the next assistant/user message, stop counting
					break
				}
			}

			if foundToolResponses < expectedToolCalls {
				ch.agent.debugLog("🚨 DeepSeek: WARNING - Assistant message at index %d has %d tool_calls but only %d tool responses found\n",
					i, expectedToolCalls, foundToolResponses)
			} else {
				ch.agent.debugLog("✅ DeepSeek: Assistant message at index %d has %d tool_calls with %d tool responses\n",
					i, expectedToolCalls, foundToolResponses)
			}
		}
	}
}

// enqueueTransientMessage adds a message that will be sent once and then discarded
func (ch *ConversationHandler) enqueueTransientMessage(msg api.Message) {
	// Check for duplicate transient messages to avoid role alternation issues
	for _, existing := range ch.transientMessages {
		if existing.Role == msg.Role && existing.Content == msg.Content {
			ch.agent.debugLog("⚠️ Skipping duplicate transient message: role=%s, content=%q\n", msg.Role, msg.Content)
			return
		}
	}
	ch.transientMessages = append(ch.transientMessages, msg)
}

// appendTransientMessages adds transient messages to the message list and clears the buffer
func (ch *ConversationHandler) appendTransientMessages(messages []api.Message) []api.Message {
	if len(ch.transientMessages) == 0 {
		return messages
	}
	messages = append(messages, ch.transientMessages...)
	ch.transientMessages = nil
	return messages
}

// sanitizeContent removes ANSI escape sequences and other problematic characters from content
func (ch *ConversationHandler) sanitizeContent(content string) string {
	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[mGKHJABCD]`)
	cleaned := ansiRegex.ReplaceAllString(content, "")

	// Remove other potential ANSI sequences
	ansiRegex2 := regexp.MustCompile(`\x1b\([0-9;]*[AB]`)
	cleaned = ansiRegex2.ReplaceAllString(cleaned, "")

	// Remove any remaining escape characters
	cleaned = strings.ReplaceAll(cleaned, "\x1b", "")

	if ch.agent.debug && cleaned != content {
		ch.agent.debugLog("🧹 Sanitized content, removed %d ANSI chars\n", len(content)-len(cleaned))
	}

	return cleaned
}

// processImagesInQuery handles image processing in queries
func (ch *ConversationHandler) processImagesInQuery(query string) (string, error) {
	// Move image processing logic here
	return ch.agent.processImagesInQuery(query)
}

// estimateTokens provides a rough estimate of token count for messages
func (ch *ConversationHandler) estimateTokens(messages []api.Message) int {
	totalTokens := 0

	for _, msg := range messages {
		// Estimate core content tokens
		totalTokens += EstimateTokens(msg.Content)

		if msg.ReasoningContent != "" {
			totalTokens += EstimateTokens(msg.ReasoningContent)
		}

		// Include tool call metadata (arguments can be sizeable JSON payloads)
		for _, toolCall := range msg.ToolCalls {
			totalTokens += EstimateTokens(toolCall.Function.Name)
			totalTokens += EstimateTokens(toolCall.Function.Arguments)
			// modest overhead for call framing/ids
			totalTokens += 20
		}

		// Role/formatting overhead per message
		totalTokens += 10
	}

	// Apply a small safety buffer but stay close to measured estimate
	buffered := int(float64(totalTokens) * 1.05)
	if buffered < totalTokens {
		return totalTokens
	}
	return buffered
}

// appendTurnLogFile writes turn evaluation data to a log file if configured
func (ch *ConversationHandler) appendTurnLogFile(turn TurnEvaluation) {
	path := os.Getenv("LEDIT_TURN_LOG_FILE")
	if path == "" {
		return
	}
	data, err := json.Marshal(turn)
	if err != nil {
		ch.agent.debugLog("⚠️ Failed to marshal turn log: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		ch.agent.debugLog("⚠️ Failed to open turn log file %s: %v\n", path, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		ch.agent.debugLog("⚠️ Failed to write turn log: %v\n", err)
	}
}
