package cmd

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

type recordingSteerer struct {
	mu   sync.Mutex
	msgs []string
}

func (r *recordingSteerer) InjectInputContext(s string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.msgs = append(r.msgs, s)
	return nil
}

func (r *recordingSteerer) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.msgs)
}

func TestDeadlineWarningFiresBeforeTheDeadline(t *testing.T) {
	now := time.Now()
	// A deadline 2m+50ms away: the warning is due in 50ms.
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(now.Add(deadlineWarnBefore+50*time.Millisecond).Unix()+1, 10))
	r := &recordingSteerer{}
	stop := startDeadlineWarning(r, now)
	defer stop()
	deadline := time.Now().Add(3 * time.Second)
	for r.count() == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if r.count() != 1 || r.msgs[0] != deadlineWarning {
		t.Fatalf("warnings = %v, want one wrap-up warning", r.msgs)
	}
}

func TestDeadlineWarningNeedsADeadline(t *testing.T) {
	r := &recordingSteerer{}
	for _, v := range []string{"", "soon", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10)} {
		t.Setenv("SPROUT_TASK_DEADLINE", v)
		stop := startDeadlineWarning(r, time.Now())
		stop()
	}
	time.Sleep(50 * time.Millisecond)
	if r.count() != 0 {
		t.Fatalf("no deadline, or one too close, must not warn: %v", r.msgs)
	}
}

// A run told to wrap up that left a handoff note hands it to the next run;
// a run that was not told to, or wrote no note, has none. A note left by an
// earlier run is removed when the run's deadline is set.
func TestHandoffNote(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if err := os.MkdirAll(filepath.Join(dir, ".sprout"), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(s string) {
		if err := os.WriteFile(filepath.Join(dir, handoffNotePath), []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	deadlineWarned.Store(false)
	t.Cleanup(func() { deadlineWarned.Store(false) })

	write("stale note from an earlier run")
	t.Setenv("SPROUT_TASK_DEADLINE", strconv.FormatInt(time.Now().Add(time.Hour).Unix(), 10))
	stop := startDeadlineWarning(&recordingSteerer{}, time.Now())
	stop()
	if _, err := os.Stat(filepath.Join(dir, handoffNotePath)); !os.IsNotExist(err) {
		t.Fatalf("an earlier run's note must be removed: %v", err)
	}

	write("Done: cart.\nNext: checkout.\n")
	if got := handoffNote(dir); got != "" {
		t.Fatalf("a run not told to wrap up has no handoff, got %q", got)
	}
	deadlineWarned.Store(true)
	if got := handoffNote(dir); got != "Done: cart.\nNext: checkout." {
		t.Fatalf("handoff = %q", got)
	}
	if err := os.Remove(filepath.Join(dir, handoffNotePath)); err != nil {
		t.Fatal(err)
	}
	if got := handoffNote(dir); got != "" {
		t.Fatalf("a run that finished (no note) has no handoff, got %q", got)
	}
}
