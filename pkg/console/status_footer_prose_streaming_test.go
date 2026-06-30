package console

import (
	"bytes"
	"testing"
	"time"
)

// TestSetProseStreaming_NoDeadlockUnderOutputLock is a regression test for
// a re-entrant outputMu self-deadlock. SetProseStreaming(false) previously
// called Refresh() → draw() → LockOutput() unconditionally. But the turn
// renderer calls resetSegment() (which calls SetProseStreaming(false)) from
// FinalizeAtTurnEnd, a path that ALREADY holds LockOutput. Go's
// sync.Mutex is non-reentrant, so the second LockOutput deadlocked the
// REPL goroutine at every turn end — leaving the steer panel on screen
// and blocking the next ReadLine ("can't submit follow-ups").
//
// This test calls SetProseStreaming while holding LockOutput with a
// timeout; if the deadlock is reintroduced, the test fails instead of
// hanging the suite.
func TestSetProseStreaming_NoDeadlockUnderOutputLock(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{model: "test"})

	done := make(chan struct{})
	go func() {
		LockOutput()
		defer UnlockOutput()
		// Must not block: SetProseStreaming must not acquire outputMu.
		f.SetProseStreaming(true)
		f.SetProseStreaming(false)
		close(done)
	}()

	select {
	case <-done:
		// success — no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("SetProseStreaming deadlocked when called under LockOutput (re-entrant outputMu)")
	}
}

// TestSetProseStreaming_GatesRefresh verifies the core contract of the
// flag: while proseStreaming is true, Refresh is a no-op (it must not
// call draw → LockOutput); once cleared, Refresh draws again. We check
// the gate via the footer's own fields because terminalSize() returns
// (0,0) for the test's non-TTY fd, which makes drawLocked early-return
// regardless. The deadlock regression is fully covered by the test above;
// this test pins the flag's gating semantics so a future refactor can't
// silently drop the Refresh suppression that motivated the flag.
func TestSetProseStreaming_GatesRefresh(t *testing.T) {
	f := NewStatusFooter(&bytes.Buffer{}, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true

	// Streaming on → Refresh must short-circuit before reaching draw.
	f.SetProseStreaming(true)
	f.mu.Lock()
	gating := f.proseStreaming
	f.mu.Unlock()
	if !gating {
		t.Fatal("SetProseStreaming(true) did not set the proseStreaming flag")
	}
	// A non-nil, TTY, active footer with proseStreaming=true will skip
	// draw() in Refresh. Asserting via a goroutine that would block if
	// draw() took LockOutput while we hold it proves the suppression.
	LockOutput()
	refreshed := make(chan struct{})
	go func() {
		f.Refresh()
		close(refreshed)
	}()
	select {
	case <-refreshed:
		// good — Refresh returned without needing outputMu
	case <-time.After(2 * time.Second):
		t.Fatal("Refresh under proseStreaming blocked on outputMu — suppression gate broken")
	}
	UnlockOutput()

	// Streaming off → flag cleared so the next Refresh (from the REPL
	// loop, which owns LockOutput) will draw again.
	f.SetProseStreaming(false)
	f.mu.Lock()
	gating = f.proseStreaming
	f.mu.Unlock()
	if gating {
		t.Fatal("SetProseStreaming(false) did not clear the proseStreaming flag")
	}
}
