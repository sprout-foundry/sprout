package agent

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent/changes"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
)

// SP-141 phase 2: the Agent-facing half of the transcript surface. The
// snapshot/diff structs and the *Agent methods live here because
// TranscriptSnapshot.State is *ConversationState (a core type that
// moves last); the pure manifest/path/retention logic behind them moved
// to pkg/agent/changes (transcript_manifest.go) and is reached through
// the changes import — pkg/agent imports changes, never the reverse.

// TranscriptSnapshotFormat identifies snapshot file shape. Bump when
// breaking shape changes land so older readers don't silently misparse.
const TranscriptSnapshotFormat = "sprout-transcript/v1"

// MessageSource tags how a message arrived in the live conversation.
// The type and its values moved to pkg/agent/changes; these aliases
// keep the pkg/agent surface byte-compatible.
type MessageSource = changes.MessageSource

const (
	MessageSourceOriginal      = changes.MessageSourceOriginal
	MessageSourceLLMCheckpoint = changes.MessageSourceLLMCheckpoint
)

// MessageAnnotation is the per-message diagnostic view. Index aligns
// 1:1 with TranscriptSnapshot.State.Messages.
type MessageAnnotation = changes.MessageAnnotation

// CompactPreview captures the would-be result of running /compact right
// now, without applying it. Populated only when CaptureTranscriptSnapshot
// is called with includePreview=true.
type CompactPreview struct {
	BeforeMessageCount   int              `json:"before_message_count"`
	AfterMessageCount    int              `json:"after_message_count"`
	WouldReduce          bool             `json:"would_reduce"`
	CompactedMessages    []api.Message    `json:"compacted_messages"`
	RemainingCheckpoints []TurnCheckpoint `json:"remaining_checkpoints"`
}

// TranscriptFileChange is the slim per-file projection embedded in a
// snapshot's top-level FileChanges field. It deliberately omits the
// full original/new file bodies that the ChangeTracker keeps for
// recovery — those can be multi-megabyte per file and would blow up
// snapshot size. The path, operation, and tool-call identifier give a
// reader enough to answer "what files were touched between snapshot A
// and snapshot B" without loading the bytes themselves.
//
// Source distinguishes changes the primary agent made directly
// ("primary") from rollups parsed out of subagent tool results
// ("subagent"). The subagent's [subagent files modified] block is the
// authoritative per-call manifest, so the parser is a deterministic
// text scan rather than heuristic prose extraction.
type TranscriptFileChange = changes.TranscriptFileChange

// TranscriptSnapshot is the file shape written by /transcript and by
// the auto-capture path on compaction events. It is intentionally a
// superset of ConversationState so a reader can diff message lists,
// inspect checkpoint summaries, and compare snapshots across time.
type TranscriptSnapshot struct {
	Format             string                 `json:"format"`
	Timestamp          time.Time              `json:"timestamp"`
	Label              string                 `json:"label"`
	SessionID          string                 `json:"session_id"`
	WorkingDirectory   string                 `json:"working_directory"`
	State              *ConversationState     `json:"state"`
	MessageAnnotations []MessageAnnotation    `json:"message_annotations"`
	FileChanges        []TranscriptFileChange `json:"file_changes,omitempty"`
	ChangeTrackerRev   string                 `json:"change_tracker_revision,omitempty"`
	CompactPreview     *CompactPreview        `json:"compact_preview,omitempty"`
}

// BuildTranscriptSnapshot constructs an in-memory snapshot of the
// agent's current conversation state plus diagnostic annotations. Pure
// read — does not mutate the agent or touch disk.
func (a *Agent) BuildTranscriptSnapshot(label string, includePreview bool) *TranscriptSnapshot {
	if a == nil {
		return nil
	}
	workingDir, _ := os.Getwd()
	cleanWorkingDir, err := normalizeWorkingDirectory(workingDir)
	if err != nil {
		cleanWorkingDir = workingDir
	}
	sessionID := a.GetSessionID()
	messages := a.GetMessages()
	checkpoints := a.copyTurnCheckpoints()

	state := &ConversationState{
		Messages:                append([]api.Message(nil), messages...),
		TurnCheckpoints:         checkpoints,
		TaskActions:             a.GetTaskActions(),
		TotalCost:               a.state.GetTotalCost(),
		TotalTokens:             a.state.GetTotalTokens(),
		PromptTokens:            a.state.GetPromptTokens(),
		CompletionTokens:        a.state.GetCompletionTokens(),
		EstimatedTokenResponses: a.state.GetEstimatedTokenResponses(),
		ContinuationNudges:      a.state.GetContinuationNudges(),
		CachedTokens:            a.state.GetCachedTokens(),
		CachedCostSavings:       a.state.GetCachedCostSavings(),
		LastUpdated:             time.Now(),
		SessionID:               sessionID,
		Name:                    a.generateSessionName(),
		WorkingDirectory:        cleanWorkingDir,
		ConfigOverrides:         a.state.GetConfigOverrides(),
		SessionIntentEmbedding:  a.state.GetSessionIntentEmbedding(),
		LastProviderError:       a.state.GetLastProviderError(),
	}

	// File-change manifest: combine ChangeTracker's authoritative
	// primary record (which catches shell mutations the tool_calls
	// scan would miss) with the manifest extracted from message
	// content (which catches subagent rollups and prior-compaction
	// summary blocks). Dedupes on path+op+source so a primary write
	// reported by both sources collapses to one entry.
	var trackerChanges []TranscriptFileChange
	var trackerRev string
	if tracker := a.GetChangeTracker(); tracker != nil {
		trackerChanges = changes.TrackedChangesAsTranscript(tracker.GetChanges())
		trackerRev = tracker.GetRevisionID()
	}
	messageChanges := changes.ExtractFileChangesFromMessages(messages)
	fileChanges := changes.MergeFileChanges(trackerChanges, messageChanges)

	snap := &TranscriptSnapshot{
		Format:             TranscriptSnapshotFormat,
		Timestamp:          time.Now().UTC(),
		Label:              label,
		SessionID:          sessionID,
		WorkingDirectory:   cleanWorkingDir,
		State:              state,
		MessageAnnotations: changes.AnnotateMessages(messages),
		FileChanges:        fileChanges,
		ChangeTrackerRev:   trackerRev,
	}

	if includePreview && len(checkpoints) > 0 {
		compacted, remaining := a.BuildCheckpointCompactedMessages(messages)
		snap.CompactPreview = &CompactPreview{
			BeforeMessageCount:   len(messages),
			AfterMessageCount:    len(compacted),
			WouldReduce:          len(compacted) < len(messages),
			CompactedMessages:    compacted,
			RemainingCheckpoints: remaining,
		}
	}

	return snap
}

// CaptureTranscriptSnapshot builds a snapshot and writes it to
// ~/.sprout/transcripts/<scope-hash>/<session-id>/<UTC-ts>-<label>.json.
// Returns the absolute path of the file written so callers can report
// it to the user or log it.
func (a *Agent) CaptureTranscriptSnapshot(label string, includePreview bool) (string, error) {
	snap := a.BuildTranscriptSnapshot(label, includePreview)
	if snap == nil {
		return "", agenterrors.NewTool("transcript", "agent unavailable for transcript snapshot", nil)
	}
	dir, err := changes.TranscriptSessionDir(snap.SessionID, snap.WorkingDirectory)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", agenterrors.NewTool("transcript", "failed to create transcript dir", err)
	}
	cleanLabel := changes.SanitizeSnapshotLabel(label)
	filename := fmt.Sprintf("%s-%s.json", snap.Timestamp.Format("20060102T150405Z"), cleanLabel)
	path := filepath.Join(dir, filename)
	data, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return "", agenterrors.NewTool("transcript", "failed to marshal transcript snapshot", err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		return "", agenterrors.NewTool("transcript", "failed to write transcript snapshot", err)
	}
	changes.PruneTranscriptDir(dir)
	return path, nil
}

// LoadTranscriptSnapshot reads a snapshot file back into memory.
func LoadTranscriptSnapshot(path string) (*TranscriptSnapshot, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, agenterrors.NewTool("transcript", fmt.Sprintf("failed to read transcript snapshot %s", path), err)
	}
	var snap TranscriptSnapshot
	if err := json.Unmarshal(data, &snap); err != nil {
		return nil, agenterrors.NewTool("transcript", fmt.Sprintf("failed to parse transcript snapshot %s", path), err)
	}
	return &snap, nil
}

// ListTranscriptSnapshots returns snapshot file paths for the given
// session within the current workspace scope, sorted oldest-first.
func ListTranscriptSnapshots(sessionID, workingDir string) ([]string, error) {
	return changes.ListTranscriptSnapshots(sessionID, workingDir)
}

// TranscriptDiff is a compact, human-friendly comparison of two
// snapshots. Used by `/transcript diff` to expose what compaction (or
// some other state mutation) changed between snapshots.
type TranscriptDiff struct {
	OlderPath              string                 `json:"older_path"`
	NewerPath              string                 `json:"newer_path"`
	OlderTimestamp         time.Time              `json:"older_timestamp"`
	NewerTimestamp         time.Time              `json:"newer_timestamp"`
	OlderMessageCount      int                    `json:"older_message_count"`
	NewerMessageCount      int                    `json:"newer_message_count"`
	OlderCheckpointCount   int                    `json:"older_checkpoint_count"`
	NewerCheckpointCount   int                    `json:"newer_checkpoint_count"`
	OlderTotalTokens       int                    `json:"older_total_tokens"`
	NewerTotalTokens       int                    `json:"newer_total_tokens"`
	OlderFileChangeCount   int                    `json:"older_file_change_count"`
	NewerFileChangeCount   int                    `json:"newer_file_change_count"`
	NewFileChanges         []TranscriptFileChange `json:"new_file_changes,omitempty"`
	MessagesDroppedAtTail  int                    `json:"messages_dropped_at_tail"`
	MessagesReplacedByRole map[string]int         `json:"messages_replaced_by_role,omitempty"`
	ChangedIndices         []TranscriptDiffEntry  `json:"changed_indices,omitempty"`
	Notes                  []string               `json:"notes,omitempty"`
}

// TranscriptDiffEntry is a single divergence in the per-index walk.
// Truncated content makes diffs scannable; full text is in the raw JSON.
type TranscriptDiffEntry struct {
	Index          int    `json:"index"`
	OlderRole      string `json:"older_role,omitempty"`
	NewerRole      string `json:"newer_role,omitempty"`
	OlderSource    string `json:"older_source,omitempty"`
	NewerSource    string `json:"newer_source,omitempty"`
	OlderFirstLine string `json:"older_first_line,omitempty"`
	NewerFirstLine string `json:"newer_first_line,omitempty"`
}

// DiffTranscriptSnapshots compares two snapshots and returns a
// human-readable diff structure. Older should be the chronologically
// earlier snapshot; the function does not re-sort.
func DiffTranscriptSnapshots(older, newer *TranscriptSnapshot) *TranscriptDiff {
	if older == nil || newer == nil || older.State == nil || newer.State == nil {
		return nil
	}
	diff := &TranscriptDiff{
		OlderTimestamp:       older.Timestamp,
		NewerTimestamp:       newer.Timestamp,
		OlderMessageCount:    len(older.State.Messages),
		NewerMessageCount:    len(newer.State.Messages),
		OlderCheckpointCount: len(older.State.TurnCheckpoints),
		NewerCheckpointCount: len(newer.State.TurnCheckpoints),
		OlderTotalTokens:     older.State.TotalTokens,
		NewerTotalTokens:     newer.State.TotalTokens,
		OlderFileChangeCount: len(older.FileChanges),
		NewerFileChangeCount: len(newer.FileChanges),
		NewFileChanges:       changes.FileChangesAdded(older.FileChanges, newer.FileChanges),
	}

	olderMsgs := older.State.Messages
	newerMsgs := newer.State.Messages
	olderAnn := older.MessageAnnotations
	newerAnn := newer.MessageAnnotations

	if len(newerMsgs) < len(olderMsgs) {
		diff.MessagesDroppedAtTail = len(olderMsgs) - len(newerMsgs)
	}

	limit := len(olderMsgs)
	if len(newerMsgs) < limit {
		limit = len(newerMsgs)
	}
	replacedByRole := map[string]int{}
	for i := 0; i < limit; i++ {
		o := olderMsgs[i]
		n := newerMsgs[i]
		if o.Role == n.Role && o.Content == n.Content && len(o.ToolCalls) == len(n.ToolCalls) {
			continue
		}
		role := n.Role
		if role == "" {
			role = o.Role
		}
		replacedByRole[role]++
		entry := TranscriptDiffEntry{Index: i, OlderRole: o.Role, NewerRole: n.Role}
		if i < len(olderAnn) {
			entry.OlderSource = string(olderAnn[i].Source)
			entry.OlderFirstLine = olderAnn[i].FirstLine
		}
		if i < len(newerAnn) {
			entry.NewerSource = string(newerAnn[i].Source)
			entry.NewerFirstLine = newerAnn[i].FirstLine
		}
		diff.ChangedIndices = append(diff.ChangedIndices, entry)
	}
	if len(replacedByRole) > 0 {
		diff.MessagesReplacedByRole = replacedByRole
	}

	if diff.NewerCheckpointCount < diff.OlderCheckpointCount {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("turn checkpoints decreased by %d — likely consumed by /compact",
				diff.OlderCheckpointCount-diff.NewerCheckpointCount))
	}
	if diff.MessagesDroppedAtTail > 0 {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("%d trailing messages dropped — could indicate /clear or pruner activity",
				diff.MessagesDroppedAtTail))
	}
	if len(diff.ChangedIndices) > 0 && diff.MessagesDroppedAtTail == 0 {
		diff.Notes = append(diff.Notes,
			fmt.Sprintf("%d messages were replaced in place — checkpoint substitution or pruner rewrite",
				len(diff.ChangedIndices)))
	}

	return diff
}

// CompactedFilesHeader marks the file-change manifest block that
// `/compact` appends to its LLM-generated summary. Future compactions
// re-parse this block to keep the running file-change history visible
// across the summary boundary — without this, every `/compact` would
// lose the manifest of files touched in the summarized turns. Defined
// in pkg/agent/changes (with the parser); aliased above for the
// pkg/agent surface.
const CompactedFilesHeader = changes.CompactedFilesHeader

// ExtractFileChangesFromMessages walks the supplied message slice and
// returns a deduped manifest of files touched. SP-141 phase 2
// forwarder — the implementation lives in pkg/agent/changes.
func ExtractFileChangesFromMessages(messages []api.Message) []TranscriptFileChange {
	return changes.ExtractFileChangesFromMessages(messages)
}

// FormatFileChangesForSummary renders a manifest into the canonical
// text block appended to a /compact summary. SP-141 phase 2 forwarder —
// the implementation lives in pkg/agent/changes.
func FormatFileChangesForSummary(changesManifest []TranscriptFileChange) string {
	return changes.FormatFileChangesForSummary(changesManifest)
}
