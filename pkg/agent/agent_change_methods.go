package agent

import (
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// EnableChangeTracking enables change tracking for this agent session.
//
// Side effect: primes the shell-mutation snapshot cache against the
// agent's workspace root. This is the one-time cost (~280 ms on a
// 5000-file workspace) that lets every subsequent shell_command be
// tracked via a cheap stat-only diff. Without this prime the first
// shell command's mutations would silently establish the baseline
// (auto-prime in TrackShellTurn) and go un-recorded — fine for
// read-only commands, but a real loss if the first shell does any
// writes.
func (a *Agent) EnableChangeTracking(instructions string) {
	if a.debug {
		a.Logger().Debug("DEBUG: EnableChangeTracking called (tracker nil: %v)\n", a.changeTracker == nil)
	}

	if a.changeTracker == nil {
		a.changeTracker = NewChangeTracker(a, instructions)
		if a.debug {
			a.Logger().Debug("DEBUG: Created new change tracker and enabled it\n")
		}
	} else {
		a.changeTracker.Reset(instructions)
		a.changeTracker.Enable()
		if a.debug {
			a.Logger().Debug("DEBUG: Reset existing change tracker and enabled it\n")
		}
	}

	// Apply ChangeTrackingConfig: read the user/workspace setting and
	// stamp it into the tracker so per-tracker overrides take effect
	// before the prime walk runs.
	a.applyChangeTrackingConfig()

	if root := a.effectiveCwd(); root != "" {
		a.changeTracker.PrimeShellTracking(root)
	}
}

// applyChangeTrackingConfig reads the configuration.ChangeTracking
// section (if present), resolves defaults, and stamps the values onto
// the active changeTracker. Called from EnableChangeTracking before
// PrimeShellTracking so the prime walk honors any custom budgets.
// When the agent has no config manager (test path) or no
// ChangeTracking override, defaults apply.
func (a *Agent) applyChangeTrackingConfig() {
	if a.changeTracker == nil {
		return
	}
	var raw *configuration.ChangeTrackingConfig
	if a.configManager != nil {
		cfg := a.configManager.GetConfig()
		if cfg != nil {
			raw = cfg.ChangeTracking
		}
	}
	resolved := raw.Resolve()

	enabled := true
	if resolved.ShellWalkEnabled != nil {
		enabled = *resolved.ShellWalkEnabled
	}
	a.changeTracker.shellWalkEnabled = enabled
	a.changeTracker.shellMaxFiles = resolved.MaxFiles
	a.changeTracker.shellMaxTotalBytes = resolved.MaxTotalBytes
	a.changeTracker.shellMaxDuration = time.Duration(resolved.MaxDurationMs) * time.Millisecond
	a.changeTracker.shellAutoSkipFileCountThreshold = resolved.AutoSkipFileCountThreshold
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
		a.Logger().Debug("DEBUG: IsChangeTrackingEnabled = %v (tracker nil: %v, tracker enabled: %v)\n",
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
		// Get the full conversation from the agent
		conversation := a.GetMessages()
		return a.changeTracker.Commit(llmResponse, conversation)
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
		err := a.changeTracker.TrackFileWrite(filePath, content)
		// Keep the shell-snapshot cache in sync so the next
		// TrackShellTurn walk doesn't see this write as a stat
		// mismatch and record a duplicate entry attributed to
		// shell_command.
		a.changeTracker.SyncShellCacheForPath(filePath)
		return err
	}

	// Also record as a task action for conversation summary
	a.AddTaskAction("file_created", fmt.Sprintf("Created/updated file: %s", filePath), filePath)

	return nil
}

// TrackFileEdit is called by the EditFile tool to track file edits
func (a *Agent) TrackFileEdit(filePath string, originalContent string, newContent string) error {
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		err := a.changeTracker.TrackFileEdit(filePath, originalContent, newContent)
		a.changeTracker.SyncShellCacheForPath(filePath)
		return err
	}

	// Also record as a task action for conversation summary
	a.AddTaskAction("file_modified", fmt.Sprintf("Modified file: %s", filePath), filePath)

	return nil
}
