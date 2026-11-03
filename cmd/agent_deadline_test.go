package cmd

import (
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
