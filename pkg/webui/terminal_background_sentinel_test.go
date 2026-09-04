//go:build !js

package webui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// waitForBgDone polls until the session's bgDone channel closes or the
// timeout elapses. Returns whether the command completed.
func waitForBgDone(t *testing.T, tm *TerminalManager, sessionID string, timeout time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if done, ok := tm.BackgroundDoneChan(sessionID); ok {
			select {
			case <-done:
				return true
			default:
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// The background sentinel closes bgDone with the real exit code as soon as
// the command finishes — while the session (and its shell) stays active.
// This is the regression guard for the "completion reported only when the
// session was reaped, always exit code 0" bug.
func TestBackgroundSentinel_CompletionWithRealExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := NewTerminalManager(t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-sentinel", "echo done-marker; true")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}
	t.Cleanup(func() { _ = tm.CloseSession(sessionID) })

	if !waitForBgDone(t, tm, sessionID, 10*time.Second) {
		t.Fatal("bgDone never closed — sentinel not observed")
	}

	if code := tm.BackgroundExitCode(sessionID); code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}

	// The shell outlives the command: the session must still be active.
	if !tm.IsSessionActive(sessionID) {
		t.Fatal("session should stay alive after the command completes (shell persists)")
	}

	// The sentinel echo itself is stripped from user-visible output? No —
	// the ring keeps everything; but the output must contain the command's
	// real output.
	out, err := tm.GetBackgroundOutput(sessionID)
	if err != nil {
		t.Fatalf("GetBackgroundOutput failed: %v", err)
	}
	if !strings.Contains(out, "done-marker") {
		t.Fatalf("expected output to contain 'done-marker', got: %q", out)
	}
}

// A failing command reports its real (non-zero) exit code through the
// sentinel.
func TestBackgroundSentinel_NonZeroExitCode(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := NewTerminalManager(t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-fail", "echo boom >&2; sh -c 'exit 3'")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}
	t.Cleanup(func() { _ = tm.CloseSession(sessionID) })

	if !waitForBgDone(t, tm, sessionID, 10*time.Second) {
		t.Fatal("bgDone never closed for failing command")
	}
	if code := tm.BackgroundExitCode(sessionID); code != 3 {
		t.Fatalf("expected exit code 3, got %d", code)
	}
}

// StopBackgroundSession records BgExitStopped rather than a fake success,
// and the code remains readable after CloseSession removes the session
// (the wakeup watcher reads it after bgDone closes).
func TestBackgroundStop_ReportsStopped(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := NewTerminalManager(t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-stop", "sleep 30")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}

	if err := tm.StopBackgroundSession(sessionID); err != nil {
		t.Fatalf("StopBackgroundSession failed: %v", err)
	}

	// The watcher reads the exit code after the session is gone — the
	// tombstone must carry BgExitStopped, not BgExitNone.
	if code := tm.BackgroundExitCode(sessionID); code != BgExitStopped {
		t.Fatalf("expected tombstoned exit code BgExitStopped (%d), got %d", BgExitStopped, code)
	}
}

// A session killed before its sentinel (cleanup reaping a running command)
// closes bgDone with BgExitNone so waiters do not hang.
func TestBackgroundSession_DeathClosesDone(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := NewTerminalManager(t.TempDir())

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-death", "sleep 30")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}

	// Grab the channel before the session is reaped from the map.
	done, ok := tm.BackgroundDoneChan(sessionID)
	if !ok {
		t.Fatal("expected a done channel for the live background session")
	}

	if err := tm.CloseSession(sessionID); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	select {
	case <-done:
		// Expected: released with BgExitNone.
	case <-time.After(5 * time.Second):
		t.Fatal("bgDone not closed after session death — waiters would hang")
	}
}

// HasRunningBackgroundSessions gates the idle-context evictor: true while a
// command is in flight, false once the sentinel fires.
func TestHasRunningBackgroundSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := NewTerminalManager(t.TempDir())

	if tm.HasRunningBackgroundSessions() {
		t.Fatal("no sessions: should report false")
	}

	sessionID, err := tm.ExecuteCommandInBackground(context.Background(), "chat-running", "sleep 2")
	if err != nil {
		t.Fatalf("ExecuteCommandInBackground failed: %v", err)
	}
	t.Cleanup(func() { _ = tm.CloseSession(sessionID) })

	if !tm.HasRunningBackgroundSessions() {
		t.Fatal("command in flight: should report true")
	}

	if !waitForBgDone(t, tm, sessionID, 10*time.Second) {
		t.Fatal("sleep 2 did not complete within budget")
	}
	if tm.HasRunningBackgroundSessions() {
		t.Fatal("command finished: should report false")
	}
}

// wrapBackgroundCommand must escape single quotes so a command containing
// them can't terminate the /bin/sh -c wrapper early.
func TestWrapBackgroundCommand_EscapesQuotes(t *testing.T) {
	wrapped := wrapBackgroundCommand("echo 'nested'", "deadbeef")
	// The inner command's quotes must be escaped, not terminate the outer
	// single-quoted wrapper.
	if strings.Count(wrapped, "'")%2 != 0 {
		t.Fatalf("unbalanced quotes in wrapped command: %q", wrapped)
	}
	if !strings.Contains(wrapped, "/bin/sh -c '") {
		t.Fatalf("expected /bin/sh -c wrapper, got: %q", wrapped)
	}
}

// checkBackgroundSentinel detects a marker split across chunk boundaries
// via the carried tail.
func TestCheckBackgroundSentinel_SplitMarker(t *testing.T) {
	s := &TerminalSession{
		bgMarker: "aabbccdd",
		bgDone:   make(chan struct{}),
		ring:     newSessRing(),
	}
	marker := sentinelPrefix + "aabbccdd:"

	// First chunk: half the marker.
	if completed, _, _ := s.checkBackgroundSentinel([]byte("output...\n" + marker[:len(marker)/2])); completed {
		t.Fatal("half a marker must not complete")
	}
	// Second chunk: rest of the marker + exit code.
	completed, code, _ := s.checkBackgroundSentinel([]byte(marker[len(marker)/2:] + "7\n"))
	if !completed {
		t.Fatal("split marker should complete")
	}
	if code != 7 {
		t.Fatalf("expected exit code 7, got %d", code)
	}

	select {
	case <-s.bgDone:
	default:
		t.Fatal("bgDone should be closed")
	}
}

// The PTY echo of the wrapped command line contains the marker text
// followed by "$?" — that must NOT be treated as the completion sentinel
// (only marker+digits is).
func TestCheckBackgroundSentinel_IgnoresEcho(t *testing.T) {
	s := &TerminalSession{
		bgMarker: "aabbccdd",
		bgDone:   make(chan struct{}),
		ring:     newSessRing(),
	}
	echo := sentinelPrefix + "aabbccdd:$?\n" + "real output\n"
	if completed, _, _ := s.checkBackgroundSentinel([]byte(echo)); completed {
		t.Fatal("echo line (marker:$?) must not complete the sentinel")
	}
	select {
	case <-s.bgDone:
		t.Fatal("bgDone must stay open for a bare echo")
	default:
	}
}
