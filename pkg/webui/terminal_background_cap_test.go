//go:build !js

package webui

import (
	"context"
	"strings"
	"testing"
)

// commandSequence supplies one distinct long-running command per background
// session, so cap tests never trip the duplicate-command guard.
var commandSequence = []string{
	"sleep 30", "sleep 31", "sleep 32", "sleep 33", "sleep 34",
	"sleep 35", "sleep 36", "sleep 37", "sleep 38", "sleep 39",
	"sleep 40", "sleep 41",
}

// TestBackgroundCap_RejectsAtLimit verifies that ExecuteCommandInBackground
// refuses to create a new background session once a chat already owns
// maxBackgroundSessionsPerChat of them. Without this cap a runaway agent
// can pile sessions up indefinitely (each holds a PTY + scrollback for
// up to 2 hours of inactivity).
func TestBackgroundCap_RejectsAtLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := newTestTerminalManager(t, dir)
	t.Cleanup(func() {
		for _, sess := range tm.sessions {
			tm.CloseSession(sess.ID)
		}
	})

	chat := "cap-chat"

	// Fill the cap. Each session runs a distinct command: these tests are
	// about the cap, and the duplicate-command guard would (correctly)
	// refuse the same command repeatedly.
	var created []string
	for i := 0; i < maxBackgroundSessionsPerChat; i++ {
		sid, err := tm.ExecuteCommandInBackground(context.Background(), chat, commandSequence[i])
		if err != nil {
			t.Fatalf("ExecuteCommandInBackground #%d unexpectedly failed: %v", i, err)
		}
		created = append(created, sid)
	}

	if got := tm.countBackgroundSessionsForChat(chat); got != maxBackgroundSessionsPerChat {
		t.Fatalf("after filling cap: countBackgroundSessionsForChat=%d, want %d", got, maxBackgroundSessionsPerChat)
	}

	// One more should be rejected.
	_, err := tm.ExecuteCommandInBackground(context.Background(), chat, "sleep 130")
	if err == nil {
		t.Fatal("expected cap rejection, got nil error")
	}
	if !strings.Contains(err.Error(), "background session cap reached") {
		t.Errorf("error should explain the cap; got: %v", err)
	}
	// Error must include at least one existing session ID so the agent can
	// pick one to stop_background.
	foundID := false
	for _, sid := range created {
		if strings.Contains(err.Error(), sid) {
			foundID = true
			break
		}
	}
	if !foundID {
		t.Errorf("cap error should list existing session IDs; got: %v", err)
	}
}

// TestBackgroundCap_OtherChatNotAffected verifies the cap is per-chat,
// not global: filling chat A's quota doesn't block chat B from creating
// new background sessions.
func TestBackgroundCap_OtherChatNotAffected(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := newTestTerminalManager(t, dir)
	t.Cleanup(func() {
		for _, sess := range tm.sessions {
			tm.CloseSession(sess.ID)
		}
	})

	// Fill chat A's cap.
	for i := 0; i < maxBackgroundSessionsPerChat; i++ {
		if _, err := tm.ExecuteCommandInBackground(context.Background(), "chat-A", commandSequence[i]); err != nil {
			t.Fatalf("chat-A create #%d failed: %v", i, err)
		}
	}

	// Chat B should still be free to create.
	if _, err := tm.ExecuteCommandInBackground(context.Background(), "chat-B", "sleep 30"); err != nil {
		t.Fatalf("chat-B should be unaffected by chat-A's cap; got error: %v", err)
	}
}

// TestBackgroundCap_FreedAfterClose verifies that closing a background
// session frees its slot so the chat can create another.
func TestBackgroundCap_FreedAfterClose(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := newTestTerminalManager(t, dir)
	t.Cleanup(func() {
		for _, sess := range tm.sessions {
			tm.CloseSession(sess.ID)
		}
	})

	chat := "recycle-chat"

	var created []string
	for i := 0; i < maxBackgroundSessionsPerChat; i++ {
		sid, err := tm.ExecuteCommandInBackground(context.Background(), chat, commandSequence[i])
		if err != nil {
			t.Fatalf("create #%d failed: %v", i, err)
		}
		created = append(created, sid)
	}

	// Close one — should free a slot.
	if err := tm.CloseSession(created[0]); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}
	if got := tm.countBackgroundSessionsForChat(chat); got != maxBackgroundSessionsPerChat-1 {
		t.Fatalf("after close: countBackgroundSessionsForChat=%d, want %d", got, maxBackgroundSessionsPerChat-1)
	}

	// New create should succeed.
	if _, err := tm.ExecuteCommandInBackground(context.Background(), chat, "sleep 130"); err != nil {
		t.Fatalf("after freeing a slot, create should succeed; got error: %v", err)
	}
}
