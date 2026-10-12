package agent

import (
	"os"

	"github.com/sprout-foundry/sprout/pkg/console"
)

// ForceSaveAndExit performs a best-effort synchronous state save and exits.
// It backs the CLI's force-quit paths (second Ctrl+C, post-shutdown signal)
// where deferred saves never run — without it, an impatient exit discards
// the entire turn. Never returns.
func (a *Agent) ForceSaveAndExit(code int) {
	if a != nil && a.state != nil && !a.IsSubagent() {
		a.autoSaveState()
	}
	console.RestoreTerminal()
	os.Exit(code)
}

// FlushState synchronously writes the conversation to its session file. The
// web daemon calls it before the workspace stops so the transcript on disk
// matches what the user last saw. Agents with no messages are skipped so an
// idle chat does not leave an empty session file behind.
func (a *Agent) FlushState() {
	if a == nil || a.state == nil || a.IsSubagent() || len(a.GetMessages()) == 0 {
		return
	}
	a.autoSaveState()
}
