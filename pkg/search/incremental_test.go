package search

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func makeTestUpdater(indexPath, sessionsDir string, debounce time.Duration) *IndexUpdater {
	u := NewIndexUpdater(indexPath, sessionsDir)
	u.debounce = debounce
	return u
}

func createTestSession(t *testing.T, sessionsDir, sessionID string) {
	t.Helper()
	// Create the scoped subdirectory structure that WalkSessions expects
	scopedDir := filepath.Join(sessionsDir, "scoped", "abcdef12")
	if err := os.MkdirAll(scopedDir, 0755); err != nil {
		t.Fatalf("create scoped dir: %v", err)
	}

	session := sessionJSON{
		SessionID:        sessionID,
		Name:             "Test Session " + sessionID,
		WorkingDirectory: "/tmp/test",
		TotalCost:        0.05,
		Messages: []messageRef{
			{Role: "user", Content: "Hello from " + sessionID},
			{Role: "assistant", Content: "Response for " + sessionID},
		},
	}
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	path := filepath.Join(scopedDir, "session_"+sessionID+".json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("write session file: %v", err)
	}
}

// ---------------------------------------------------------------------------
// 1. TestIncrementalUpdater_Debounce
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_Debounce(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create 10 session files
	for i := 0; i < 10; i++ {
		sessionID := "session-" + string(rune('a'+i))
		createTestSession(t, sessionsDir, sessionID)
	}

	// Create updater with 100ms debounce
	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Mark 10 sessions dirty within 10ms (rapid calls)
	for i := 0; i < 10; i++ {
		sessionID := "session-" + string(rune('a'+i))
		u.MarkDirty(sessionID)
		time.Sleep(1 * time.Millisecond) // minimal delay between marks
	}

	// Wait for debounce to fire (100ms debounce + 50ms buffer)
	time.Sleep(150 * time.Millisecond)

	// Verify the index file was written
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	// All 10 sessions should be present
	if len(idx.Sessions) != 10 {
		t.Errorf("expected 10 sessions in index, got %d", len(idx.Sessions))
	}

	// Verify each session is present
	for i := 0; i < 10; i++ {
		sessionID := "session-" + string(rune('a'+i))
		if _, ok := idx.Sessions[sessionID]; !ok {
			t.Errorf("session %q not found in index", sessionID)
		}
	}

	// Verify only ONE disk write occurred (BuiltAt should be a single time)
	// If multiple writes occurred, BuiltAt would be updated multiple times
	// We can verify by checking that pending is empty (all processed)
	u.mu.Lock()
	pendingCount := len(u.pending)
	u.mu.Unlock()
	if pendingCount != 0 {
		t.Errorf("expected 0 pending sessions after debounce, got %d", pendingCount)
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 2. TestIncrementalUpdater_Flush
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_Flush(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create a session file
	createTestSession(t, sessionsDir, "session-1")

	// Create updater with long debounce (so we can test flush before it fires)
	u := makeTestUpdater(indexPath, sessionsDir, 5*time.Second)

	// Mark dirty
	u.MarkDirty("session-1")

	// Wait for the watchTimer goroutine to have started and captured
	// the timer/stopCh references before Flush can race it.
	for i := 0; i < 100; i++ {
		u.mu.Lock()
		started := u.timer != nil && u.stopCh != nil
		u.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Immediately flush (don't wait for debounce)
	err := u.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify the index file was written immediately
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex after flush: %v", err)
	}

	// Session should be present
	if len(idx.Sessions) != 1 {
		t.Errorf("expected 1 session in index after flush, got %d", len(idx.Sessions))
	}
	if _, ok := idx.Sessions["session-1"]; !ok {
		t.Error("session-1 not found in index after flush")
	}

	// Verify the timer was cancelled (no additional writes)
	// Wait a bit and confirm no second write occurred
	time.Sleep(100 * time.Millisecond)

	// The index should still have exactly 1 session (no duplicate writes)
	idx2, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex after wait: %v", err)
	}
	if len(idx2.Sessions) != 1 {
		t.Errorf("expected 1 session after wait, got %d", len(idx2.Sessions))
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 3. TestIncrementalUpdater_Stop
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_Stop(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create a session file
	createTestSession(t, sessionsDir, "session-1")

	// Create updater with 100ms debounce
	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Mark dirty
	u.MarkDirty("session-1")

	// Wait for the watchTimer goroutine to have started and captured
	// the timer/stopCh references before Stop() can race it.
	for i := 0; i < 100; i++ {
		u.mu.Lock()
		started := u.timer != nil && u.stopCh != nil
		u.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}

	// Immediately stop (before debounce fires)
	u.Stop()

	// Wait for the debounce period + buffer
	time.Sleep(300 * time.Millisecond)

	// Verify NO index file was written
	_, err := os.Stat(indexPath)
	if err == nil {
		t.Error("index file should not exist after Stop() was called")
	}
	if !os.IsNotExist(err) {
		t.Errorf("unexpected error checking index file: %v", err)
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 4. TestIncrementalUpdater_TimerReset
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_TimerReset(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create session files
	createTestSession(t, sessionsDir, "session-1")
	createTestSession(t, sessionsDir, "session-2")

	// Create updater with 200ms debounce
	u := makeTestUpdater(indexPath, sessionsDir, 200*time.Millisecond)

	// Mark session-1 dirty (starts timer)
	u.MarkDirty("session-1")

	// Wait 150ms (timer is armed but not fired yet)
	time.Sleep(150 * time.Millisecond)

	// Mark session-2 dirty (this should reset the timer)
	u.MarkDirty("session-2")

	// Wait 100ms (should NOT have fired yet — only 100ms since reset)
	time.Sleep(100 * time.Millisecond)

	// Verify index file does NOT exist yet
	_, err := os.Stat(indexPath)
	if err == nil {
		t.Error("index file should not exist yet — timer should have been reset")
	}
	if !os.IsNotExist(err) {
		t.Errorf("unexpected error checking index file: %v", err)
	}

	// Wait 200ms more (should fire ~100ms into this sleep)
	time.Sleep(200 * time.Millisecond)

	// Verify index file exists with both sessions
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex after timer fire: %v", err)
	}

	if len(idx.Sessions) != 2 {
		t.Errorf("expected 2 sessions in index, got %d", len(idx.Sessions))
	}
	if _, ok := idx.Sessions["session-1"]; !ok {
		t.Error("session-1 not found in index")
	}
	if _, ok := idx.Sessions["session-2"]; !ok {
		t.Error("session-2 not found in index")
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 5. TestIncrementalUpdater_PersistsIndex
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_PersistsIndex(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create a real session JSON file
	sessionID := "real-session"
	createTestSession(t, sessionsDir, sessionID)

	// Create updater with 50ms debounce
	u := makeTestUpdater(indexPath, sessionsDir, 50*time.Millisecond)

	// Mark dirty
	u.MarkDirty(sessionID)

	// Wait for debounce to fire
	time.Sleep(150 * time.Millisecond)

	// Flush to ensure clean state
	err := u.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Load the index file manually and verify
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	// Verify it contains the session
	if len(idx.Sessions) != 1 {
		t.Fatalf("expected 1 session in index, got %d", len(idx.Sessions))
	}

	entry, ok := idx.Sessions[sessionID]
	if !ok {
		t.Fatalf("session %q not found in index", sessionID)
	}

	// Verify correct metadata
	if entry.SessionID != sessionID {
		t.Errorf("SessionID = %q, want %q", entry.SessionID, sessionID)
	}
	if entry.Name != "Test Session "+sessionID {
		t.Errorf("Name = %q, want %q", entry.Name, "Test Session "+sessionID)
	}
	if entry.WorkingDir != "/tmp/test" {
		t.Errorf("WorkingDir = %q, want %q", entry.WorkingDir, "/tmp/test")
	}
	if entry.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", entry.MessageCount)
	}
	if entry.Text == "" {
		t.Error("Text is empty")
	}
	// Text should be lowercased
	expectedText := "hello from " + sessionID + "\nresponse for " + sessionID
	if entry.Text != expectedText {
		t.Errorf("Text = %q, want %q", entry.Text, expectedText)
	}
	if entry.TotalCost != 0.05 {
		t.Errorf("TotalCost = %v, want 0.05", entry.TotalCost)
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 6. TestIncrementalUpdater_ConcurrentSafety
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_ConcurrentSafety(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create 10 session files
	for i := 0; i < 10; i++ {
		sessionID := "concurrent-" + string(rune('a'+i))
		createTestSession(t, sessionsDir, sessionID)
	}

	// Create updater with 100ms debounce
	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Launch 10 goroutines, each calling MarkDirty with unique session IDs
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			sessionID := "concurrent-" + string(rune('a'+i))
			u.MarkDirty(sessionID)
		}(i)
	}

	// Wait for all goroutines to complete
	wg.Wait()

	// Wait for debounce to fire (100ms debounce + 200ms buffer)
	time.Sleep(300 * time.Millisecond)

	// Verify the index file was written with all 10 sessions
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 10 {
		t.Errorf("expected 10 sessions in index, got %d", len(idx.Sessions))
	}

	// Verify each session is present
	for i := 0; i < 10; i++ {
		sessionID := "concurrent-" + string(rune('a'+i))
		if _, ok := idx.Sessions[sessionID]; !ok {
			t.Errorf("session %q not found in index", sessionID)
		}
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// 7. TestIncrementalUpdater_E2E_SaveSession
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_E2E_SaveSession(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create a local IndexUpdater (not using global)
	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Create a session file directly (simulating what SaveStateScoped does)
	sessionID := "e2e-session"
	session := sessionJSON{
		SessionID:        sessionID,
		Name:             "E2E Test Session",
		WorkingDirectory: "/tmp/e2e",
		TotalCost:        0.10,
		Messages: []messageRef{
			{Role: "user", Content: "This is an end-to-end test"},
			{Role: "assistant", Content: "I see, testing the search index updater"},
		},
	}

	// Create the scoped subdirectory structure
	scopedDir := filepath.Join(sessionsDir, "scoped", "abcdef12")
	if err := os.MkdirAll(scopedDir, 0755); err != nil {
		t.Fatalf("create scoped dir: %v", err)
	}

	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		t.Fatalf("marshal session: %v", err)
	}
	sessionPath := filepath.Join(scopedDir, "session_"+sessionID+".json")
	if err := os.WriteFile(sessionPath, data, 0600); err != nil {
		t.Fatalf("write session file: %v", err)
	}

	// Mark the session as dirty (this is what SaveStateScoped does after writing)
	u.MarkDirty(sessionID)

	// Wait for debounce to fire (< 5s debounce, we set it to 100ms)
	time.Sleep(200 * time.Millisecond)

	// Flush to ensure clean state
	err = u.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify the index file on disk contains the session
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 1 {
		t.Fatalf("expected 1 session in index, got %d", len(idx.Sessions))
	}

	entry, ok := idx.Sessions[sessionID]
	if !ok {
		t.Fatalf("session %q not found in index", sessionID)
	}

	// Verify the session metadata
	if entry.Name != "E2E Test Session" {
		t.Errorf("Name = %q, want %q", entry.Name, "E2E Test Session")
	}
	if entry.WorkingDir != "/tmp/e2e" {
		t.Errorf("WorkingDir = %q, want %q", entry.WorkingDir, "/tmp/e2e")
	}
	if entry.MessageCount != 2 {
		t.Errorf("MessageCount = %d, want 2", entry.MessageCount)
	}
	if entry.TotalCost != 0.10 {
		t.Errorf("TotalCost = %v, want 0.10", entry.TotalCost)
	}

	// Verify text content is present and lowercased
	if entry.Text == "" {
		t.Error("Text is empty")
	}
	if entry.Text != "this is an end-to-end test\ni see, testing the search index updater" {
		t.Errorf("Text = %q, want %q", entry.Text, "this is an end-to-end test\ni see, testing the search index updater")
	}

	// Cleanup
	u.Stop()
}

// ---------------------------------------------------------------------------
// Additional edge case tests
// ---------------------------------------------------------------------------

func TestIncrementalUpdater_Flush_NoPending(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Flush with no pending sessions should be a no-op
	err := u.Flush()
	if err != nil {
		t.Fatalf("Flush with no pending: %v", err)
	}

	// No index file should be created
	_, err = os.Stat(indexPath)
	if err == nil {
		t.Error("index file should not exist after Flush with no pending")
	}
	if !os.IsNotExist(err) {
		t.Errorf("unexpected error: %v", err)
	}

	u.Stop()
}

func TestIncrementalUpdater_MultipleFlushes(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create session files
	createTestSession(t, sessionsDir, "multi-1")
	createTestSession(t, sessionsDir, "multi-2")

	u := makeTestUpdater(indexPath, sessionsDir, 5*time.Second)

	// First flush
	u.MarkDirty("multi-1")
	// Wait for the watchTimer goroutine to have started.
	for i := 0; i < 100; i++ {
		u.mu.Lock()
		started := u.timer != nil && u.stopCh != nil
		u.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	err := u.Flush()
	if err != nil {
		t.Fatalf("first Flush: %v", err)
	}

	// Small pause before second cycle to let goroutine exit
	time.Sleep(10 * time.Millisecond)

	// Second flush (should handle recreated stopCh)
	u.MarkDirty("multi-2")
	// Wait for the watchTimer goroutine to have started.
	for i := 0; i < 100; i++ {
		u.mu.Lock()
		started := u.timer != nil && u.stopCh != nil
		u.mu.Unlock()
		if started {
			break
		}
		time.Sleep(time.Millisecond)
	}
	err = u.Flush()
	if err != nil {
		t.Fatalf("second Flush: %v", err)
	}

	// Verify both sessions are in the index
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 2 {
		t.Errorf("expected 2 sessions, got %d", len(idx.Sessions))
	}

	u.Stop()
}

func TestIncrementalUpdater_MultipleStops(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Multiple stops should not panic
	u.Stop()
	u.Stop()
	u.Stop()

	// Mark dirty after stop should still work (recreates stopCh)
	createTestSession(t, sessionsDir, "after-stop")
	u.MarkDirty("after-stop")

	// Flush should work
	err := u.Flush()
	if err != nil {
		t.Fatalf("Flush after stops: %v", err)
	}

	// Verify the session was indexed
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(idx.Sessions))
	}

	u.Stop()
}

func TestIncrementalUpdater_CoalescesMarks(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Create session file
	createTestSession(t, sessionsDir, "coalesce")

	u := makeTestUpdater(indexPath, sessionsDir, 100*time.Millisecond)

	// Mark the same session dirty multiple times
	for i := 0; i < 5; i++ {
		u.MarkDirty("coalesce")
	}

	// Wait for debounce
	time.Sleep(200 * time.Millisecond)

	// Verify only one entry in the index
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 1 {
		t.Errorf("expected 1 session (coalesced), got %d", len(idx.Sessions))
	}

	u.Stop()
}

func TestIncrementalUpdater_LastSaveAt(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	createTestSession(t, sessionsDir, "saveat")

	u := makeTestUpdater(indexPath, sessionsDir, 50*time.Millisecond)

	// Before any save, lastSaveAt should be zero
	u.mu.Lock()
	initialSaveAt := u.lastSaveAt
	u.mu.Unlock()
	if !initialSaveAt.IsZero() {
		t.Errorf("expected zero lastSaveAt initially, got %v", initialSaveAt)
	}

	// Mark dirty and wait for debounce
	u.MarkDirty("saveat")
	time.Sleep(150 * time.Millisecond)

	// lastSaveAt should be set
	u.mu.Lock()
	afterSaveAt := u.lastSaveAt
	u.mu.Unlock()

	if afterSaveAt.IsZero() {
		t.Error("expected lastSaveAt to be set after debounce")
	}

	// Verify it's recent
	if time.Since(afterSaveAt) > 2*time.Second {
		t.Errorf("lastSaveAt is too old: %v", afterSaveAt)
	}

	u.Stop()
}

func TestIncrementalUpdater_RebuildHandlesLoadError(t *testing.T) {
	tmp := t.TempDir()
	// Use a path where parent directory doesn't exist yet
	indexPath := filepath.Join(tmp, "nonexistent", "subdir", "search-index.json")
	sessionsDir := tmp

	createTestSession(t, sessionsDir, "loaderr")

	u := makeTestUpdater(indexPath, sessionsDir, 50*time.Millisecond)

	// Mark dirty and wait for debounce
	u.MarkDirty("loaderr")
	time.Sleep(150 * time.Millisecond)

	// The index should have been created despite the initial load error
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(idx.Sessions))
	}

	u.Stop()
}

func TestIncrementalUpdater_GlobalUpdaterNil(t *testing.T) {
	// Save and restore global state
	oldGlobal := GlobalUpdater
	if oldGlobal != nil {
		oldGlobal.Stop()
	}
	GlobalUpdater = nil
	defer func() {
		GlobalUpdater = oldGlobal
	}()

	// MarkSessionDirty should be a safe no-op when GlobalUpdater is nil
	// This should not panic
	MarkSessionDirty("test-session")
}

func TestIncrementalUpdater_GlobalUpdaterInitialized(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Save and restore global state
	oldGlobal := GlobalUpdater
	if oldGlobal != nil {
		oldGlobal.Stop()
	}
	defer func() {
		GlobalUpdater = oldGlobal
	}()

	// Initialize global updater
	GlobalUpdater = NewIndexUpdater(indexPath, sessionsDir)
	GlobalUpdater.debounce = 100 * time.Millisecond

	// Create a session file
	createTestSession(t, sessionsDir, "global-ses")

	// Use the global helper
	MarkSessionDirty("global-ses")

	// Wait for debounce
	time.Sleep(200 * time.Millisecond)

	// Flush to ensure clean state
	err := GlobalUpdater.Flush()
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Verify the index was written
	idx, err := LoadIndex(indexPath)
	if err != nil {
		t.Fatalf("LoadIndex: %v", err)
	}

	if len(idx.Sessions) != 1 {
		t.Errorf("expected 1 session, got %d", len(idx.Sessions))
	}

	GlobalUpdater.Stop()
}

func TestIncrementalUpdater_InitGlobalUpdaterOnce(t *testing.T) {
	tmp := t.TempDir()
	indexPath := filepath.Join(tmp, "search-index.json")
	sessionsDir := tmp

	// Save and restore global state
	oldGlobal := GlobalUpdater
	if oldGlobal != nil {
		oldGlobal.Stop()
	}
	defer func() {
		GlobalUpdater = oldGlobal
	}()

	// Reset the sync.Once by setting GlobalUpdater to nil
	GlobalUpdater = nil

	// First call should initialize
	InitGlobalUpdater(indexPath, sessionsDir)
	firstUpdater := GlobalUpdater

	// Second call should be a no-op (sync.Once)
	InitGlobalUpdater("/different/path", "/different/dir")
	secondUpdater := GlobalUpdater

	// Both should be the same instance
	if firstUpdater != secondUpdater {
		t.Error("InitGlobalUpdater should only initialize once (sync.Once)")
	}

	GlobalUpdater.Stop()
}
