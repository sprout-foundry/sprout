package agent

// workspace_sync.go — the workspace file-metadata + patch-reconciliation
// layer: the WorkspaceFileMetadata type, the patch sequence-number state,
// the workspace metadata store, the seq-number reconciliation
// (ReconcileSeqNumbers, determineReconcileAction, CheckPatchConflict), the
// turn file-read tracker, and the write-staleness check. The sync-op apply
// layer lives in workspace_sync_ops.go.

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// WorkspaceFileMetadata describes the per-file sync state for enforcing
// consistency between the browser-side OPFS replica and the container FS.
// On native sprout (single-replica), only ModifiedAt and the agent's
// turn-scoped read tracking matter; sequence fields are placeholders for
// the eventual WS-based sync layer.
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

// Sentinel errors for write-staleness and conflict detection.
// Both are wrappable via errors.Is for caller distinction.
var (
	ErrWriteStale            = errors.New("write refused: file may be stale")
	ErrWriteHasUnsyncedEdits = errors.New("write refused: user has unsynced edits to this file")
)

// patchSeqNum assigns unique sequence numbers to workspace_patch events.
var patchSeqNum int64

// nextPatchSeq returns the next patch sequence number. Thread-safe via
// atomic increment.
func nextPatchSeq() int64 {
	return atomic.AddInt64(&patchSeqNum, 1)
}

// stalenessFreshnessWindow is the "modified recently" cutoff for the staleness rule.
const stalenessFreshnessWindow = 30 * time.Second

// workspaceMetadataStore is the in-memory per-path metadata the agent
// consults from checkWriteStaleness. The platform-side sync layer
// populates it via Agent.SetFileMetadata.
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

// SetFileMetadata replaces the cached sync metadata for `path`.
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

// ReconciliationActionType enumerates the possible outcomes of comparing
// browser and container sequence numbers for a single file.
type ReconciliationActionType string

const (
	// ReconcileSyncOK means browser and container are at the same seq.
	ReconcileSyncOK ReconciliationActionType = "sync_ok"
	// ReconcileContainerAhead means the container has patches the browser hasn't seen.
	ReconcileContainerAhead ReconciliationActionType = "container_ahead"
	// ReconcileBrowserAhead means the browser has edits the container hasn't applied.
	ReconcileBrowserAhead ReconciliationActionType = "browser_ahead"
	// ReconcileDiverged means both sides have diverged and conflict resolution is needed.
	ReconcileDiverged ReconciliationActionType = "diverged"
)

// ReconciliationActionResult is the per-file reconciliation outcome.
type ReconciliationActionResult struct {
	FilePath     string                   `json:"file_path"`
	Action       ReconciliationActionType `json:"action"`
	ContainerSeq int64                    `json:"container_seq"`
	BrowserSeq   int64                    `json:"browser_seq"`
}

// ReconcileSeqNumbers compares browser-supplied per-file sequence numbers
// against the container's stored metadata and returns a reconciliation plan.
func ReconcileSeqNumbers(ag *Agent, browserSeqs map[string]int64) ([]ReconciliationActionResult, error) {
	if ag == nil {
		return nil, agenterrors.NewTool("workspace_sync", "agent is nil", nil)
	}
	if ag.fileMetadata == nil {
		// No metadata store means no files tracked — everything is browser_ahead
		results := make([]ReconciliationActionResult, 0, len(browserSeqs))
		for path, bSeq := range browserSeqs {
			if bSeq > 0 {
				results = append(results, ReconciliationActionResult{
					FilePath:     path,
					Action:       ReconcileBrowserAhead,
					BrowserSeq:   bSeq,
					ContainerSeq: 0,
				})
			}
		}
		return results, nil
	}

	results := make([]ReconciliationActionResult, 0, len(browserSeqs))
	for path, bSeq := range browserSeqs {
		md, exists := ag.GetFileMetadata(path)
		if !exists {
			// Container has no record of this file — browser is ahead
			if bSeq > 0 {
				results = append(results, ReconciliationActionResult{
					FilePath:     path,
					Action:       ReconcileBrowserAhead,
					BrowserSeq:   bSeq,
					ContainerSeq: 0,
				})
			}
			continue
		}

		action := determineReconcileAction(bSeq, md)
		results = append(results, ReconciliationActionResult{
			FilePath:     path,
			Action:       action,
			BrowserSeq:   bSeq,
			ContainerSeq: md.ContainerSeq,
		})
	}
	// Sort results by file path for deterministic output
	sort.Slice(results, func(i, j int) bool {
		return results[i].FilePath < results[j].FilePath
	})
	return results, nil
}

// determineReconcileAction decides the recovery action for one file.
func determineReconcileAction(browserSeq int64, md WorkspaceFileMetadata) ReconciliationActionType {
	// If both agree on the same seq, we're in sync
	if browserSeq == md.ContainerSeq {
		return ReconcileSyncOK
	}

	// Browser has unsynced edits AND container has patches browser hasn't seen
	browserHasEdits := browserSeq > md.LastSyncedBrowser
	containerAhead := md.ContainerSeq > md.LastSyncedContainer

	if browserHasEdits && containerAhead {
		return ReconcileDiverged
	}
	if containerAhead {
		return ReconcileContainerAhead
	}
	if browserHasEdits {
		return ReconcileBrowserAhead
	}

	// Fallback: if browser seq doesn't match container seq but neither
	// has explicit unsynced state, compare directly
	if browserSeq < md.ContainerSeq {
		return ReconcileContainerAhead
	}
	if browserSeq > md.ContainerSeq {
		return ReconcileBrowserAhead
	}

	return ReconcileSyncOK
}

// CheckPatchConflict checks whether a container patch to the given path
// conflicts with unsynced browser edits. Returns (conflict bool, theirsPath string).
// theirsPath is "<path>.theirs" when conflict is true, empty otherwise.
func (a *Agent) CheckPatchConflict(path string) (bool, string) {
	if a == nil {
		return false, ""
	}
	md, ok := a.GetFileMetadata(path)
	if !ok {
		return false, ""
	}
	if md.HasUnsyncedBrowserEdits() {
		return true, path + ".theirs"
	}
	return false, ""
}

// normalizeFilePath resolves a path to its absolute form so the staleness
// tracker's map keys match regardless of relative or absolute input.
func normalizeFilePath(path, workspaceRoot string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	base := workspaceRoot
	if base == "" {
		var err error
		base, err = os.Getwd()
		if err != nil {
			return path
		}
	}
	return filepath.Clean(filepath.Join(base, path))
}

// turnFileTracker records which files the agent has called read_file on
// during the current turn. Paths are normalized to absolute form.
type turnFileTracker struct {
	mu    sync.Mutex
	reads map[string]time.Time
}

func newTurnFileTracker() *turnFileTracker {
	return &turnFileTracker{reads: make(map[string]time.Time)}
}

func (t *turnFileTracker) recordRead(path, workspaceRoot string) {
	if t == nil {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.reads == nil {
		t.reads = make(map[string]time.Time)
	}
	t.reads[normalizeFilePath(path, workspaceRoot)] = time.Now()
}

func (t *turnFileTracker) hasReadThisTurn(path, workspaceRoot string) bool {
	if t == nil {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	_, ok := t.reads[normalizeFilePath(path, workspaceRoot)]
	return ok
}

// getReadTime returns the time the file was read this turn (zero time if not read).
// Caller must NOT hold t.mu.
func (t *turnFileTracker) getReadTime(path, workspaceRoot string) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	readAt, ok := t.reads[normalizeFilePath(path, workspaceRoot)]
	return readAt, ok
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
// current turn. Called from the read_file tool handler.
func (a *Agent) RecordFileReadThisTurn(path string) {
	if a == nil {
		return
	}
	a.fileReadsMu.Lock()
	if a.filesReadThisTurn == nil {
		a.filesReadThisTurn = newTurnFileTracker()
	}
	a.fileReadsMu.Unlock()
	a.filesReadThisTurn.recordRead(path, a.currentWorkspaceRoot())
}

// ResetFileReadsForNewTurn clears the per-turn read tracker at turn boundaries.
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

// checkWriteStaleness enforces the staleness and conflict rules:
//  1. If the file has unsynced browser edits, REFUSE with ErrWriteHasUnsyncedEdits.
//  2. If the file doesn't exist, allow the write.
//  3. If the agent hasn't read_file(path) this turn, REFUSE with ErrWriteStale.
//  4. If the file was modified within the freshness window after the read, REFUSE.
//
// On nil Agent, the check is a no-op.
func (a *Agent) checkWriteStaleness(path string) error {
	if a == nil {
		return nil
	}

	if md, ok := a.GetFileMetadata(path); ok && md.HasUnsyncedBrowserEdits() {
		return agenterrors.Wrapf(
			ErrWriteHasUnsyncedEdits, "%q has %d unsynced edits from the user (browser_seq=%d, last_synced=%d); ask the user whether to overwrite",
			path,
			md.BrowserSeq-md.LastSyncedBrowser,
			md.BrowserSeq, md.LastSyncedBrowser,
		)
	}

	info, statErr := os.Stat(path)
	if statErr != nil {
		// File doesn't exist — creating new files is fine.
		return nil
	}

	hasReadThisTurn := false
	workspaceRoot := a.currentWorkspaceRoot()
	a.fileReadsMu.Lock()
	tracker := a.filesReadThisTurn
	a.fileReadsMu.Unlock()
	if tracker != nil {
		hasReadThisTurn = tracker.hasReadThisTurn(path, workspaceRoot)
	}

	if !hasReadThisTurn {
		return agenterrors.Wrapf(
			ErrWriteStale, "must call read_file(%q) first; the file may be stale and overwriting blindly is rarely correct",
			path,
		)
	}

	// File was read this turn — but if it's been modified within the
	// freshness window by something OTHER than the prior read, refuse.
	if tracker != nil {
		readAt, ok := tracker.getReadTime(path, workspaceRoot)
		if ok && info.ModTime().After(readAt) &&
			time.Since(info.ModTime()) < stalenessFreshnessWindow {
			return agenterrors.Wrapf(
				ErrWriteStale, "%q was modified after your last read_file call; re-read before writing",
				path,
			)
		}
	}

	return nil
}
