// Package agent: change tracking and revision management.
package agent

import (
	"fmt"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// EnableChangeTracking enables change tracking for this agent session.
func (a *Agent) EnableChangeTracking(instructions string) {
	if a.debug {
		a.Logger().Debug("DEBUG: EnableChangeTracking called (tracker nil: %v)\n", a.changeTracker == nil)
	}

	// Check the config gate BEFORE creating or enabling the tracker.
	// When change_tracking.enabled is explicitly false, the entire subsystem stays dormant.
	if !a.isChangeTrackingEnabledByConfig() {
		if a.debug {
			a.Logger().Debug("DEBUG: change tracking disabled by config (change_tracking.enabled = false)\n")
		}
		return
	}

	if a.changeTracker == nil {
		// First enable of this session — create the tracker with a stable revisionID + instructions.
		a.changeTracker = NewChangeTracker(a, instructions)
		if a.debug {
			a.Logger().Debug("DEBUG: Created new change tracker (session start)\n")
		}
	} else {
		// Subsequent enable within the same session. Ensure enabled, but DO NOT Reset: the buffer must accumulate across queries.
		a.changeTracker.Enable()
		if a.debug {
			a.Logger().Debug("DEBUG: Re-enabled existing change tracker (buffer preserved, %d entries)\n", a.GetChangeCount())
		}
	}

	// Apply ChangeTrackingConfig so per-tracker overrides take effect before the prime walk runs.
	a.applyChangeTrackingConfig()

	if root := a.effectiveCwd(); root != "" {
		a.changeTracker.PrimeShellTracking(root)
	}

	// One-shot revision-history compaction. Runs in the background so the agent's startup isn't blocked by I/O.
	go a.compactRevisionHistoryAsync()
}

// compactRevisionHistoryAsync runs one pass of pkg/history.CompactRevisions using the policy resolved from configuration.
func (a *Agent) compactRevisionHistoryAsync() {
	var raw *configuration.RevisionRetentionConfig
	if a.configManager != nil {
		cfg := a.configManager.GetConfig()
		if cfg != nil && cfg.ChangeTracking != nil {
			raw = cfg.ChangeTracking.RevisionRetention
		}
	}
	resolved := raw.Resolve()

	var maxChangesAge time.Duration
	if resolved.MaxChangesAgeDays > 0 {
		maxChangesAge = time.Duration(resolved.MaxChangesAgeDays) * 24 * time.Hour
	}
	policy := history.RetentionPolicy{
		HotCount:              resolved.HotCount,
		WarmCount:             resolved.WarmCount,
		MaxDirBytes:           resolved.MaxDirBytes,
		ArchiveFrozen:         resolved.ArchiveFrozen,
		MaxChangesPerRevision: resolved.MaxChangesPerRevision,
		MaxChangesAge:         maxChangesAge,
	}
	stats, err := history.CompactRevisions(policy)
	if err != nil {
		a.Logger().Debug("revision compaction failed: %v\n", err)
		return
	}
	changesDropped := stats.OrphanChangesDropped + stats.OverCapChangesDropped + stats.AgedChangesDropped
	if stats.WarmDemoted+stats.Dropped+stats.HardCapTrimmed+changesDropped > 0 {
		a.Logger().Debug("revision compaction: %d total / %d hot / %d→warm / %d dropped / %d trimmed / %d orphan-changes / %d over-cap-changes / %d aged-changes / %.2f MiB reclaimed",
			stats.TotalRevisions, stats.HotKept, stats.WarmDemoted,
			stats.Dropped, stats.HardCapTrimmed,
			stats.OrphanChangesDropped, stats.OverCapChangesDropped, stats.AgedChangesDropped,
			float64(stats.BytesReclaimed)/(1024*1024))
	}
}

// applyChangeTrackingConfig reads the configuration.ChangeTracking section and stamps the values onto the active changeTracker.
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

// isChangeTrackingEnabledByConfig reads the change_tracking.enabled setting. Defaults to true.
func (a *Agent) isChangeTrackingEnabledByConfig() bool {
	// No config manager (test path) → preserve historical behavior: enabled.
	if a.configManager == nil {
		return true
	}
	cfg := a.configManager.GetConfig()
	if cfg == nil || cfg.ChangeTracking == nil {
		return true // production default: enabled
	}
	resolved := cfg.ChangeTracking.Resolve()
	if resolved.Enabled == nil {
		return true
	}
	return *resolved.Enabled
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

// IsPathOutsideWorkspace reports whether the resolved absolute path falls outside the agent's workspace root.
func (a *Agent) IsPathOutsideWorkspace(path string) bool {
	if a.changeTracker == nil || !a.changeTracker.IsEnabled() {
		return false
	}
	return a.changeTracker.isOutsideWorkspace(path)
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

// Public façade for the WebUI / external callers.

// ListChanges returns the session manifest.
func (a *Agent) ListChanges(args map[string]interface{}) (string, error) {
	return handleListChanges(nil, a, args)
}

// ShowMyChange returns a unified diff JSON envelope for `path`.
func (a *Agent) ShowMyChange(path string) (string, error) {
	return handleListChanges(nil, a, map[string]interface{}{
		"path_pattern": path,
		"include_diff": true,
	})
}

// RevertMyChanges performs a bulk revert.
func (a *Agent) RevertMyChanges(scope, file, since string) (string, error) {
	if file != "" {
		return handleRecoverFile(nil, a, map[string]interface{}{
			"path":  file,
			"scope": "session_start",
		})
	}
	args := map[string]interface{}{}
	if scope != "" {
		args["scope"] = scope
	}
	if since != "" {
		args["since"] = since
	}
	return handleRevertMyChanges(nil, a, args)
}

// SummarizeMySession returns the activity-block digest. Thin wrapper
// around list_changes(group_by="block").
func (a *Agent) SummarizeMySession() (string, error) {
	return handleListChanges(nil, a, map[string]interface{}{"group_by": "block"})
}

// MyRecentChanges returns the cross-session timeline. Thin wrapper
// around list_changes(include_persisted=true, include_cross_session=true, since=…).
func (a *Agent) MyRecentChanges(since string) (string, error) {
	args := map[string]interface{}{"include_persisted": true, "include_cross_session": true}
	if since != "" {
		args["since"] = since
	}
	return handleListChanges(nil, a, args)
}

// RecoverFile restores one file from the tracker's session buffer.
func (a *Agent) RecoverFile(path string) (string, error) {
	return handleRecoverFile(nil, a, map[string]interface{}{"path": path})
}

// TrackFileWrite is called by the WriteFile tool to track file writes
func (a *Agent) TrackFileWrite(filePath string, content string) error {
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		err := a.changeTracker.TrackFileWrite(filePath, content)
		// Keep the shell-snapshot cache in sync to avoid duplicate entries.
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

// Standalone (no-agent) query functions.

// ListChangesPersistedOnly returns a session manifest from the persisted history store.
func ListChangesPersistedOnly(args map[string]interface{}) (string, error) {
	return handleListChangesPersistedOnly(args)
}

// ListChangesEmpty returns the disabled-tracker response: an empty manifest.
func ListChangesEmpty() string {
	return `{"revision_id":"","enabled":false,"count":0,"files":[]}`
}

// SummarizeMySessionEmpty returns the disabled-tracker block-summary response.
func SummarizeMySessionEmpty() string {
	return `{"enabled":false,"blocks":[],"totals":{"changes":0,"files":0}}`
}

// MergeSubagentChanges merges a completed subagent's tracked changes into this agent's ChangeTracker.
func (a *Agent) MergeSubagentChanges(changes []TrackedFileChange, persona string) {
	if a.changeTracker == nil || !a.changeTracker.IsEnabled() {
		return
	}
	if len(changes) == 0 {
		return
	}
	source := "subagent"
	if persona != "" {
		source = "subagent:" + persona
	}
	a.changeTracker.MergeChild(changes, source)
}
