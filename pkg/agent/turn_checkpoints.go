package agent

import (
	"sort"
	"strings"

	"github.com/google/uuid"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// newCheckpointID returns a stable identifier for a new TurnCheckpoint.
// Used by rollups to reference source checkpoints via SourceCheckpointIDs
// independently of slice position.
func newCheckpointID() string {
	return "cp-" + uuid.NewString()
}

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
//   - Files: A pkg/auth/session.go, M pkg/auth/jwt.go
//   - Revision: rev-7a3c2e (call view_history with this revision_id to inspect the diff)
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

func (a *Agent) RecordTurnCheckpoint(startIndex, endIndex int) {
	msgs := a.state.GetMessages()
	if a == nil || startIndex < 0 || endIndex < startIndex || endIndex >= len(msgs) {
		return
	}

	turnMessages := append([]api.Message(nil), msgs[startIndex:endIndex+1]...)
	a.recordTurnCheckpointFromMessages(startIndex, endIndex, turnMessages)
	// Turn boundary — the next turn must call read_file again
	// before writing the same paths.
	a.ResetFileReadsForNewTurn()
}

// rebaseTurnCheckpoints shifts every checkpoint's StartIndex/EndIndex
// through a compaction survivor map (old message index -> new message
// index) and drops checkpoints whose range lost an endpoint. Called from
// syncSeedStateToSprout after seed persisted a mid-turn compaction: the
// message list shrank, so unrebased indices would point at the wrong
// messages (the exact corruption class fixed for session-name shifts in
// 95feea807 — substitution would replace the wrong ranges).
func (a *Agent) rebaseTurnCheckpoints(survivorOf map[int]int) {
	if a == nil || a.state == nil || len(survivorOf) == 0 {
		return
	}
	mu := a.state.GetCheckpointMutex()
	mu.Lock()
	defer mu.Unlock()

	checkpoints := a.state.GetTurnCheckpoints()
	if len(checkpoints) == 0 {
		return
	}
	out := make([]TurnCheckpoint, 0, len(checkpoints))
	dropped := 0
	for _, cp := range checkpoints {
		newStart, okS := survivorOf[cp.StartIndex]
		newEnd, okE := survivorOf[cp.EndIndex]
		if !okS || !okE || newEnd < newStart {
			dropped++
			continue
		}
		cp.StartIndex = newStart
		cp.EndIndex = newEnd
		out = append(out, cp)
	}
	if dropped == 0 && len(out) == len(checkpoints) {
		return // nothing moved — skip the state write
	}
	a.state.SetTurnCheckpoints(out)
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
	// Reset the read tracker synchronously even though the
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
		ID:                newCheckpointID(),
		StartIndex:        startIndex,
		EndIndex:          endIndex,
		Summary:           summary,
		ActionableSummary: actionableSummary,
		FileChanges:       fileChanges,
		RevisionID:        revisionID,
	}

	// Record checkpoint under mutex.
	func() {
		mu := a.state.GetCheckpointMutex()
		mu.Lock()
		defer mu.Unlock()
		checkpoints := a.state.GetTurnCheckpoints()
		if n := len(checkpoints); n > 0 && checkpoints[n-1].StartIndex == startIndex {
			// Preserve the prior ID so any rollup that already referenced
			// this checkpoint via SourceCheckpointIDs remains valid.
			if existing := checkpoints[n-1].ID; existing != "" {
				checkpoint.ID = existing
			}
			checkpoints[n-1] = checkpoint
			a.state.SetTurnCheckpoints(checkpoints)
		} else {
			checkpoints = append(checkpoints, checkpoint)
			sort.Slice(checkpoints, func(i, j int) bool {
				return checkpoints[i].StartIndex < checkpoints[j].StartIndex
			})
			a.state.SetTurnCheckpoints(checkpoints)
		}
	}()

	a.journalTurnCheckpoint(checkpoint)

	// Check asynchronously whether any level is now over its rollup
	// threshold, so the synchronous path stays fast. Idempotent and bounded:
	// at most one rollup runs at a time per agent; subsequent turns retrigger
	// for additional levels.
	go a.scheduleRollupIfNeeded()
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

// GetTurnCheckpoints returns a defensive copy of the agent's turn
// checkpoints. Callers (e.g. the /rewind slash command) can read the
// list safely without holding the internal mutex.
func (a *Agent) GetTurnCheckpoints() []TurnCheckpoint {
	return a.copyTurnCheckpoints()
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
