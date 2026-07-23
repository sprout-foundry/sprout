package agent

import (
	"fmt"
	"os"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// RedactedContentMarker aliases history.RedactedContentMarker so existing call
// sites within this package keep working. The canonical constant lives in
// pkg/history (the lower-level package, which pkg/agent already imports) so the
// two packages can never drift apart.
const RedactedContentMarker = history.RedactedContentMarker

// ChangeTracker manages change tracking for the agent workflow
type ChangeTracker struct {
	// mu protects revisionID, instructions, changes, baseRevisionRecorded,
	// committedChangeCount, and checkpointedChangeCount. RecordTurnCheckpointAsync
	// reads ct.changes from a goroutine while the main flow may Clear()/Reset()
	// and tool execution appends — without this mutex those collide.
	mu           sync.Mutex
	revisionID   string
	sessionID    string
	instructions string
	changes      []TrackedFileChange
	// enabled is the on/off flag for change tracking. The field is a
	// regular bool to keep struct-literal init working in tests, but
	// every concurrent read in production code MUST go through
	// IsEnabled() — that locks ct.mu so the read doesn't race with
	// Enable()/Disable() writes. The race detector caught a direct
	// `!ct.enabled` read on the hot path during
	// TestRunSeamlessPlanning_ContextCancelled.
	enabled              bool
	agent                *Agent
	baseRevisionRecorded bool
	committedChangeCount int
	// checkpointedChangeCount is len(changes) at the time of the most
	// recent turn-checkpoint capture. CollectFileChangesForCheckpoint
	// returns only changes appended since this watermark so each
	// checkpoint's manifest scopes to its own turn's writes, not the
	// cumulative session.
	checkpointedChangeCount int

	// shellCache is the long-lived baseline used by the shell-mutation
	// diff path (change_tracking_shell.go). Keyed by absolute path,
	// each entry holds the file's content (or a path-only sentinel)
	// plus stat metadata for the fast path. Built once via prime, then
	// rebased on every shell_command — content is re-read only for
	// files whose (size, mtime) actually changed. nil until first
	// shell tracking call.
	shellCache   map[string]*shellSnapshotEntry
	shellCacheMu sync.Mutex

	// shellCacheRoot is the absolute workspace path the shellCache was
	// built against. effectiveCwd() follows `cd` commands, so a shell
	// turn can target a directory outside the original workspace —
	// without tracking the baseline's root, the cache built for
	// /Users/x/dev/proj would be empty for /Users/x and the diff would
	// classify every file under home as a "create" (see the 14k-entry
	// runaway session that motivated this field). When TrackShellTurn
	// sees a workDir that differs from shellCacheRoot, it re-primes
	// silently rather than emitting spurious creates.
	shellCacheRoot string

	// autoSkipDirs is the adaptive companion to shellSnapshotSkipDirs.
	// Static skip list catches the well-known bloat (node_modules,
	// .git, …); this set catches per-user surprises: a workspace that
	// happens to contain a giant `releases/` directory, a misplaced
	// data dump, an unexpected vendor mirror — anything we don't know
	// the name of in advance. Populated at walk end by analyzing
	// per-directory file counts; consulted by the next walk so the
	// cost is paid at most once per session per fat directory.
	// Absolute paths, not bare names — we want to skip *this* `data/`
	// dir, not all directories named `data`.
	autoSkipDirs map[string]bool

	// shellWalkEnabled gates the per-shell_command walk. When false,
	// TrackShellTurn is a no-op and direct-tool hooks are the only
	// source of change records. Set from configuration.ChangeTracking.
	// Defaults to true; nil is treated as true.
	shellWalkEnabled bool

	// Per-tracker overrides for the shell-walk budgets / thresholds.
	// Zero values mean "use the package-level defaults"; positive
	// values replace them for this tracker only. Configured by
	// EnableChangeTracking via configuration.ChangeTracking.Resolve().
	shellMaxFiles                   int
	shellMaxTotalBytes              int64
	shellMaxDuration                time.Duration
	shellAutoSkipFileCountThreshold int
}

// TrackedFileChange represents a file change made during agent execution
type TrackedFileChange struct {
	FilePath     string    `json:"file_path"`
	OriginalCode string    `json:"original_code"`
	NewCode      string    `json:"new_code"`
	Operation    string    `json:"operation"` // "write", "edit", "create", "delete", "bulk"
	Timestamp    time.Time `json:"timestamp"`
	ToolCall     string    `json:"tool_call"` // Which tool was used

	// Source attributes a change to its origin. Empty for direct
	// primary-agent edits; "subagent:<persona>" for changes merged
	// in from a subagent run. Surfaced by list_changes so the user/LLM
	// can tell which subagent touched a file. TrackedBulkItem does NOT
	// carry this (bulk entries are always shell-mutation rollups).
	Source string `json:"source,omitempty"`

	// BulkCount is set on a rollup entry produced when a single shell
	// command churns more than the bulk threshold — typical of
	// `make build`, `npm ci`, `cargo build`, or `git checkout .`.
	// FilePath then names the directory or command label (workspace-
	// relative, trailing "/") and Operation is "bulk". When zero, the
	// entry represents a normal single-file change. SP-061-1.
	BulkCount int `json:"bulk_count,omitempty"`

	// BulkItems carries the per-file recovery payload for bulk entries.
	// Populated when the bulk fits inside the walk's content budget
	// (~32 MiB). When present, recover_file can match a specific path
	// inside the bulk and recover_bulk can restore the whole set.
	// Empty when the bulk row is count-only (build-output rollup that
	// the user said is cheap to regenerate, or destructive bulk that
	// blew through the memory cap).
	BulkItems []TrackedBulkItem `json:"bulk_items,omitempty"`
}

// TrackedBulkItem is the per-file payload packed inside a bulk
// TrackedFileChange. Shape mirrors TrackedFileChange's recoverable
// fields so the recovery helpers (`isRecoverableOriginal`,
// `restoreFile`) can be reused without translation.
type TrackedBulkItem struct {
	FilePath     string `json:"file_path"`
	OriginalCode string `json:"original_code"`
	NewCode      string `json:"new_code"`
	Operation    string `json:"operation"` // "create" | "edit" | "delete"
}

// NewChangeTracker creates a new change tracker for an agent session
func NewChangeTracker(agent *Agent, instructions string) *ChangeTracker {
	// Initialize history paths based on configuration
	history.InitializeHistoryPaths(nil)

	sessionID := agent.GetSessionID()
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	// Generate revision ID based on session and timestamp
	revisionID := generateRevisionID(sessionID, instructions)

	return &ChangeTracker{
		revisionID:   revisionID,
		sessionID:    sessionID,
		instructions: instructions,
		changes:      make([]TrackedFileChange, 0),
		enabled:      true,
		agent:        agent,
	}
}

// Enable enables change tracking. Holds ct.mu so the write doesn't
// race with concurrent IsEnabled() reads from background goroutines
// (e.g. RecordTurnCheckpointAsync).
func (ct *ChangeTracker) Enable() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.enabled = true
}

// Disable disables change tracking. Same lock discipline as Enable.
func (ct *ChangeTracker) Disable() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.enabled = false
}

// IsEnabled returns whether change tracking is enabled. Production
// code must call this instead of reading ct.enabled directly so
// concurrent Enable()/Disable() calls don't race the read.
func (ct *ChangeTracker) IsEnabled() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.enabled
}

// GetRevisionID returns the current revision ID
func (ct *ChangeTracker) GetRevisionID() string {
	return ct.revisionID
}

// TrackFileWrite tracks a write operation (WriteFile tool)
func (ct *ChangeTracker) TrackFileWrite(filePath string, newContent string) error {
	if !ct.IsEnabled() {
		return nil
	}

	// H3: Normalize to absolute at track time so the stored FilePath
	// is independent of the process's CWD. If the agent later does a
	// `cd` via a shell command, recovery (which resolves via
	// filepath.Abs against the CURRENT CWD) still points to the
	// correct location, and dedup in resolveRecoveryTarget compares
	// consistently. See resolveAbsPath for the resolution strategy.
	filePath = ct.resolveAbsPath(filePath)

	// Get original content (if file exists)
	originalContent := ""
	if _, err := os.Stat(filePath); err == nil {
		if content, readErr := os.ReadFile(filePath); readErr == nil {
			originalContent = string(content)
		}
	}

	// Redact content if file is outside the workspace root
	if ct.isOutsideWorkspace(filePath) {
		originalContent = RedactedContentMarker
		newContent = RedactedContentMarker
	}

	// Record the change
	change := TrackedFileChange{
		FilePath:     filePath,
		OriginalCode: originalContent,
		NewCode:      newContent,
		Operation:    determineWriteOperation(originalContent, newContent),
		Timestamp:    time.Now(),
		ToolCall:     "WriteFile",
	}

	ct.mu.Lock()
	ct.changes = append(ct.changes, change)
	ct.mu.Unlock()
	return nil
}

// TrackFileEdit tracks an edit operation (EditFile tool)
func (ct *ChangeTracker) TrackFileEdit(filePath string, originalContent string, newContent string) error {
	if !ct.IsEnabled() {
		return nil
	}

	// H3: Normalize to absolute at track time. See TrackFileWrite.
	filePath = ct.resolveAbsPath(filePath)

	// Redact content if file is outside the workspace root
	if ct.isOutsideWorkspace(filePath) {
		originalContent = RedactedContentMarker
		newContent = RedactedContentMarker
	}

	change := TrackedFileChange{
		FilePath:     filePath,
		OriginalCode: originalContent,
		NewCode:      newContent,
		Operation:    "edit",
		Timestamp:    time.Now(),
		ToolCall:     "EditFile",
	}

	ct.mu.Lock()
	ct.changes = append(ct.changes, change)
	ct.mu.Unlock()
	return nil
}

// appendChange appends a single tracked change under ct.mu. The caller
// may NOT already hold ct.mu — this method acquires it. This is the
// single safe entry point for appending to ct.changes from the
// shell-mutation pipeline (RecordShellMutations / appendShellMutation
// / appendBulkRollup / appendDestructiveBulkRollup), which runs
// concurrently with the turn-checkpoint goroutine that reads
// ct.changes via CollectFileChangesForCheckpoint. Holding ct.mu only
// around the slice mutation (NOT around event publishing or other
// potentially-blocking calls) keeps the critical section tight and
// avoids lock-ordering hazards.
func (ct *ChangeTracker) appendChange(change TrackedFileChange) {
	ct.mu.Lock()
	ct.changes = append(ct.changes, change)
	ct.mu.Unlock()
}

// Commit commits all tracked changes to the change tracker
func (ct *ChangeTracker) Commit(llmResponse string, conversation []api.Message) error {
	if !ct.IsEnabled() {
		return nil
	}
	ct.mu.Lock()
	if len(ct.changes) == 0 {
		ct.mu.Unlock()
		return nil
	}
	if ct.committedChangeCount >= len(ct.changes) {
		ct.mu.Unlock()
		return nil
	}

	// Convert agent_api.Message to history.APIMessage for storage
	historyConversation := convertToHistoryMessages(conversation)

	if !ct.baseRevisionRecorded {
		// Record base revision with conversation
		revisionID, err := history.RecordBaseRevision(ct.revisionID, ct.instructions, llmResponse, historyConversation)
		if err != nil {
			ct.mu.Unlock()
			return agenterrors.Wrap(err, "failed to record base revision")
		}

		// Update our revision ID to match what was actually recorded
		ct.revisionID = revisionID
		ct.baseRevisionRecorded = true
	}

	// Record each file change. Advance committedChangeCount after each
	// SUCCESSFUL RecordChangeWithDetails so a mid-loop failure (disk
	// full, permission error, …) doesn't leave the counter stale — the
	// next Commit call must resume from the change that actually
	// failed, not re-record the ones that already succeeded. The lock
	// is held for the whole loop, so the increment is safe.
	for ct.committedChangeCount < len(ct.changes) {
		change := ct.changes[ct.committedChangeCount]
		description := fmt.Sprintf("%s via %s", change.Operation, change.ToolCall)
		note := fmt.Sprintf("Agent session: %s", ct.sessionID)

		err := history.RecordChangeWithDetails(
			ct.revisionID,
			change.FilePath,
			change.OriginalCode,
			change.NewCode,
			description,
			note,
			ct.instructions,    // originalPrompt
			llmResponse,        // llmMessage
			ct.getAgentModel(), // editingModel
		)
		if err != nil {
			ct.mu.Unlock()
			return agenterrors.Wrap(err, fmt.Sprintf("failed to record change for %s", change.FilePath))
		}
		ct.committedChangeCount++
	}

	// Snapshot the changes for the sweep. The sweep (below) invokes git
	// subprocess calls which may block — holding ct.mu during that would
	// risk deadlocking with the turn-checkpoint goroutine that reads
	// ct.changes. Copy under the lock, then release.
	changesSnapshot := make([]TrackedFileChange, len(ct.changes))
	copy(changesSnapshot, ct.changes)
	ct.mu.Unlock()

	// SP-077 Phase 2: sweep committed changes and mark any whose NewCode
	// now matches git HEAD as "superseded". These changes have been
	// committed to version control — they are no longer recoverable agent
	// edits. This prevents a stale snapshot from a prior session from
	// being reverted and silently undoing committed work.
	//
	// The sweep is best-effort: a git error or non-repo workspace just
	// means the sweep is skipped (the changes remain "active"). This is
	// safe — the Phase 1 filter already prevents NEW git-sourced deltas
	// from being recorded, and the existing IsRevertSafe guard catches
	// committed content on the write-back path.
	ct.sweepCommittedSnapshots(changesSnapshot)

	return nil
}

// sweepCommittedSnapshots marks committed snapshots as "superseded"
// when their NewCode matches git HEAD. A snapshot whose content is now
// committed to version control is no longer a recoverable agent edit —
// reverting it would undo committed work (SP-077 Phase 2).
//
// Takes a pre-snapshotted copy of ct.changes (so the caller can release
// ct.mu before invoking the git subprocess calls inside). The snapshot
// should be taken under ct.mu to avoid a race with concurrent
// Clear()/Reset().
//
// Best-effort: a git error or non-repo workspace skips the sweep
// entirely (changes remain "active"). Safe to call after every Commit;
// already-superseded entries are not re-marked (idempotent).
func (ct *ChangeTracker) sweepCommittedSnapshots(changes []TrackedFileChange) {
	if ct.agent == nil {
		return
	}
	workDir := ct.agent.workspaceRoot
	if workDir == "" {
		return
	}
	committed, err := git.CommittedFilePaths(workDir)
	if err != nil || committed == nil {
		return
	}
	for _, ch := range changes {
		// Only sweep changes with recoverable content and non-empty
		// NewCode (the state the agent produced). Deletes and creates
		// without content have nothing to compare.
		if ch.NewCode == "" || ch.NewCode == RedactedContentMarker {
			continue
		}
		if !committed[ch.FilePath] {
			continue
		}
		// Recompute the file revision hash (same formula as
		// history.RecordChangeWithDetails) so we can target the exact
		// on-disk metadata record.
		hash := utils.GenerateFileRevisionHash(ch.FilePath, ch.NewCode)
		if markErr := history.MarkChangeSuperseded(hash); markErr != nil {
			ct.logf("SP-077: failed to mark %s as superseded: %v", ch.FilePath, markErr)
		}
	}
}

// GetTrackedFiles returns a list of files that have been modified
func (ct *ChangeTracker) GetTrackedFiles() []string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	files := make([]string, len(ct.changes))
	for i, change := range ct.changes {
		files[i] = change.FilePath
	}
	return files
}

// GetChangeCount returns the number of tracked changes
func (ct *ChangeTracker) GetChangeCount() int {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return len(ct.changes)
}

// GetChanges returns a copy of the tracked changes
func (ct *ChangeTracker) GetChanges() []TrackedFileChange {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	changesCopy := make([]TrackedFileChange, len(ct.changes))
	copy(changesCopy, ct.changes)
	return changesCopy
}

// MergeChild appends a subagent's tracked changes into this (parent)
// tracker so list_changes / recover_file / revert_my_changes see
// subagent edits too. Each merged entry is tagged with Source so
// list_changes can attribute it; the shell-snapshot cache is
// re-baselined per path so the next shell-command walk doesn't
// record a duplicate entry for the same file.
//
// This closes the SP-059 Phase 2c gap where subagent edits were
// captured in a child tracker but never surfaced to the parent's
// user-facing change tools.
//
// Safe to call when tracking is disabled (no-op). The input slice is
// copied; nil/empty input is a no-op.
func (ct *ChangeTracker) MergeChild(changes []TrackedFileChange, source string) {
	if ct == nil || !ct.IsEnabled() || len(changes) == 0 {
		return
	}
	// Defensive copy + tag each entry under the lock so a concurrent
	// Clear()/Reset() can't race the append.
	merged := make([]TrackedFileChange, len(changes))
	for i, ch := range changes {
		merged[i] = ch
		merged[i].Source = source
	}
	ct.mu.Lock()
	ct.changes = append(ct.changes, merged...)
	ct.mu.Unlock()
	// Re-baseline the shell cache for each touched path so the next
	// TrackShellTurn walk doesn't re-discover these writes as
	// "stat mismatch" edits (duplicates of what we just recorded).
	for _, ch := range merged {
		ct.SyncShellCacheForPath(ch.FilePath)
	}
}

// Clear clears all tracked changes (but keeps the tracker enabled).
// Also resets the shell-snapshot cache so a subsequent
// EnableChangeTracking / PrimeShellTracking call re-baselines against
// current disk state — otherwise a stale cache would attribute
// post-Clear shell mutations to "the workspace as it looked at session
// start", which is wrong after a Reset. The autoSkipDirs adaptive set
// is preserved across Clear (it's an optimization, not state about
// the user's changes); a Reset that wants to re-learn from scratch
// can null it manually.
func (ct *ChangeTracker) Clear() {
	ct.mu.Lock()
	ct.clearLocked()
	ct.mu.Unlock()
}

// clearLocked is the body of Clear, callable from sites that already hold
// ct.mu (e.g., Reset). Preconditions: ct.mu is held by the caller.
func (ct *ChangeTracker) clearLocked() {
	ct.changes = ct.changes[:0]
	ct.baseRevisionRecorded = false
	ct.committedChangeCount = 0
	ct.checkpointedChangeCount = 0
	ct.shellCacheMu.Lock()
	ct.shellCache = nil
	ct.shellCacheMu.Unlock()
}

// Reset resets the change tracker with a new revision ID and instructions
func (ct *ChangeTracker) Reset(instructions string) {
	revID := generateRevisionID(ct.sessionID, instructions)
	ct.mu.Lock()
	ct.instructions = instructions
	ct.revisionID = revID
	ct.clearLocked()
	ct.mu.Unlock()
}

// Helper functions

// Helper functions
