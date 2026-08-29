package console

import (
	"bytes"
	"strings"
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

// TestSetProseStreaming_DeferredResizeNoDeadlock is a regression test for
// the re-entrant outputMu self-deadlock that was introduced when
// pendingResize was added. SetProseStreaming(false) fires a deferred
// Resize() when pendingResize is true. The synchronous call deadlocked
// because SetProseStreaming is called from paths that already hold
// LockOutput (resetSegment inside FinalizeAtTurnEnd / OnExternalWrite).
// The fix fires Resize asynchronously via a goroutine.
//
// This test verifies that calling SetProseStreaming(false) with
// pendingResize=true while holding LockOutput does not deadlock.
func TestSetProseStreaming_DeferredResizeNoDeadlock(t *testing.T) {
	f := NewStatusFooter(&nonTTYWriter{}, &stubSource{model: "test"})

	// Simulate: SIGWINCH arrived during streaming, pendingResize was set.
	f.mu.Lock()
	f.pendingResize = true
	f.mu.Unlock()

	done := make(chan struct{})
	go func() {
		LockOutput()
		defer UnlockOutput()
		// Must not block: SetProseStreaming(false) fires Resize
		// asynchronously, so it must not re-enter outputMu.
		f.SetProseStreaming(false)
		close(done)
	}()

	select {
	case <-done:
		// success — no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("SetProseStreaming(false) with pendingResize deadlocked under LockOutput (re-entrant outputMu)")
	}

	// pendingResize must have been consumed.
	f.mu.Lock()
	pending := f.pendingResize
	f.mu.Unlock()
	if pending {
		t.Fatal("pendingResize was not cleared after SetProseStreaming(false)")
	}
}

// TestApplyPendingResizeStreamingLocked_EmitsSaveRestoreAndRegion pins the
// mid-stream DECSTBM re-apply: when pendingResize is set (a resize arrived
// during streaming), ApplyPendingResizeStreamingLocked must wrap the
// scroll-region re-application in a DECSC/DECRC save/restore pair so the
// streaming writer's cursor is preserved, and must NOT consume
// pendingResize (the full resize still fires at segment end).
func TestApplyPendingResizeStreamingLocked_EmitsSaveRestoreAndRegion(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}

	f.mu.Lock()
	f.pendingResize = true
	f.mu.Unlock()

	LockOutput()
	f.ApplyPendingResizeStreamingLocked()
	UnlockOutput()

	out := buf.String()
	// 24 rows − 2 reserved → DECSTBM "1;22".
	const decstbm = "\033[1;22r"
	saveIdx := strings.Index(out, "\0337")
	regionIdx := strings.Index(out, decstbm)
	restoreIdx := strings.Index(out, "\0338")
	if saveIdx < 0 || regionIdx < 0 || restoreIdx < 0 {
		t.Fatalf("expected \\0337, %q, \\0338 in output; got %q", decstbm, out)
	}
	if !(saveIdx < regionIdx && regionIdx < restoreIdx) {
		t.Fatalf("sequences out of order (save<region<restore required); got %q", out)
	}

	// pendingResize must be left set for the segment-end full resize.
	f.mu.Lock()
	pending := f.pendingResize
	f.mu.Unlock()
	if !pending {
		t.Fatal("ApplyPendingResizeStreamingLocked consumed pendingResize; the full resize must still fire at segment end")
	}
}

// TestApplyPendingResizeStreamingLocked_NoopWhenNotPending verifies the
// method is a no-op (no bytes emitted) when pendingResize is false, and
// that calling it while holding LockOutput does not deadlock.
func TestApplyPendingResizeStreamingLocked_NoopWhenNotPending(t *testing.T) {
	var buf bytes.Buffer
	f := NewStatusFooter(&buf, &stubSource{model: "test"})
	f.isTTY = true
	f.active = true
	f.sizeOverride = &terminalSizeOverride{cols: 80, rows: 24}

	// pendingResize is false by default.
	done := make(chan struct{})
	go func() {
		LockOutput()
		defer UnlockOutput()
		f.ApplyPendingResizeStreamingLocked()
		close(done)
	}()
	select {
	case <-done:
		// good — no deadlock
	case <-time.After(2 * time.Second):
		t.Fatal("ApplyPendingResizeStreamingLocked deadlocked under LockOutput")
	}

	if got := buf.String(); got != "" {
		t.Fatalf("expected no bytes emitted when pendingResize is false; got %q", got)
	}
}
