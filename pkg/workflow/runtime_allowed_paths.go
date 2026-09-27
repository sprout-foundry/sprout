//go:build !js

package workflow

// runtime_allowed_paths.go — per-step allowed-paths allowlist management,
// split out of runtime.go. ApplyWorkflowRuntimeAllowedPaths snapshots the
// agent's session allowed-folder state and adds the step's declared paths;
// RestoreWorkflowRuntimeAllowedPaths undoes exactly the net-new additions on
// step exit so paths never leak into the next step.
import (
	"errors"
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
)

// ApplyWorkflowRuntimeAllowedPaths adds each path in paths (derived from
// AllowedPath entries in the step's runtime config) to the agent's session
// allowlist and records the declared mode. This lets the cd-target gate
// (Phase 2.1) and the filesystem gate (Phase 2.2) see the paths as approved
// for the duration of the step.
//
// Snapshot semantics: the snapshot captures the state BEFORE any paths are
// added, so restoreWorkflowRuntimeAllowedPaths can undo only the paths this
// step added — not paths contributed by earlier steps. This means consecutive
// steps that declare overlapping paths have the documented "last restore wins"
// behavior: step 2's snapshot contains step 1's contribution; step 2's restore
// removes both step 2's new paths AND step 1's old paths. Declare overlapping
// paths in each step that needs them, or promote them to the workflow-level
// AllowedPaths, if you need per-step isolation.
//
// Idempotent: adding a path that's already on the allowlist is a no-op;
// it is NOT included in addedPaths, so it will NOT be removed on restore.
func ApplyWorkflowRuntimeAllowedPaths(chatAgent *agent.Agent, paths []AllowedPath) (snapshotPaths []string, snapshotModes map[string]string, addedPaths []string, err error) {
	if chatAgent == nil {
		return nil, nil, nil, errors.New("agent is required")
	}
	if len(paths) == 0 {
		// No paths to add: snapshot captures current state (empty for an
		// agent with no prior allowlist), nothing added, nothing to restore.
		snapshotPaths = chatAgent.SnapshotSessionAllowedFolders()
		snapshotModes = chatAgent.SnapshotSessionAllowedFolderModes()
		return snapshotPaths, snapshotModes, nil, nil
	}

	// Snapshot BEFORE mutating so we can restore exactly to the pre-step state.
	snapshotPaths = chatAgent.SnapshotSessionAllowedFolders()
	snapshotModes = chatAgent.SnapshotSessionAllowedFolderModes()

	// Track what we actually added so the restore path can remove only the
	// net-new entries without disturbing pre-existing ones.
	currentSet := make(map[string]bool)
	for _, f := range snapshotPaths {
		currentSet[f] = true
	}

	addedPaths = nil
	for _, ap := range paths {
		normalized := ap.Path
		if normalized == "" {
			continue
		}
		if currentSet[normalized] {
			// Already on the allowlist: nothing to do. Not added to
			// addedPaths so restore won't touch it.
			continue
		}
		chatAgent.AddSessionAllowedFolder(normalized)
		mode := strings.TrimSpace(ap.Mode)
		if mode == "" {
			mode = PathModeReadWrite // default
		}
		chatAgent.SetSessionAllowedFolderMode(normalized, mode)
		currentSet[normalized] = true // prevent dup in same step
		addedPaths = append(addedPaths, normalized)
	}

	return snapshotPaths, snapshotModes, addedPaths, nil
}

// restoreWorkflowRuntimeAllowedPaths undoes the paths added by the matching
// ApplyWorkflowRuntimeAllowedPaths call for this step. It is called at every
// step exit point (success, failure, skip, shell) to ensure no step's
// allowed_paths leak into the next step.
//
// Restore semantics:
//   - For each path in addedPaths: if it was NOT in snapshotPaths, remove it
//     from the allowlist (it was added by this step).
//   - For each path in snapshotPaths: restore its mode from snapshotModes
//     (may be different from what a prior step left behind).
//
// This means if step 1 adds /a and step 2 adds /b then /a:
//   - After step 1: allowlist = [/a]
//   - Step 2 snapshot: [/a]
//   - Step 2 addedPaths: [/b] (/a was already there)
//   - Step 2 restore removes [/b], leaves [/a]
//   - After step 2: allowlist = [/a]
//
// This is the documented "steps don't inherit paths from prior steps" behavior.
func RestoreWorkflowRuntimeAllowedPaths(chatAgent *agent.Agent, snapshotPaths []string, snapshotModes map[string]string, addedPaths []string) error {
	if chatAgent == nil {
		return errors.New("agent is required")
	}

	// Build a set from snapshotPaths for O(1) membership checks.
	snapshotSet := make(map[string]bool)
	for _, f := range snapshotPaths {
		snapshotSet[f] = true
	}

	// Remove net-new paths: any addedPath not in the snapshot.
	for _, ap := range addedPaths {
		if ap == "" {
			continue
		}
		if snapshotSet[ap] {
			// This path existed before the step started — leave it alone.
			continue
		}
		// This path was added by this step — remove it.
		if err := chatAgent.RemoveSessionAllowedFolder(ap); err != nil {
			return fmt.Errorf("failed to remove allowed folder %q: %w", ap, err)
		}
	}

	// Restore modes for all paths that were present in the snapshot.
	// This overwrites whatever mode the current step left behind.
	for _, f := range snapshotPaths {
		if f == "" {
			continue
		}
		mode := ""
		if snapshotModes != nil {
			mode = snapshotModes[f]
		}
		// SetSessionAllowedFolderMode handles the no-op case when the
		// folder isn't on the allowlist (shouldn't happen here since
		// we checked snapshotPaths is the current state).
		chatAgent.SetSessionAllowedFolderMode(f, mode)
	}

	return nil
}
