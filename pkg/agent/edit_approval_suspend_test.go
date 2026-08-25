package agent

// Regression tests for the CLI spinner-coordination pattern.
//
// Background — the CLI has a multi-line activity indicator (spinner) that
// runs in raw mode on the terminal during a turn. When interactive prompts
// are rendered to stderr while the spinner is active, the spinner overwrites
// the prompt text. To prevent this, callers must suspend three things before
// reading from stdin / rendering a prompt:
//
//   1. The activity indicator (clihooks.SuspendIndicator)
//   2. The SP-055 SteerInputReader that holds stdin in raw mode
//      (clihooks.PauseSteer)
//   3. The streaming callback that writes prose chunks to the terminal
//      (clihooks.SuspendStreaming)
//
// Each must be paired with the corresponding Resume so the spinner / steer
// reader / streaming come back online after the prompt returns.
//
// The canonical pattern is wrapped in clihooks.WithCookedStdin(fn) (which
// handles SuspendIndicator + PauseSteer / their inverse pairing). Callers
// that also need streaming suspension must call clihooks.SuspendStreaming
// explicitly with a deferred ResumeStreaming.
//
// The bugs this guards against are particularly insidious because:
//
//   - They only manifest on a real TTY with a live spinner. CI / `go test`
//     runs never see the corruption, so the code can ship with the hooks
//     removed and pass every automated test.
//   - The production hook functions are no-ops when nothing is registered,
//     so a regression that drops the Suspend call is silently equivalent
//     to working code in any test that doesn't install a recorder.
//
// These tests catch the regression by installing recorder hooks on
// clihooks that count invocations, then driving the production function
// paths and asserting the counts. If a future change removes any of the
// Suspend calls, the assertion fails loudly.

import (
	"sync"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/clihooks"
)

// hookRecorder counts invocations of each clihooks hook that production
// callers must fire before reading stdin / rendering a prompt. It uses
// atomic-friendly counters guarded by a mutex so it is safe to read
// after the production call returns even if other tests have left the
// global hooks in unexpected state.
type hookRecorder struct {
	mu          sync.Mutex
	suspend     int
	resume      int
	pauseSteer  int
	resumeSteer int
	// streamingSuspendedSeen is true if SuspendStreaming was observed
	// at any point during the production call. We sample IsStreamingSuspended
	// from inside the recorder hooks themselves so we don't have to rely
	// on the order in which Go schedules the test goroutine vs the
	// production code path.
	streamingSuspendedSeen bool
}

func newHookRecorder() *hookRecorder { return &hookRecorder{} }

func (r *hookRecorder) snapshot() (suspend, resume, pause, resumeSteer int, sawStreaming bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.suspend, r.resume, r.pauseSteer, r.resumeSteer, r.streamingSuspendedSeen
}

// installHooks wires the recorder into clihooks and returns a cleanup
// function that resets every hook back to nil. The cleanup is
// idempotent and safe to call multiple times.
//
// Streaming is a process-global atomic.Bool (no install / uninstall
// API), so we sample IsStreamingSuspended from inside the Suspend
// hooks themselves rather than wrapping SuspendStreaming. See
// hookRecorder.streamingSuspendedSeen for details.
func installHooks(t *testing.T, r *hookRecorder) func() {
	t.Helper()

	// Reset streaming flag first so leftover state from a previous test
	// can't mask a regression that forgets to call SuspendStreaming.
	clihooks.ResumeStreaming()

	clihooks.SetSuspendIndicator(func() {
		r.mu.Lock()
		r.suspend++
		// Record whether streaming is currently suspended — the order in
		// which SuspendIndicator and SuspendStreaming fire is fixed by
		// production code, but the recorder only needs "saw it set" to
		// satisfy the regression check.
		r.streamingSuspendedSeen = r.streamingSuspendedSeen || clihooks.IsStreamingSuspended()
		r.mu.Unlock()
	})
	clihooks.SetResumeIndicator(func() {
		r.mu.Lock()
		r.resume++
		r.mu.Unlock()
	})
	clihooks.SetSteerHooks(
		func() {
			r.mu.Lock()
			r.pauseSteer++
			r.streamingSuspendedSeen = r.streamingSuspendedSeen || clihooks.IsStreamingSuspended()
			r.mu.Unlock()
		},
		func() {
			r.mu.Lock()
			r.resumeSteer++
			r.mu.Unlock()
		},
	)

	return func() {
		clihooks.SetSuspendIndicator(nil)
		clihooks.SetResumeIndicator(nil)
		clihooks.SetSteerHooks(nil, nil)
		clihooks.ResumeStreaming()
	}
}

// ---------------------------------------------------------------------------
// requestCLIEditApproval must suspend all three hooks before prompting.
// ---------------------------------------------------------------------------

// TestRequestCLIEditApproval_SuspendsAllHooks is the primary regression
// guard. It drives requestCLIEditApproval (the unexported CLI path
// that RequestEditApproval falls through to when stdin is a TTY) and
// verifies each of the three Suspend hooks fires.
//
// The function reads from os.Stdin; we point that at a freshly created
// pipe whose write end is closed so the very first Scan() returns
// false / EOF. requestCLIEditApproval's documented behaviour on EOF
// is "treat empty as the user-default 'y'" — matching the historical
// fmt.Fscanln behaviour where mid-hunk EOF silently accepted the
// remaining hunks. The hook assertions below pin the spinner-dance
// without depending on which exact decision is returned.
func TestRequestCLIEditApproval_SuspendsAllHooks(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	recorder := newHookRecorder()
	cleanup := installHooks(t, recorder)
	defer cleanup()

	// Redirect os.Stdin to a closed pipe so the scanner gets EOF on the
	// first Scan(). After this returns, the reader is in EOF state and
	// any call to read from it returns immediately with io.EOF.
	w, restore := redirectStdinToPipe(t)
	defer restore()
	_ = w.Close() // close write end immediately -> EOF on read

	proposal := EditProposal{
		Path:     "spinner_regression.go",
		Original: "package x\n\n// old\nfunc F() {}\n",
		Proposed: "package x\n\n// new\nfunc F() {}\n",
	}

	// On EOF, requestCLIEditApproval treats the empty read as "y" (the
	// user-default behaviour preserved from fmt.Fscanln). We don't pin
	// the decision here so a future semantics change doesn't break
	// this test — the assertions below only depend on the function
	// returning at all.
	_ = a.requestCLIEditApproval(proposal)

	suspend, _, pause, resumeSteer, sawStreaming := recorder.snapshot()

	// 1) The activity indicator must have been suspended exactly once
	//    per closure invocation. We pin the count to 1 (rather than >=1)
	//    so a future refactor that accidentally calls SuspendIndicator
	//    from inside a helper invoked twice during the same prompt path
	//    is also caught.
	if suspend != 1 {
		t.Errorf("regression: requestCLIEditApproval should call clihooks.SuspendIndicator exactly once "+
			"(calls=%d) — the spinner would clobber the prompt", suspend)
	}

	// 2) The steer reader must have been paused exactly once so stdin is
	//    released from raw mode for the bufio.Scanner below it.
	if pause != 1 {
		t.Errorf("regression: requestCLIEditApproval should call clihooks.PauseSteer exactly once "+
			"(calls=%d) — stdin would remain in raw mode and Scan() would return garbage", pause)
	}

	// 3) The streaming callback must have been suspended at least once so
	//    a mid-turn fall-through isn't trampled by a concurrent chunk
	//    write. Streaming is a process-global atomic.Bool so we sampled
	//    IsStreamingSuspended from inside the suspend hooks themselves.
	if !sawStreaming {
		t.Errorf("regression: requestCLIEditApproval did not call clihooks.SuspendStreaming " +
			"before SuspendIndicator/PauseSteer — streaming prose could clobber the prompt")
	}

	// 4) Pairing: WithCookedStdin defers ResumeSteer on its own. We
	//    check it explicitly so a future refactor that drops the
	//    ResumeSteer defer is caught here. (ResumeIndicator is not
	//    paired by WithCookedStdin — see comment above.)
	if resumeSteer != pause {
		t.Errorf("regression: ResumeSteer under/over-paired — paused=%d but resumed=%d", pause, resumeSteer)
	}
}

// TestRequestCLIEditApproval_SuspendsIndicatorEvenOnNoHunks documents that
// the hook dance fires regardless of how many hunks the proposal carries.
// A naive "short-circuit when there are no hunks" optimisation would
// reintroduce the bug for empty / single-hunk proposals.
func TestRequestCLIEditApproval_SuspendsIndicatorEvenOnNoHunks(t *testing.T) {
	a := newTestAgent(t)
	defer a.Shutdown()

	recorder := newHookRecorder()
	cleanup := installHooks(t, recorder)
	defer cleanup()

	w, restore := redirectStdinToPipe(t)
	defer restore()
	_ = w.Close()

	// Empty proposal: original == proposed => SplitIntoHunks returns nil.
	proposal := EditProposal{
		Path:     "empty.go",
		Original: "package x\n",
		Proposed: "package x\n",
	}

	_ = a.requestCLIEditApproval(proposal)

	suspend, _, pause, _, sawStreaming := recorder.snapshot()
	if suspend != 1 || pause != 1 || !sawStreaming {
		t.Errorf("regression: hooks not fired correctly on no-hunks proposal "+
			"(suspend=%d, pauseSteer=%d, sawStreaming=%v)", suspend, pause, sawStreaming)
	}
}

// ---------------------------------------------------------------------------
// Sanity: the global hook plumbing is what we think it is.
// ---------------------------------------------------------------------------

// TestInstallHooks_StripsRecorderOnCleanup pins the test harness: after
// cleanup(), no recorder state should remain registered. Without this
// guarantee, a flaky test could leak Suspend calls into the next test
// and produce false positives.
func TestInstallHooks_StripsRecorderOnCleanup(t *testing.T) {
	recorder := newHookRecorder()
	cleanup := installHooks(t, recorder)
	cleanup()

	// Trigger each hook entry point. None should reach the recorder.
	clihooks.SuspendIndicator()
	clihooks.ResumeIndicator()
	clihooks.PauseSteer()
	clihooks.ResumeSteer()

	suspend, resume, pause, resumeSteer, _ := recorder.snapshot()
	if suspend != 0 || resume != 0 || pause != 0 || resumeSteer != 0 {
		t.Errorf("hooks leaked after cleanup: suspend=%d resume=%d pause=%d resumeSteer=%d",
			suspend, resume, pause, resumeSteer)
	}
}
