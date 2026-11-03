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
	mu             sync.RWMutex
	suspendFunc    func()
	resumeFunc     func()
	steerPause     func()
	steerResume    func()
	streamingDepth atomic.Int32
	steerDepth     atomic.Int32
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
//
// SetSteerHooks also resets the pause refcount. That makes it a hard reset
// for tests and teardown, not a turn boundary: production turns use
// BeginSteerTurn / EndSteerTurn, which keep the count, because a prompt can
// be open across a boundary (a parallel or background run's question when
// the turn starts or ends) and its pause must still be honored.
func SetSteerHooks(pause, resume func()) {
	steerMu.Lock()
	defer steerMu.Unlock()
	mu.Lock()
	defer mu.Unlock()
	steerPause = pause
	steerResume = resume
	steerDepth.Store(0)
}

// steerMu orders the reader's start/stop transitions: the outermost pause,
// the outermost resume, and a turn installing or removing the hooks. Without
// it a turn could start the reader between a prompt's pause and the prompt's
// first read, leaving two readers on stdin.
var steerMu sync.Mutex

// BeginSteerTurn installs a turn's steer hooks and starts the reader —
// unless a prompt holds a pause, in which case the reader stays stopped and
// the prompt's outermost ResumeSteer starts it when the prompt closes.
func BeginSteerTurn(pause, resume func()) {
	steerMu.Lock()
	defer steerMu.Unlock()
	mu.Lock()
	steerPause, steerResume = pause, resume
	mu.Unlock()
	if steerDepth.Load() == 0 && resume != nil {
		resume()
	}
}

// EndSteerTurn removes the turn's hooks and stops the reader. When a prompt
// holds a pause the reader is already stopped and is left alone: stopping it
// again would restore cooked termios under a picker reading in raw mode.
func EndSteerTurn() {
	steerMu.Lock()
	defer steerMu.Unlock()
	mu.Lock()
	pause := steerPause
	steerPause, steerResume = nil, nil
	mu.Unlock()
	if steerDepth.Load() == 0 && pause != nil {
		pause()
	}
}

// SteerPaused reports whether a prompt currently holds a steer pause.
func SteerPaused() bool { return steerDepth.Load() > 0 }

// SteerHooks returns the currently installed steer pause/resume pair (both
// nil when none). Callers that must install their own pair temporarily — a
// test, or a nested prompt that needs to restore the outer owner's hooks —
// use it to save the pair before SetSteerHooks and restore it afterwards.
func SteerHooks() (pause, resume func()) {
	mu.RLock()
	defer mu.RUnlock()
	return steerPause, steerResume
}

// PauseSteer runs the registered pause hook if one is set. Callers
// that are about to read stdin in cooked mode (interactive prompts)
// MUST pair this with a deferred ResumeSteer so the reader resumes
// when the prompt returns. No-op when no hook is registered (e.g.
// non-interactive run, no active turn).
//
// Refcounted like SuspendStreaming: overlapping prompts (an approval
// prompt inside a WithCookedStdin callback, a parallel subagent's
// ask_user) each keep the pause alive for their own duration. Only
// the OUTERMOST PauseSteer actually stops the reader; inner pauses
// just count. A stray early resume from one prompt cannot resurrect
// the reader while another prompt is still on screen — the last-
// writer-wins behavior previously let an inner ResumeSteer restart
// the readLoop under a cooked-mode prompt, and the prompt's termios
// restore then wedged that loop in a blocking Read (VMIN=1) past
// Stop()'s 2s timeout.
func PauseSteer() {
	steerMu.Lock()
	defer steerMu.Unlock()
	if steerDepth.Add(1) > 1 {
		return
	}
	mu.RLock()
	fn := steerPause
	mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// ResumeSteer unwinds one PauseSteer. Only the outermost resume (depth
// returns to zero) restarts the reader. Clamped at zero so an
// unbalanced extra resume can never go negative and suppress a later
// legit pause.
func ResumeSteer() {
	steerMu.Lock()
	defer steerMu.Unlock()
	if steerDepth.Load() <= 0 {
		return
	}
	if steerDepth.Add(-1) > 0 {
		return
	}
	mu.RLock()
	fn := steerResume
	mu.RUnlock()
	if fn != nil {
		fn()
	}
}

// WithCookedStdin runs fn with the activity-indicator suspended, the
// SteerInputReader paused, and streaming suspended so stdin is back in
// cooked mode for the duration of the call and no streaming prose
// clobbers anything the callback renders. Wraps the SuspendIndicator +
// SuspendStreaming + PauseSteer / ResumeSteer + ResumeStreaming +
// ResumeIndicator dance that interactive prompts and child-process
// editors must perform when invoked during a turn — the steer reader
// otherwise holds stdin in raw mode and bufio.Reader / term.ReadPassword
// calls hit EOF immediately.
//
// Safe to call even when no indicator or steer reader is registered
// (e.g. non-interactive runs, slash commands that fire before the
// first turn): all hooks no-op in that case.
//
// Callers that prefer the explicit pattern can still do so directly;
// this helper exists so the most common "I'm about to read stdin or
// spawn an interactive child process" idiom is a one-liner that's
// hard to get wrong.
func WithCookedStdin(fn func() error) error {
	SuspendIndicator()
	SuspendStreaming()
	PauseSteer()
	defer ResumeSteer()
	defer ResumeStreaming()
	defer ResumeIndicator()
	return fn()
}

// SuspendStreaming increments the streaming-suspension depth. The
// streaming callback (see IsStreamingSuspended) suppresses prose while
// the depth is greater than zero. Used by interactive prompts (security
// approvals, edit review) that render to the terminal while the agent's
// streaming goroutine may still be receiving chunks — without this, the
// streaming callback clobbers the prompt with mid-stream prose.
//
// The depth is refcounted so overlapping prompts (e.g. a parallel
// subagent's ask_user plus a security elevation prompt) each keep the
// suspension alive for their own duration; a stray early ResumeStreaming
// from one prompt cannot clear the flag while another prompt is still on
// screen. Callers MUST pair each SuspendStreaming with a deferred
// ResumeStreaming.
func SuspendStreaming() {
	streamingDepth.Add(1)
}

// ResumeStreaming decrements the streaming-suspension depth. Clamped at
// zero so an unbalanced extra resume can never drive the counter
// negative and mask a later suspend. Safe to call when nothing was
// suspended (the counter floors at zero).
func ResumeStreaming() {
	for {
		cur := streamingDepth.Load()
		if cur <= 0 {
			return
		}
		if streamingDepth.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// IsStreamingSuspended reports whether SuspendStreaming is active.
// Called by the streaming callback to decide whether to suppress a chunk.
func IsStreamingSuspended() bool {
	return streamingDepth.Load() > 0
}

// foregroundChildDepth counts children currently holding the terminal in
// sprout's own process group (a `!` command). Ctrl+C at the terminal then
// reaches both processes; it belongs to the child — answering a password
// prompt with Ctrl+C must cancel sudo, not shut sprout down.
var foregroundChildDepth atomic.Int32

// BeginForegroundChild marks a terminal-attached child as running. Pair
// with a deferred EndForegroundChild.
func BeginForegroundChild() { foregroundChildDepth.Add(1) }

// EndForegroundChild unwinds one BeginForegroundChild.
func EndForegroundChild() {
	for {
		cur := foregroundChildDepth.Load()
		if cur <= 0 || foregroundChildDepth.CompareAndSwap(cur, cur-1) {
			return
		}
	}
}

// ForegroundChildActive reports whether a terminal-attached child is
// running, so sprout's SIGINT handler leaves the signal to it.
func ForegroundChildActive() bool { return foregroundChildDepth.Load() > 0 }
