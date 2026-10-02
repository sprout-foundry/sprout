package agent

import (
	"context"
	"strings"
	"testing"
	"time"
)

// disableWakeupBatching makes TryAutoResume act on pending notifications
// immediately, for tests about resume mechanics rather than timing.
func disableWakeupBatching(t *testing.T) {
	t.Helper()
	settle, batch := wakeupSettleWindow, wakeupBatchWindow
	wakeupSettleWindow, wakeupBatchWindow = 0, 0
	t.Cleanup(func() { wakeupSettleWindow, wakeupBatchWindow = settle, batch })
}

func queueAt(a *Agent, id string, at time.Time) {
	a.notifMu.Lock()
	defer a.notifMu.Unlock()
	a.pendingNotifications = append(a.pendingNotifications, Notification{SessionID: id, Kind: NotifSubagent, Content: id, Timestamp: at})
}

func TestShouldDeferWakeup(t *testing.T) {
	a, _ := newBackgroundTestAgent(t, reviewerReply, 30*time.Second)
	now := time.Now()

	if a.shouldDeferWakeup(now) {
		t.Error("deferred with nothing pending")
	}

	queueAt(a, "fresh", now.Add(-500*time.Millisecond))
	if !a.shouldDeferWakeup(now) {
		t.Error("a completion still inside the settle window should wait for a burst to finish")
	}
	if a.shouldDeferWakeup(now.Add(wakeupSettleWindow)) {
		t.Error("settled completion with no other background work should wake")
	}

	// Another background task is still running: hold for its result, up to
	// the batch window.
	out, err := handleRunSubagent(context.Background(), a, map[string]any{"persona": "reviewer", "prompt": "slow review"})
	if err != nil {
		t.Fatal(err)
	}
	startedTaskID(t, out)
	if !a.shouldDeferWakeup(now.Add(wakeupSettleWindow + time.Second)) {
		t.Error("should hold while another background task is running")
	}
	if a.shouldDeferWakeup(now.Add(wakeupBatchWindow)) {
		t.Error("must not hold past the batch window — a long task would starve finished ones")
	}
	a.GetSubagentRunner().CancelAll()
}

func TestTryAutoResume_BatchesCompletionsIntoOneTurn(t *testing.T) {
	a, wakes := newBackgroundTestAgent(t, reviewerReply, 0)
	settle := wakeupSettleWindow
	wakeupSettleWindow = 200 * time.Millisecond
	t.Cleanup(func() { wakeupSettleWindow = settle })

	queueAt(a, "first", time.Now())
	queueAt(a, "second", time.Now())
	if a.TryAutoResume() {
		t.Fatal("resumed before the burst settled")
	}
	time.Sleep(250 * time.Millisecond)
	if !a.TryAutoResume() {
		t.Fatal("did not resume once settled")
	}
	if wakes.Load() != 1 {
		t.Fatalf("wakes = %d, want 1", wakes.Load())
	}
	batch := a.DrainWakeupForREPL()
	if len(batch) != 1 || !strings.Contains(batch[0], "2 background tasks completed") {
		t.Errorf("completions not batched into one resume: %q", batch)
	}
}
