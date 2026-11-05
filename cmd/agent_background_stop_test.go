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
// run ends. A run without a deadline keeps its background sessions. The tests
// go through startTaskRun, which is what runDirectMode defers.

// countingStop is a background stop that counts its calls.
func countingStop() (func(), *atomic.Int32) {
	calls := &atomic.Int32{}
	return func() { calls.Add(1) }, calls
}

func TestDeadlineWarningStopsBackgroundSessions(t *testing.T) {
	now := time.Now()
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(now.Add(deadlineWarnBefore+50*time.Millisecond).Unix()+1, 10))
	stop, calls := countingStop()
	r := &recordingSteerer{}

	end := startTaskRun(r, stop, now)
	deadline := time.Now().Add(3 * time.Second)
	for (r.count() == 0 || calls.Load() == 0) && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.count() != 1 {
		t.Fatalf("warnings = %v, want one wrap-up warning", r.msgs)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("background stops at handoff = %d, want 1", got)
	}
	end()
	if got := calls.Load(); got != 2 {
		t.Fatalf("background stops after the run ends = %d, want 2 (handoff and run end)", got)
	}
}

// The run's end stops background sessions even when the run finished before
// it was warned.
func TestTaskRunEndStopsBackgroundSessions(t *testing.T) {
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	stop, calls := countingStop()
	r := &recordingSteerer{}

	run := func() {
		defer startTaskRun(r, stop, time.Now())()
		if got := calls.Load(); got != 0 {
			t.Fatalf("background stops during the run = %d, want 0", got)
		}
	}
	run()
	if got := calls.Load(); got != 1 {
		t.Fatalf("background stops at the run's end = %d, want 1", got)
	}
	if r.count() != 0 {
		t.Fatalf("warnings = %v, want none an hour before the deadline", r.msgs)
	}
}

// No deadline, or one too close to warn: the run is not told to hand off, and
// a run without a deadline keeps its background sessions when it ends.
func TestTaskRunWithoutDeadlineStopsNothing(t *testing.T) {
	stop, calls := countingStop()
	for _, v := range []string{"", "soon", "-5"} {
		t.Setenv("SPROUT_TASK_DEADLINE", v)
		startTaskRun(&recordingSteerer{}, stop, time.Now())()
	}
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	startDeadlineWarning(&recordingSteerer{}, time.Now(), stop)()
	time.Sleep(50 * time.Millisecond)
	if got := calls.Load(); got != 0 {
		t.Fatalf("background stops = %d, want 0", got)
	}
}

// A run without a background process manager has nothing to stop.
func TestTaskRunWithoutStopper(t *testing.T) {
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	startTaskRun(&recordingSteerer{}, backgroundStopper(nil), time.Now())()
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

// Stopping background sessions at the run's end leaves the handoff note in
// place, so the next run finds it.
func TestHandoffNoteSurvivesBackgroundStop(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".sprout"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	notePath := filepath.Join(dir, handoffNotePath)

	deadlineWarned.Store(true)
	t.Cleanup(func() { deadlineWarned.Store(false) })

	var noteAtStop string
	stop := func() {
		if b, err := os.ReadFile(notePath); err == nil {
			noteAtStop = string(b)
		}
	}
	end := startTaskRun(&recordingSteerer{}, stop, time.Now())
	// The agent writes its note before the run ends.
	if err := os.WriteFile(notePath, []byte("Left: build.\nNext: run it.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	end()
	if noteAtStop == "" {
		t.Fatal("background stop ran while the handoff note was missing")
	}
	if got := handoffNote(dir); got != "Left: build.\nNext: run it." {
		t.Fatalf("handoff = %q", got)
	}
}
