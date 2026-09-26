package agent

// transcript_snapshot_diff.go — the snapshot-diff types and
// DiffTranscriptSnapshots, split out of transcript_snapshot.go.

import (
	"fmt"
	"time"
)

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
		NewFileChanges:       fileChangesAdded(older.FileChanges, newer.FileChanges),
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
