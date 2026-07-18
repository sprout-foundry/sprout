package agent

// CollectFileChangesForCheckpoint returns the (path, op) manifest of
// changes appended since the most recent checkpoint capture, along with
// the current revision ID. Advances the internal watermark so a
// subsequent call returns only the next turn's changes. Safe to call
// when tracking is disabled — returns (nil, "") in that case.
//
// Ops are git-style: "A" (added/created), "M" (modified), "D" (deleted),
// "R" (renamed). The ChangeTracker today only records create/write/edit
// — never delete or rename — so the manifest produced here will only
// contain A and M entries. When the tracker grows D/R support, extend
// the mapping table below.
//
// Multiple writes to the same path within the same turn collapse to one
// entry, preferring "A" over "M" (a turn that creates then modifies
// the same file is recorded as A).
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
func (ct *ChangeTracker) CollectFileChangesForCheckpoint() ([]CheckpointFileChange, string) {
	if ct == nil || !ct.IsEnabled() {
		return nil, ""
	}
	ct.mu.Lock()
	if ct.checkpointedChangeCount >= len(ct.changes) {
		// No new changes since last capture.
		revID := ct.revisionID
		ct.mu.Unlock()
		return nil, revID
	}

	// Snapshot the window under the lock so concurrent Clear/Reset can't
	// truncate the underlying slice while we iterate below.
	window := make([]TrackedFileChange, len(ct.changes)-ct.checkpointedChangeCount)
	copy(window, ct.changes[ct.checkpointedChangeCount:])
	ct.checkpointedChangeCount = len(ct.changes)
	revID := ct.revisionID
	ct.mu.Unlock()

	if len(window) == 0 {
		return nil, revID
	}

	// Collapse multiple writes to the same path → one entry per path.
	// Track first-seen op; "create" beats "edit"/"write" so a turn that
	// creates a file (and then edits it) shows up as A, not M.
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
		// "A" wins over "M"; otherwise keep the first.
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

// mapTrackedOperationToGit maps a TrackedFileChange.Operation value to the
// git-style op code used in CheckpointFileChange.Op.
//
// SP-075-extension: extracted from change_tracking.go. No behavior change.
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
