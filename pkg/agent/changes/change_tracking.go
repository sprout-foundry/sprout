package changes

import (
	"fmt"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/git"
	"github.com/sprout-foundry/sprout/pkg/history"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// RedactedContentMarker aliases history.RedactedContentMarker so existing call
// sites within this package keep working.
const RedactedContentMarker = history.RedactedContentMarker

// Bounds on the in-memory session change buffer. TrackedFileChange keeps a
// full copy of each file's before/after content, and the slice was
// append-only — it only reset at session end — so a long session with many
// edits grew without bound. These caps evict the OLDEST entries once the
// buffer is over budget, while always retaining the earliest entry per path
// (session_start recovery reads it) and the most recent entries.
var (
	// maxTrackedChanges caps the number of entries held in the buffer.
	maxTrackedChanges = 5000
	// maxTrackedBytes caps the total before+after content bytes held.
	maxTrackedBytes = int64(64 * 1024 * 1024) // 64 MiB
)

// SetTrackedChangeCaps overrides the buffer caps (tests / tuning).
func SetTrackedChangeCaps(maxChanges int, maxBytes int64) {
	if maxChanges > 0 {
		maxTrackedChanges = maxChanges
	}
	if maxBytes > 0 {
		maxTrackedBytes = maxBytes
	}
}

// trimChangesLocked enforces the buffer caps by evicting OLDEST-first until
// the buffer is back under both the count and byte caps. Earlier I protected
// each path's earliest entry to preserve session_start semantics, but that
// made the count cap a no-op whenever every edit touched a distinct file
// (every entry was "earliest" and un-evictable). The bound has to bite, so
// the newest entries win and the oldest are dropped; the persisted history
// store remains the source of truth for anything aged out of the live buffer.
// Caller must hold ct.mu.
func (ct *ChangeTracker) trimChangesLocked() {
	if maxTrackedChanges <= 0 && maxTrackedBytes <= 0 {
		return
	}
	overCount := maxTrackedChanges > 0 && len(ct.changes) > maxTrackedChanges
	overBytes := maxTrackedBytes > 0 && trackedContentBytes(ct.changes) > maxTrackedBytes
	if !overCount && !overBytes {
		return
	}

	// Keep a suffix of the most recent entries that fits both caps. Walk from
	// the newest end backwards, accumulating until a cap would be exceeded.
	start := len(ct.changes)
	var bytes int64
	for i := len(ct.changes) - 1; i >= 0; i-- {
		add := entryBytes(ct.changes[i])
		if maxTrackedChanges > 0 && len(ct.changes)-i > maxTrackedChanges {
			break
		}
		if maxTrackedBytes > 0 && bytes+add > maxTrackedBytes {
			break
		}
		bytes += add
		start = i
	}
	if start == 0 {
		return
	}
	kept := make([]TrackedFileChange, len(ct.changes)-start)
	copy(kept, ct.changes[start:])
	// Zero the evicted prefix so its content strings are GC-reachable no more.
	for i := 0; i < start; i++ {
		ct.changes[i] = TrackedFileChange{}
	}
	ct.changes = kept
}

// entryBytes is the approximate retained size of one change.
func entryBytes(ch TrackedFileChange) int64 {
	n := int64(len(ch.FilePath) + len(ch.OriginalCode) + len(ch.NewCode))
	for _, item := range ch.BulkItems {
		n += int64(len(item.FilePath) + len(item.OriginalCode) + len(item.NewCode))
	}
	return n
}

// trackedContentBytes sums the retained size of a change slice.
func trackedContentBytes(changes []TrackedFileChange) int64 {
	var n int64
	for _, ch := range changes {
		n += entryBytes(ch)
	}
	return n
}

// AgentView is the seam between the change tracker and its host agent
// (SP-141 phase 2). The tracker lives in pkg/agent/changes; the agent
// lives in pkg/agent, and the import arrow is one-way (pkg/agent →
// changes), so the tracker sees only this narrow interface. pkg/agent
// adapts *Agent to it via the unexported changesAgentView adapter —
// the same shape phase 1 used for workflow.LoopAgent. Every method is
// nil-tolerant on the implementation side where the old direct-field
// access was nil-checked.
type AgentView interface {
	// GetSessionID returns the session identifier ("" when unset).
	GetSessionID() string
	// GetModel returns the model identifier ("unknown" fallback is the
	// tracker's, not the agent's).
	GetModel() string
	// GetWorkspaceRoot returns the logical workspace root ("" when unset).
	GetWorkspaceRoot() string
	// GenerateResponse runs an LLM completion (AI summary path).
	GenerateResponse(messages []api.Message) (string, error)
	// DebugLogger returns the agent logger or nil; the tracker logs
	// debug-level shell-snapshot messages through it when non-nil.
	DebugLogger() DebugLogger
	// PublishFileChange emits a file_changed event for tracker-detected
	// mutations. Implementations drop the event when no bus is wired.
	PublishFileChange(filePath, action, content string)
	// PublishRawFileChanged publishes a file_changed event WITHOUT the
	// agent's event-metadata decoration (bulk rollups and destructive
	// rollups publish command labels / directory paths, not real files,
	// and historically bypassed decorateEventPayload via a raw
	// eventBus.Publish). Implementations must preserve that behavior.
	PublishRawFileChanged(eventType string, payload interface{})
}

// DebugLogger is the logger facet the tracker needs from the agent
// logger (mirrors *agent.AgentLogger.Debug).
type DebugLogger interface {
	Debug(format string, args ...interface{})
}

// ChangeTracker manages change tracking for the agent workflow
type ChangeTracker struct {
	// mu protects revisionID, instructions, changes, baseRevisionRecorded,
	// committedChangeCount, and checkpointedChangeCount.
	mu           sync.Mutex
	commitMu     sync.Mutex
	revisionID   string
	sessionID    string
	instructions string
	changes      []TrackedFileChange
	// enabled is the on/off flag for change tracking. Every concurrent read
	// in production code MUST go through IsEnabled() to avoid races.
	enabled              bool
	view                 AgentView
	baseRevisionRecorded bool
	committedChangeCount int
	// checkpointedChangeCount is len(changes) at the most recent turn-checkpoint capture.
	checkpointedChangeCount int

	// shellCache is the long-lived baseline for the shell-mutation diff path.
	shellCache   map[string]*shellSnapshotEntry
	shellCacheMu sync.Mutex

	// shellCacheRoot tracks the workspace path the shellCache was built against.
	shellCacheRoot string

	// autoSkipDirs is the adaptive companion to shellSnapshotSkipDirs.
	autoSkipDirs map[string]bool

	// shellWalkEnabled gates the per-shell_command walk.
	shellWalkEnabled bool

	// Per-tracker overrides for the shell-walk budgets / thresholds.
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
	ToolCall     string    `json:"tool_call"`

	// Source attributes a change to its origin. Empty for direct
	// primary-agent edits; "subagent:<persona>" for subagent changes.
	Source string `json:"source,omitempty"`

	// BulkCount is set on a rollup entry when a single shell command
	// churns more than the bulk threshold. FilePath names the directory
	// or command label and Operation is "bulk".
	BulkCount int `json:"bulk_count,omitempty"`

	// BulkItems carries the per-file recovery payload for bulk entries.
	BulkItems []TrackedBulkItem `json:"bulk_items,omitempty"`
}

// TrackedBulkItem is the per-file payload packed inside a bulk TrackedFileChange.
type TrackedBulkItem struct {
	FilePath     string `json:"file_path"`
	OriginalCode string `json:"original_code"`
	NewCode      string `json:"new_code"`
	Operation    string `json:"operation"` // "create" | "edit" | "delete"
}

// NewChangeTracker creates a new change tracker for an agent session.
// view may be nil (bare trackers in tests); every view access is
// nil-guarded exactly where the old ct.agent accesses were.
func NewChangeTracker(view AgentView, instructions string) *ChangeTracker {
	history.InitializeHistoryPaths(nil)

	sessionID := ""
	if view != nil {
		sessionID = view.GetSessionID()
	}
	if sessionID == "" {
		sessionID = generateSessionID()
	}

	revisionID := generateRevisionID(sessionID, instructions)

	return &ChangeTracker{
		revisionID:   revisionID,
		sessionID:    sessionID,
		instructions: instructions,
		changes:      make([]TrackedFileChange, 0),
		enabled:      true,
		view:         view,
	}
}

// Enable enables change tracking.
func (ct *ChangeTracker) Enable() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.enabled = true
}

// Disable disables change tracking.
func (ct *ChangeTracker) Disable() {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.enabled = false
}

// IsEnabled returns whether change tracking is enabled. Production
// code must call this instead of reading ct.enabled directly.
func (ct *ChangeTracker) IsEnabled() bool {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.enabled
}

// GetRevisionID returns the current revision ID
func (ct *ChangeTracker) GetRevisionID() string {
	return ct.revisionID
}

// TrackFileWrite tracks a write operation (WriteFile tool).
// originalContent is the file's pre-write state, captured by the
// handler before the write. Empty means the file did not exist (a
// create). Never re-read filePath here: the write has already happened,
// so a re-read returns new content and recovery becomes a no-op.
func (ct *ChangeTracker) TrackFileWrite(filePath string, originalContent string, newContent string) error {
	return ct.TrackFileWriteState(filePath, originalContent, newContent, originalContent != "")
}

// TrackFileWriteState is TrackFileWrite for callers that know whether the
// file existed before the write. An empty original alone can't distinguish
// a new file from an existing empty one (.gitkeep, __init__.py); recording
// the latter as a create made revert delete it.
func (ct *ChangeTracker) TrackFileWriteState(filePath string, originalContent string, newContent string, existed bool) error {
	if !ct.IsEnabled() {
		return nil
	}
	// Normalize to absolute at track time so stored FilePath is
	// independent of the process's CWD.
	filePath = ct.resolveAbsPath(filePath)

	// Redact content if file is outside the workspace root
	if ct.IsOutsideWorkspace(filePath) {
		originalContent = RedactedContentMarker
		newContent = RedactedContentMarker
	}

	// Record the change
	change := TrackedFileChange{
		FilePath:     filePath,
		OriginalCode: originalContent,
		NewCode:      newContent,
		Operation:    determineWriteOperationState(originalContent, newContent, existed),
		Timestamp:    time.Now(),
		ToolCall:     "WriteFile",
	}

	ct.mu.Lock()
	ct.changes = append(ct.changes, change)
	ct.trimChangesLocked()
	ct.mu.Unlock()
	return nil
}

// TrackFileEdit tracks an edit operation (EditFile tool)
func (ct *ChangeTracker) TrackFileEdit(filePath string, originalContent string, newContent string) error {
	if !ct.IsEnabled() {
		return nil
	}
	filePath = ct.resolveAbsPath(filePath)

	// Redact content if file is outside the workspace root
	if ct.IsOutsideWorkspace(filePath) {
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
	ct.trimChangesLocked()
	ct.mu.Unlock()
	return nil
}

// appendChange appends a single tracked change under ct.mu.
func (ct *ChangeTracker) appendChange(change TrackedFileChange) {
	ct.mu.Lock()
	ct.changes = append(ct.changes, change)
	ct.trimChangesLocked()
	ct.mu.Unlock()
}

// Commit commits all tracked changes to the change tracker
func (ct *ChangeTracker) Commit(llmResponse string, conversation []api.Message) error {
	if !ct.IsEnabled() {
		return nil
	}

	// commitMu serializes commits (turn end + session cleanup can race
	// with a WebUI-triggered commit). It lets ct.mu stay held only for
	// microsecond snapshot/counter work: history.Record* performs disk
	// I/O (base64 + two file writes per change), and holding ct.mu
	// across it stalled every concurrent GetChanges/TrackFileWrite —
	// including the WebUI changes panel — for the whole write burst.
	ct.commitMu.Lock()
	defer ct.commitMu.Unlock()

	historyConversation := convertToHistoryMessages(conversation)

	for {
		ct.mu.Lock()
		if len(ct.changes) == 0 || ct.committedChangeCount >= len(ct.changes) {
			ct.mu.Unlock()
			break
		}
		if !ct.baseRevisionRecorded {
			// RecordBaseRevision is I/O; do it outside ct.mu. The flag
			// is only set after success under ct.mu, so a concurrent
			// commit (serialized by commitMu anyway) can't duplicate it.
			revisionID := ct.revisionID
			ct.mu.Unlock()
			newRevisionID, err := history.RecordBaseRevision(revisionID, ct.instructions, llmResponse, historyConversation)
			if err != nil {
				return agenterrors.Wrap(err, "failed to record base revision")
			}
			ct.mu.Lock()
			if ct.revisionID == revisionID {
				ct.revisionID = newRevisionID
				ct.baseRevisionRecorded = true
			}
			ct.mu.Unlock()
		} else {
			ct.mu.Unlock()
		}

		// Snapshot the uncommitted slice under ct.mu, then release the
		// lock for the I/O burst. committedChangeCount advances per
		// successful record so a mid-burst failure resumes without
		// re-recording completed entries.
		ct.mu.Lock()
		start := ct.committedChangeCount
		end := len(ct.changes)
		pending := make([]TrackedFileChange, end-start)
		copy(pending, ct.changes[start:end])
		ct.mu.Unlock()

		for i, change := range pending {
			description := fmt.Sprintf("%s via %s", change.Operation, change.ToolCall)
			note := fmt.Sprintf("Agent session: %s", ct.sessionID)

			ct.mu.Lock()
			revisionID := ct.revisionID
			instructions := ct.instructions
			ct.mu.Unlock()
			err := history.RecordChangeWithDetails(
				revisionID,
				change.FilePath,
				change.OriginalCode,
				change.NewCode,
				description,
				note,
				instructions,
				llmResponse,
				ct.getAgentModel(),
			)
			if err != nil {
				return agenterrors.Wrap(err, fmt.Sprintf("failed to record change for %s", change.FilePath))
			}
			ct.mu.Lock()
			if start+i+1 > ct.committedChangeCount {
				ct.committedChangeCount = start + i + 1
			}
			ct.mu.Unlock()
		}

		// Loop: entries may have been appended while the lock was
		// released. The re-check at the top exits when caught up.
	}

	// Snapshot the changes for the sweep. Copy under the lock, then release.
	ct.mu.Lock()
	changesSnapshot := make([]TrackedFileChange, len(ct.changes))
	copy(changesSnapshot, ct.changes)
	ct.mu.Unlock()

	// Sweep committed changes and mark any whose NewCode matches git HEAD
	// as "superseded" so they aren't reverted and undo committed work.
	ct.sweepCommittedSnapshots(changesSnapshot)

	return nil
}

// sweepCommittedSnapshots marks committed snapshots as "superseded"
// when their NewCode matches git HEAD.
func (ct *ChangeTracker) sweepCommittedSnapshots(changes []TrackedFileChange) {
	if ct.view == nil {
		return
	}
	workDir := ct.view.GetWorkspaceRoot()
	if workDir == "" {
		return
	}
	committed, err := git.CommittedFilePaths(workDir)
	if err != nil || committed == nil {
		return
	}
	for _, ch := range changes {
		if ch.NewCode == "" || ch.NewCode == RedactedContentMarker {
			continue
		}
		if !committed[ch.FilePath] {
			continue
		}
		hash := utils.GenerateFileRevisionHash(ch.FilePath, ch.NewCode)
		if markErr := history.MarkChangeSuperseded(hash); markErr != nil {
			ct.logf("failed to mark %s as superseded: %v", ch.FilePath, markErr)
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
// subagent edits too. Each merged entry is tagged with Source.
func (ct *ChangeTracker) MergeChild(changes []TrackedFileChange, source string) {
	if ct == nil || !ct.IsEnabled() || len(changes) == 0 {
		return
	}
	merged := make([]TrackedFileChange, len(changes))
	for i, ch := range changes {
		merged[i] = ch
		merged[i].Source = source
	}
	ct.mu.Lock()
	ct.changes = append(ct.changes, merged...)
	ct.trimChangesLocked()
	ct.mu.Unlock()
	// Re-baseline the shell cache for each touched path to avoid duplicates.
	for _, ch := range merged {
		ct.SyncShellCacheForPath(ch.FilePath)
	}
}

// Clear clears all tracked changes (but keeps the tracker enabled).
// Also resets the shell-snapshot cache.
func (ct *ChangeTracker) Clear() {
	// commitMu first: a Commit mid-I/O-burst must finish (or fail)
	// before the buffer resets, preserving the historical ordering
	// where Clear blocked on ct.mu for the whole commit. Lock order
	// commitMu → mu → shellCacheMu is acyclic.
	ct.commitMu.Lock()
	defer ct.commitMu.Unlock()
	ct.mu.Lock()
	ct.clearLocked()
	ct.mu.Unlock()
}

// clearLocked is the body of Clear, callable from sites that already hold ct.mu.
func (ct *ChangeTracker) clearLocked() {
	// Zero the elements before truncating: changes[:0] alone keeps the
	// backing array (and every OriginalCode/NewCode string in it) GC-
	// reachable until later appends overwrite the slots — up to the
	// walk's 32 MiB content budget retained per cleared session.
	clear(ct.changes)
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
	// Same ordering as Clear: serialize against in-flight commits.
	ct.commitMu.Lock()
	defer ct.commitMu.Unlock()
	ct.mu.Lock()
	ct.instructions = instructions
	ct.revisionID = revID
	ct.clearLocked()
	ct.mu.Unlock()
}

// SetView (re)binds the tracker's host view. Construction wires the view;
// tests that build a bare tracker (nil view) then attach an Agent rebind
// here, mirroring the old `tracker.agent = a` in-package assignment.
func (ct *ChangeTracker) SetView(view AgentView) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.view = view
}

// SetRevisionIDForTest stamps a fixed revision ID (tests need deterministic
// revision ids that the generated one can't provide).
func (ct *ChangeTracker) SetRevisionIDForTest(id string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.revisionID = id
}

// TrackerSessionID exposes the tracker's current session identifier
// (the rotation contract: pkg/agent re-points it on session rotate and
// its test reads it back; SP-141 phase 2).
func (ct *ChangeTracker) TrackerSessionID() string {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	return ct.sessionID
}

func (ct *ChangeTracker) SetSessionID(sessionID string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	ct.sessionID = sessionID
}

// ShellCachePrimed reports whether the shell-snapshot baseline cache
// has been primed. Exported as part of the phase-2 seam: pkg/agent's
// TestAgent_EnableChangeTracking_SubagentSkipsShellPrime reads the old
// unexported shellCache field to distinguish eager (primary) from lazy
// (subagent) priming.
func (ct *ChangeTracker) ShellCachePrimed() bool {
	ct.shellCacheMu.Lock()
	defer ct.shellCacheMu.Unlock()
	return ct.shellCache != nil
}

// Helper functions

// Helper functions

// RecordRevert records that a revert or recovery restored filePath, so the
// change history reflects it: list_changes nets the file out (it is back to
// its original state) instead of still listing the reverted change, and a
// later revert sees the restored content as current. existsAfter=false
// records the removal of a file the session created.
func (ct *ChangeTracker) RecordRevert(filePath, before, after string, existsAfter bool, toolCall string) {
	if !ct.IsEnabled() {
		return
	}
	op := "write"
	if !existsAfter {
		op = "delete"
		after = ""
	}
	ct.mu.Lock()
	ct.changes = append(ct.changes, TrackedFileChange{
		FilePath:     ct.resolveAbsPath(filePath),
		OriginalCode: before,
		NewCode:      after,
		Operation:    op,
		Timestamp:    time.Now(),
		ToolCall:     toolCall,
	})
	ct.trimChangesLocked()
	ct.mu.Unlock()
}
