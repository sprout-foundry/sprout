package agent

import (
	"fmt"
	"os"
	"sync"
	"time"
)

// WorkspaceFileMetadata describes the per-file sync state that the
// browser-primary workspace model in SP-046 needs to enforce consistency
// between the browser-side OPFS replica and the container-side filesystem.
//
// On native sprout (single-replica), only ModifiedAt and the agent's
// turn-scoped read tracking actually matter. The sequence fields are
// placeholders for the eventual WS-based sync layer to populate from the
// browser side; until then they're zero. Storing the struct now (rather
// than retrofitting later) keeps the persistence shape stable.
//
// Spec: roadmap/SP-046-workspace-sync-model.md §3.
type WorkspaceFileMetadata struct {
	// BrowserSeq counts user-driven edits to the file from the browser side.
	// Bumped each time the user types and the change flushes to OPFS.
	BrowserSeq int64 `json:"browser_seq"`

	// ContainerSeq counts agent-driven writes to the file via the agent's
	// tool handlers. Bumped each time writeFileContent succeeds.
	ContainerSeq int64 `json:"container_seq"`

	// LastSyncedBrowser is the BrowserSeq value the container has
	// acknowledged. BrowserSeq > LastSyncedBrowser means the browser has
	// unsynced edits — see the conflict rule.
	LastSyncedBrowser int64 `json:"last_synced_browser"`

	// LastSyncedContainer is the ContainerSeq value the browser has
	// acknowledged.
	LastSyncedContainer int64 `json:"last_synced_container"`

	// ModifiedAt is the wall-clock time of the most recent write to the
	// file from any source. Used by the staleness rule's "recent
	// modification" check.
	ModifiedAt time.Time `json:"modified_at"`
}

// hasUnsyncedBrowserEdits reports whether the browser side has writes
// the container hasn't applied yet. The agent's write_file tool wrapper
// refuses to overwrite such files without explicit user confirmation.
func (m WorkspaceFileMetadata) hasUnsyncedBrowserEdits() bool {
	return m.BrowserSeq > m.LastSyncedBrowser
}

// stalenessFreshnessWindow is the "modified recently" cutoff for the
// staleness rule. Files written this recently are considered possibly-
// stale-on-read, so the agent must re-read before writing.
//
// 30s is chosen to comfortably cover a typed-edit flush from the browser
// (which queues through OPFS → outbound op → container apply) under
// realistic network conditions, while still keeping the false-positive
// rate low when the agent does its own write→write sequences.
const stalenessFreshnessWindow = 30 * time.Second

// turnFileTracker records which files the agent has called read_file on
// during the current turn. Used by the staleness rule's "must read before
// write this turn" check.
type turnFileTracker struct {
	mu    sync.Mutex
	reads map[string]time.Time
}

func newTurnFileTracker() *turnFileTracker {
	return &turnFileTracker{reads: make(map[string]time.Time)}
}

func (t *turnFileTracker) recordRead(path string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reads == nil {
		t.reads = make(map[string]time.Time)
	}
	t.reads[path] = time.Now()
}

func (t *turnFileTracker) hasReadThisTurn(path string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.reads[path]
	return ok
}

func (t *turnFileTracker) reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.reads = make(map[string]time.Time)
}

// RecordFileReadThisTurn marks `path` as read by the agent during the
// current turn. Called from the read_file tool handler. Safe on a nil
// receiver so test scaffolding doesn't have to initialize the tracker.
func (a *Agent) RecordFileReadThisTurn(path string) {
	if a == nil {
		return
	}
	a.fileReadsMu.Lock()
	if a.filesReadThisTurn == nil {
		a.filesReadThisTurn = newTurnFileTracker()
	}
	a.fileReadsMu.Unlock()
	a.filesReadThisTurn.recordRead(path)
}

// ResetFileReadsForNewTurn clears the per-turn read tracker. Called at
// turn boundaries so the staleness rule resets between turns: a file the
// agent read on turn N still needs a fresh read_file on turn N+1 before
// writing.
func (a *Agent) ResetFileReadsForNewTurn() {
	if a == nil {
		return
	}
	a.fileReadsMu.Lock()
	defer a.fileReadsMu.Unlock()
	if a.filesReadThisTurn == nil {
		a.filesReadThisTurn = newTurnFileTracker()
		return
	}
	a.filesReadThisTurn.reset()
}

// checkWriteStaleness applies the SP-046 §7 staleness rule:
//
//  1. If the file doesn't exist, allow the write (creating new files
//     never needs a prior read).
//  2. If the agent hasn't called read_file(path) this turn, REFUSE.
//  3. If the file was modified within the freshness window AND the
//     modification was NOT by this turn's earlier read, REFUSE.
//
// Returns nil to allow the write, or a tool error the agent can react to.
// The error wording is deliberately actionable: the agent's response to
// the error should be to call read_file(path) and retry.
//
// On nil Agent (test scaffolding), the check is a no-op.
func (a *Agent) checkWriteStaleness(path string) error {
	if a == nil {
		return nil
	}
	info, statErr := os.Stat(path)
	if statErr != nil {
		// File doesn't exist (or some unrelated stat error). Creating
		// new files via write_file is fine — no prior read required.
		return nil
	}

	hasReadThisTurn := false
	a.fileReadsMu.Lock()
	tracker := a.filesReadThisTurn
	a.fileReadsMu.Unlock()
	if tracker != nil {
		hasReadThisTurn = tracker.hasReadThisTurn(path)
	}

	if !hasReadThisTurn {
		return fmt.Errorf(
			"staleness check: must call read_file(%q) first; the file may be stale and overwriting blindly is rarely correct",
			path,
		)
	}

	// File was read this turn — but if it's been modified within the
	// freshness window by something OTHER than the prior read (which the
	// stat ModTime ≤ read time would tell us), refuse. We approximate
	// "modified by something other than the agent's read" with: ModTime
	// is more recent than when the agent recorded the read.
	if tracker != nil {
		tracker.mu.Lock()
		readAt, ok := tracker.reads[path]
		tracker.mu.Unlock()
		if ok && info.ModTime().After(readAt) &&
			time.Since(info.ModTime()) < stalenessFreshnessWindow {
			return fmt.Errorf(
				"staleness check: %q was modified after your last read_file call; re-read before writing",
				path,
			)
		}
	}

	return nil
}
