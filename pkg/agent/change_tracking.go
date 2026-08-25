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
// sites within this package keep working.
const RedactedContentMarker = history.RedactedContentMarker

// ChangeTracker manages change tracking for the agent workflow
type ChangeTracker struct {
	// mu protects revisionID, instructions, changes, baseRevisionRecorded,
	// committedChangeCount, and checkpointedChangeCount.
	mu           sync.Mutex
	revisionID   string
	sessionID    string
	instructions string
	changes      []TrackedFileChange
	// enabled is the on/off flag for change tracking. Every concurrent read
	// in production code MUST go through IsEnabled() to avoid races.
	enabled              bool
	agent                *Agent
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

// NewChangeTracker creates a new change tracker for an agent session
func NewChangeTracker(agent *Agent, instructions string) *ChangeTracker {
	history.InitializeHistoryPaths(nil)

	sessionID := agent.GetSessionID()
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
		agent:        agent,
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

// TrackFileWrite tracks a write operation (WriteFile tool)
func (ct *ChangeTracker) TrackFileWrite(filePath string, newContent string) error {
	if !ct.IsEnabled() {
		return nil
	}
	// Normalize to absolute at track time so stored FilePath is
	// independent of the process's CWD.
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

// appendChange appends a single tracked change under ct.mu.
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

	historyConversation := convertToHistoryMessages(conversation)

	if !ct.baseRevisionRecorded {
		revisionID, err := history.RecordBaseRevision(ct.revisionID, ct.instructions, llmResponse, historyConversation)
		if err != nil {
			ct.mu.Unlock()
			return agenterrors.Wrap(err, "failed to record base revision")
		}
		ct.revisionID = revisionID
		ct.baseRevisionRecorded = true
	}

	// Record each file change. Advance committedChangeCount after each
	// successful record so a mid-loop failure doesn't leave the counter stale.
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
			ct.instructions,
			llmResponse,
			ct.getAgentModel(),
		)
		if err != nil {
			ct.mu.Unlock()
			return agenterrors.Wrap(err, fmt.Sprintf("failed to record change for %s", change.FilePath))
		}
		ct.committedChangeCount++
	}

	// Snapshot the changes for the sweep. Copy under the lock, then release.
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
	ct.mu.Unlock()
	// Re-baseline the shell cache for each touched path to avoid duplicates.
	for _, ch := range merged {
		ct.SyncShellCacheForPath(ch.FilePath)
	}
}

// Clear clears all tracked changes (but keeps the tracker enabled).
// Also resets the shell-snapshot cache.
func (ct *ChangeTracker) Clear() {
	ct.mu.Lock()
	ct.clearLocked()
	ct.mu.Unlock()
}

// clearLocked is the body of Clear, callable from sites that already hold ct.mu.
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
