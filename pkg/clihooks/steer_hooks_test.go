package clihooks

import (
	"sync"
	"testing"
)

// steerHookRecorder captures pause/resume hook invocations in order.
type steerHookRecorder struct {
	mu    sync.Mutex
	calls []string
}

func (r *steerHookRecorder) pause()  { r.record("pause") }
func (r *steerHookRecorder) resume() { r.record("resume") }

func (r *steerHookRecorder) record(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, s)
}

func (r *steerHookRecorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

func withSteerHooks(t *testing.T, r *steerHookRecorder) {
	t.Helper()
	SetSteerHooks(r.pause, r.resume)
	t.Cleanup(func() { SetSteerHooks(nil, nil) })
}

// The outermost pause runs the hook; nested pauses only count.
func TestPauseSteer_RefcountedNesting(t *testing.T) {
	rec := &steerHookRecorder{}
	withSteerHooks(t, rec)

	PauseSteer()
	PauseSteer()
	PauseSteer()

	calls := rec.snapshot()
	if len(calls) != 1 || calls[0] != "pause" {
		t.Fatalf("nested pauses must fire the hook once; got %v", calls)
	}

	// Inner resumes must not resume while an outer pause is live.
	ResumeSteer()
	ResumeSteer()
	calls = rec.snapshot()
	if len(calls) != 1 {
		t.Fatalf("inner resumes must not fire the resume hook; got %v", calls)
	}

	// The final (outermost) resume fires the hook exactly once.
	ResumeSteer()
	calls = rec.snapshot()
	if len(calls) != 2 || calls[1] != "resume" {
		t.Fatalf("outermost resume must fire the hook; got %v", calls)
	}
}

// An unbalanced extra resume clamps at zero and must not suppress a
// subsequent pause.
func TestResumeSteer_ClampedAtZero(t *testing.T) {
	rec := &steerHookRecorder{}
	withSteerHooks(t, rec)

	ResumeSteer() // no matching pause — must be a no-op
	if calls := rec.snapshot(); len(calls) != 0 {
		t.Fatalf("unbalanced resume must not fire the hook; got %v", calls)
	}

	PauseSteer()
	ResumeSteer()
	calls := rec.snapshot()
	if len(calls) != 2 || calls[0] != "pause" || calls[1] != "resume" {
		t.Fatalf("pause after clamp must work normally; got %v", calls)
	}
}

// SetSteerHooks is the hard reset (tests, teardown): it clears the refcount
// so state left by one owner cannot affect the next.
func TestSetSteerHooks_ResetsDepth(t *testing.T) {
	rec := &steerHookRecorder{}
	withSteerHooks(t, rec)

	PauseSteer()
	PauseSteer() // leaked: never resumed

	// Turn boundary — EndTurn clears the hooks (and in production the
	// next StartTurn reinstalls them).
	SetSteerHooks(nil, nil)
	rec2 := &steerHookRecorder{}
	SetSteerHooks(rec2.pause, rec2.resume)

	// The new turn's first pause must actually stop the reader.
	PauseSteer()
	ResumeSteer()
	calls := rec2.snapshot()
	if len(calls) != 2 || calls[0] != "pause" || calls[1] != "resume" {
		t.Fatalf("first pause after turn boundary must fire the hook; got %v", calls)
	}
}

// Pauses with no hook installed must still count, so a pause/resume
// pair around a hook installation stays balanced.
func TestPauseSteer_NoHookStillCounts(t *testing.T) {
	SetSteerHooks(nil, nil)
	PauseSteer() // depth 1, no hook

	rec := &steerHookRecorder{}
	SetSteerHooks(rec.pause, rec.resume) // resets depth — see ResestsDepth test

	PauseSteer()
	if calls := rec.snapshot(); len(calls) != 1 {
		t.Fatalf("pause after reset must fire; got %v", calls)
	}
	ResumeSteer()
	if calls := rec.snapshot(); len(calls) != 2 {
		t.Fatalf("resume must fire; got %v", calls)
	}
}

// A turn that starts while a question is open (a parallel or background
// run's) must not start the reader under the question's picker; the
// question's own resume starts it when the question closes.
func TestBeginSteerTurn_DuringOpenPromptDefersStart(t *testing.T) {
	SetSteerHooks(nil, nil)
	t.Cleanup(func() { SetSteerHooks(nil, nil) })

	PauseSteer() // the question opens between turns
	rec := &steerHookRecorder{}
	BeginSteerTurn(rec.pause, rec.resume)
	if calls := rec.snapshot(); len(calls) != 0 {
		t.Fatalf("the reader must not start under an open question; got %v", calls)
	}
	ResumeSteer() // the question closes
	if calls := rec.snapshot(); len(calls) != 1 || calls[0] != "resume" {
		t.Fatalf("closing the question starts the turn's reader; got %v", calls)
	}
	EndSteerTurn()
	if calls := rec.snapshot(); len(calls) != 2 || calls[1] != "pause" {
		t.Fatalf("ending the turn stops the reader; got %v", calls)
	}
}

// A turn that ends while a question is open leaves the reader to the
// question (stopping it again would re-cook the terminal under the picker),
// and the question's resume does not restart a reader whose turn is over.
func TestEndSteerTurn_DuringOpenPromptLeavesReaderAlone(t *testing.T) {
	SetSteerHooks(nil, nil)
	t.Cleanup(func() { SetSteerHooks(nil, nil) })

	rec := &steerHookRecorder{}
	BeginSteerTurn(rec.pause, rec.resume) // resume: reader starts
	PauseSteer()                          // pause: a question opens
	EndSteerTurn()                        // nothing: the question owns stdin
	ResumeSteer()                         // nothing: no turn to resume
	got := rec.snapshot()
	want := []string{"resume", "pause"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("calls = %v, want %v", got, want)
	}
	if SteerPaused() {
		t.Fatal("the count is balanced once the question closes")
	}
}
