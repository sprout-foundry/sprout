package agent

import (
	"errors"
)

// TryBeginQuery attempts to mark this Agent as "query in progress." Returns
// ErrQueryInProgress if a query is already running on this Agent instance.
// The caller MUST call EndQuery when done (typically via defer) to release
// the flag.
//
// This is the concurrency guard for shared-agent mode: when the CLI REPL and
// the WebUI use the same *Agent (non-daemon interactive mode), only one
// ProcessQuery can execute at a time. The losing caller gets the error and
// must either retry or present a "busy" message to the user.
//
// For standalone daemon mode (separate agents per chat session) this flag is
// never contended because each chat has its own Agent, so it's effectively
// a no-op.
func (a *Agent) TryBeginQuery() error {
	if a == nil {
		return errors.New("agent is nil")
	}
	if !a.queryInProgress.CompareAndSwap(false, true) {
		return ErrQueryInProgress
	}
	return nil
}

// EndQuery releases the "query in progress" flag set by TryBeginQuery.
// Safe to call multiple times and safe to call when the flag is already
// clear (idempotent).
func (a *Agent) EndQuery() {
	if a == nil {
		return
	}
	a.queryInProgress.Store(false)
}

// IsQueryInProgress reports whether a query is currently executing on this
// Agent. Used by the WebUI to report busy state and by the CLI to check
// before starting a new query.
func (a *Agent) IsQueryInProgress() bool {
	if a == nil {
		return false
	}
	return a.queryInProgress.Load()
}
