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
	if ct.autoSkipDirs == nil {
		ct.autoSkipDirs = map[string]bool{}
	}
	if ct.autoSkipDirs[abs] {
		return
	}
	ct.autoSkipDirs[abs] = true
	if err := saveAutoSkipDirsFor(workspaceRoot, ct.autoSkipDirs); err != nil {
		log.Printf("[change-tracker] failed to persist auto-skip dirs after bulk rollup: %v", err)
	}
}
