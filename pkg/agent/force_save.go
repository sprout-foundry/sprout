package agent

import (
	"os"
)

// ForceSaveAndExit performs a best-effort synchronous state save and exits.
// It backs the CLI's force-quit paths (second Ctrl+C, post-shutdown signal)
// where deferred saves never run — without it, an impatient exit discards
// the entire turn. Never returns.
func (a *Agent) ForceSaveAndExit(code int) {
	if a != nil && a.state != nil && !a.IsSubagent() {
		a.autoSaveState()
	}
	os.Exit(code)
}
