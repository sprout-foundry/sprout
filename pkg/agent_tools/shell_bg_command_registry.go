// Registry mapping background shell sessions to the command string that
// started them.
//
// The ChangeTracker's destructive-command classifier (shellIsDestructive)
// needs the ORIGINAL command to decide whether a completed background
// session reverts working-tree state (git checkout / reset --hard / stash
// pop run with background=true). The completion-observation points
// (check_background, stop_background, wakeup watcher) only receive the
// session ID, so without this registry they pass a synthetic
// "background session <id> (completed)" label — the classifier sees an
// unknown program and the walk runs in non-destructive mode, losing
// before-content recovery for auto-skipped directories.
package tools

import (
	"sync"
)

// bgCommandRegistry is a process-wide bounded map of sessionID → command.
// Entries are written when a background session starts and removed once
// the command string is consumed at completion observation. Bounded so a
// long-lived daemon that leaks sessions (killed without check_background)
// can't grow it without limit.
var bgCommandRegistry = struct {
	mu       sync.RWMutex
	commands map[string]string
}{commands: map[string]string{}}

// bgCommandRegistryMax bounds the registry. 1024 concurrent background
// sessions in one process is far beyond anything a single agent session
// produces; overflow drops the OLDEST entries (map iteration order is
// random, which is acceptable — the fallback is the synthetic label, not
// an error).
const bgCommandRegistryMax = 1024

// rememberBackgroundCommand records the command for a background session.
// Callers pass the session ID parsed from the start result and the exact
// command string the agent submitted.
func rememberBackgroundCommand(sessionID, command string) {
	if sessionID == "" || command == "" {
		return
	}
	bgCommandRegistry.mu.Lock()
	defer bgCommandRegistry.mu.Unlock()
	if len(bgCommandRegistry.commands) >= bgCommandRegistryMax {
		for k := range bgCommandRegistry.commands {
			delete(bgCommandRegistry.commands, k)
			if len(bgCommandRegistry.commands) < bgCommandRegistryMax {
				break
			}
		}
	}
	bgCommandRegistry.commands[sessionID] = command
}

// backgroundCommandFor returns the recorded command for a session and
// forgets it. The fallback keeps the historical synthetic label so
// provenance in the tracked-change manifest survives unknown sessions
// (pre-promotion sessions, process restarts).
func backgroundCommandFor(sessionID string) string {
	bgCommandRegistry.mu.Lock()
	defer bgCommandRegistry.mu.Unlock()
	cmd, ok := bgCommandRegistry.commands[sessionID]
	if ok {
		delete(bgCommandRegistry.commands, sessionID)
	}
	return cmd
}
