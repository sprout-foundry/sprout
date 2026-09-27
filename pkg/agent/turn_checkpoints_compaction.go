package agent

// turn_checkpoints_compaction.go — the compaction layer over turn
// checkpoints: BuildCheckpointCompactedMessages and its helpers
// (expandCheckpointRangeForToolResults, dropOrphanToolMessages).
// Split out of turn_checkpoints.go.
import (
	"sort"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func (a *Agent) BuildCheckpointCompactedMessages(messages []api.Message) ([]api.Message, []TurnCheckpoint) {
	checkpoints := a.copyTurnCheckpoints()
	if len(checkpoints) == 0 || len(messages) == 0 {
		return messages, checkpoints
	}
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})

	compacted := make([]api.Message, 0, len(messages))
	remaining := make([]TurnCheckpoint, 0, len(checkpoints))
	nextIndex := 0
	cumulativeDelta := 0 // tracks how many fewer messages exist after each consumed checkpoint
	lastSummaryIdx := -1 // track index of last inserted summary for boundary checking

	for _, checkpoint := range checkpoints {
		if checkpoint.StartIndex < nextIndex {
			continue
		}
		if checkpoint.StartIndex < 0 || checkpoint.EndIndex < checkpoint.StartIndex || checkpoint.EndIndex >= len(messages) {
			continue
		}

		// Expand EndIndex to absorb any trailing tool messages whose tool_call_id
		// references an assistant message within the checkpoint range. Without this,
		// partial coverage of an assistant+tool_calls block leaves orphan tool
		// messages in the conversation — providers with strict tool-call syntax
		// (MiniMax, DeepSeek) reject the whole request as
		// "tool call result does not follow tool call".
		expandedEnd := expandCheckpointRangeForToolResults(messages, checkpoint.StartIndex, checkpoint.EndIndex)
		if expandedEnd > checkpoint.EndIndex {
			checkpoint.EndIndex = expandedEnd
		}

		// This checkpoint is consumed (applied to the compaction)
		compacted = append(compacted, messages[nextIndex:checkpoint.StartIndex]...)

		// When this checkpoint is about to drop a user message at
		// the boundary (messages[nextIndex] with role="user"), preserve it in the
		// compacted output before inserting the assistant summary. Without this,
		// strict-syntax chat templates (Qwen3.5, any provider with raise_exception
		// guards) reject the request with "No user query found in messages" because
		// the compacted conversation would contain zero role:user entries.
		//
		// The condition: nextIndex==checkpoint.StartIndex means the slice
		// messages[nextIndex:checkpoint.StartIndex] is empty — i.e., there are no
		// messages to preserve from the gap, and messages[nextIndex] (which is
		// about to be consumed) would be dropped. We preserve it if it's a user
		// message so the conversation always has at least one user turn for strict
		// chat templates that require it.
		if nextIndex == checkpoint.StartIndex && nextIndex < len(messages) && messages[nextIndex].Role == "user" {
			compacted = append(compacted, messages[nextIndex])
		}

		// FIX 4: Use ActionableSummary if available, prepended to the base summary.
		summaryText := checkpoint.Summary
		if checkpoint.ActionableSummary != "" {
			summaryText = checkpoint.ActionableSummary + "\n\n" + checkpoint.Summary
		}
		compacted = append(compacted, api.Message{
			Role:    "assistant",
			Content: summaryText,
		})

		// Track the index of this summary message for boundary checking later
		lastSummaryIdx = len(compacted) - 1

		// This checkpoint replaced (EndIndex - StartIndex + 1) messages with 1 summary message.
		replacedCount := checkpoint.EndIndex - checkpoint.StartIndex + 1
		cumulativeDelta += replacedCount - 1 // 1 summary replaces N messages

		nextIndex = checkpoint.EndIndex + 1
	}

	// Collect remaining (unused) checkpoints whose ranges didn't overlap the consumed ranges,
	// then shift their indices to account for the compaction shrinkage.
	for _, cp := range checkpoints {
		if cp.StartIndex < 0 || cp.EndIndex < cp.StartIndex || cp.EndIndex >= len(messages) {
			continue
		}
		if cp.StartIndex < nextIndex {
			// Already consumed or overlapped — skip
			continue
		}
		// Shift indices by the cumulative delta of all consumed checkpoints that came before
		remaining = append(remaining, TurnCheckpoint{
			StartIndex:        cp.StartIndex - cumulativeDelta,
			EndIndex:          cp.EndIndex - cumulativeDelta,
			Summary:           cp.Summary,
			ActionableSummary: cp.ActionableSummary,
		})
	}

	compacted = append(compacted, messages[nextIndex:]...)

	// Defense in depth: walk the final compacted slice and drop any orphan
	// tool messages that survived all other paths (manual edits, restored
	// sessions, rollups from prior sessions). An orphan is a tool-role
	// message whose tool_call_id has no parent assistant tool_calls block
	// immediately preceding it.
	compacted = dropOrphanToolMessages(compacted, a.debug)

	// Belt-and-suspenders: ensure at least one user message exists in
	// the compacted output. Strict-syntax chat templates (Qwen3.5, others with
	// raise_exception) reject requests with zero role:user entries. This guards
	// against future code path changes that might bypass the per-checkpoint
	// preservation above.
	//
	// lastSummaryIdx is intentionally NOT updated when we prepend the fallback.
	// The fallback lands at index 0, and lastSummaryIdx still points to the last
	// *real* summary — so the consecutive-assistant boundary check below still
	// scans the correct pair (summary, message-after-summary), unaffected by the
	// prepended fallback.
	hasUserMessage := false
	for _, m := range compacted {
		if m.Role == "user" {
			hasUserMessage = true
			break
		}
	}
	if !hasUserMessage && len(compacted) > 0 {
		// Inject a minimal fallback user message. Prefer the original task
		// content from messages[0] if it was a user message, otherwise use a
		// generic placeholder.
		fallbackContent := "Continue the task."
		if len(messages) > 0 && messages[0].Role == "user" && messages[0].Content != "" {
			fallbackContent = messages[0].Content
		}
		fallbackMsg := api.Message{
			Role:    "user",
			Content: fallbackContent,
		}
		compacted = append([]api.Message{fallbackMsg}, compacted...)
		if a.debug {
			a.Logger().Debug("[compaction] injected fallback user message — no user role found in compacted output\n")
		}
	}

	// FIX: Ensure we don't have consecutive assistant messages at the boundary.
	// If the last inserted summary is followed by an assistant message without tool_calls,
	// remove the following assistant message to avoid llama.cpp error:
	// "Cannot have 2 or more assistant messages at the end of the list"
	//
	// Note: lastSummaryIdx is only set if at least one checkpoint was consumed.
	// If no checkpoints were consumed, lastSummaryIdx remains -1 and this check is skipped.
	if lastSummaryIdx >= 0 && lastSummaryIdx+1 < len(compacted) {
		if compacted[lastSummaryIdx].Role == "assistant" && len(compacted[lastSummaryIdx].ToolCalls) == 0 &&
			compacted[lastSummaryIdx+1].Role == "assistant" && len(compacted[lastSummaryIdx+1].ToolCalls) == 0 {
			// Remove the duplicate assistant message (keep the summary, remove the original)
			if a.debug {
				a.Logger().Debug("[clean] Removed consecutive assistant at compaction boundary\n")
			}
			compacted = append(compacted[:lastSummaryIdx+1], compacted[lastSummaryIdx+2:]...)
		}
	}

	return compacted, remaining
}

// expandCheckpointRangeForToolResults grows endIndex to include any trailing
// tool-role messages whose tool_call_id matches an assistant tool_calls
// block inside [startIndex, endIndex]. Bounds the expansion to the message
// slice so we never run past the end.
func expandCheckpointRangeForToolResults(messages []api.Message, startIndex, endIndex int) int {
	if endIndex >= len(messages)-1 {
		return endIndex
	}

	// Collect the set of tool_call_ids declared by assistant messages in the range.
	parentIDs := make(map[string]struct{})
	for i := startIndex; i <= endIndex; i++ {
		m := messages[i]
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				parentIDs[tc.ID] = struct{}{}
			}
		}
	}
	if len(parentIDs) == 0 {
		return endIndex
	}

	// Walk forward as long as we keep finding tool messages that match an
	// in-range parent. We don't expand across other role boundaries — once
	// we hit a non-tool message (assistant, user, system) we stop, because
	// that message was intentionally excluded from the checkpoint.
	for endIndex+1 < len(messages) && messages[endIndex+1].Role == "tool" {
		if _, ok := parentIDs[messages[endIndex+1].ToolCallID]; !ok {
			break
		}
		endIndex++
	}
	return endIndex
}

// dropOrphanToolMessages scans messages in order and drops tool-role messages
// whose tool_call_id has no preceding assistant tool_calls block with a
// matching ID. Returns the cleaned slice. Used as a final invariant guard
// before the conversation reaches a strict-syntax provider.
func dropOrphanToolMessages(messages []api.Message, debug bool) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	// Build a set of every tool_call_id any assistant message in this
	// conversation has ever declared. An orphan is a tool message whose
	// tool_call_id isn't in this set — that means no parent assistant
	// exists anywhere upstream of it.
	knownIDs := make(map[string]struct{})
	for _, m := range messages {
		if m.Role != "assistant" {
			continue
		}
		for _, tc := range m.ToolCalls {
			if tc.ID != "" {
				knownIDs[tc.ID] = struct{}{}
			}
		}
	}

	out := make([]api.Message, 0, len(messages))
	dropped := 0
	for _, m := range messages {
		if m.Role == "tool" && m.ToolCallID != "" {
			if _, ok := knownIDs[m.ToolCallID]; !ok {
				dropped++
				continue
			}
		}
		out = append(out, m)
	}
	if debug && dropped > 0 {
		_ = dropped // debug-only counter; log via caller if needed
	}
	return out
}

// TriggerCompaction used to live here as a 3-tier compaction fallback
// (checkpoint → LLM-summary → emergency truncate). It was never wired into
// a live call path. Context-limit recovery now happens inside seed's chat
// loop and retry layer via core.Options.LLMSummarizer / Options.Pruner /
// Options.CompactionTriggerFraction (set in seed_integration.go), so this
// duplicate path is no longer needed. The TurnCheckpoint primitives above
// (HasTurnCheckpoints, BuildCheckpointCompactedMessages,
// ReplaceTurnCheckpoints) remain because pkg/agent_commands/compact.go
// still uses them for the /compact slash command.
