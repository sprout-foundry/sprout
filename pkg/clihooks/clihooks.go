// Package clihooks holds tiny callback hooks that wire higher-level CLI
// behavior (e.g. the activity-indicator spinner in pkg/console) into
// lower-level code paths (e.g. pkg/utils' AskForConfirmation, pkg/agent_tools'
// AskUser) without creating import cycles.
//
// Owners of the hooks call SetSuspendIndicator at startup to register their
// implementation. Callers that are about to render a prompt invoke
// SuspendIndicator to clear the spinner from the terminal first. If no
// implementation is registered, SuspendIndicator is a no-op.
package clihooks

import "sync"

var (
	mu              sync.RWMutex
	suspendFunc     func()
)

// SetSuspendIndicator installs (or clears, with nil) the global function
// used to stop the active CLI activity indicator. Called by the indicator
// owner (typically the agent CLI entry point).
func SetSuspendIndicator(fn func()) {
	mu.Lock()
	defer mu.Unlock()
	suspendFunc = fn
}

// SuspendIndicator runs the registered suspend hook if one is set. Safe to
// call from anywhere; no-op when nothing is registered.
func SuspendIndicator() {
	mu.RLock()
	fn := suspendFunc
	mu.RUnlock()
	if fn != nil {
		fn()
	}
}
