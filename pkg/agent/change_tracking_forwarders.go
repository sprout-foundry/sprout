package agent

import (
	"github.com/sprout-foundry/sprout/pkg/agent/changes"
)

// SP-141 phase 2: the change-tracking cluster now lives in
// pkg/agent/changes. These forwarders keep every in-package and
// external call site compiling unchanged.

type ChangeTracker = changes.ChangeTracker
type TrackedFileChange = changes.TrackedFileChange
type TrackedBulkItem = changes.TrackedBulkItem
type CheckpointFileChange = changes.CheckpointFileChange

const RedactedContentMarker = changes.RedactedContentMarker

// NewChangeTracker creates a tracker for an agent session. *Agent
// satisfies changes.ChangeAgent structurally.
func NewChangeTracker(agent *Agent, instructions string) *ChangeTracker {
	return changes.NewChangeTracker(agent, instructions)
}
