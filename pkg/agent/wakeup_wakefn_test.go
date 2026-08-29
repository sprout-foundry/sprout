package agent

import (
	"testing"
	"time"
)

// TestTryAutoResume_RoutesThroughWakeFn verifies the REPL-routed resume
// path: when a wake function is registered, TryAutoResume stashes the
// wakeup batch for the REPL (DrainWakeupForREPL returns it) and invokes
// the wake function instead of running the turn on a background
// goroutine. This is the fix for prose rendering one chunk per line —
// the resume turn must run inside the REPL loop's turn machinery.
func TestTryAutoResume_RoutesThroughWakeFn(t *testing.T) {
	a := newTestAgentWithWakeup(t, true)
	t.Cleanup(func() { a.Shutdown() })

	woken := make(chan struct{}, 1)
	a.SetWakeupWakeFn(func() { woken <- struct{}{} })

	a.QueueNotification(Notification{
		Content:   "Background task completed",
		SessionID: "test-session",
		Kind:      NotifShellBg,
	})

	if !a.TryAutoResume() {
		t.Fatal("TryAutoResume should return true when notifications are pending")
	}

	select {
	case <-woken:
	case <-time.After(2 * time.Second):
		t.Fatal("wake function was not invoked")
	}

	msgs := a.DrainWakeupForREPL()
	if len(msgs) != 1 {
		t.Fatalf("DrainWakeupForREPL() len = %d, want 1", len(msgs))
	}
	if msgs[0] == "" {
		t.Fatal("stashed wakeup batch is empty")
	}

	// Second drain returns nothing — the stash is single-shot.
	if again := a.DrainWakeupForREPL(); len(again) != 0 {
		t.Fatalf("second DrainWakeupForREPL() len = %d, want 0", len(again))
	}
}

// TestTryAutoResume_NoWakeFnUsesGoroutine verifies the headless path is
// unchanged: without a wake function the resume runs on a background
// goroutine (the WebUI-daemon surface), and nothing is stashed for the
// REPL.
func TestTryAutoResume_NoWakeFnUsesGoroutine(t *testing.T) {
	a := newTestAgentWithWakeup(t, true)
	t.Cleanup(func() { a.Shutdown() })

	a.QueueNotification(Notification{
		Content:   "Background task completed",
		SessionID: "test-session",
		Kind:      NotifShellBg,
	})

	if !a.TryAutoResume() {
		t.Fatal("TryAutoResume should return true when notifications are pending")
	}
	if msgs := a.DrainWakeupForREPL(); len(msgs) != 0 {
		t.Fatalf("DrainWakeupForREPL() len = %d, want 0 without wake fn", len(msgs))
	}
	waitQuerySettled(t, a)
}

// TestSetWakeupWakeFn_NilReverts verifies passing nil unregisters the
// wake function so TryAutoResume falls back to the goroutine path.
func TestSetWakeupWakeFn_NilReverts(t *testing.T) {
	a := newTestAgentWithWakeup(t, true)
	t.Cleanup(func() { a.Shutdown() })

	a.SetWakeupWakeFn(func() {})
	a.SetWakeupWakeFn(nil)

	a.QueueNotification(Notification{
		Content:   "Background task completed",
		SessionID: "test-session",
		Kind:      NotifShellBg,
	})

	if !a.TryAutoResume() {
		t.Fatal("TryAutoResume should return true when notifications are pending")
	}
	if msgs := a.DrainWakeupForREPL(); len(msgs) != 0 {
		t.Fatalf("DrainWakeupForREPL() len = %d, want 0 after nil wake fn", len(msgs))
	}
	waitQuerySettled(t, a)
}

// waitQuerySettled blocks until the agent's query guard is released (or
// a timeout fires). Used by goroutine-path TryAutoResume tests so the
// background resume finishes BEFORE the test's state-dir teardown — a
// query that outlives NewTestStateDir's cleanup writes its journal /
// auto-save into the real state dir (the "[state-leak]" CI failure).
func waitQuerySettled(t *testing.T, a *Agent) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for a.IsQueryInProgress() {
		if time.Now().After(deadline) {
			t.Log("waitQuerySettled: query still in progress after 10s; continuing")
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
}
