package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

// A run with a task deadline must leave a quiet machine: its background
// sessions are stopped when the agent is told to hand off, and again when the
// run ends. A run without a deadline keeps its background sessions.

// withStopHook records calls to the run's stop hook and restores the previous
// hook afterwards.
func withStopHook(t *testing.T) *atomic.Int32 {
	t.Helper()
	previous := stopBackgroundProcesses
	calls := &atomic.Int32{}
	stopBackgroundProcesses = func() { calls.Add(1) }
	t.Cleanup(func() { stopBackgroundProcesses = previous })
	return calls
}

func TestDeadlineWarningStopsBackgroundSessions(t *testing.T) {
	now := time.Now()
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(now.Add(deadlineWarnBefore+50*time.Millisecond).Unix()+1, 10))
	calls := withStopHook(t)
	r := &recordingSteerer{}

	stop := startDeadlineWarning(r, now)
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for (r.count() == 0 || calls.Load() == 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.count() != 1 {
		t.Fatalf("warnings = %v, want one wrap-up warning", r.msgs)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("stop hook calls at handoff = %d, want 1", got)
	}
}

// No deadline, or one too close to warn: the run is not told to hand off, so
// nothing is stopped on its behalf.
func TestDeadlineWarningWithoutDeadlineStopsNothing(t *testing.T) {
	calls := withStopHook(t)
	for _, v := range []string{"", "soon", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)} {
		t.Setenv("SPROUT_TASK_DEADLINE", v)
		startDeadlineWarning(&recordingSteerer{}, time.Now())()
	}
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("stop hook calls = %d, want 0 without a deadline", got)
	}
}

// The end-of-run hook is what runDirectMode defers: armed by
// stopBackgroundSessionsAtRunEnd for a deadline run, called on the way out,
// and disarmed afterwards.
func TestStopBackgroundSessionsAtRunEnd(t *testing.T) {
	previous := stopBackgroundProcesses
	t.Cleanup(func() { stopBackgroundProcesses = previous })

	t.Run("deadline run", func(t *testing.T) {
		var calls atomic.Int32
		before := func() { calls.Add(100) }
		stopBackgroundProcesses = before
		t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))

		// While the run is armed, its stop hook replaces the previous one.
		runBody := func() {
			defer stopBackgroundSessionsAtRunEnd(nil)()
			if stopBackgroundProcesses == nil {
				t.Fatal("a deadline run must arm a stop hook")
			}
			stopBackgroundSessionsIfSet() // the end-of-run stop
		}
		runBody()
		// The run's stop fired once on the way out, and the hook that was
		// there before is restored rather than left armed.
		stopBackgroundProcesses()
		if got := calls.Load(); got != 100 {
			t.Fatalf("hook calls = %d; want the pre-run hook restored and the run's own hook not called after the run", got)
		}
	})

	t.Run("no deadline", func(t *testing.T) {
		stopBackgroundProcesses = nil
		t.Setenv("SPROUT_TASK_DEADLINE", "")

		undo := stopBackgroundSessionsAtRunEnd(nil)
		if stopBackgroundProcesses != nil {
			t.Fatalf("a run without a deadline must not stop background sessions")
		}
		undo()
	})
}

func TestHasTaskDeadline(t *testing.T) {
	tests := []struct {
		value string
		want  bool
	}{
		{"", false},
		{"soon", false},
		{"-5", false},
		{"0", false},
		{strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10), true},
	}
	for _, tt := range tests {
		t.Setenv("SPROUT_TASK_DEADLINE", tt.value)
		if got := hasTaskDeadline(); got != tt.want {
			t.Errorf("hasTaskDeadline(%q) = %v, want %v", tt.value, got, tt.want)
		}
	}
}

// A handoff run's note and its background cleanup happen together: the hook
// fires while the note is already on disk, so the next run finds both.
func TestHandoffNoteSurvivesBackgroundStop(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".sprout"), 0o755); err != nil {
		t.Fatal(err)
	}
	notePath := filepath.Join(dir, handoffNotePath)
	if err := os.WriteFile(notePath, []byte("Left: build.\nNext: run it.\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deadlineWarned.Store(true)
	t.Cleanup(func() { deadlineWarned.Store(false) })

	previous := stopBackgroundProcesses
	var noteAtStop string
	stopBackgroundProcesses = func() {
		if b, err := os.ReadFile(notePath); err == nil {
			noteAtStop = string(b)
		}
	}
	t.Cleanup(func() { stopBackgroundProcesses = previous })

	stopBackgroundSessionsIfSet()
	if noteAtStop == "" {
		t.Fatal("background stop ran while the handoff note was missing")
	}
	if got := handoffNote(dir); got != "Left: build.\nNext: run it." {
		t.Fatalf("handoff = %q", got)
	}
}
