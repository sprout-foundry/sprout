package agent

import (
	"context"
	"sort"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/redact"
)

// collectCheckpointFileMetadata returns the file-change manifest + revision
// ID to embed in the turn checkpoint about to be recorded. Pulls from the
// agent's ChangeTracker via its checkpoint watermark so each checkpoint's
// manifest covers only the turn's own writes. Returns (nil, "") when
// tracking isn't enabled.
func (a *Agent) collectCheckpointFileMetadata() ([]CheckpointFileChange, string) {
	if a == nil {
		return nil, ""
	}
	tracker := a.GetChangeTracker()
	if tracker == nil {
		return nil, ""
	}
	return tracker.CollectFileChangesForCheckpoint()
}

// appendFileMetadataToSummary glues the git-style file manifest + revision
// pointer onto the end of an actionable summary string so the model sees
// them once this turn is substituted for its summary text. Returns the
// original string unchanged when no metadata is available.
//
// Output shape (added as additional bullet lines):
//
//	- Files: A pkg/auth/session.go, M pkg/auth/jwt.go
//	- Revision: rev-7a3c2e (call view_history with this revision_id to inspect the diff)
func appendFileMetadataToSummary(summary string, changes []CheckpointFileChange, revisionID string) string {
	if len(changes) == 0 && revisionID == "" {
		return summary
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(summary, "\n"))
	if len(changes) > 0 {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- Files: ")
		for i, c := range changes {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(c.Op)
			b.WriteString(" ")
			b.WriteString(c.Path)
		}
	}
	if revisionID != "" {
		if b.Len() > 0 {
			b.WriteString("\n")
		}
		b.WriteString("- Revision: ")
		b.WriteString(revisionID)
		b.WriteString(" (call view_history with this revision_id to inspect the diff)")
	}
	return b.String()
}

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
	// SP-046 §7: turn boundary — the next turn must call read_file again
	// before writing the same paths.
	a.ResetFileReadsForNewTurn()
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
	// SP-046 §7: reset the read tracker synchronously even though the
	// summary job runs in the background — the next turn's tool calls
	// must not see the previous turn's reads.
	a.ResetFileReadsForNewTurn()
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

	// Capture file-change manifest + revision pointer from the agent's
	// ChangeTracker (if tracking is enabled). Seed's checkpoint substitution
	// surfaces these to the model so it can call view_history when it needs
	// the exact diff for a turn that's been collapsed to a summary.
	fileChanges, revisionID := a.collectCheckpointFileMetadata()

	// Append the git-style manifest + revision pointer to the actionable
	// summary text so the model sees them when this turn is later
	// substituted for its summary. Without this the structured fields in
	// the TurnCheckpoint are model-invisible (only Summary/ActionableSummary
	// reach the prompt via BuildCheckpointCompactedMessages).
	actionableSummary = appendFileMetadataToSummary(actionableSummary, fileChanges, revisionID)

	checkpoint := TurnCheckpoint{
		StartIndex:        startIndex,
		EndIndex:          endIndex,
		Summary:           summary,
		ActionableSummary: actionableSummary,
		FileChanges:       fileChanges,
		RevisionID:        revisionID,
	}

	// Extract the first user message content for embedding.
	var userPrompt string
	for _, msg := range turnMessages {
		if msg.Role == "user" && msg.Content != "" {
			userPrompt = msg.Content
			break
		}
	}

	// Record checkpoint under mutex — capture embedding decision and related
	// data inside the lock so the embedding call can run *after* release.
	shouldEmbed := false
	var turnNumber int
	var sessionID, workspaceRoot string

	func() {
		mu := a.state.GetCheckpointMutex()
		mu.Lock()
		defer mu.Unlock()
		checkpoints := a.state.GetTurnCheckpoints()
		if n := len(checkpoints); n > 0 && checkpoints[n-1].StartIndex == startIndex {
			checkpoints[n-1] = checkpoint
			a.state.SetTurnCheckpoints(checkpoints)
		} else {
			checkpoints = append(checkpoints, checkpoint)
			sort.Slice(checkpoints, func(i, j int) bool {
				return checkpoints[i].StartIndex < checkpoints[j].StartIndex
			})
			a.state.SetTurnCheckpoints(checkpoints)
		}

		// Capture embedding decision while still holding the lock so all
		// related values come from the same consistent state snapshot.
		if a.embeddingMgr != nil && userPrompt != "" && len(checkpoints) > 0 {
			shouldEmbed = true
			sessionID = a.state.GetSessionID()
			workspaceRoot = a.currentWorkspaceRoot()
			for i, cp := range checkpoints {
				if cp.StartIndex == startIndex {
					turnNumber = i + 1 // 1-based
					break
				}
			}
		}
	}()

	// Embed and store the turn *after* releasing the mutex so embedding I/O
	// does not block concurrent checkpoint access.
	if shouldEmbed {
		// Redact secrets before embedding to avoid persisting them in the
		// embedding store's conversation_turns index.
		safeUserPrompt := redact.String(userPrompt)
		safeActionableSummary := redact.String(actionableSummary)

		turn, err := NewConversationTurn(sessionID, turnNumber, safeUserPrompt, workspaceRoot)
		if err == nil {
			turn.ActionableSummary = safeActionableSummary
			// FilesTouched, Duration, TokenUsage are left as zero values to be enriched later
			_ = EmbedAndStoreTurn(context.Background(), a.embeddingMgr, turn)

			// Set session intent embedding from the first turn's prompt embedding.
			// Uses atomic check-and-set to avoid TOCTOU races with concurrent turns.
			a.state.SetSessionIntentEmbeddingIfNil(turn.PromptEmbedding)
		}
	}
}

func (a *Agent) buildTurnCheckpointSummary(messages []api.Message) string {
	return buildTurnCheckpointGoSummary(messages)
}

func (a *Agent) buildActionableTurnCheckpointSummary(messages []api.Message) string {
	return buildTurnCheckpointActionableSummary(messages)
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
				a.Logger().Debug("[clean] Removed consecutive assistant at compaction boundary\n")
			}
			compacted = append(compacted[:lastSummaryIdx+1], compacted[lastSummaryIdx+2:]...)
		}
	}

	return compacted, remaining
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
