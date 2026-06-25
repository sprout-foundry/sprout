package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// lastRewindSnapshot holds the state before the most recent rewind
// so a future "undo rewind" command (SP-071-2) can restore it.
var lastRewindSnapshot struct {
	messages    []api.Message
	checkpoints []TurnCheckpoint
}

// RewindOptions configures a rewind operation.
type RewindOptions struct {
	ToTurnIndex int  // 0-based: rewind to BEFORE this turn's messages (truncate at the start of this turn)
	RevertFiles bool // default true: revert file changes from discarded turns
}

// RewindResult reports what a rewind operation did.
type RewindResult struct {
	TurnsDiscarded     int      // number of turns removed
	MessagesRemoved    int      // number of messages removed from the history
	FilesReverted      []string // files that were reverted
	FilesSkipped       []string // files that could NOT be reverted (modified outside agent)
	CheckpointsDropped int      // orphaned checkpoints removed
}

// Rewind truncates the agent's message history and checkpoints back to a
// prior turn, optionally reverting file changes made during the discarded
// turns. The operation is undoable via the package-level lastRewindSnapshot.
func (a *Agent) Rewind(opts RewindOptions) (*RewindResult, error) {
	// 1. Validate inputs — checkpoints are already sorted by StartIndex
	checkpoints := a.copyTurnCheckpoints()
	n := len(checkpoints)
	if opts.ToTurnIndex < 0 || opts.ToTurnIndex >= n {
		return nil, fmt.Errorf("rewind: invalid turn index %d (have %d checkpoints, valid range [0, %d])",
			opts.ToTurnIndex, n, n-1)
	}

	// 2. Snapshot before rewind (so rewind is undoable via SP-071-2)
	msgs := a.GetMessages()
	lastRewindSnapshot.messages = append([]api.Message(nil), msgs...)
	lastRewindSnapshot.checkpoints = append([]TurnCheckpoint(nil), checkpoints...)

	// 3. Find the target checkpoint at ToTurnIndex
	target := checkpoints[opts.ToTurnIndex]

	// 4. Determine the truncation point
	startIndex := target.StartIndex

	// 5. Count what will be discarded (includes the checkpoint at ToTurnIndex,
	// which will be dropped in step 9 since its StartIndex == startIndex).
	discardedCheckpoints := checkpoints[opts.ToTurnIndex:]
	turnsDiscarded := len(discardedCheckpoints)
	messagesRemoved := len(msgs) - startIndex

	// 6. Collect file changes from discarded checkpoints (in REVERSE order — last first).
	// Build a deduplicated set keyed by absolute path so each file is only
	// attempted once, preferring the most-recent checkpoint's entry.
	seen := make(map[string]bool)
	var filePaths []string // deduplicated paths in reverse-checkpoint order

	for i := len(discardedCheckpoints) - 1; i >= 0; i-- {
		cp := discardedCheckpoints[i]
		for _, fc := range cp.FileChanges {
			abs, err := filepath.Abs(fc.Path)
			if err != nil {
				abs = fc.Path
			}
			if seen[abs] {
				continue
			}
			seen[abs] = true
			filePaths = append(filePaths, abs)
		}
	}

	// 7. Revert files if enabled (default true)
	var filesReverted []string
	var filesSkipped []string

	if opts.RevertFiles != false {
		tracker := a.GetChangeTracker()

		for _, abs := range filePaths {
			// If tracker is nil or disabled, we can't verify or recover — skip
			if tracker == nil || !tracker.IsEnabled() {
				filesSkipped = append(filesSkipped, abs)
				continue
			}

			// Check if the file has been modified since the agent last touched it.
			// We compare the current on-disk content against the ChangeTracker's
			// NewCode from the most recent change (the last content the agent
			// wrote). If they differ, someone else modified the file and we skip it.
			if match := resolveRecoveryTarget(tracker.GetChanges(), abs); match != nil && match.NewCode != "" {
				current, err := os.ReadFile(abs)
				if err == nil && string(current) != match.NewCode {
					// File was modified outside the agent — skip
					filesSkipped = append(filesSkipped, abs)
					continue
				}
			}

			// Call handleRecoverFile with scope="session_start" to restore
			// the file to its state before the agent first touched it.
			result, err := handleRecoverFile(nil, a, map[string]interface{}{
				"path":  abs,
				"scope": "session_start",
			})
			if err != nil {
				filesSkipped = append(filesSkipped, abs)
				continue
			}

			if isRecoverResultOK(result) {
				filesReverted = append(filesReverted, abs)
			} else {
				filesSkipped = append(filesSkipped, abs)
			}
		}
	}

	// 8. Truncate messages
	truncated := make([]api.Message, startIndex)
	copy(truncated, msgs[:startIndex])
	a.SetMessages(truncated)

	// 9. Drop orphaned checkpoints — keep only those with StartIndex < startIndex
	var remaining []TurnCheckpoint
	for _, cp := range checkpoints {
		if cp.StartIndex < startIndex {
			remaining = append(remaining, cp)
		}
	}
	a.ReplaceTurnCheckpoints(remaining)
	checkpointsDropped := len(checkpoints) - len(remaining)

	// 10. Return result
	return &RewindResult{
		TurnsDiscarded:     turnsDiscarded,
		MessagesRemoved:    messagesRemoved,
		FilesReverted:      filesReverted,
		FilesSkipped:       filesSkipped,
		CheckpointsDropped: checkpointsDropped,
	}, nil
}

// isRecoverResultOK parses a JSON result string from handleRecoverFile and
// returns true if the "recovered" field is true.
func isRecoverResultOK(result string) bool {
	var payload struct {
		Recovered bool `json:"recovered"`
	}
	if err := json.Unmarshal([]byte(result), &payload); err != nil {
		return false
	}
	return payload.Recovered
}
