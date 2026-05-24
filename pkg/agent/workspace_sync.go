package agent

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"sync/atomic"
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

// HasUnsyncedBrowserEdits reports whether the browser side has writes
// the container hasn't applied yet. The agent's write_file tool wrapper
// refuses to overwrite such files without explicit user confirmation.
func (m WorkspaceFileMetadata) HasUnsyncedBrowserEdits() bool {
	return m.BrowserSeq > m.LastSyncedBrowser
}

// ErrWriteStale is the sentinel returned by checkWriteStaleness for the
// "no recent read" / "modified after read" cases. The agent's correct
// response is to read_file(path) and retry.
//
// ErrWriteHasUnsyncedEdits is the sentinel for the "browser has edits
// the container hasn't seen yet" case. The agent must NOT auto-retry;
// instead it should ask the user whether to overwrite. The platform's
// WS sync layer populates the WorkspaceFileMetadata that drives this.
//
// Both are deliberately wrappable via errors.Is so callers (including
// the tool-result formatter) can distinguish them without string-matching.
var (
	ErrWriteStale            = errors.New("write refused: file may be stale")
	ErrWriteHasUnsyncedEdits = errors.New("write refused: user has unsynced edits to this file")
)

// patchSeqNum is a monotonically increasing atomic counter used to assign
// unique sequence numbers to workspace_patch events emitted during tool-call
// writes. The sequence ensures the browser can detect and order patches even
// when events arrive out of order.
var patchSeqNum int64

// nextPatchSeq returns the next patch sequence number. Thread-safe via
// atomic increment.
func nextPatchSeq() int64 {
	return atomic.AddInt64(&patchSeqNum, 1)
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

// workspaceMetadataStore is the in-memory per-path WorkspaceFileMetadata
// the agent consults from checkWriteStaleness. The platform-side sync
// layer (when wired up via the WS transport in SP-046-1d/1f/1g) is
// expected to populate this via Agent.SetFileMetadata as it processes
// browser-side edit notifications.
//
// Storing this in memory is fine for beta: the metadata is recomputable
// from on-disk file state plus the browser's outbound op log, and a
// container restart re-syncs the full state from the browser-side OPFS
// replica anyway. A persistent store could be added later if measurement
// shows the resync cost is meaningful.
type workspaceMetadataStore struct {
	mu sync.RWMutex
	m  map[string]WorkspaceFileMetadata
}

func newWorkspaceMetadataStore() *workspaceMetadataStore {
	return &workspaceMetadataStore{m: make(map[string]WorkspaceFileMetadata)}
}

func (s *workspaceMetadataStore) get(path string) (WorkspaceFileMetadata, bool) {
	if s == nil {
		return WorkspaceFileMetadata{}, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	md, ok := s.m[path]
	return md, ok
}

func (s *workspaceMetadataStore) set(path string, md WorkspaceFileMetadata) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]WorkspaceFileMetadata)
	}
	s.m[path] = md
}

// SetFileMetadata replaces the cached sync metadata for `path`. Called by
// the platform-side sync bridge whenever it learns about a new browser
// sequence or last-synced acknowledgement. Safe to call before the agent
// is otherwise initialized.
func (a *Agent) SetFileMetadata(path string, md WorkspaceFileMetadata) {
	if a == nil {
		return
	}
	a.fileReadsMu.Lock()
	if a.fileMetadata == nil {
		a.fileMetadata = newWorkspaceMetadataStore()
	}
	a.fileReadsMu.Unlock()
	a.fileMetadata.set(path, md)
}

// GetFileMetadata returns the cached metadata for `path` (zero-value +
// false if absent). Read-side companion to SetFileMetadata.
func (a *Agent) GetFileMetadata(path string) (WorkspaceFileMetadata, bool) {
	if a == nil {
		return WorkspaceFileMetadata{}, false
	}
	a.fileReadsMu.Lock()
	store := a.fileMetadata
	a.fileReadsMu.Unlock()
	if store == nil {
		return WorkspaceFileMetadata{}, false
	}
	return store.get(path)
}

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

// checkWriteStaleness applies the SP-046 §7 staleness rule plus the §3
// conflict rule:
//
//  1. If the file has WorkspaceFileMetadata showing unsynced browser
//     edits (BrowserSeq > LastSyncedBrowser), REFUSE with
//     ErrWriteHasUnsyncedEdits. The agent should ask the user before
//     overwriting — auto-retry is NOT correct.
//  2. If the file doesn't exist, allow the write (creating new files
//     never needs a prior read).
//  3. If the agent hasn't called read_file(path) this turn, REFUSE with
//     ErrWriteStale. The agent's correct response is to call
//     read_file(path) and retry.
//  4. If the file was modified within the freshness window AND the
//     modification was NOT by this turn's earlier read, REFUSE with
//     ErrWriteStale.
//
// Both refusals wrap their respective sentinels via fmt.Errorf("...: %w",
// sentinel) so callers can distinguish them with errors.Is.
//
// On nil Agent (test scaffolding), the check is a no-op.
func (a *Agent) checkWriteStaleness(path string) error {
	if a == nil {
		return nil
	}

	// Conflict check runs BEFORE the staleness check so the agent doesn't
	// get the "read first" hint when the real answer is "ask the user."
	// The metadata store is populated by the platform-side sync layer
	// (SP-046-1d/1f/1g); on native or free-tier WASM it's empty and this
	// check is a no-op.
	if md, ok := a.GetFileMetadata(path); ok && md.HasUnsyncedBrowserEdits() {
		return fmt.Errorf(
			"%w: %q has %d unsynced edits from the user (browser_seq=%d, last_synced=%d); ask the user whether to overwrite",
			ErrWriteHasUnsyncedEdits, path,
			md.BrowserSeq-md.LastSyncedBrowser,
			md.BrowserSeq, md.LastSyncedBrowser,
		)
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
			"%w: must call read_file(%q) first; the file may be stale and overwriting blindly is rarely correct",
			ErrWriteStale, path,
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
				"%w: %q was modified after your last read_file call; re-read before writing",
				ErrWriteStale, path,
			)
		}
	}

	return nil
}
