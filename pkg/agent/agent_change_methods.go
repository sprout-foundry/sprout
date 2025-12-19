package agent

import "fmt"

// EnableChangeTracking enables change tracking for this agent session
func (a *Agent) EnableChangeTracking(instructions string) {
	if a.debug {
		a.debugLog("DEBUG: EnableChangeTracking called (tracker nil: %v)\n", a.changeTracker == nil)
	}

	if a.changeTracker == nil {
		a.changeTracker = NewChangeTracker(a, instructions)
		if a.debug {
			a.debugLog("DEBUG: Created new change tracker and enabled it\n")
		}
	} else {
		a.changeTracker.Reset(instructions)
		a.changeTracker.Enable()
		if a.debug {
			a.debugLog("DEBUG: Reset existing change tracker and enabled it\n")
		}
	}
}

// DisableChangeTracking disables change tracking
func (a *Agent) DisableChangeTracking() {
	if a.changeTracker != nil {
		a.changeTracker.Disable()
	}
}

// IsChangeTrackingEnabled returns whether change tracking is enabled
func (a *Agent) IsChangeTrackingEnabled() bool {
	enabled := a.changeTracker != nil && a.changeTracker.IsEnabled()
	if a.debug {
		trackerEnabled := false
		if a.changeTracker != nil {
			trackerEnabled = a.changeTracker.IsEnabled()
		}
		a.debugLog("DEBUG: IsChangeTrackingEnabled = %v (tracker nil: %v, tracker enabled: %v)\n",
			enabled, a.changeTracker == nil, trackerEnabled)
	}
	return enabled
}

// GetChangeTracker returns the change tracker (can be nil)
func (a *Agent) GetChangeTracker() *ChangeTracker {
	return a.changeTracker
}

// GetRevisionID returns the current revision ID (if change tracking is enabled)
func (a *Agent) GetRevisionID() string {
	if a.changeTracker != nil {
		return a.changeTracker.GetRevisionID()
	}
	return ""
}

// GetTrackedFiles returns the list of files that have been modified in this session
func (a *Agent) GetTrackedFiles() []string {
	if a.changeTracker != nil {
		return a.changeTracker.GetTrackedFiles()
	}
	return []string{}
}

// GetChangeCount returns the number of file changes tracked in this session
func (a *Agent) GetChangeCount() int {
	if a.changeTracker != nil {
		return a.changeTracker.GetChangeCount()
	}
	return 0
}

// GetChangesSummary returns a summary of tracked changes
func (a *Agent) GetChangesSummary() string {
	if a.changeTracker != nil {
		return a.changeTracker.GetSummary()
	}
	return "Change tracking is not enabled"
}

// CommitChanges commits all tracked changes to the change tracker
func (a *Agent) CommitChanges(llmResponse string) error {
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		return a.changeTracker.Commit(llmResponse)
	}
	return nil
}

// ClearTrackedChanges clears all tracked changes (but keeps tracking enabled)
func (a *Agent) ClearTrackedChanges() {
	if a.changeTracker != nil {
		a.changeTracker.Clear()
	}
}

// TrackFileWrite is called by the WriteFile tool to track file writes
func (a *Agent) TrackFileWrite(filePath string, content string) error {
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		return a.changeTracker.TrackFileWrite(filePath, content)
	}

	// Also record as a task action for conversation summary
	a.AddTaskAction("file_created", fmt.Sprintf("Created/updated file: %s", filePath), filePath)

	return nil
}

// TrackFileEdit is called by the EditFile tool to track file edits
func (a *Agent) TrackFileEdit(filePath string, originalContent string, newContent string) error {
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		return a.changeTracker.TrackFileEdit(filePath, originalContent, newContent)
	}

	// Also record as a task action for conversation summary
	a.AddTaskAction("file_modified", fmt.Sprintf("Modified file: %s", filePath), filePath)

	return nil
}
