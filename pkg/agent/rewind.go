package agent

import (
	"encoding/json"
	"fmt"
	"path/filepath"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// lastRewindSnapshot holds the state before the most recent rewind for undo.
var lastRewindSnapshot struct {
	messages    []api.Message
	checkpoints []TurnCheckpoint
}

// RewindOptions configures a rewind operation.
type RewindOptions struct {
	ToTurnIndex int  // 0-based: rewind to BEFORE this turn's messages
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
// prior turn, optionally reverting file changes. Undoable via lastRewindSnapshot.
func (a *Agent) Rewind(opts RewindOptions) (*RewindResult, error) {
	// 1. Validate inputs
	checkpoints := a.copyTurnCheckpoints()
	n := len(checkpoints)
	if opts.ToTurnIndex < 0 || opts.ToTurnIndex >= n {
		return nil, agenterrors.NewValidation(fmt.Sprintf("rewind: invalid turn index %d (have %d checkpoints, valid range [0, %d])", opts.ToTurnIndex, n, n-1), nil)
	}

	// 2. Snapshot before rewind
	msgs := a.GetMessages()
	lastRewindSnapshot.messages = append([]api.Message(nil), msgs...)
	lastRewindSnapshot.checkpoints = append([]TurnCheckpoint(nil), checkpoints...)

	// 3. Find the target checkpoint
	target := checkpoints[opts.ToTurnIndex]

	// 4. Determine the truncation point
	startIndex := target.StartIndex

	// 5. Count what will be discarded
	discardedCheckpoints := checkpoints[opts.ToTurnIndex:]
	turnsDiscarded := len(discardedCheckpoints)
	messagesRemoved := len(msgs) - startIndex

	// 6. Collect file changes from discarded checkpoints (reverse order, deduplicated).
	seen := make(map[string]bool)
	var filePaths []string

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

	// 7. Revert files if enabled
	var filesReverted []string
	var filesSkipped []string

	if opts.RevertFiles != false {
		tracker := a.GetChangeTracker()

		for _, abs := range filePaths {
			if tracker == nil || !tracker.IsEnabled() {
				filesSkipped = append(filesSkipped, abs)
				continue
			}

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

	// 9. Drop orphaned checkpoints
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
