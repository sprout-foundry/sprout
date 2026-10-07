package tools

// checkpoint_ops.go — the on-demand checkpoint operations behind the
// checkpoint tool: list, create, and restore. They layer pkg/history's
// checkpoint store and timeline onto the tool surface; a restore is one
// action and is recorded as a checkpoint, so the timeline shows it.
//
// Every operation is scoped to the tool call's workspace (env.WorkspaceRoot)
// through pkg/history's workspace-aware checkpoint functions, so the tool
// reads and writes the same <workspace>/.sprout/checkpoints/ store the
// automatic verification and deploy capture seams write to. An empty
// workspace falls back to the process-wide store (standalone tool runs).

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/history"
)

// CheckpointResult captures the output and metadata for checkpoint operations.
type CheckpointResult struct {
	Output   string
	Metadata map[string]interface{}
	Success  bool
}

// ManageCheckpoints performs the checkpoint operation named by action for the
// given workspace (empty for the process-wide store):
//
//   - "" or "list": list the recorded checkpoints (newest first).
//   - "create": capture the current state as an on-demand checkpoint.
//   - "restore": restore the project to a checkpoint's state. It needs id
//     (or revision_id); when confirm is false it previews instead.
//
// Restoring records the restore itself as a checkpoint, so the timeline
// shows it alongside the state it returned to.
func ManageCheckpoints(workspace, action, id, revisionID, summary string, confirm bool) (CheckpointResult, error) {
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "", "list":
		return listCheckpoints(workspace)
	case "create":
		cp, err := history.CreateCheckpointInWorkspace(workspace, history.CheckpointManual, strings.TrimSpace(summary), nil)
		if err != nil {
			return CheckpointResult{}, fmt.Errorf("create checkpoint: %w", err)
		}
		return CheckpointResult{
			Output:  fmt.Sprintf("Created checkpoint %s (%s).", cp.ID, cp.Summary),
			Success: true,
			Metadata: map[string]interface{}{
				"action":        "create_checkpoint",
				"checkpoint_id": cp.ID,
				"revision_id":   cp.RevisionID,
			},
		}, nil
	case "restore":
		return restoreCheckpoint(workspace, id, revisionID, confirm)
	default:
		return CheckpointResult{}, fmt.Errorf("unknown checkpoint action %q (use list, create, or restore)", action)
	}
}

func listCheckpoints(workspace string) (CheckpointResult, error) {
	checkpoints, err := history.ListCheckpointsInWorkspace(workspace, true)
	if err != nil {
		return CheckpointResult{}, fmt.Errorf("list checkpoints: %w", err)
	}
	if len(checkpoints) == 0 {
		return CheckpointResult{
			Output:   "No checkpoints recorded yet.",
			Success:  true,
			Metadata: map[string]interface{}{"action": "list_checkpoints", "count": 0},
		}, nil
	}

	var b strings.Builder
	fmt.Fprintf(&b, "Checkpoints (%d, newest first):\n", len(checkpoints))
	ids := make([]string, 0, len(checkpoints))
	for _, cp := range checkpoints {
		ids = append(ids, cp.ID)
		fmt.Fprintf(&b, "- %s [%s] %s\n", cp.ID, cp.Origin, cp.Summary)
	}

	return CheckpointResult{
		Output:  b.String(),
		Success: true,
		Metadata: map[string]interface{}{
			"action":         "list_checkpoints",
			"count":          len(checkpoints),
			"checkpoint_ids": ids,
		},
	}, nil
}

func restoreCheckpoint(workspace, id, revisionID string, confirm bool) (CheckpointResult, error) {
	id = strings.TrimSpace(id)
	revisionID = strings.TrimSpace(revisionID)

	if id == "" && revisionID == "" {
		return CheckpointResult{}, fmt.Errorf("restore needs a checkpoint id (or revision_id)")
	}

	if id == "" {
		// Resolve a checkpoint by the revision it captures.
		checkpoints, err := history.ListCheckpointsInWorkspace(workspace, false)
		if err != nil {
			return CheckpointResult{}, fmt.Errorf("list checkpoints: %w", err)
		}
		for i := len(checkpoints) - 1; i >= 0; i-- {
			if checkpoints[i].RevisionID == revisionID && checkpoints[i].Origin != history.CheckpointRestore {
				id = checkpoints[i].ID
				break
			}
		}
		if id == "" {
			return CheckpointResult{}, fmt.Errorf("no checkpoint captures revision %q", revisionID)
		}
	}

	cp, found, err := history.GetCheckpointInWorkspace(workspace, id)
	if err != nil {
		return CheckpointResult{}, fmt.Errorf("read checkpoint: %w", err)
	}
	if !found {
		return CheckpointResult{}, fmt.Errorf("checkpoint %q not found", id)
	}

	if !confirm {
		return CheckpointResult{
			Output: fmt.Sprintf("Would restore checkpoint %s (revision %s).\nTo confirm, call again with confirm=true.",
				cp.ID, cp.RevisionID),
			Success: true,
			Metadata: map[string]interface{}{
				"action":        "preview_restore_checkpoint",
				"checkpoint_id": cp.ID,
				"revision_id":   cp.RevisionID,
			},
		}, nil
	}

	restore, err := history.RestoreCheckpointInWorkspace(workspace, id)
	if err != nil {
		return CheckpointResult{}, fmt.Errorf("restore checkpoint: %w", err)
	}

	return CheckpointResult{
		Output: fmt.Sprintf("Restored checkpoint %s (revision %s); recorded as %s.",
			cp.ID, cp.RevisionID, restore.ID),
		Success: true,
		Metadata: map[string]interface{}{
			"action":           "restore_checkpoint",
			"checkpoint_id":    cp.ID,
			"revision_id":      cp.RevisionID,
			"restore_entry_id": restore.ID,
			"file_paths":       cp.Files,
		},
	}, nil
}
