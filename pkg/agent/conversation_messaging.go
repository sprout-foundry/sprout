package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strings"
	"unicode/utf8"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// prepareMessages prepares and optimizes the message list for API submission.
// Context reduction uses checkpoint compaction, structural compaction, and emergency truncation as a last-resort fallback.
func (ch *ConversationHandler) prepareMessages(tools []api.Tool) []api.Message {
	var optimizedMessages []api.Message
	ch.transientMessagesMu.Lock()
	pendingTransientMessages := append([]api.Message(nil), ch.transientMessages...)
	ch.transientMessagesMu.Unlock()
	appendPendingTransient := func(messages []api.Message) []api.Message {
		if len(pendingTransientMessages) == 0 {
			return messages
		}
		return append(messages, pendingTransientMessages...)
	}

	// Use conversation optimizer if enabled
	if ch.agent.optimizer != nil && ch.agent.optimizer.IsEnabled() {
		optimizedMessages = ch.agent.optimizer.OptimizeConversation(ch.agent.messages)
	} else {
		optimizedMessages = ch.agent.messages
	}

	// One-shot context refresh injected after provider/model switches that required strict syntax normalization.
	if switchRefresh := ch.agent.consumePendingSwitchContextRefresh(); switchRefresh != "" {
		optimizedMessages = append(optimizedMessages, api.Message{
			Role:    "user",
			Content: switchRefresh,
		})
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
	optimizedMessages = ch.stripImagesForNonVisionModels(optimizedMessages)

	// Always include system prompt at the beginning
	allMessages := []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
	allMessages = append(allMessages, optimizedMessages...)
	allMessages = appendPendingTransient(allMessages)
	allMessages = collapseSystemMessagesToFront(allMessages)

	// Check context limits — compaction-only, no pruning.
	currentTokens := ch.apiClient.estimateRequestTokens(allMessages, tools)
	if ch.agent.maxContextTokens > 0 {
		// Compaction threshold: trigger at 87% to compact before API errors occur.
		compactionThreshold := int(float64(ch.agent.maxContextTokens) * PruningConfig.Default.StandardPercent)
		compactionApplied := false

		if currentTokens > compactionThreshold && ch.agent.HasTurnCheckpoints() {
			checkpointedMessages, remainingCheckpoints := ch.agent.BuildCheckpointCompactedMessages(optimizedMessages)
			if len(checkpointedMessages) != len(optimizedMessages) {
				compactionApplied = true
				// Persist checkpointed messages so future iterations see the compacted history.
				ch.agent.messages = checkpointedMessages
				// Persist adjusted remaining checkpoints so indices stay valid against the compacted array.
				ch.agent.ReplaceTurnCheckpoints(remainingCheckpoints)

				checkpointHistory := []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
				checkpointHistory = append(checkpointHistory, checkpointedMessages...)
				checkpointHistory = collapseSystemMessagesToFront(checkpointHistory)
				optimizedMessages = checkpointedMessages

				allMessages = []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
				allMessages = append(allMessages, optimizedMessages...)
				allMessages = appendPendingTransient(allMessages)
				allMessages = collapseSystemMessagesToFront(allMessages)
				currentTokens = ch.apiClient.estimateRequestTokens(allMessages, tools)

				if ch.agent.debug {
					ch.agent.PrintLineAsync(fmt.Sprintf("[~] Switched older completed turns to checkpoints; now %d tokens",
						currentTokens))
				}
			}
		}

		// If checkpoint compaction wasn't enough and an LLM-equipped optimizer is available,
		// try structural compaction as a second-pass fallback.
		if currentTokens > compactionThreshold && ch.agent.optimizer != nil && ch.agent.optimizer.IsEnabled() {
			llmCompacted := ch.agent.optimizer.CompactConversation(optimizedMessages)
			if len(llmCompacted) < len(optimizedMessages) {
				llmHistory := []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
				llmHistory = append(llmHistory, llmCompacted...)
				llmHistory = collapseSystemMessagesToFront(llmHistory)
				llmTokens := ch.apiClient.estimateRequestTokens(llmHistory, tools)
				if llmTokens < currentTokens {
					compactionApplied = true
					ch.agent.messages = llmCompacted
					ch.agent.clearTurnCheckpoints()
					optimizedMessages = llmCompacted

					allMessages = []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
					allMessages = append(allMessages, optimizedMessages...)
					allMessages = appendPendingTransient(allMessages)
					allMessages = collapseSystemMessagesToFront(allMessages)
					currentTokens = llmTokens

					if ch.agent.debug {
						ch.agent.PrintLineAsync(fmt.Sprintf("[~] LLM structural compaction applied; now %d tokens",
							currentTokens))
					}
				}
			}
		}

		// Third-pass emergency truncation: only when no compaction was applied.
		// If checkpoint or structural compaction succeeded, trust the result even
		// if it's still above threshold (artificially small maxContextTokens in
		// tests can cause this; in production the model's context window handles it).
		// Emergency truncation is a last resort for subagents with oversized
		// single prompts where no compaction strategy can help.
		if currentTokens > compactionThreshold && !compactionApplied {
			allMessages, currentTokens = ch.emergencyTruncateContext(
				allMessages, tools, currentTokens, ch.agent.maxContextTokens, compactionThreshold)
		}
	}

	// Sanitize tool message chains — required for providers with strict tool call
	// format requirements (Minimax error 2013, DeepSeek tool flow). This removes
	// orphaned tool results that can result from compaction or optimization dedup.
	allMessages = ch.sanitizeToolMessages(allMessages)

	// DeepSeek-specific validation (diagnostic logging — sanitizeToolMessages above is the fix)
	if strings.EqualFold(ch.agent.GetProvider(), "deepseek") || strings.Contains(strings.ToLower(ch.agent.GetModel()), "deepseek") {
		ch.validateDeepSeekToolCalls(allMessages)
	}

	// Minimax-specific validation (diagnostic logging — sanitizeToolMessages above is the fix)
	if strings.EqualFold(ch.agent.GetProvider(), "minimax") || strings.Contains(strings.ToLower(ch.agent.GetModel()), "minimax") {
		ch.validateMinimaxToolCalls(allMessages)
	}

	// Final safety net: remove any orphaned tool results before sending to API
	allMessages = ch.removeOrphanedToolResults(allMessages)

	// Strip any leading assistant prefill messages that would be incompatible
	// with thinking-mode providers (e.g., enable_thinking). Context compaction
	// can produce an assistant-role summary as the first non-system message,
	// which some providers reject as "Assistant response prefill is
	// incompatible with enable_thinking".
	allMessages = ch.stripLeadingAssistantPrefill(allMessages)

	ch.transientMessagesMu.Lock()
	ch.transientMessages = nil
	ch.transientMessagesMu.Unlock()

	return allMessages
}

func (ch *ConversationHandler) stripImagesForNonVisionModels(messages []api.Message) []api.Message {
	if ch.agent == nil || ch.agent.client == nil || ch.agent.client.SupportsVision() {
		return messages
	}

	removed := 0
	out := append([]api.Message(nil), messages...)
	for i := range out {
		if len(out[i].Images) == 0 {
			continue
		}
		removed += len(out[i].Images)
		out[i].Images = nil
	}

	if removed > 0 && ch.agent.debug {
		ch.agent.debugLog("[img] Stripped %d historical image payload(s) for non-vision model\n", removed)
	}
	return out
}

// stripLeadingAssistantPrefill removes leading assistant messages (compaction
// summaries) that appear immediately after the system prompt. Some providers
// with thinking/reasoning mode enabled reject messages where the first
// non-system message has role "assistant" (assistant response prefill).
//
// Assistant messages that carry tool_calls are preserved since they are
// part of an active tool-use flow, not prefill material.
func (ch *ConversationHandler) stripLeadingAssistantPrefill(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Find the first non-system message index
	start := 0
	for start < len(messages) && messages[start].Role == "system" {
		start++
	}
	if start >= len(messages) {
		return messages
	}

	// Count leading assistant prefill messages (no tool_calls)
	stripped := 0
	for start < len(messages) && messages[start].Role == "assistant" && len(messages[start].ToolCalls) == 0 {
		stripped++
		start++
	}

	if stripped == 0 {
		return messages
	}

	if ch.agent != nil && ch.agent.debug {
		ch.agent.debugLog("[prefill] Stripped %d leading assistant prefill message(s) to avoid incompatibility with thinking-mode providers\n", stripped)
	}

	// Keep: system messages + everything after the leading assistant prefills
	result := make([]api.Message, 0, len(messages)-stripped)
	result = append(result, messages[:start-stripped]...)
	result = append(result, messages[start:]...)
	return result
}

func collapseSystemMessagesToFront(messages []api.Message) []api.Message {
	if len(messages) <= 1 {
		return messages
	}

	firstSystemIndex := -1
	var systemParts []string
	nonSystem := make([]api.Message, 0, len(messages))

	for i, msg := range messages {
		if msg.Role != "system" {
			nonSystem = append(nonSystem, msg)
			continue
		}

		if firstSystemIndex == -1 {
			firstSystemIndex = i
		}
		if content := strings.TrimSpace(msg.Content); content != "" {
			systemParts = append(systemParts, content)
		}
	}

	if firstSystemIndex <= 0 && len(systemParts) <= 1 {
		return messages
	}

	merged := api.Message{Role: "system", Content: strings.Join(systemParts, "\n\n")}
	result := make([]api.Message, 0, len(nonSystem)+1)
	result = append(result, merged)
	result = append(result, nonSystem...)
	return result
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
					ch.agent.debugLog("[clean] Removed orphaned tool result with tool_call_id=%s\n", msg.ToolCallId)
				}
			} else {
				filtered = append(filtered, msg)
			}
		}
		if removedCount > 0 && ch.agent.debug {
			ch.agent.debugLog("[clean] Removed %d orphaned tool result(s) (no assistant with tool_calls)\n", removedCount)
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
					ch.agent.debugLog("[clean] Removed orphaned tool result with tool_call_id=%s\n", msg.ToolCallId)
				}
			}
		} else {
			filtered = append(filtered, msg)
		}
	}

	if removedCount > 0 && ch.agent.debug {
		ch.agent.debugLog("[clean] Removed %d orphaned tool result(s)\n", removedCount)
	}

	return filtered
}

// emergencyTruncateContext performs aggressive but structured content trimming when
// both checkpoint and structural compaction failed. This is the last resort before
// sending an oversized request that would cause API errors.
//
// Strategy (applied iteratively until under threshold):
//  1. Trim tool result messages to a token budget
//  2. Trim non-recent user messages (keep most recent user message intact)
//  3. Trim non-recent assistant messages
//  4. Truncate any remaining message content proportionally
//
// Returns the (possibly truncated) messages and updated token estimate.
func (ch *ConversationHandler) emergencyTruncateContext(
	messages []api.Message, tools []api.Tool,
	currentTokens, maxTokens, threshold int,
) ([]api.Message, int) {
	// Target 75% of max context to leave comfortable headroom for completion.
	targetTokens := int(float64(maxTokens) * 0.75)
	if targetTokens >= currentTokens {
		return messages, currentTokens
	}

	// truncateHeadTail returns a rune-safe truncation keeping headRunes from the
	// start and tailRunes from the end of s, joined by a marker.
	truncateHeadTail := func(s string, headRunes, tailRunes int) string {
		r := []rune(s)
		if len(r) <= headRunes+tailRunes {
			return s
		}
		return string(r[:headRunes]) + "\n\n... [truncated for context limit] ...\n\n" + string(r[len(r)-tailRunes:])
	}

	// truncateHead returns a rune-safe truncation keeping only headRunes from the start.
	truncateHead := func(s string, headRunes int) string {
		r := []rune(s)
		if len(r) <= headRunes {
			return s
		}
		return string(r[:headRunes]) + "\n\n... [truncated for context limit] ..."
	}

	// Work on copies to avoid mutating the originals.
	trimmed := make([]api.Message, len(messages))
	copy(trimmed, messages)

	// Phase 1: Truncate tool result messages (preserve structure, trim content).
	const maxToolResultTokens = 500 // ~1500 chars
	changed := false
	for i := range trimmed {
		if trimmed[i].Role != "tool" || trimmed[i].Content == "" {
			continue
		}
		toolTokens := EstimateTokens(trimmed[i].Content)
		if toolTokens > maxToolResultTokens {
			content := trimmed[i].Content
			maxRunes := maxToolResultTokens * 4
			if utf8.RuneCountInString(content) > maxRunes {
				keepHead := maxRunes / 2
				keepTail := maxRunes / 3
				trimmed[i].Content = truncateHeadTail(content, keepHead, keepTail)
				changed = true
			}
		}
	}

	if changed {
		currentTokens = ch.apiClient.estimateRequestTokens(trimmed, tools)
		if currentTokens <= targetTokens {
			ch.agent.PrintLineAsync(fmt.Sprintf("[~] Emergency truncation applied (tool trimming); now %d/%d tokens",
				currentTokens, maxTokens))
			return trimmed, currentTokens
		}
	}

	// Phase 2: Trim older user messages (keep most recent user message intact).
	lastUserIdx := -1
	for i := len(trimmed) - 1; i >= 0; i-- {
		if trimmed[i].Role == "user" {
			lastUserIdx = i
			break
		}
	}

	changed = false
	for i := range trimmed {
		if trimmed[i].Role != "user" || i == lastUserIdx || trimmed[i].Content == "" {
			continue
		}
		msgTokens := EstimateTokens(trimmed[i].Content)
		maxUserTokens := 300
		if msgTokens > maxUserTokens {
			maxRunes := maxUserTokens * 4
			if utf8.RuneCountInString(trimmed[i].Content) > maxRunes {
				trimmed[i].Content = truncateHead(trimmed[i].Content, maxRunes)
				changed = true
			}
		}
	}

	if changed {
		currentTokens = ch.apiClient.estimateRequestTokens(trimmed, tools)
		if currentTokens <= targetTokens {
			ch.agent.PrintLineAsync(fmt.Sprintf("[~] Emergency truncation applied (user msg trimming); now %d/%d tokens",
				currentTokens, maxTokens))
			return trimmed, currentTokens
		}
	}

	// Phase 3: Trim older assistant messages (keep recent ones intact).
	recentStart := len(trimmed) - 6
	if recentStart < 0 {
		recentStart = 0
	}
	changed = false
	for i := range trimmed {
		if i >= recentStart || trimmed[i].Role != "assistant" || trimmed[i].Content == "" {
			continue
		}
		msgTokens := EstimateTokens(trimmed[i].Content)
		maxAssistantTokens := 400
		if msgTokens > maxAssistantTokens {
			maxRunes := maxAssistantTokens * 4
			if utf8.RuneCountInString(trimmed[i].Content) > maxRunes {
				trimmed[i].Content = truncateHead(trimmed[i].Content, maxRunes)
				changed = true
			}
		}
	}

	if changed {
		currentTokens = ch.apiClient.estimateRequestTokens(trimmed, tools)
		if currentTokens <= targetTokens {
			ch.agent.PrintLineAsync(fmt.Sprintf("[~] Emergency truncation applied (assistant msg trimming); now %d/%d tokens",
				currentTokens, maxTokens))
			return trimmed, currentTokens
		}
	}

	// Phase 4: Proportional trimming of all older messages.
	excessRatio := float64(currentTokens) / float64(targetTokens)
	if excessRatio > 1.0 {
		limit := len(trimmed)
		if recentStart < limit {
			limit = recentStart
		}
		changed = false
		for i := range trimmed[:limit] {
			if trimmed[i].Role == "system" || trimmed[i].Content == "" {
				continue
			}
			runes := []rune(trimmed[i].Content)
			targetLen := int(float64(len(runes)) / excessRatio)
			if targetLen < len(runes) && targetLen > 50 {
				trimmed[i].Content = truncateHead(trimmed[i].Content, targetLen)
				changed = true
			}
		}
		if changed {
			currentTokens = ch.apiClient.estimateRequestTokens(trimmed, tools)
		}
	}

	if currentTokens > threshold {
		ch.agent.PrintLineAsync(fmt.Sprintf("[WARN] Context over limit after emergency truncation: %d/%d tokens",
			currentTokens, maxTokens))
	} else {
		ch.agent.PrintLineAsync(fmt.Sprintf("[~] Emergency truncation applied; now %d/%d tokens",
			currentTokens, maxTokens))
	}

	return trimmed, currentTokens
}

// validateDeepSeekToolCalls performs additional validation for DeepSeek tool call format
func (ch *ConversationHandler) validateDeepSeekToolCalls(messages []api.Message) {
	ch.agent.debugLog("[search] DeepSeek: Validating tool call format for %d messages\n", len(messages))

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
				ch.agent.debugLog("[!!] DeepSeek: WARNING - Assistant message at index %d has %d tool_calls but only %d tool responses found\n",
					i, expectedToolCalls, foundToolResponses)
			} else {
				ch.agent.debugLog("[OK] DeepSeek: Assistant message at index %d has %d tool_calls with %d tool responses\n",
					i, expectedToolCalls, foundToolResponses)
			}
		}
	}
}

// validateMinimaxToolCalls performs additional validation for Minimax tool call format
// Minimax requires that tool results immediately follow their corresponding tool calls
// and that each tool result's tool_call_id matches a tool call from the previous assistant message
func (ch *ConversationHandler) validateMinimaxToolCalls(messages []api.Message) {
	ch.agent.debugLog("[search] Minimax: Validating tool call format for %d messages\n", len(messages))

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
				ch.agent.debugLog("[WARN] Minimax: Assistant message at end with no tool results\n")
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
						ch.agent.debugLog("[!!] Minimax: ERROR - Tool result at index %d has tool_call_id=%s which doesn't match any tool call from assistant at index %d\n",
							j, messages[j].ToolCallId, i)
						ch.agent.debugLog("   Expected IDs: %v\n", expectedToolCallIDs)
						// THIS IS THE BUG - we have orphaned tool results
						ch.agent.debugLog("[!!!] MINIMAX BUG: Orphaned tool result detected! This will cause error 2013!\n")
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
				ch.agent.debugLog("[!!] Minimax: WARNING - Expected %d tool results but only found %d after assistant at index %d\n",
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
			ch.agent.debugLog("[!!] Minimax: CRITICAL - Tool result at index %d appears before any assistant message with tool calls!\n", i)
			ch.agent.debugLog("   tool_call_id=%s\n", msg.ToolCallId)
			ch.agent.debugLog("[!!!] MINIMAX BUG: Orphaned tool result at beginning! This will cause error 2013!\n")
		}
	}
}

// enqueueTransientMessage adds a message that will be sent once and then discarded
func (ch *ConversationHandler) enqueueTransientMessage(msg api.Message) {
	ch.transientMessagesMu.Lock()
	defer ch.transientMessagesMu.Unlock()

	// Check for duplicate transient messages to avoid role alternation issues
	for _, existing := range ch.transientMessages {
		if existing.Role == msg.Role && existing.Content == msg.Content {
			ch.agent.debugLog("[WARN] Skipping duplicate transient message: role=%s, content=%q\n", msg.Role, msg.Content)
			return
		}
	}
	ch.transientMessages = append(ch.transientMessages, msg)
}

// appendTransientMessages adds transient messages to the message list and clears the buffer
func (ch *ConversationHandler) appendTransientMessages(messages []api.Message) []api.Message {
	ch.transientMessagesMu.Lock()
	defer ch.transientMessagesMu.Unlock()

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
		ch.agent.debugLog("[clean] Sanitized content, removed %d chars\n", len(content)-len(cleaned))
	}

	return cleaned
}

// processImagesInQuery handles image processing in queries
func (ch *ConversationHandler) processImagesInQuery(query string) ([]api.ImageData, string, error) {
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
		ch.agent.debugLog("[WARN] Failed to marshal turn log: %v\n", err)
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		ch.agent.debugLog("[WARN] Failed to open turn log file %s: %v\n", path, err)
		return
	}
	defer f.Close()
	if _, err := f.Write(append(data, '\n')); err != nil {
		ch.agent.debugLog("[WARN] Failed to write turn log: %v\n", err)
	}
}
