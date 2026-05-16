package agent

import (
	"sort"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func (a *Agent) shiftTurnCheckpoints(delta int) {
	if a == nil || delta == 0 {
		return
	}
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()
	checkpoints := a.state.GetTurnCheckpoints()
	if len(checkpoints) == 0 {
		return
	}
	for i := range checkpoints {
		checkpoints[i].StartIndex += delta
		checkpoints[i].EndIndex += delta
	}
	a.state.SetTurnCheckpoints(checkpoints)
}

func (a *Agent) RecordTurnCheckpoint(startIndex, endIndex int) {
	msgs := a.state.GetMessages()
	if a == nil || startIndex < 0 || endIndex < startIndex || endIndex >= len(msgs) {
		return
	}

	turnMessages := append([]api.Message(nil), msgs[startIndex:endIndex+1]...)
	a.recordTurnCheckpointFromMessages(startIndex, endIndex, turnMessages)
}

func (a *Agent) RecordTurnCheckpointAsync(startIndex, endIndex int) {
	msgs := a.state.GetMessages()
	if a == nil || startIndex < 0 || endIndex < startIndex || endIndex >= len(msgs) {
		return
	}

	// Snapshot the completed turn immediately so the background summary job never
	// depends on later message mutations for its source content. The retained
	// indices still refer to the original completed-turn range and are expected to
	// remain stable because normal post-completion flow only appends newer turns;
	// disruptive operations such as clear/import replace the checkpoint set.
	turnMessages := append([]api.Message(nil), msgs[startIndex:endIndex+1]...)
	go a.recordTurnCheckpointFromMessages(startIndex, endIndex, turnMessages)
}

func (a *Agent) recordTurnCheckpointFromMessages(startIndex, endIndex int, turnMessages []api.Message) {
	if a == nil || len(turnMessages) == 0 {
		return
	}

	summary := a.buildTurnCheckpointSummary(turnMessages)
	if strings.TrimSpace(summary) == "" {
		return
	}

	actionableSummary := a.buildActionableTurnCheckpointSummary(turnMessages)

	checkpoint := TurnCheckpoint{
		StartIndex:        startIndex,
		EndIndex:          endIndex,
		Summary:           summary,
		ActionableSummary: actionableSummary,
	}

	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()
	checkpoints := a.state.GetTurnCheckpoints()
	if n := len(checkpoints); n > 0 && checkpoints[n-1].StartIndex == startIndex {
		checkpoints[n-1] = checkpoint
		a.state.SetTurnCheckpoints(checkpoints)
		return
	}

	checkpoints = append(checkpoints, checkpoint)
	sort.Slice(checkpoints, func(i, j int) bool {
		return checkpoints[i].StartIndex < checkpoints[j].StartIndex
	})
	a.state.SetTurnCheckpoints(checkpoints)
}

func (a *Agent) buildTurnCheckpointSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}
	if a != nil && a.state.GetOptimizer() != nil {
		return a.state.GetOptimizer().buildGoCompactionSummary(messages)
	}
	optimizer := NewConversationOptimizer(true, false)
	return optimizer.buildGoCompactionSummary(messages)
}

func (a *Agent) buildActionableTurnCheckpointSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}
	var optimizer *ConversationOptimizer
	if a != nil && a.state.GetOptimizer() != nil {
		optimizer = a.state.GetOptimizer()
	} else {
		optimizer = NewConversationOptimizer(true, false)
	}
	return optimizer.buildActionableSummary(messages)
}

func (a *Agent) HasTurnCheckpoints() bool {
	if a == nil {
		return false
	}
	mu := a.state.GetCheckpointMutex()
	mu.RLock()
	defer mu.RUnlock()
	return len(a.state.GetTurnCheckpoints()) > 0
}

func (a *Agent) copyTurnCheckpoints() []TurnCheckpoint {
	if a == nil {
		return nil
	}
	mu := a.state.GetCheckpointMutex()
	mu.RLock()
	defer mu.RUnlock()
	return append([]TurnCheckpoint(nil), a.state.GetTurnCheckpoints()...)
}

func (a *Agent) ReplaceTurnCheckpoints(checkpoints []TurnCheckpoint) {
	if a == nil {
		return
	}
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()
	a.state.SetTurnCheckpoints(append([]TurnCheckpoint(nil), checkpoints...))
}

func (a *Agent) clearTurnCheckpoints() {
	if a == nil {
		return
	}
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()
	a.state.SetTurnCheckpoints(nil)
}

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

		// This checkpoint is consumed (applied to the compaction)
		compacted = append(compacted, messages[nextIndex:checkpoint.StartIndex]...)

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
				a.debugLog("[clean] Removed consecutive assistant at compaction boundary\n")
			}
			compacted = append(compacted[:lastSummaryIdx+1], compacted[lastSummaryIdx+2:]...)
		}
	}

	return compacted, remaining
}

// TriggerCompaction forces a compaction of the conversation history.
// This is called when an API error indicates the context window limit was exceeded.
// Returns true if compaction was applied, false if nothing could be compacted.
func (a *Agent) TriggerCompaction() bool {
	if a == nil {
		return false
	}

	msgs := a.state.GetMessages()

	// Try checkpoint compaction first (lighter weight)
	if a.HasTurnCheckpoints() {
		checkpointed, remaining := a.BuildCheckpointCompactedMessages(msgs)
		if len(checkpointed) < len(msgs) {
			a.state.SetMessages(checkpointed)
			a.ReplaceTurnCheckpoints(remaining)
			if a.debug {
				a.debugLog("[~] Context limit exceeded - applied checkpoint compaction\n")
			}
			return true
		}
	}

	// Try structural compaction (LLM-based)
	optimizer := a.state.GetOptimizer()
	if optimizer != nil && optimizer.IsEnabled() {
		// Run observation masking first (dedup + consumed-tool-result masking),
		// then structural compaction on the optimized messages. This matches the
		// normal pruning pipeline in conversation_pruner.go.
		optimized := optimizer.OptimizeConversation(msgs)
		llmCompacted := optimizer.CompactConversation(optimized)
		if len(llmCompacted) < len(msgs) {
			a.state.SetMessages(llmCompacted)
			a.clearTurnCheckpoints()
			if a.debug {
				a.debugLog("[~] Context limit exceeded - applied LLM structural compaction\n")
			}
			return true
		}
	}

	// Last resort: emergency truncation
	// Keep at least 2 non-system messages to preserve the last conversation turn
	if len(msgs) > 2 {
		// Determine where to start (skip system prompt if present)
		keepStart := 0
		if len(msgs) > 0 && msgs[0].Role == "system" {
			keepStart = 1
		}
		// Only truncate if we'd still have at least 2 messages after truncation
		if len(msgs)-keepStart > 2 {
			keepEnd := len(msgs)
			a.state.SetMessages(append(msgs[:keepStart:keepStart], msgs[keepEnd-2:]...))
			a.clearTurnCheckpoints()
			if a.debug {
				a.debugLog("[~] Context limit exceeded - applied emergency truncation\n")
			}
			return true
		}
	}

	return false
}
