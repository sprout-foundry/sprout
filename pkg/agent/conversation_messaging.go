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

	// Minimax-specific validation
	if strings.EqualFold(ch.agent.GetProvider(), "minimax") {
		ch.validateMinimaxToolCalls(allMessages)
	}

	// Final safeguard: remove any orphaned tool results before sending to API
	allMessages = ch.removeOrphanedToolResults(allMessages)

	return allMessages
}

// removeOrphanedToolResults removes tool result messages whose tool_call_id
// doesn't match any assistant message with tool_calls. This can happen when
// conversation pruning removes assistant messages but leaves their tool results.
func (ch *ConversationHandler) removeOrphanedToolResults(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Collect all valid tool_call_ids from assistant messages with tool_calls
	validToolCallIDs := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validToolCallIDs[tc.ID] = struct{}{}
				}
			}
		}
	}

	if len(validToolCallIDs) == 0 {
		// No tool calls, remove all tool results
		filtered := make([]api.Message, 0, len(messages))
		removedCount := 0
		for _, msg := range messages {
			if msg.Role == "tool" {
				removedCount++
				if ch.agent.debug {
					ch.agent.debugLog("🧹 Removed orphaned tool result with tool_call_id=%s\n", msg.ToolCallId)
				}
			} else {
				filtered = append(filtered, msg)
			}
		}
		if removedCount > 0 && ch.agent.debug {
			ch.agent.debugLog("🧹 Removed %d orphaned tool result(s) (no assistant with tool_calls)\n", removedCount)
		}
		return filtered
	}

	// Filter out tool results whose tool_call_id isn't in the valid set
	filtered := make([]api.Message, 0, len(messages))
	removedCount := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			if _, ok := validToolCallIDs[msg.ToolCallId]; ok {
				filtered = append(filtered, msg)
			} else {
				removedCount++
				if ch.agent.debug {
					ch.agent.debugLog("🧹 Removed orphaned tool result with tool_call_id=%s\n", msg.ToolCallId)
				}
			}
		} else {
			filtered = append(filtered, msg)
		}
	}

	if removedCount > 0 && ch.agent.debug {
		ch.agent.debugLog("🧹 Removed %d orphaned tool result(s)\n", removedCount)
	}

	return filtered
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

// validateMinimaxToolCalls performs additional validation for Minimax tool call format
// Minimax requires that tool results immediately follow their corresponding tool calls
// and that each tool result's tool_call_id matches a tool call from the previous assistant message
func (ch *ConversationHandler) validateMinimaxToolCalls(messages []api.Message) {
	ch.agent.debugLog("🔍 Minimax: Validating tool call format for %d messages\n", len(messages))

	// Track expected tool call IDs from the last assistant message
	var expectedToolCallIDs []string

	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Collect tool call IDs from this assistant message
			expectedToolCallIDs = nil
			for _, tc := range msg.ToolCalls {
				expectedToolCallIDs = append(expectedToolCallIDs, tc.ID)
				ch.agent.debugLog("  Minimax: Assistant[%d] has tool_call id=%s name=%s\n",
					i, tc.ID, tc.Function.Name)
			}

			// Check that tool results immediately follow
			if i+1 >= len(messages) {
				ch.agent.debugLog("⚠️ Minimax: Assistant message at end with no tool results\n")
				continue
			}

			// Count tool results that immediately follow
			foundToolResults := 0
			for j := i + 1; j < len(messages); j++ {
				if messages[j].Role == "tool" {
					foundToolResults++
					// Verify this tool_call_id is in our expected list
					found := false
					for _, expectedID := range expectedToolCallIDs {
						if messages[j].ToolCallId == expectedID {
							found = true
							break
						}
					}
					if !found {
						ch.agent.debugLog("🚨 Minimax: ERROR - Tool result at index %d has tool_call_id=%s which doesn't match any tool call from assistant at index %d\n",
							j, messages[j].ToolCallId, i)
						ch.agent.debugLog("   Expected IDs: %v\n", expectedToolCallIDs)
						// THIS IS THE BUG - we have orphaned tool results
						ch.agent.debugLog("🚨🚨🚨 MINIMAX BUG: Orphaned tool result detected! This will cause error 2013!\n")
					} else {
						ch.agent.debugLog("  Minimax: Tool[%d] result for tool_call_id=%s\n", j, messages[j].ToolCallId)
					}

					// Check if we've found all expected tool results
					if foundToolResults >= len(expectedToolCallIDs) {
						break
					}
				} else if messages[j].Role == "assistant" || messages[j].Role == "user" {
					// Tool results block ended
					break
				}
			}

			if foundToolResults < len(expectedToolCallIDs) {
				ch.agent.debugLog("🚨 Minimax: WARNING - Expected %d tool results but only found %d after assistant at index %d\n",
					len(expectedToolCallIDs), foundToolResults, i)
			}
		}
	}

	// NEW: Check for tool results that appear BEFORE any assistant message with tool calls
	// This can happen if conversation pruning removes the assistant message
	firstAssistantWithTools := -1
	for i, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			firstAssistantWithTools = i
			break
		}
	}

	for i, msg := range messages {
		if msg.Role == "tool" && (firstAssistantWithTools == -1 || i < firstAssistantWithTools) {
			ch.agent.debugLog("🚨 Minimax: CRITICAL - Tool result at index %d appears before any assistant message with tool calls!\n", i)
			ch.agent.debugLog("   tool_call_id=%s\n", msg.ToolCallId)
			ch.agent.debugLog("🚨🚨🚨 MINIMAX BUG: Orphaned tool result at beginning! This will cause error 2013!\n")
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

// sanitizeContent removes ANSI escape sequences, think tags, and other problematic characters from content
func (ch *ConversationHandler) sanitizeContent(content string) string {
	// Remove think tags (some models output <think>...</think>)
	thinkRegex := regexp.MustCompile(`<think>.*?</think>`)
	cleaned := thinkRegex.ReplaceAllString(content, "")

	// Remove ANSI escape sequences
	ansiRegex := regexp.MustCompile(`\x1b\[[0-9;]*[mGKHJABCD]`)
	cleaned = ansiRegex.ReplaceAllString(cleaned, "")

	// Remove other potential ANSI sequences
	ansiRegex2 := regexp.MustCompile(`\x1b\([0-9;]*[AB]`)
	cleaned = ansiRegex2.ReplaceAllString(cleaned, "")

	// Remove any remaining escape characters
	cleaned = strings.ReplaceAll(cleaned, "\x1b", "")

	if ch.agent.debug && cleaned != content {
		ch.agent.debugLog("🧹 Sanitized content, removed %d chars\n", len(content)-len(cleaned))
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
