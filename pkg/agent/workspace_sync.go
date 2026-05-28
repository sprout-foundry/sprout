package agent

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	FilePath     string                  `json:"file_path"`
	Action       ReconciliationActionType `json:"action"`
	ContainerSeq int64                   `json:"container_seq"`
	BrowserSeq   int64                   `json:"browser_seq"`
}

// ReconcileSeqNumbers compares browser-supplied per-file sequence numbers
// against the container's stored metadata and returns a reconciliation plan.
// The browser sends {file_path: browser_seq}; for each file, we compare
// the browser's seq with the container's seq to determine the action.
//
// Rules:
//   - browser_seq == container_seq → sync_ok
//   - browser_seq == last_synced_container AND container_seq > browser_seq → container_ahead
//   - browser_seq > last_synced_browser → browser_ahead (unsynced browser edits)
//   - both sides diverged → diverged
func ReconcileSeqNumbers(ag *Agent, browserSeqs map[string]int64) ([]ReconciliationActionResult, error) {
	if ag == nil {
		return nil, fmt.Errorf("agent is nil")
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

// normalizeFilePath resolves a path to its absolute form so that the
// staleness tracker's map keys match regardless of whether the LLM passes
// a relative path ("foo/bar.go") or an absolute one ("/home/user/foo/bar.go").
// Uses workspaceRoot to resolve relative paths. Falls back to filepath.Abs
// if workspaceRoot is empty.
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
// during the current turn. Used by the staleness rule's "must read before
// write this turn" check. All paths are normalized to absolute form so
// that relative and absolute references to the same file share one key.
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
	a.filesReadThisTurn.recordRead(path, a.currentWorkspaceRoot())
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
	workspaceRoot := a.currentWorkspaceRoot()
	a.fileReadsMu.Lock()
	tracker := a.filesReadThisTurn
	a.fileReadsMu.Unlock()
	if tracker != nil {
		hasReadThisTurn = tracker.hasReadThisTurn(path, workspaceRoot)
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
		readAt, ok := tracker.getReadTime(path, workspaceRoot)
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

// SyncOp represents a single file operation sent from the browser to the
// container as part of the workspace sync protocol (SP-046 §2).
// The browser queues these in OPFS and flushes them via HTTP POST when
// the WebSocket is up.
type SyncOp struct {
	OpType     string `json:"op_type"`     // "write", "delete", or "rename"
	Path       string `json:"path"`        // Target file path (relative to workspace root)
	Content    string `json:"content"`     // For write ops: the file content
	NewPath    string `json:"new_path"`    // For rename ops: the destination path
	BrowserSeq int64  `json:"browser_seq"` // Monotonically increasing browser-side seq number
	Timestamp  int64  `json:"timestamp"`   // Unix milliseconds when the op was created
}

// SyncOpResult is the server response to a SyncOp application.
type SyncOpResult struct {
	Accepted     bool   `json:"accepted"`      // Whether the op was applied
	ConflictPath string `json:"conflict_path"` // Set if there's a container-side conflict (path to .theirs file)
	ContainerSeq int64  `json:"container_seq"` // Current container sequence after applying
	Error        string `json:"error,omitempty"` // Error message if not accepted
}

// resolveWorkspacePath joins workspaceRoot with relPath, resolves symlinks,
// and verifies the result is within workspaceRoot. Returns an error if
// path traversal is attempted.
func resolveWorkspacePath(workspaceRoot, relPath string) (string, error) {
	if relPath == "" {
		return "", fmt.Errorf("relative path must not be empty")
	}

	absRoot, err := filepath.Abs(workspaceRoot)
	if err != nil {
		return "", fmt.Errorf("resolve workspace root: %w", err)
	}

	// Resolve symlinks in workspace root for consistent path resolution and
	// comparison. Without this, on platforms where /tmp → /private/tmp,
	// paths joined against absRoot would fail the Rel() check against
	// the symlink-resolved path.
	resolvedRoot, err := filepath.EvalSymlinks(absRoot)
	if err != nil {
		resolvedRoot = absRoot
	}

	resolved := filepath.Join(resolvedRoot, relPath)

	// Try to resolve symlinks in the full path.
	evaluated, evalErr := filepath.EvalSymlinks(resolved)
	if evalErr != nil {
		if os.IsNotExist(evalErr) {
			// File doesn't exist yet; resolve parent directory symlinks instead.
			// NOTE: when EvalSymlinks fails it returns an empty string, so we
			// MUST use `resolved` (the pre-evaluation path) for parent/base ops.
			parent := filepath.Dir(resolved)
			resolvedParent, perr := filepath.EvalSymlinks(parent)
			if perr == nil {
				resolved = filepath.Join(resolvedParent, filepath.Base(resolved))
			} else {
				// Parent also doesn't exist (e.g. deeply nested new file).
				// Fall back to the original joined path — it's still safe because
				// resolvedRoot was resolved above and the join uses relPath directly.
				resolved = filepath.Join(resolvedRoot, relPath)
			}
		} else {
			return "", fmt.Errorf("resolve path: %w", evalErr)
		}
	} else {
		resolved = evaluated
	}

	// Verify the resolved path is within the workspace root
	rel, err := filepath.Rel(resolvedRoot, resolved)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("path traversal attempted: %q is outside workspace root %q", resolved, resolvedRoot)
	}

	return resolved, nil
}

// applySyncMu serializes all ApplySyncOp and ApplySyncOpBatch calls to
// prevent TOCTOU races between the stat/check/apply phases.
var applySyncMu sync.Mutex

// ApplySyncOp applies a single SyncOp to the workspace filesystem.
// It validates the operation, checks for conflicts with container-side
// changes, applies the change, and updates the file metadata.
func (a *Agent) ApplySyncOp(op SyncOp, workspaceRoot string) SyncOpResult {
	if a == nil {
		return SyncOpResult{
			Accepted: false,
			Error:    "agent is nil",
		}
	}
	applySyncMu.Lock()
	defer applySyncMu.Unlock()
	return a.applySyncOpInternal(op, workspaceRoot)
}

// applySyncOpInternal is the unlocked core of ApplySyncOp. It contains all
// the validation, conflict detection, and filesystem work. Callers are
// responsible for holding applySyncMu (ApplySyncOp does this individually,
// ApplySyncOpBatch holds it across the whole batch).
func (a *Agent) applySyncOpInternal(op SyncOp, workspaceRoot string) SyncOpResult {

	// 1. Validate op type
	switch op.OpType {
	case "write", "delete", "rename":
	default:
		return SyncOpResult{
			Accepted: false,
			Error:    fmt.Sprintf("invalid op_type %q: must be write, delete, or rename", op.OpType),
		}
	}

	// 2. Validate path is non-empty
	if op.Path == "" {
		return SyncOpResult{
			Accepted: false,
			Error:    "path must not be empty",
		}
	}

	// 3. Resolve file path with traversal protection
	resolvedPath, err := resolveWorkspacePath(workspaceRoot, op.Path)
	if err != nil {
		return SyncOpResult{
			Accepted: false,
			Error:    err.Error(),
		}
	}

	// 4. Get current metadata for the path
	md, hasMetadata := a.GetFileMetadata(op.Path)

	// 5. Conflict detection: if container has unsynced writes, produce a .theirs file
	if hasMetadata && md.ContainerSeq > md.LastSyncedContainer {
		// Container has writes the browser hasn't acknowledged — potential conflict.
		// Save current container content to <path>.theirs
		themsPath := resolvedPath + ".theirs"
		currentContent, readErr := os.ReadFile(resolvedPath)
		if readErr == nil {
			writeErr := os.WriteFile(themsPath, currentContent, 0644)
			if writeErr != nil {
				return SyncOpResult{
					Accepted:     false,
					ConflictPath: op.Path + ".theirs",
					Error:        fmt.Sprintf("failed to write .theirs file: %v", writeErr),
				}
			}
		}
		return SyncOpResult{
			Accepted:     false,
			ConflictPath: op.Path + ".theirs",
			ContainerSeq: md.ContainerSeq,
			Error:        fmt.Sprintf("container has unsynced writes: container_seq=%d > last_synced_container=%d", md.ContainerSeq, md.LastSyncedContainer),
		}
	}

	// 6. Apply the operation
	switch op.OpType {
	case "write":
		parentDir := filepath.Dir(resolvedPath)
		if mkdirErr := os.MkdirAll(parentDir, 0755); mkdirErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to create parent directories: %v", mkdirErr),
			}
		}
		if writeErr := os.WriteFile(resolvedPath, []byte(op.Content), 0644); writeErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to write file: %v", writeErr),
			}
		}

	case "delete":
		if removeErr := os.Remove(resolvedPath); removeErr != nil && !os.IsNotExist(removeErr) {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to delete file: %v", removeErr),
			}
		}
		// Remove metadata for deleted file
		a.fileMetadata.set(op.Path, WorkspaceFileMetadata{})
		// Return early — do NOT fall through to the shared metadata update
		// block below. The file is gone; its metadata should be cleared and
		// ContainerSeq must be 0 to signal that the path no longer exists.
		return SyncOpResult{
			Accepted:     true,
			ContainerSeq: 0,
		}

	case "rename":
		if op.NewPath == "" {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        "new_path must not be empty for rename operation",
			}
		}
		resolvedNewPath, err := resolveWorkspacePath(workspaceRoot, op.NewPath)
		if err != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        err.Error(),
			}
		}
		// Create parent directories for destination
		parentDir := filepath.Dir(resolvedNewPath)
		if mkdirErr := os.MkdirAll(parentDir, 0755); mkdirErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to create parent directories for rename target: %v", mkdirErr),
			}
		}
		if renameErr := os.Rename(resolvedPath, resolvedNewPath); renameErr != nil {
			return SyncOpResult{
				Accepted:     false,
				ContainerSeq: md.ContainerSeq,
				Error:        fmt.Sprintf("failed to rename file: %v", renameErr),
			}
		}
		// Move metadata from old path to new path
		a.fileMetadata.set(op.Path, WorkspaceFileMetadata{}) // clear old
		op.Path = op.NewPath                                // update key for subsequent metadata update
	}

	// 7. Update metadata
	md.BrowserSeq = op.BrowserSeq
	md.LastSyncedBrowser = op.BrowserSeq
	md.ContainerSeq++
	md.ModifiedAt = time.Now()
	a.SetFileMetadata(op.Path, md)

	return SyncOpResult{
		Accepted:     true,
		ContainerSeq: md.ContainerSeq,
	}
}

// ApplySyncOpBatch applies a slice of SyncOps in order, collecting results.
// Stops on the first conflict, returning Accepted=false for remaining ops.
func (a *Agent) ApplySyncOpBatch(ops []SyncOp, workspaceRoot string) []SyncOpResult {
	if a == nil {
		results := make([]SyncOpResult, len(ops))
		for i := range results {
			results[i] = SyncOpResult{
				Accepted: false,
				Error:    "agent is nil",
			}
		}
		return results
	}
	applySyncMu.Lock()
	defer applySyncMu.Unlock()

	results := make([]SyncOpResult, 0, len(ops))

	for _, op := range ops {
		result := a.applySyncOpInternal(op, workspaceRoot)
		results = append(results, result)
		if !result.Accepted {
			// Remaining ops get Accepted=false
			for i := len(ops) - len(results); i > 0; i-- {
				results = append(results, SyncOpResult{
					Accepted: false,
					Error:    "skipped due to earlier conflict in batch",
				})
			}
			break
		}
	}

	return results
}

// GetSyncStatus returns a map of path → WorkspaceFileMetadata for all
// currently tracked files in the workspace metadata store.
func (a *Agent) GetSyncStatus() map[string]WorkspaceFileMetadata {
	if a == nil {
		return nil
	}

	a.fileReadsMu.Lock()
	store := a.fileMetadata
	a.fileReadsMu.Unlock()

	if store == nil {
		return nil
	}

	store.mu.RLock()
	defer store.mu.RUnlock()

	result := make(map[string]WorkspaceFileMetadata, len(store.m))
	for path, md := range store.m {
		result[path] = md
	}

	return result
}
