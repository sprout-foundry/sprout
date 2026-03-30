package agent

import (
	"sort"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

func (a *Agent) shiftTurnCheckpoints(delta int) {
	if a == nil || delta == 0 {
		return
	}
	a.checkpointMu.Lock()
	defer a.checkpointMu.Unlock()
	if len(a.turnCheckpoints) == 0 {
		return
	}
	for i := range a.turnCheckpoints {
		a.turnCheckpoints[i].StartIndex += delta
		a.turnCheckpoints[i].EndIndex += delta
	}
}

func (a *Agent) RecordTurnCheckpoint(startIndex, endIndex int) {
	if a == nil || startIndex < 0 || endIndex < startIndex || endIndex >= len(a.messages) {
		return
	}

	turnMessages := append([]api.Message(nil), a.messages[startIndex:endIndex+1]...)
	a.recordTurnCheckpointFromMessages(startIndex, endIndex, turnMessages)
}

func (a *Agent) RecordTurnCheckpointAsync(startIndex, endIndex int) {
	if a == nil || startIndex < 0 || endIndex < startIndex || endIndex >= len(a.messages) {
		return
	}

	// Snapshot the completed turn immediately so the background summary job never
	// depends on later message mutations for its source content. The retained
	// indices still refer to the original completed-turn range and are expected to
	// remain stable because normal post-completion flow only appends newer turns;
	// disruptive operations such as clear/import replace the checkpoint set.
	turnMessages := append([]api.Message(nil), a.messages[startIndex:endIndex+1]...)
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

	a.checkpointMu.Lock()
	defer a.checkpointMu.Unlock()
	if n := len(a.turnCheckpoints); n > 0 && a.turnCheckpoints[n-1].StartIndex == startIndex {
		a.turnCheckpoints[n-1] = checkpoint
		return
	}

	a.turnCheckpoints = append(a.turnCheckpoints, checkpoint)
	sort.Slice(a.turnCheckpoints, func(i, j int) bool {
		return a.turnCheckpoints[i].StartIndex < a.turnCheckpoints[j].StartIndex
	})
}

func (a *Agent) buildTurnCheckpointSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}
	if a != nil && a.optimizer != nil {
		return a.optimizer.buildGoCompactionSummary(messages)
	}
	optimizer := NewConversationOptimizer(true, false)
	return optimizer.buildGoCompactionSummary(messages)
}

func (a *Agent) buildActionableTurnCheckpointSummary(messages []api.Message) string {
	if len(messages) == 0 {
		return ""
	}
	var optimizer *ConversationOptimizer
	if a != nil && a.optimizer != nil {
		optimizer = a.optimizer
	} else {
		optimizer = NewConversationOptimizer(true, false)
	}
	return optimizer.buildActionableSummary(messages)
}

func (a *Agent) HasTurnCheckpoints() bool {
	if a == nil {
		return false
	}
	a.checkpointMu.RLock()
	defer a.checkpointMu.RUnlock()
	return len(a.turnCheckpoints) > 0
}

func (a *Agent) copyTurnCheckpoints() []TurnCheckpoint {
	if a == nil {
		return nil
	}
	a.checkpointMu.RLock()
	defer a.checkpointMu.RUnlock()
	return append([]TurnCheckpoint(nil), a.turnCheckpoints...)
}

func (a *Agent) replaceTurnCheckpoints(checkpoints []TurnCheckpoint) {
	if a == nil {
		return
	}
	a.checkpointMu.Lock()
	defer a.checkpointMu.Unlock()
	a.turnCheckpoints = append([]TurnCheckpoint(nil), checkpoints...)
}

func (a *Agent) clearTurnCheckpoints() {
	if a == nil {
		return
	}
	a.checkpointMu.Lock()
	defer a.checkpointMu.Unlock()
	a.turnCheckpoints = nil
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
	return compacted, remaining
}
