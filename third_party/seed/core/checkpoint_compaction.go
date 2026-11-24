package core

import (
	"maps"
	"sort"
	"time"
)

// isCheckpointSummary reports whether a message was inserted by a prior
// compaction pass. Synthetic summaries use role "user" so the strict-chat-
// template guard in BuildCheckpointCompactedMessages doesn't preserve them
// as if they were original user turns — otherwise re-application would
// duplicate the summary at the head of the new range.
func isCheckpointSummary(m Message) bool {
	return m.Role == "user" && m.Meta != nil && m.Meta[MetaKeyCheckpoint] == "true"
}

// cloneMessage deep-copies a message including its Meta map, so consumers
// can append it to their own slice without sharing map references.
func cloneMessage(m Message) Message {
	out := m
	if m.Meta != nil {
		out.Meta = maps.Clone(m.Meta)
	}
	return out
}

// RecordTurnCheckpointAsync asynchronously builds a checkpoint from the given
// messages and stores it in state. It spawns a goroutine to compute the summary
// so it doesn't block the conversation loop. The onCheckpoint callback, if
// non-nil, is invoked with the built checkpoint in its own goroutine after it
// is stored in state. If the callback panics, the panic is caught and the
// agent continues normally.
//
// The message slice is snapshotted immediately (before the goroutine starts) so
// the background computation sees a consistent view even if the caller mutates
// the original slice. If the summary computation takes longer than timeout, a
// minimal checkpoint is stored instead.
func RecordTurnCheckpointAsync(state *State, messages []Message, startIndex, endIndex int, timeout time.Duration, onCheckpoint func(TurnCheckpoint)) {
	// Snapshot messages immediately so the goroutine sees a consistent view.
	turnMessages := make([]Message, len(messages))
	copy(turnMessages, messages)

	go func() {
		done := make(chan TurnCheckpoint, 1)

		go func() {
			builder := NewTurnSummaryBuilder()
			cp := builder.Build(turnMessages)
			cp.StartIndex = startIndex
			cp.EndIndex = endIndex
			done <- cp
		}()

		var cp TurnCheckpoint
		select {
		case cp = <-done:
		case <-time.After(timeout):
			// Store minimal checkpoint if computation timed out.
			cp = TurnCheckpoint{
				StartIndex:        startIndex,
				EndIndex:          endIndex,
				Summary:           "Turn completed (summary timed out)",
				ActionableSummary: "Turn completed (summary timed out)",
			}
		}
		state.AddCheckpoint(cp)
		if onCheckpoint != nil {
			go func() {
				defer func() {
					recover()
				}()
				onCheckpoint(cp)
			}()
		}
	}()
}

// isConsumableCheckpoint determines whether a checkpoint can be compacted.
// A checkpoint is consumable when:
// - Its StartIndex and EndIndex are valid within the messages slice
// - StartIndex <= EndIndex
// - The checkpoint has a non-empty Summary
func isConsumableCheckpoint(cp TurnCheckpoint, messageCount int) bool {
	if cp.Summary == "" {
		return false
	}
	if cp.StartIndex < 0 || cp.EndIndex < 0 || cp.StartIndex > cp.EndIndex {
		return false
	}
	if cp.EndIndex >= messageCount {
		return false
	}
	return true
}

// BuildCheckpointCompactedMessages replaces consumed checkpoint ranges with
// summary messages and returns the compacted message list and updated checkpoints.
//
// A checkpoint is "consumable" when:
// - Its StartIndex and EndIndex are valid (within the messages slice)
// - The messages in the range [StartIndex, EndIndex] exist
// - The checkpoint has a non-empty Summary
//
// All checkpoint indices reference the raw state.Messages() slice. Because
// state.Messages() only appends (never inserts or deletes in the middle),
// checkpoint indices remain stable across calls. Consumed checkpoints are
// kept in the returned list so that every call to prepareMessages() can
// re-apply their summaries against the growing message history.
//
// The function works from oldest to newest checkpoint (by StartIndex):
//  1. For each consumable checkpoint, replace messages[StartIndex:EndIndex+1]
//     with a single summary message (role "user", content from checkpoint.ActionableSummary
//     if non-empty and ≤500 bytes, otherwise checkpoint.Summary)
//  2. Handle consecutive-assistant boundaries: if the inserted summary message
//     would create two consecutive assistant messages with no tool calls between
//     them, merge or adjust.
//
// The summary message role is "user" to maintain proper conversation flow
// (system -> user -> assistant pattern).
//
// Return:
//   - compactedMessages: the new message slice with checkpoints applied
//   - updatedCheckpoints: all checkpoints preserved with their original indices
//     (consumed checkpoints are kept so future calls re-apply their summaries)
func BuildCheckpointCompactedMessages(messages []Message, checkpoints []TurnCheckpoint) ([]Message, []TurnCheckpoint) {
	// Guard: nothing to do with empty inputs.
	if len(messages) == 0 || len(checkpoints) == 0 {
		outMsgs := make([]Message, len(messages))
		copy(outMsgs, messages)
		outCps := make([]TurnCheckpoint, len(checkpoints))
		copy(outCps, checkpoints)
		return outMsgs, outCps
	}

	// Work on a copy so we never mutate the original.
	msgs := make([]Message, len(messages))
	copy(msgs, messages)

	// Sort a copy of checkpoints by StartIndex so we process oldest first.
	sorted := make([]TurnCheckpoint, len(checkpoints))
	copy(sorted, checkpoints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIndex < sorted[j].StartIndex
	})

	// Phase 1: Walk through sorted checkpoints and determine which are
	// consumable. Record consumable ranges for replacement in Phase 2.
	// All checkpoints (consumed and unconsumed) are kept in the returned
	// list so that every prepareMessages() call can re-apply summaries
	// against the growing state.Messages() history.
	//
	// We track lastConsumedEnd to reject overlapping ranges.
	type rangeInfo struct {
		summary           string
		actionableSummary string
		origStart         int // position in original msgs array
		origEnd           int // inclusive end in original msgs array
	}
	var consumables []rangeInfo
	lastConsumedEnd := -1 // highest original index consumed by a previous checkpoint

	for _, cp := range sorted {
		// Reject if not consumable (invalid range or empty summary).
		if !isConsumableCheckpoint(cp, len(msgs)) {
			continue
		}

		// Reject if this checkpoint overlaps with a previously consumed range.
		if cp.StartIndex <= lastConsumedEnd {
			continue
		}

		// ArchiveLine takes priority when meta-compaction has already folded
		// this checkpoint into the session archive — the model gets a
		// 1-line breadcrumb instead of a paragraph summary.
		summaryText := cp.Summary
		actionableText := cp.ActionableSummary
		if cp.ArchiveLine != "" {
			summaryText = cp.ArchiveLine
			actionableText = ""
		}
		ri := rangeInfo{
			summary:           summaryText,
			actionableSummary: actionableText,
			origStart:         cp.StartIndex,
			origEnd:           cp.EndIndex,
		}
		consumables = append(consumables, ri)

		lastConsumedEnd = cp.EndIndex
	}

	// If nothing was consumed, return copies of the original data.
	if len(consumables) == 0 {
		outMsgs := make([]Message, len(msgs))
		copy(outMsgs, msgs)
		outCps := make([]TurnCheckpoint, len(checkpoints))
		copy(outCps, checkpoints)
		return outMsgs, outCps
	}

	// Phase 2: Build compacted messages by walking through msgs and
	// replacing consumed ranges with summary messages.
	newMsgs := make([]Message, 0, len(msgs))
	msgIdx := 0
	consIdx := 0

	for msgIdx < len(msgs) {
		if consIdx < len(consumables) && msgIdx == consumables[consIdx].origStart {
			ri := consumables[consIdx]

			// Preserve the leading user message of the checkpoint range if it's
			// a real user turn. Strict chat templates (e.g., Qwen3.5) require
			// every assistant message to be preceded by a real user message —
			// a synthetic summary at position N is read by the model as the new
			// query, dropping the original request from its context. We keep
			// the user's original message and follow it with the summary, so
			// the model sees: real query → summary of what happened → ...
			// Skip when the leading message is already a synthetic summary
			// (role=user but Meta[checkpoint]=true) to avoid duplicating
			// summaries on re-application of already-consumed checkpoints.
			head := msgs[ri.origStart]
			if head.Role == "user" && !isCheckpointSummary(head) {
				newMsgs = append(newMsgs, cloneMessage(head))
			}

			// Choose summary content: prefer ActionableSummary if ≤500 bytes;
			// fall back to Summary otherwise.
			// len() uses byte count — acceptable because actionable summaries
			// are ASCII-only bullet lists generated by buildActionableSummary().
			content := ri.summary
			if len(ri.actionableSummary) > 0 && len(ri.actionableSummary) <= 500 {
				content = ri.actionableSummary
			}
			newMsgs = append(newMsgs, Message{
				Role:    "user",
				Content: content,
				Meta:    map[string]string{MetaKeyCheckpoint: "true"},
			})
			msgIdx = ri.origEnd + 1
			consIdx++
			continue
		}
		// Deep-copy Meta to avoid shared map references.
		msg := msgs[msgIdx]
		if msg.Meta != nil {
			msg.Meta = maps.Clone(msg.Meta)
		}
		newMsgs = append(newMsgs, msg)
		msgIdx++
	}

	// Phase 3: Resolve consecutive-assistant boundaries.
	// Since summaries are inserted with role "user", this should rarely fire,
	// but it guards against edge cases (e.g., ranges containing only assistant msgs).
	newMsgs = resolveConsecutiveAssistantMessages(newMsgs)

	// Phase 4: Return all checkpoints with their original indices intact.
	// Checkpoint indices always reference the raw state.Messages() slice,
	// which only appends (never inserts/deletes in the middle). Consumed
	// checkpoints are kept so future prepareMessages() calls re-apply their
	// summaries against the growing message history.
	outCps := make([]TurnCheckpoint, len(checkpoints))
	copy(outCps, checkpoints)

	return newMsgs, outCps
}

// IterativelySubstituteCheckpoints replaces checkpointed turns with their
// summaries *one at a time*, oldest first, until either:
//   - the resulting message list's estimated token count is at or below the
//     supplied target (success), or
//   - every available checkpoint has been substituted and the result is still
//     above target (caller falls through to Phase 1+ in the compaction
//     cascade).
//
// This is the Phase 0a primitive. The key property is
// *minimum information loss*: we never collapse a turn into its summary
// unless that's the smallest step that can relieve context pressure.
// Short-and-medium conversations get to keep their raw history.
//
// Returns:
//   - newMessages: the compacted message list (or the original if applied == 0)
//   - applied: how many checkpoints were substituted (0 = none, len(sorted) = all)
//   - under: true if applied substitutions brought estimate to or below target
//
// The estimateFn is supplied by the caller so it can use the live provider's
// tokenizer (which costs a function call per iteration but stays accurate).
// For very-large checkpoint lists this is O(applied × len(messages)) work;
// in practice applied is small because each substitution typically saves
// thousands of tokens.
func IterativelySubstituteCheckpoints(
	messages []Message,
	checkpoints []TurnCheckpoint,
	target int,
	estimateFn func([]Message) int,
) ([]Message, int, bool) {
	if len(messages) == 0 || len(checkpoints) == 0 || target <= 0 {
		return messages, 0, estimateFn(messages) <= target
	}

	// Sort a copy of checkpoints by StartIndex so oldest is first. We must
	// not mutate the caller's slice — state.GetCheckpoints already returns
	// a copy, but defensive copy is cheap.
	sorted := make([]TurnCheckpoint, len(checkpoints))
	copy(sorted, checkpoints)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].StartIndex < sorted[j].StartIndex
	})

	// Early exit: already under target without doing anything.
	if estimateFn(messages) <= target {
		return messages, 0, true
	}

	// Apply checkpoints incrementally: n=1 (oldest only), then n=2 (oldest two), etc.
	// Each iteration rebuilds the compacted message list from the raw input
	// using the prefix of `sorted` of length n. This is conceptually clean
	// (BuildCheckpointCompactedMessages is the one source of truth for the
	// substitution mechanics — index shifting, role smoothing, etc.) at the
	// cost of re-applying earlier substitutions on each step. For a
	// 30-checkpoint session that's ~30 small passes; cheap.
	var lastResult []Message = messages
	for n := 1; n <= len(sorted); n++ {
		active := sorted[:n]
		candidate, _ := BuildCheckpointCompactedMessages(messages, active)
		lastResult = candidate
		if estimateFn(candidate) <= target {
			return candidate, n, true
		}
	}

	// Exhausted: every checkpoint applied, still over target. Caller proceeds
	// to Phase 1+ (drop summaries, drop turns, emergency truncate).
	return lastResult, len(sorted), false
}

// resolveConsecutiveAssistantMessages fixes consecutive assistant messages in the
// compacted result. This can happen when checkpoint ranges are replaced and the
// boundary messages happen to be assistant role.
//
// Strategy (applied left-to-right):
//  1. Both messages have no tool calls → merge content into the first, drop the second.
//  2. First has tool calls, second does not → merge content, drop the second.
//  3. First has no tool calls, second does → merge content into the first and
//     transfer the tool calls, then drop the second.
//  4. Both have tool calls → merge content, drop the second.
//
// Note: Images and ToolCallID from the dropped (second) message are not preserved.
// In well-formed OpenAI-format conversations, assistant messages never carry
// ToolCallID (that field belongs to tool-result messages), so this is not a
// practical concern.
//
// The function works on a copy of the input slice so it never mutates the caller's
// data. After each merge/drop the loop index is decremented so the next pair is checked.
func resolveConsecutiveAssistantMessages(msgs []Message) []Message {
	// Work on a copy so we never mutate the caller's slice.
	out := make([]Message, len(msgs))
	copy(out, msgs)

	for i := 1; i < len(out); i++ {
		if out[i-1].Role != "assistant" || out[i].Role != "assistant" {
			continue
		}

		prevHasTools := len(out[i-1].ToolCalls) > 0
		currHasTools := len(out[i].ToolCalls) > 0

		switch {
		case !prevHasTools && !currHasTools:
			// Both plain — merge content.
			out[i-1].Content += "\n" + out[i].Content

		case !prevHasTools && currHasTools:
			// Second carries tool calls — merge content and transfer them.
			out[i-1].Content += "\n" + out[i].Content
			out[i-1].ToolCalls = out[i].ToolCalls

		default:
			// First already has tool calls (or both do) — merge content and drop the second.
			out[i-1].Content += "\n" + out[i].Content
		}

		// Remove the second message and re-check this index.
		out = append(out[:i], out[i+1:]...)
		i--
	}
	return out
}
