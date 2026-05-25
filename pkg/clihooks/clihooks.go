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
	mu          sync.RWMutex
	suspendFunc func()
	steerPause  func()
	steerResume func()
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

// SetSteerHooks installs the pause/resume hooks for the SP-055
// SteerInputReader. The reader holds stdin in raw mode while a turn
// is in flight, which blocks any cooked-mode bufio reader (e.g. the
// security elevation prompt in pkg/utils.AskForConfirmation) from
// receiving input. Owners — typically SteerCoordinator — register
// the pair on StartTurn and clear it (with nil/nil) on EndTurn.
//
// pause must stop the reader's goroutine and restore cooked termios.
// resume must re-enter raw mode and restart the reader. Both must be
// idempotent.
func SetSteerHooks(pause, resume func()) {
	mu.Lock()
	defer mu.Unlock()
	steerPause = pause
	steerResume = resume
}

// PauseSteer runs the registered pause hook if one is set. Callers
// that are about to read stdin in cooked mode (interactive prompts)
// MUST pair this with a deferred ResumeSteer so the reader resumes
// when the prompt returns. No-op when no hook is registered (e.g.
// non-interactive run, no active turn).
func PauseSteer() {
	mu.RLock()
	fn := steerPause
	mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ResumeSteer runs the registered resume hook if one is set.
func ResumeSteer() {
	mu.RLock()
	fn := steerResume
	mu.RUnlock()
	if fn != nil {
		fn()
	}
}
