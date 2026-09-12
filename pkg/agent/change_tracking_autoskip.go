// Auto-skip learning for ChangeTracker shell-mutation tracking.
//
// After the first walk of a fat directory (one whose file count exceeds
// autoSkipFileCountThreshold or autoSkipCumulativeThreshold), the dir
// is added to autoSkipDirs so subsequent walks skip it entirely.
// Learned sets persist across agent sessions via change_tracking_shell_persist.go.
package agent

import (
	"log"
	"path/filepath"
)

// addAutoSkipDir registers an absolute directory path with the shell-
// walk auto-skip set so subsequent walks don't re-traverse it. Safe to
// call repeatedly; persisting failures are logged but not surfaced —
// the in-process skip still applies for the rest of the session.
func (ct *ChangeTracker) addAutoSkipDir(workspaceRoot, relDir string) {
	if ct == nil {
		return
	}
	abs := filepath.Join(workspaceRoot, relDir)
	// Walks read autoSkipDirs under shellCacheMu (walkWorkspace runs
	// with that lock held); mutate under it too so a concurrent
	// walk can't race on the map.
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()
	ct.addAutoSkipDirLocked(workspaceRoot, abs)
}

// addAutoSkipDirLocked is addAutoSkipDir's body for callers already
// holding shellCacheMu (TrackShellTurn → emitWithBulkRollup). The mutex
// is NOT re-entrant — locking again here would self-deadlock.
// Persists an iteration-safe snapshot of the map keyed by workspaceRoot.
func (ct *ChangeTracker) addAutoSkipDirLocked(workspaceRoot, abs string) {
	if ct.autoSkipDirs == nil {
		ct.autoSkipDirs = map[string]bool{}
	}
	if ct.autoSkipDirs[abs] {
		return
	}
	ct.autoSkipDirs[abs] = true
	snapshot := make(map[string]bool, len(ct.autoSkipDirs))
	for d := range ct.autoSkipDirs {
		snapshot[d] = true
	}
	if err := saveAutoSkipDirsFor(workspaceRoot, snapshot); err != nil {
		log.Printf("[change-tracker] failed to persist auto-skip dirs after bulk rollup: %v", err)
	}
}
