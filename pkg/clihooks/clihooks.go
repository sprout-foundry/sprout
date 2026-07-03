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

import (
	"sync"
	"sync/atomic"
)

var (
	mu                sync.RWMutex
	suspendFunc       func()
	resumeFunc        func()
	steerPause        func()
	steerResume       func()
	streamingSuspended atomic.Bool
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

// SetResumeIndicator installs (or clears, with nil) the global function
// used to resume the active CLI activity indicator. Called by the indicator
// owner (typically the agent CLI entry point).
func SetResumeIndicator(fn func()) {
	mu.Lock()
	defer mu.Unlock()
	resumeFunc = fn
}

// ResumeIndicator runs the registered resume hook if one is set. Safe to
// call from anywhere; no-op when nothing is registered.
func ResumeIndicator() {
	mu.RLock()
	fn := resumeFunc
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

// WithCookedStdin runs fn with the activity-indicator suspended and the
// SteerInputReader paused so stdin is back in cooked mode for the
// duration of the call. Wraps the SuspendIndicator + PauseSteer /
// ResumeSteer dance that interactive prompts and child-process
// editors must perform when invoked during a turn — the steer reader
// otherwise holds stdin in raw mode and bufio.Reader / term.ReadPassword
// calls hit EOF immediately.
//
// Safe to call even when no indicator or steer reader is registered
// (e.g. non-interactive runs, slash commands that fire before the
// first turn): both hooks no-op in that case.
//
// Callers that prefer the explicit pattern can still do so directly;
// this helper exists so the most common "I'm about to read stdin or
// spawn an interactive child process" idiom is a one-liner that's
// hard to get wrong.
func WithCookedStdin(fn func() error) error {
	SuspendIndicator()
	PauseSteer()
	defer ResumeSteer()
	return fn()
}

// SuspendStreaming sets a flag that the streaming callback checks before
// writing prose to the terminal. Used by interactive prompts (security
// approvals, edit review) that render to the terminal while the agent's
// streaming goroutine may still be receiving chunks — without this, the
// streaming callback clobbers the prompt with mid-stream prose.
//
// The flag is process-global and atomic; callers MUST pair this with a
// deferred ResumeStreaming to avoid permanently suppressing output.
func SuspendStreaming() {
	streamingSuspended.Store(true)
}

// ResumeStreaming clears the SuspendStreaming flag. Safe to call when
// streaming was never suspended (the flag defaults to false).
func ResumeStreaming() {
	streamingSuspended.Store(false)
}

// IsStreamingSuspended reports whether SuspendStreaming is active.
// Called by the streaming callback to decide whether to suppress a chunk.
func IsStreamingSuspended() bool {
	return streamingSuspended.Load()
}
