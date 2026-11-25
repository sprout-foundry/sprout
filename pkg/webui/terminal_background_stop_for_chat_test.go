//go:build !js

package webui

import (
	"context"
	"testing"
	"time"
)

// Session rotation (/clear) must end the cleared chat's background
// commands: StopBackgroundSessionsForChat stops every active background
// session the chat owns and leaves other chats' sessions running.

func TestStopBackgroundSessionsForChat_StopsOwnedSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	tm := newTestTerminalManager(t, t.TempDir())

	mine, err := tm.ExecuteCommandInBackground(context.Background(), "chat-1", "sleep 30")
	if err != nil {
		t.Fatalf("start background command: %v", err)
	}
	other, err := tm.ExecuteCommandInBackground(context.Background(), "chat-2", "sleep 30")
	if err != nil {
		t.Fatalf("start background command in other chat: %v", err)
	}

	stopped := tm.StopBackgroundSessionsForChat("chat-1")

	if len(stopped) != 1 || stopped[0] != mine {
		t.Fatalf("expected to stop [%s], got %v", mine, stopped)
	}
	// Allow the Ctrl+C → close sequence to land before asserting state.
	waitForSessionClosed(t, tm, mine)

	if tm.IsSessionActive(mine) {
		t.Error("chat-1's background session should be stopped after rotation")
	}
	if !tm.IsSessionActive(other) {
		t.Error("chat-2's background session must be untouched")
	}
}

func TestStopBackgroundSessionsForChat_NoSessions(t *testing.T) {
	tm := newTestTerminalManager(t, t.TempDir())

	if got := tm.StopBackgroundSessionsForChat("chat-none"); got != nil {
		t.Errorf("expected no stops for an unknown chat, got %v", got)
	}
	if got := tm.StopBackgroundSessionsForChat(""); got != nil {
		t.Errorf("expected no stops for an empty chat ID, got %v", got)
	}
}

// waitForSessionClosed polls until the session is gone or reaped, bounded
// (the flaky-test policy forbids a bare sleep).
func waitForSessionClosed(t *testing.T, tm *TerminalManager, sessionID string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if !tm.IsSessionActive(sessionID) {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("session %s still active after stop", sessionID)
}
