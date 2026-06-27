package tools

import (
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// ------------------------------------------------------------------------
// TurnReadTracker — per-turn read tracking for the agent staleness rule
// (SP-046 §7). Records which files the agent has read during the current
// turn and their browser_seq at the time of read.
// ------------------------------------------------------------------------

// TurnReadTracker tracks per-turn read state for staleness enforcement.
type TurnReadTracker struct {
	mu          sync.Mutex
	readPaths   map[string]int64  // path → browser_seq at time of read
	readTimes   map[string]time.Time // path → time when read_file was called
}

// NewTurnReadTracker creates a fresh tracker ready for a new turn.
func NewTurnReadTracker() *TurnReadTracker {
	return &TurnReadTracker{
		readPaths: make(map[string]int64),
		readTimes: make(map[string]time.Time),
	}
}

// RecordRead records that the agent read the given path at the current
// turn, capturing the browser_seq from the file's metadata.
func (t *TurnReadTracker) RecordRead(path string, browserSeq int64) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.readPaths[path] = browserSeq
	t.readTimes[path] = time.Now()
}

// HasReadThisTurn returns true if the agent called read_file on this path
// during the current turn.
func (t *TurnReadTracker) HasReadThisTurn(path string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.readPaths[path]
	return ok
}

// GetLastReadSeq returns the browser_seq captured when the agent last read
// the given path this turn, along with whether the path was seen.
func (t *TurnReadTracker) GetLastReadSeq(path string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	seq, ok := t.readPaths[path]
	return seq, ok
}

// GetLastReadTime returns the time the agent last read the given path this
// turn, and whether the path was seen.
func (t *TurnReadTracker) GetLastReadTime(path string) time.Time {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.readTimes[path]
}

// ------------------------------------------------------------------------
// StalenessChecker — orchestrates the staleness check per spec §7
// ------------------------------------------------------------------------

// StalenessChecker checks whether a file write is stale based on the
// agent's read tracking and the workspace sync state.
type StalenessChecker struct {
	syncState       *SyncState
	tracker         *TurnReadTracker
	stalenessWindow time.Duration
}

// NewStalenessChecker creates a checker with the given sync state and
// tracker. Default staleness window is 30 seconds.
func NewStalenessChecker(syncState *SyncState, tracker *TurnReadTracker) *StalenessChecker {
	return &StalenessChecker{
		syncState:       syncState,
		tracker:         tracker,
		stalenessWindow: 30 * time.Second,
	}
}

// Check returns nil if the write is allowed, or an error if the file may
// be stale. The error uses the exact format from spec §7.
func (sc *StalenessChecker) Check(path string) error {
	// Condition 3: agent has not called read_file(path) this turn
	if !sc.tracker.HasReadThisTurn(path) {
		return fmt.Errorf("must call read_file(%s) first; the file may be stale", path)
	}

	meta, ok := sc.syncState.GetMetadata(path)
	if !ok {
		// No metadata for this path — treat as fresh (no browser edits)
		// but still require the read to have happened (already checked above)
		return nil
	}

	// Condition 1: file's browser_seq has changed since the agent last read it
	lastReadSeq, ok := sc.tracker.GetLastReadSeq(path)
	if !ok || meta.BrowserSeq != lastReadSeq {
		return fmt.Errorf("must call read_file(%s) first; the file may be stale", path)
	}

	// Condition 2: file's last-modified is within the staleness window
	if time.Since(meta.ModifiedAt) < sc.stalenessWindow {
		return fmt.Errorf("must call read_file(%s) first; the file may be stale", path)
	}

	return nil
}

// ------------------------------------------------------------------------
// Global accessors — package-level globals so existing handlers can opt in
// with a simple call without requiring constructor wiring.
// ------------------------------------------------------------------------

var (
	globalSyncState *SyncState
	globalSyncOnce  sync.Once

	globalChecker atomic.Pointer[StalenessChecker]
)

// initGlobalSyncState lazily initializes the global SyncState.
func initGlobalSyncState() {
	globalSyncOnce.Do(func() {
		globalSyncState = NewSyncState()
	})
}

// GetGlobalSyncState returns the package-level SyncState singleton.
func GetGlobalSyncState() *SyncState {
	initGlobalSyncState()
	return globalSyncState
}

// GetGlobalTurnReadTracker returns the tracker from the global checker,
// or nil if no checker has been configured yet.
func GetGlobalTurnReadTracker() *TurnReadTracker {
	c := globalChecker.Load()
	if c != nil {
		return c.tracker
	}
	return nil
}

// SetGlobalStalenessChecker installs the global checker. Existing code
// that doesn't set a checker continues to work (CheckStaleness is a no-op).
func SetGlobalStalenessChecker(checker *StalenessChecker) {
	globalChecker.Store(checker)
}

// CheckStaleness is the convenience wrapper called by write handlers.
// Returns nil when no global checker is set (no-op), otherwise delegates
// to the checker's Check method.
func CheckStaleness(path string) error {
	c := globalChecker.Load()
	if c == nil {
		return nil
	}
	return c.Check(path)
}
