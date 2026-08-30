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

// SetSteerHooks resets the refcount at turn boundaries so a leaked
// pause can't poison the next turn.
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
