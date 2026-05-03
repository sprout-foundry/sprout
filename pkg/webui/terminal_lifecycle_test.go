package webui

import (
	"context"
	"strings"
	"testing"
	"time"
)

// ====================================================================
// CloseSession
// ====================================================================

func TestCloseSession_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.CloseSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

func TestCloseSession_ActiveSession(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.CreateSession("close-test")
	if err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}

	err = tm.CloseSession("close-test")
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	// Verify session is removed
	_, exists := tm.GetSession("close-test")
	if exists {
		t.Error("session should be removed after CloseSession")
	}
}

func TestCloseSession_WithSubscribers(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "sub-test",
		Active: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["sub-test"] = session
	tm.mutex.Unlock()

	// Subscribe to the session
	sub := session.subscribe()

	// Close the session — this should close all subscriber channels
	err := tm.CloseSession("sub-test")
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	// Verify subscriber channel is closed
	select {
	case _, ok := <-sub.ch:
		if ok {
			t.Error("subscriber channel should be closed after CloseSession")
		}
	case <-time.After(100 * time.Millisecond):
		t.Error("subscriber channel should be closed (timed out waiting)")
	}

	// Verify session is removed
	_, exists := tm.GetSession("sub-test")
	if exists {
		t.Error("session should be removed after CloseSession")
	}
}

func TestCloseSession_MultipleSubscribers(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "multi-sub",
		Active: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["multi-sub"] = session
	tm.mutex.Unlock()

	// Create multiple subscribers
	sub1 := session.subscribe()
	sub2 := session.subscribe()
	sub3 := session.subscribe()

	err := tm.CloseSession("multi-sub")
	if err != nil {
		t.Fatalf("CloseSession failed: %v", err)
	}

	// All subscriber channels should be closed
	for i, sub := range []*termSub{sub1, sub2, sub3} {
		select {
		case _, ok := <-sub.ch:
			if ok {
				t.Errorf("subscriber %d channel should be closed", i+1)
			}
		case <-time.After(100 * time.Millisecond):
			t.Errorf("subscriber %d channel should be closed (timed out)", i+1)
		}
	}
}

func TestCloseSession_ClosedTwice(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "double-close",
		Active: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["double-close"] = session
	tm.mutex.Unlock()

	tm.CloseSession("double-close")

	// Closing again should error (not found)
	err := tm.CloseSession("double-close")
	if err == nil {
		t.Fatal("expected error for second CloseSession")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, should mention 'not found'", err.Error())
	}
}

// ====================================================================
// DetachFromSession
// ====================================================================

func TestDetachFromSession_AlwaysNil(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Even for nonexistent sessions, should return nil
	err := tm.DetachFromSession("nonexistent")
	if err != nil {
		t.Fatalf("DetachFromSession returned error: %v", err)
	}
}

func TestDetachFromSession_ExistingSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "detach-test",
		Active: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["detach-test"] = session
	tm.mutex.Unlock()

	err := tm.DetachFromSession("detach-test")
	if err != nil {
		t.Fatalf("DetachFromSession returned error: %v", err)
	}

	// Session should still exist (no-op)
	_, exists := tm.GetSession("detach-test")
	if !exists {
		t.Error("session should still exist after DetachFromSession")
	}

	tm.CloseSession("detach-test")
}

// ====================================================================
// CloseAllSessions
// ====================================================================

func TestCloseAllSessions_Empty(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	err := tm.CloseAllSessions()
	if err != nil {
		t.Fatalf("CloseAllSessions on empty manager returned error: %v", err)
	}
}

func TestCloseAllSessions_MultipleSessions(t *testing.T) {
	if testing.Short() {
		t.Skip("requires PTY")
	}

	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.CreateSession("all-1")
	if err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}
	_, err = tm.CreateSession("all-2")
	if err != nil {
		t.Skipf("CreateSession failed: %v", err)
	}

	err = tm.CloseAllSessions()
	if err != nil {
		t.Fatalf("CloseAllSessions failed: %v", err)
	}

	if tm.SessionCount() != 0 {
		t.Errorf("expected 0 sessions, got %d", tm.SessionCount())
	}
}

// ====================================================================
// ReattachSession
// ====================================================================

func TestReattachSession_SessionNotFound(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	_, err := tm.ReattachSession("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent session")
	}
	if !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("error = %q, should mention 'does not exist'", err.Error())
	}
}

func TestReattachSession_InactiveSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "inactive-reattach",
		Active: false,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["inactive-reattach"] = session
	tm.mutex.Unlock()

	_, err := tm.ReattachSession("inactive-reattach")
	if err == nil {
		t.Fatal("expected error for inactive session")
	}
	if !strings.Contains(err.Error(), "no longer active") {
		t.Errorf("error = %q, should mention 'no longer active'", err.Error())
	}
}

func TestReattachSession_HiddenSession(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "hidden-reattach",
		Active: true,
		Hidden: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["hidden-reattach"] = session
	tm.mutex.Unlock()

	_, err := tm.ReattachSession("hidden-reattach")
	if err == nil {
		t.Fatal("expected error for hidden session")
	}
	// Error message includes session ID which may contain "hidden" in the name —
	// we just verify the response is "not accessible" (does not distinguish "hidden" from "nonexistent").
	if !strings.Contains(err.Error(), "not accessible") {
		t.Errorf("error = %q, should mention 'not accessible'", err.Error())
	}
}

func TestReattachSession_ActiveReturnsScrollback(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "reattach-ok",
		Active: true,
		ring:   newSessRing(),
	}
	session.ring.write([]byte("hello terminal output"))
	tm.mutex.Lock()
	tm.sessions["reattach-ok"] = session
	tm.mutex.Unlock()

	scrollback, err := tm.ReattachSession("reattach-ok")
	if err != nil {
		t.Fatalf("ReattachSession failed: %v", err)
	}
	if !strings.Contains(scrollback, "hello terminal output") {
		t.Errorf("scrollback = %q, should contain 'hello terminal output'", scrollback)
	}
}

func TestReattachSession_UpdatesLastUsed(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:       "reattach-time",
		Active:   true,
		LastUsed: time.Now().Add(-1 * time.Hour), // 1 hour ago
		ring:     newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["reattach-time"] = session
	tm.mutex.Unlock()

	before := time.Now()
	_, err := tm.ReattachSession("reattach-time")
	if err != nil {
		t.Fatalf("ReattachSession failed: %v", err)
	}
	after := time.Now()

	session.mutex.RLock()
	lastUsed := session.LastUsed
	session.mutex.RUnlock()

	if lastUsed.Before(before) || lastUsed.After(after) {
		t.Errorf("LastUsed should be updated to now, got %v", lastUsed)
	}
}

func TestReattachSession_EmptyScrollback(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:     "reattach-empty",
		Active: true,
		ring:   newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["reattach-empty"] = session
	tm.mutex.Unlock()

	scrollback, err := tm.ReattachSession("reattach-empty")
	if err != nil {
		t.Fatalf("ReattachSession failed: %v", err)
	}
	if scrollback != "" {
		t.Errorf("scrollback = %q, want empty string", scrollback)
	}
}

// ====================================================================
// CleanupInactiveSessions
// ====================================================================

func TestCleanupInactiveSessions_CleansUpOld(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:       "old-session",
		Active:   true,
		LastUsed: time.Now().Add(-1 * time.Hour), // 1 hour ago
		ring:     newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["old-session"] = session
	tm.mutex.Unlock()

	tm.CleanupInactiveSessions(30 * time.Minute)

	_, exists := tm.GetSession("old-session")
	if exists {
		t.Error("old session should be cleaned up")
	}
}

func TestCleanupInactiveSessions_PreservesActive(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	session := &TerminalSession{
		ID:       "recent-session",
		Active:   true,
		LastUsed: time.Now().Add(-1 * time.Minute), // 1 min ago
		ring:     newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["recent-session"] = session
	tm.mutex.Unlock()

	tm.CleanupInactiveSessions(30 * time.Minute)

	_, exists := tm.GetSession("recent-session")
	if !exists {
		t.Error("recent session should be preserved")
	}

	tm.CloseSession("recent-session")
}

func TestCleanupInactiveSessions_BackgroundTimeoutSeparate(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Regular session (not background) - set to 5 min ago
	regular := &TerminalSession{
		ID:           "regular-bg",
		Active:       true,
		IsBackground: false,
		LastUsed:     time.Now().Add(-5 * time.Minute),
		ring:         newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["regular-bg"] = regular
	tm.mutex.Unlock()

	// Background session - set to 5 min ago (should survive 30 min bg timeout)
	background := &TerminalSession{
		ID:           "background-session",
		Active:       true,
		IsBackground: true,
		LastUsed:     time.Now().Add(-5 * time.Minute),
		ring:         newSessRing(),
	}
	tm.mutex.Lock()
	tm.sessions["background-session"] = background
	tm.mutex.Unlock()

	// Cleanup with 3 min regular timeout, 30 min background timeout
	tm.CleanupInactiveSessions(3*time.Minute, 30*time.Minute)

	// Regular should be cleaned up (5 min > 3 min)
	_, exists := tm.GetSession("regular-bg")
	if exists {
		t.Error("regular session should be cleaned up (5 min > 3 min timeout)")
	}

	// Background should survive (5 min < 30 min timeout)
	_, exists = tm.GetSession("background-session")
	if !exists {
		t.Error("background session should survive (5 min < 30 min timeout)")
	}

	tm.CloseSession("background-session")
}

func TestCleanupInactiveSessions_NoSessions(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	// Should not panic with empty manager
	tm.CleanupInactiveSessions(30 * time.Minute)
}

// ====================================================================
// StartCleanupWorker
// ====================================================================

func TestStartCleanupWorker_StopsOnContextCancel(t *testing.T) {
	dir := t.TempDir()
	tm := NewTerminalManager(dir)

	ctx, cancel := context.WithCancel(context.Background())
	tm.StartCleanupWorker(ctx, 10*time.Millisecond, 1*time.Minute)

	// Cancel the context
	cancel()

	// Give goroutine time to notice cancellation
	time.Sleep(50 * time.Millisecond)

	// Should not cause issues; just verifying no panic
}
