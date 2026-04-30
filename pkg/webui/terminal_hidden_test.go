package webui

import (
	"testing"
	"time"
)

func TestCreateHiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-123")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	session.mutex.RLock()
	hidden := session.Hidden
	owner := session.Owner
	chatID := session.ChatID
	autoClose := session.AutoClose
	session.mutex.RUnlock()

	if !hidden {
		t.Error("session should be hidden")
	}
	if owner != "agent" {
		t.Errorf("expected owner 'agent', got %q", owner)
	}
	if chatID != "chat-123" {
		t.Errorf("expected chatID 'chat-123', got %q", chatID)
	}
	if !autoClose {
		t.Error("hidden session should have AutoClose=true by default")
	}

	tm.CloseSession("hidden-1")
}

func TestCreateHiddenSessionWithOptions(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("hidden-2", "agent", "chat-456",
		WithName("npm run dev"),
		WithAutoClose(false),
	)
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	session.mutex.RLock()
	name := session.Name
	autoClose := session.AutoClose
	session.mutex.RUnlock()

	if name != "npm run dev" {
		t.Errorf("expected name 'npm run dev', got %q", name)
	}
	if autoClose {
		t.Error("AutoClose should be overridden to false")
	}

	tm.CloseSession("hidden-2")
}

func TestListSessionsExcludesHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Create a regular session.
	if _, err := tm.CreateSession("regular-1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	// Create a hidden session.
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// ListSessions should only return the regular session.
	sessions := tm.ListSessions()
	if len(sessions) != 1 {
		t.Errorf("expected 1 visible session, got %d: %v", len(sessions), sessions)
	}
	if len(sessions) > 0 && sessions[0] != "regular-1" {
		t.Errorf("expected 'regular-1', got %q", sessions[0])
	}

	// ListAllSessions should return both.
	allSessions := tm.ListAllSessions()
	if len(allSessions) != 2 {
		t.Errorf("expected 2 total sessions, got %d: %v", len(allSessions), allSessions)
	}

	// ListHiddenSessions should return only hidden.
	hiddenSessions := tm.ListHiddenSessions()
	if len(hiddenSessions) != 1 {
		t.Errorf("expected 1 hidden session, got %d: %v", len(hiddenSessions), hiddenSessions)
	}

	tm.CloseSession("regular-1")
	tm.CloseSession("hidden-1")
}

func TestCloseSessionWorksForHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Create a hidden session.
	if _, err := tm.CreateHiddenSession("hidden-close", "agent", "chat-1"); err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Verify it exists.
	if _, exists := tm.GetSession("hidden-close"); !exists {
		t.Fatal("hidden session should exist")
	}

	// Close it.
	if err := tm.CloseSession("hidden-close"); err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	// Verify it's gone.
	if _, exists := tm.GetSession("hidden-close"); exists {
		t.Error("hidden session should be removed after close")
	}
}

func TestCleanupInactivePicksUpHiddenSessions(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("hidden-inactive", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Force LastUsed to be in the past so it appears inactive.
	session.mutex.Lock()
	session.LastUsed = time.Now().Add(-1 * time.Hour)
	session.mutex.Unlock()

	// Run cleanup with a 1-second timeout — the session is 1 hour old.
	tm.CleanupInactiveSessions(time.Second)

	// Wait briefly for the async goroutine to complete.
	time.Sleep(200 * time.Millisecond)

	_, exists := tm.GetSession("hidden-inactive")
	if exists {
		t.Error("hidden session should be cleaned up by inactivity worker")
	}
}

func TestCloseAllSessionsIncludesHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	if _, err := tm.CreateSession("regular-1"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if _, err := tm.CreateHiddenSession("hidden-1", "agent", "chat-1"); err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	if err := tm.CloseAllSessions(); err != nil {
		t.Fatalf("CloseAllSessions failed: %v", err)
	}

	// Both sessions should be gone.
	if tm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions after CloseAllSessions, got %d", tm.SessionCount())
	}
}

func TestCreateHiddenSessionDuplicateID(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	if _, err := tm.CreateSession("dup-id"); err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}

	_, err := tm.CreateHiddenSession("dup-id", "agent", "chat-1")
	if err == nil {
		t.Error("expected error when creating hidden session with duplicate ID")
	}
}

func TestReattachSessionRejectsHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("hidden-reattach", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// Wait for PTY reader to be running so the session is active.
	session.mutex.RLock()
	active := session.Active
	session.mutex.RUnlock()
	if !active {
		t.Fatal("hidden session should be active")
	}

	_, err = tm.ReattachSession("hidden-reattach")
	if err == nil {
		t.Error("ReattachSession should reject hidden sessions")
	}

	tm.CloseSession("hidden-reattach")
}

func TestGetSessionReturnsHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session, err := tm.CreateHiddenSession("hidden-get", "agent", "chat-1")
	if err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	// GetSession should still find hidden sessions.
	retrieved, exists := tm.GetSession("hidden-get")
	if !exists {
		t.Error("GetSession should find hidden sessions")
	}
	if retrieved != session {
		t.Error("GetSession should return the same session pointer")
	}

	tm.CloseSession("hidden-get")
}

func TestHasSessionReturnsTrueForHidden(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	if _, err := tm.CreateHiddenSession("hidden-has", "agent", "chat-1"); err != nil {
		t.Fatalf("CreateHiddenSession failed: %v", err)
	}

	if !tm.HasSession("hidden-has") {
		t.Error("HasSession should return true for hidden sessions")
	}

	tm.CloseSession("hidden-has")
}

func TestCreateHiddenSessionValidation(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	cases := []struct {
		name   string
		id     string
		owner  string
		chatID string
	}{
		{"empty id", "", "agent", "chat-1"},
		{"empty owner", "session-1", "", "chat-1"},
		{"empty chatID", "session-1", "agent", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tm.CreateHiddenSession(tc.id, tc.owner, tc.chatID)
			if err == nil {
				t.Errorf("expected error for %s", tc.name)
				// Clean up if unexpectedly created
				tm.CloseSession(tc.id)
			}
		})
	}
}
