package changes

import (
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// CheckpointFileChange is one git-style file-change manifest entry
// recorded on a turn checkpoint. Op mirrors git status codes: "A" added,
// "M" modified, "D" deleted, "R" renamed, anything else is "?" (other).
// Defined here (with the cluster, SP-141 phase 2); pkg/agent keeps a
// type alias so turn-checkpoint / rollup / rewind code resolves it
// unchanged.
type CheckpointFileChange struct {
	Path string `json:"path"`
	Op   string `json:"op"`
}

// ApplyShellConfig applies a resolved change_tracking config to the
// tracker's shell-walk budgets (SP-141 phase 2: the agent-side
// applyChangeTrackingConfig funnels through here). raw may be nil;
// Resolve() applies the defaults.
func (ct *ChangeTracker) ApplyShellConfig(raw *configuration.ChangeTrackingConfig) {
	resolved := raw.Resolve()

	enabled := true
	if resolved.ShellWalkEnabled != nil {
		enabled = *resolved.ShellWalkEnabled
	}
	ct.shellWalkEnabled = enabled
	ct.shellMaxFiles = resolved.MaxFiles
	ct.shellMaxTotalBytes = resolved.MaxTotalBytes
	ct.shellMaxDuration = time.Duration(resolved.MaxDurationMs) * time.Millisecond
	ct.shellAutoSkipFileCountThreshold = resolved.AutoSkipFileCountThreshold
}

// IsOutsideWorkspace is the exported form of isOutsideWorkspace for
// agent-side callers (Agent.IsPathOutsideWorkspace).
func (ct *ChangeTracker) IsOutsideWorkspace(filePath string) bool {
	return ct.isOutsideWorkspace(filePath)
}

// SetSessionID retargets the tracker at a new session (session
// rotation). Unlocked on purpose, matching the pre-move direct field
// write: the rotation path commits the prior session first, and the
// Reset commitMu barrier guarantees no in-flight Commit still reads the
// old value.
func (ct *ChangeTracker) SetSessionID(id string) {
	ct.sessionID = id
}

// CollectFileChangesForCheckpoint returns the (path, op) manifest of
// changes appended since the most recent checkpoint capture.
func (ct *ChangeTracker) CollectFileChangesForCheckpoint() ([]CheckpointFileChange, string) {
	if ct == nil || !ct.IsEnabled() {
		return nil, ""
	}
	ct.mu.Lock()
	if ct.checkpointedChangeCount >= len(ct.changes) {
		revID := ct.revisionID
		ct.mu.Unlock()
		return nil, revID
	}

	window := make([]TrackedFileChange, len(ct.changes)-ct.checkpointedChangeCount)
	copy(window, ct.changes[ct.checkpointedChangeCount:])
	ct.checkpointedChangeCount = len(ct.changes)
	revID := ct.revisionID
	ct.mu.Unlock()

	if len(window) == 0 {
		return nil, revID
	}

	// Collapse multiple writes to the same path → one entry per path.
	// "create" beats "edit"/"write" so a turn that creates then edits shows as A.
	seen := make(map[string]string, len(window))
	order := make([]string, 0, len(window))
	for _, c := range window {
		op := mapTrackedOperationToGit(c.Operation)
		existing, ok := seen[c.FilePath]
		if !ok {
			order = append(order, c.FilePath)
			seen[c.FilePath] = op
			continue
		}
		if op == "A" && existing != "A" {
			seen[c.FilePath] = op
		}
	}

	manifest := make([]CheckpointFileChange, 0, len(order))
	for _, path := range order {
		manifest = append(manifest, CheckpointFileChange{Path: path, Op: seen[path]})
	}
	return manifest, revID
}

// mapTrackedOperationToGit maps a TrackedFileChange.Operation to a git-style op code.
func mapTrackedOperationToGit(op string) string {
	switch op {
	case "create":
		return "A"
	case "write", "edit", "overwrite":
		return "M"
	case "delete":
		return "D"
	case "rename":
		return "R"
	default:
		return "?"
	}
}
