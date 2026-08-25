package agent

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
