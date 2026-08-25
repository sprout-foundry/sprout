package tools

import (
	"strings"
	"sync"
	"testing"
	"time"
)

// ------------------------------------------------------------------------
// StalenessChecker.Check — spec §7 scenarios
// ------------------------------------------------------------------------

func TestStalenessChecker_StaleBySequence(t *testing.T) {
	// browser_seq is 5, but the agent read it when seq was 4.
	syncState := NewSyncState()
	syncState.files["test.txt"] = &FileMetadata{
		BrowserSeq: 5,
		ModifiedAt: time.Now().Add(-1 * time.Minute), // old enough to pass mtime check
	}

	tracker := NewTurnReadTracker()
	tracker.RecordRead("test.txt", 4) // recorded old seq

	checker := NewStalenessChecker(syncState, tracker)
	err := checker.Check("test.txt")

	if err == nil {
		t.Fatal("expected error because browser_seq changed since read, got nil")
	}
	expected := "must call read_file(test.txt) first; the file may be stale"
	if err.Error() != expected {
		t.Fatalf("wrong error message:\n got: %q\nwant: %q", err.Error(), expected)
	}
}

func TestStalenessChecker_StaleByRecentMtime(t *testing.T) {
	// browser_seq matches, but the file was modified right now (within 30s).
	syncState := NewSyncState()
	syncState.files["test.txt"] = &FileMetadata{
		BrowserSeq: 5,
		ModifiedAt: time.Now(), // just now — definitely within 30s window
	}

	tracker := NewTurnReadTracker()
	tracker.RecordRead("test.txt", 5) // seq matches

	checker := NewStalenessChecker(syncState, tracker)
	err := checker.Check("test.txt")

	if err == nil {
		t.Fatal("expected error because file was recently modified, got nil")
	}
	expected := "must call read_file(test.txt) first; the file may be stale"
	if err.Error() != expected {
		t.Fatalf("wrong error message:\n got: %q\nwant: %q", err.Error(), expected)
	}
}

func TestStalenessChecker_StaleByNoReadThisTurn(t *testing.T) {
	// Agent never called read_file on this path this turn.
	syncState := NewSyncState()
	syncState.files["test.txt"] = &FileMetadata{
		BrowserSeq: 3,
		ModifiedAt: time.Now().Add(-1 * time.Minute),
	}

	tracker := NewTurnReadTracker()
	// Do NOT record any read for test.txt

	checker := NewStalenessChecker(syncState, tracker)
	err := checker.Check("test.txt")

	if err == nil {
		t.Fatal("expected error because read_file was not called this turn, got nil")
	}
	expected := "must call read_file(test.txt) first; the file may be stale"
	if err.Error() != expected {
		t.Fatalf("wrong error message:\n got: %q\nwant: %q", err.Error(), expected)
	}
}

func TestStalenessChecker_HappyPath(t *testing.T) {
	// All three conditions satisfied: read happened, seq matches, mtime is old.
	syncState := NewSyncState()
	syncState.files["test.txt"] = &FileMetadata{
		BrowserSeq: 5,
		ModifiedAt: time.Now().Add(-1 * time.Minute), // > 30s ago
	}

	tracker := NewTurnReadTracker()
	tracker.RecordRead("test.txt", 5) // seq matches current browser_seq

	checker := NewStalenessChecker(syncState, tracker)
	err := checker.Check("test.txt")

	if err != nil {
		t.Fatalf("expected no error on happy path, got: %v", err)
	}
}

// ------------------------------------------------------------------------
// GetMetadata on nonexistent path — check returns nil
// ------------------------------------------------------------------------

func TestStalenessChecker_NoMetadata_ReturnsNil(t *testing.T) {
	syncState := NewSyncState()
	// No metadata registered for "new.txt"

	tracker := NewTurnReadTracker()
	tracker.RecordRead("new.txt", 0) // read recorded but no metadata exists

	checker := NewStalenessChecker(syncState, tracker)
	err := checker.Check("new.txt")

	if err != nil {
		t.Fatalf("expected nil when file has no metadata, got: %v", err)
	}
}

// ------------------------------------------------------------------------
// Global CheckStaleness no-op when no checker is set
// ------------------------------------------------------------------------

func TestCheckStaleness_NoGlobalChecker_NoOp(t *testing.T) {
	// Save current global state and restore after test.
	saved := globalChecker.Load()
	defer func() { globalChecker.Store(saved) }()

	// Explicitly ensure no checker is set for this test.
	globalChecker.Store(nil)

	err := CheckStaleness("anything")
	if err != nil {
		t.Fatalf("expected nil when no global checker is set, got: %v", err)
	}
}

// ------------------------------------------------------------------------
// TurnReadTracker thread safety
// ------------------------------------------------------------------------

func TestTurnReadTracker_ConcurrentRecordRead(t *testing.T) {
	tracker := NewTurnReadTracker()
	const goroutines = 10
	var wg sync.WaitGroup
	wg.Add(goroutines)

	for i := 0; i < goroutines; i++ {
		go func(i int) {
			defer wg.Done()
			path := "file" + string(rune('a'+i)) + ".txt"
			tracker.RecordRead(path, int64(i))
		}(i)
	}

	wg.Wait()

	// Verify all 10 paths were recorded.
	for i := 0; i < goroutines; i++ {
		path := "file" + string(rune('a'+i)) + ".txt"
		if !tracker.HasReadThisTurn(path) {
			t.Errorf("expected path %q to be recorded after concurrent writes", path)
		}
		seq, ok := tracker.GetLastReadSeq(path)
		if !ok {
			t.Errorf("path %q: expected to be found", path)
		}
		if seq != int64(i) {
			t.Errorf("path %q: expected seq %d, got %d", path, i, seq)
		}
	}
}

// ------------------------------------------------------------------------
// TurnReadTracker method correctness
// ------------------------------------------------------------------------

func TestTurnReadTracker_HasReadThisTurn(t *testing.T) {
	tracker := NewTurnReadTracker()

	if tracker.HasReadThisTurn("never-read.txt") {
		t.Fatal("HasReadThisTurn returned true for a path never recorded")
	}

	tracker.RecordRead("foo.txt", 42)
	if !tracker.HasReadThisTurn("foo.txt") {
		t.Fatal("HasReadThisTurn returned false after RecordRead")
	}

	// A different path should still be absent.
	if tracker.HasReadThisTurn("bar.txt") {
		t.Fatal("HasReadThisTurn returned true for an unrelated path")
	}
}

func TestTurnReadTracker_GetLastReadSeq(t *testing.T) {
	tracker := NewTurnReadTracker()

	// No read yet — should return (0, false).
	seq, ok := tracker.GetLastReadSeq("foo.txt")
	if ok {
		t.Fatal("expected false for unrecorded path")
	}
	if seq != 0 {
		t.Fatalf("expected 0 for unrecorded path, got %d", seq)
	}

	tracker.RecordRead("foo.txt", 7)
	seq, ok = tracker.GetLastReadSeq("foo.txt")
	if !ok {
		t.Fatal("expected true after RecordRead")
	}
	if seq != 7 {
		t.Fatalf("expected seq 7, got %d", seq)
	}

	// Overwrite with a new read — seq should update.
	tracker.RecordRead("foo.txt", 99)
	seq, ok = tracker.GetLastReadSeq("foo.txt")
	if !ok {
		t.Fatal("expected true after second RecordRead")
	}
	if seq != 99 {
		t.Fatalf("expected seq 99 after second read, got %d", seq)
	}
}

func TestTurnReadTracker_GetLastReadTime(t *testing.T) {
	tracker := NewTurnReadTracker()

	// No read yet — should return zero time.
	ts := tracker.GetLastReadTime("foo.txt")
	if !ts.IsZero() {
		t.Fatalf("expected zero time for unrecorded path, got %v", ts)
	}

	before := time.Now()
	tracker.RecordRead("foo.txt", 1)
	ts = tracker.GetLastReadTime("foo.txt")

	if ts.IsZero() {
		t.Fatal("expected non-zero time after RecordRead")
	}
	if ts.Before(before) {
		t.Fatalf("recorded time %v is before the call was made %v", ts, before)
	}
	if ts.After(time.Now()) {
		t.Fatalf("recorded time %v is in the future", ts)
	}
}

// ------------------------------------------------------------------------
// CheckStaleness with global checker installed
// ------------------------------------------------------------------------

func TestCheckStaleness_WithGlobalChecker(t *testing.T) {
	// Save and restore global state.
	savedChecker := globalChecker.Load()
	defer func() { globalChecker.Store(savedChecker) }()

	syncState := NewSyncState()
	tracker := NewTurnReadTracker()
	checker := NewStalenessChecker(syncState, tracker)

	globalChecker.Store(checker)

	// Scenario: no read → error
	err := CheckStaleness("x.txt")
	if err == nil {
		t.Fatal("expected error for unread path, got nil")
	}
	if !strings.Contains(err.Error(), "must call read_file") {
		t.Fatalf("unexpected error: %v", err)
	}

	// Now record a read with no metadata → should pass.
	tracker.RecordRead("y.txt", 0)
	err = CheckStaleness("y.txt")
	if err != nil {
		t.Fatalf("expected nil for path with read but no metadata, got: %v", err)
	}
}

// ------------------------------------------------------------------------
// GetGlobalTurnReadTracker
// ------------------------------------------------------------------------

func TestGetGlobalTurnReadTracker_NoChecker(t *testing.T) {
	saved := globalChecker.Load()
	defer func() { globalChecker.Store(saved) }()

	globalChecker.Store(nil)

	tracker := GetGlobalTurnReadTracker()
	if tracker != nil {
		t.Fatal("expected nil tracker when no global checker is set")
	}
}

func TestGetGlobalTurnReadTracker_WithChecker(t *testing.T) {
	saved := globalChecker.Load()
	defer func() { globalChecker.Store(saved) }()

	syncState := NewSyncState()
	tracker := NewTurnReadTracker()
	checker := NewStalenessChecker(syncState, tracker)

	globalChecker.Store(checker)

	got := GetGlobalTurnReadTracker()
	if got != tracker {
		t.Fatal("GetGlobalTurnReadTracker should return the tracker from the global checker")
	}
}
