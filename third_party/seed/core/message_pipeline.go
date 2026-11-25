package core

import "strings"

// prepareMessages assembles the message list for the API request.
//
// Progressive pipeline:
//
//   - First, estimate the raw conversation against the trigger fraction.
//   - If over: run **Phase 0a** (iterative checkpoint substitution) on the
//     raw state.Messages() slice — this is where checkpoint indices are
//     valid. Stop as soon as the estimate falls to target.
//   - If still over: run **Phase 0b** (iterative observation masking)
//     against the post-substitution slice. Same iterative contract.
//   - Below the trigger: raw history flows through. No substitution, no
//     masking. The model gets full fidelity for short and medium chats
//     and for long single-turn tool-iteration chains.
//
// The loop's downstream pressure check (in conversation.go) then runs
// **Phase 1+** drops via Compact() only if Phase 0 didn't relieve enough
// pressure (or if the consumer wired pruner/llmSummarizer). Recovery from
// provider-overflow errors uses Compact()'s drop path directly.
//
// See docs/compaction.md for the full design.
func (ch *ConversationHandler) prepareMessages() []Message {
	// Get current messages
	messages := ch.agent.state.Messages()

	// Persistent dedup pass: mask earlier duplicate file reads / transient
	// command outputs in STATE, not just in the request. The request-only
	// variant rewrote the wire prefix between iterations as new duplicates
	// appeared (a re-read masks the earlier read), which both invalidated the
	// provider's prompt cache and left state carrying content the model never
	// sees again — inflating every fallback token estimate. Dedup is
	// content-in-place and index-stable, so checkpoint indices remain valid.
	if ch.agent.optimizer != nil {
		if deduped, changed := ch.agent.optimizer.OptimizeConversationWithReport(messages, OptimizeOptions{
			MaskConsumedToolResults: false,
		}); changed {
			ch.agent.state.SetMessages(deduped)
			messages = deduped
		}
	}

	// Phase 0 (iterative, loss-minimizing) — operate on the RAW slice so
	// checkpoint indices remain valid. Only fires when raw history exceeds
	// the trigger × context_size threshold; below that, raw flows through.
	messages = ch.runIterativeCompactionPhase0(messages)

	// Strip system messages from history (we always prepend current system prompt)
	filtered := make([]Message, 0, len(messages))
	for _, m := range messages {
		if m.Role != "system" {
			filtered = append(filtered, m)
		}
	}

	// Strip images for non-vision models
	if !ch.agent.provider.Info().HasVision {
		filtered = ch.stripImages(filtered)
	}

	// Prepend system prompt
	allMessages := []Message{{Role: "system", Content: ch.agent.systemPrompt}}
	allMessages = append(allMessages, filtered...)

	// Append transient messages (one-shot, then discard)
	ch.transientMu.Lock()
	if len(ch.transientMsgs) > 0 {
		allMessages = append(allMessages, ch.transientMsgs...)
		ch.transientMsgs = nil
	}
	ch.transientMu.Unlock()

	// Collapse multiple system messages into one at the front
	allMessages = collapseSystemMessages(allMessages)

	// Sanitize: remove orphaned tool results
	allMessages = ch.removeOrphanedToolResults(allMessages)

	// Reorder: ensure tool results follow their tool calls in the correct
	// order. Some providers (MiniMax, DeepSeek) require strict threading:
	// tool results must appear immediately after the assistant message
	// containing their tool calls, and in the same order as the tool calls.
	allMessages = reorderToolResultsForThreading(allMessages)

	// Sanitize: strip ANSI escape codes from message content to prevent
	// terminal formatting codes from polluting LLM context.
	allMessages = sanitizeMessages(allMessages)

	// Optimize: dedup-only pass already ran at the top of prepareMessages on
	// the raw state slice (persisted). Observation masking is NOT run here —
	// it's part of Compact()'s iterative Phase 0b, gated on pressure.
	// nothing else to do for the request slice.

	return allMessages
}

// stripImages removes image data from messages for non-vision models.
func (ch *ConversationHandler) stripImages(messages []Message) []Message {
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Images = nil
	}
	return out
}

// removeOrphanedToolResults removes tool messages whose tool_call_id doesn't
// match any assistant message with tool_calls.
func (ch *ConversationHandler) removeOrphanedToolResults(messages []Message) []Message {
	validIDs := make(map[string]struct{})
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			for _, tc := range msg.ToolCalls {
				if tc.ID != "" {
					validIDs[tc.ID] = struct{}{}
				}
			}
		}
	}

	filtered := make([]Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role == "tool" {
			if _, ok := validIDs[msg.ToolCallID]; ok {
				filtered = append(filtered, msg)
			} else {
				ch.agent.debugLog("[clean] Removed orphaned tool result: %s\n", msg.ToolCallID)
			}
		} else {
			filtered = append(filtered, msg)
		}
	}
	return filtered
}

// reorderToolResultsForThreading ensures tool result messages appear in the
// same order as their corresponding tool calls in the preceding assistant
// message. Some providers (MiniMax, DeepSeek) enforce strict threading: a
// tool result must follow the assistant message that issued its tool call,
// and multiple results must match the tool call order.
//
// The function walks the message list. When it finds an assistant message
// with tool calls, it collects the immediately-following tool results and
// reorders them to match the tool call ID sequence. Non-tool messages
// (user, system, assistant without tool calls) pass through unchanged.
func reorderToolResultsForThreading(messages []Message) []Message {
	if len(messages) <= 1 {
		return messages
	}

	// Fast path: check if any assistant message has tool calls.
	hasToolCalls := false
	for _, msg := range messages {
		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			hasToolCalls = true
			break
		}
	}
	if !hasToolCalls {
		return messages
	}

	result := make([]Message, 0, len(messages))
	i := 0

	for i < len(messages) {
		msg := messages[i]

		if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
			// Build the expected order: tool call ID -> index in tool calls slice.
			expectedOrder := make(map[string]int, len(msg.ToolCalls))
			for j, tc := range msg.ToolCalls {
				if tc.ID != "" {
					expectedOrder[tc.ID] = j
				}
			}

			// Collect immediately-following tool results.
			i++
			var toolResults []Message
			for i < len(messages) && messages[i].Role == "tool" {
				toolResults = append(toolResults, messages[i])
				i++
			}

			// Add the assistant message.
			result = append(result, msg)

			// Reorder tool results to match tool call order.
			if len(toolResults) > 1 {
				// Build a map from tool_call_id to result.
				resultByCallID := make(map[string]Message, len(toolResults))
				for _, tr := range toolResults {
					resultByCallID[tr.ToolCallID] = tr
				}

				// Emit results in tool call order.
				for _, tc := range msg.ToolCalls {
					if tc.ID == "" {
						continue
					}
					if tr, ok := resultByCallID[tc.ID]; ok {
						result = append(result, tr)
						delete(resultByCallID, tc.ID)
					}
				}

				// Any remaining results (orphaned within this block) are
				// appended at the end to preserve them rather than dropping.
				for _, tr := range toolResults {
					if _, ok := resultByCallID[tr.ToolCallID]; ok {
						result = append(result, tr)
					}
				}
			} else {
				// Single result — no reordering needed.
				result = append(result, toolResults...)
			}
		} else {
			result = append(result, msg)
			i++
		}
	}

	return result
}

// collapseSystemMessages merges multiple system messages into one at the front.
func collapseSystemMessages(messages []Message) []Message {
	if len(messages) <= 1 {
		return messages
	}

	var systemParts []string
	nonSystem := make([]Message, 0, len(messages))

	for _, msg := range messages {
		if msg.Role == "system" {
			if content := strings.TrimSpace(msg.Content); content != "" {
				systemParts = append(systemParts, content)
			}
		} else {
			nonSystem = append(nonSystem, msg)
		}
	}

	if len(systemParts) == 0 {
		return messages
	}

	merged := Message{Role: "system", Content: strings.Join(systemParts, "\n\n")}
	result := make([]Message, 0, len(nonSystem)+1)
	result = append(result, merged)
	result = append(result, nonSystem...)
	return result
}

// runIterativeCompactionPhase0 applies Phase 0 (loss-minimizing)
// compaction to the raw state.Messages() slice:
//   - Phase 0a: iterative checkpoint substitution (oldest first, one at a
//     time, stop when under target). Operates on the raw slice so the
//     checkpoint StartIndex/EndIndex references remain valid.
//   - Phase 0b: iterative observation masking of big consumed tool results
//     (oldest first, one at a time, honoring the keep-last window).
//
// Returns the raw slice unchanged when the conversation is under the
// trigger fraction of the model's context window. Returns the most
// aggressively-compacted variant Phase 0 could produce otherwise; the
// caller's downstream pressure check decides whether Phase 1+ drops are
// still needed.
func (ch *ConversationHandler) runIterativeCompactionPhase0(rawMessages []Message) []Message {
	contextSize := ch.agent.provider.Info().ContextSize
	if contextSize <= 0 || len(rawMessages) == 0 {
		return rawMessages
	}

	// Caller-supplied estimator so this code and the loop's trigger
	// decision use the same token math. Without it, the loop may decide
	// "compact" and Phase 0 may decide "nothing to do" (or vice versa)
	// and the model silently sees one of the two views.
	tools := ch.agent.executor.GetTools()
	provider := ch.agent.provider
	estimate := func(msgs []Message) int {
		return provider.EstimateTokens(&ChatRequest{Messages: msgs, Tools: tools})
	}

	// Trigger gate: same threshold the loop's downstream check uses.
	triggerLimit := int(float64(contextSize) * ch.agent.triggerFractionOrDefault())
	if estimate(rawMessages) <= triggerLimit {
		return rawMessages
	}
	// Phase 0a targets the substitution fraction (default 0.50), well below
	// the trigger, so each substitution pass buys many turns of headroom
	// rather than re-substituting one checkpoint every turn.
	target := int(float64(contextSize) * ch.agent.substitutionTargetOrDefault())

	// Phase 0a: iterative checkpoint substitution.
	checkpoints := ch.agent.state.GetCheckpoints()
	substituted := false
	if len(checkpoints) > 0 {
		newMsgs, applied, under := IterativelySubstituteCheckpoints(rawMessages, checkpoints, target, estimate)
		substituted = applied > 0
		rawMessages = newMsgs
		if under {
			return rawMessages
		}
	}

	// Phase 0b: iterative observation masking. Only enabled when the
	// optimizer is wired — without it we can't resolve tool-call IDs to
	// human-readable tool names for the placeholder text.
	if ch.agent.optimizer != nil {
		nameFn := func(callID string) string {
			for _, m := range rawMessages {
				if m.Role != "assistant" {
					continue
				}
				for _, tc := range m.ToolCalls {
					if tc.ID == callID {
						return tc.Function.Name
					}
				}
			}
			if callID != "" {
				return callID
			}
			return "tool"
		}
		newMsgs, masked, _ := IterativelyMaskOldestConsumedToolResults(rawMessages, nameFn, target, estimate)
		// Masking is index-stable (same message count, content rewritten in
		// place) and ran on a slice derived directly from state, so persist it
		// — otherwise every iteration re-masks the same results. Only persist
		// when Phase 0a did not substitute: substitution changes the message
		// structure and is re-applied per-request from checkpoint indices by
		// design.
		if masked > 0 && !substituted {
			ch.agent.state.SetMessages(newMsgs)
		}
		rawMessages = newMsgs
	}

	return rawMessages
}

// sanitizeMessages applies sanitizeANSI to every message's content.
func sanitizeMessages(messages []Message) []Message {
	changed := false
	for i := range messages {
		if messages[i].Content != sanitizeANSI(messages[i].Content) {
			changed = true
		}
	}
	if !changed {
		return messages
	}
	// Only copy if something changed to avoid unnecessary allocation.
	out := make([]Message, len(messages))
	copy(out, messages)
	for i := range out {
		out[i].Content = sanitizeANSI(out[i].Content)
	}
	return out
}
