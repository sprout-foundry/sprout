package console

// activity_indicator_global.go — the process-wide indicator registry
// (globalIndicator + its suspend/resumable-suspend bookkeeping) and the
// Register/Suspend/Resume entry points used by far-flung CLI prompt sites.
// Split out of activity_indicator.go.
import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// Global indicator registration so far-flung CLI prompt sites can suspend
// the active spinner without taking a direct dependency on whatever owns
// the indicator. Use RegisterGlobalIndicator from the CLI entry point.
// Code that cannot import pkg/console (e.g. pkg/utils, pkg/agent_tools)
// suspends via the leaf-only bridge in pkg/clihooks.
var (
	globalIndicator   *ActivityIndicator
	globalIndicatorMu sync.RWMutex
	// Resumable-suspend bookkeeping (all guarded by globalIndicatorMu).
	// indicatorSuspendDepth refcounts SuspendIndicatorResumable calls so
	// nested suspends (e.g. a password prompter invoked inside
	// WithCookedStdin) don't clobber the outermost suspend's restore
	// state. indicatorResume holds the restore closure captured at the
	// outermost level; it is nil when nothing is (or no longer is)
	// suspended.
	indicatorSuspendDepth int32
	indicatorResume       func()
)

// RegisterGlobalIndicator installs ind as the process-wide indicator that
// SuspendIndicator and clihooks.SuspendIndicator both target. Pass nil to
// clear. Safe to call multiple times.
//
// The clihooks suspend hook is wired to SuspendIndicatorResumable so
// prompt sites that pair clihooks.SuspendIndicator with a deferred
// clihooks.ResumeIndicator restore the spinner (same message) when the
// prompt ends, instead of leaving it dead until the next tool event.
func RegisterGlobalIndicator(ind *ActivityIndicator) {
	globalIndicatorMu.Lock()
	defer globalIndicatorMu.Unlock()
	globalIndicator = ind
	if ind != nil {
		clihooks.SetSuspendIndicator(SuspendIndicatorResumable)
		clihooks.SetResumeIndicator(ResumeIndicatorFromSuspend)
	} else {
		clihooks.SetSuspendIndicator(nil)
		clihooks.SetResumeIndicator(nil)
	}
	indicatorResume = nil
}

// SuspendIndicator stops the registered global activity indicator if one is
// active. Safe to call when no indicator is registered (no-op) or when the
// indicator is already stopped (idempotent). Use this immediately before
// rendering an interactive CLI prompt to keep the spinner from overwriting
// the prompt text. Mirrored by clihooks.SuspendIndicator for callers that
// can't import pkg/console.
//
// This local function intentionally does NOT auto-resume: callers that
// suspend "until the next event" (e.g. PrintExternal, whose message is
// followed by a subscriber ToolStart that restarts the spinner) must keep
// that semantic. The resumable variant is SuspendIndicatorResumable.
func SuspendIndicator() {
	globalIndicatorMu.RLock()
	a := globalIndicator
	globalIndicatorMu.RUnlock()
	a.Stop()
}

// SuspendIndicatorResumable stops the registered global activity indicator
// (at the outermost suspend) and remembers its pre-suspend message + active
// state so a later call to ResumeIndicatorFromSuspend can restore it.
// This is the hook wired into clihooks.SuspendIndicator by
// RegisterGlobalIndicator; prompt sites that own the whole prompt lifetime
// pair it with a deferred clihooks.ResumeIndicator.
//
// Restoring by unconditionally restarting the spinner would resurrect a
// dead spinner after prompt completion even when the terminal subscriber
// already restarted it mid-tool-execution — so the restore closure checks
// whether the spinner is already active before starting it.
func SuspendIndicatorResumable() {
	globalIndicatorMu.Lock()
	a := globalIndicator
	if a == nil {
		globalIndicatorMu.Unlock()
		return
	}
	indicatorSuspendDepth++
	depth := indicatorSuspendDepth
	// Only the outermost suspend snapshots and stops; nested suspends
	// just refcount so the restore fires exactly when the last one
	// unwinds.
	if depth == 1 {
		msg, wasActive := a.ActiveMessage()
		a.Stop()
		if wasActive {
			// Restore closure captures the suspended indicator instance
			// and its message.
			indicatorResume = func() {
				if _, active := a.ActiveMessage(); active {
					return
				}
				// Skip the restart when assistant prose is actively
				// streaming: resurrecting the spinner mid-stream would
				// clobber the prose line (the streaming callback only
				// re-stops it on the next chunk, up to 80ms later).
				if footer := GetGlobalStatusFooter(); footer != nil && footer.proseStreamingActive() {
					return
				}
				a.Start(msg)
			}
		} else {
			indicatorResume = nil
		}
	}
	globalIndicatorMu.Unlock()
}

// ResumeIndicatorFromSuspend unwinds one SuspendIndicatorResumable. Only
// the outermost resume performs the actual restore; redundant or extra
// resumes are no-ops. Wired into clihooks.ResumeIndicator by
// RegisterGlobalIndicator.
func ResumeIndicatorFromSuspend() {
	globalIndicatorMu.Lock()
	if indicatorSuspendDepth > 0 {
		indicatorSuspendDepth--
	}
	if indicatorSuspendDepth > 0 {
		globalIndicatorMu.Unlock()
		return
	}
	restore := indicatorResume
	indicatorResume = nil
	globalIndicatorMu.Unlock()
	if restore != nil {
		restore()
	}
}
