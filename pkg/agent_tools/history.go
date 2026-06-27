package tools

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
	"github.com/sprout-foundry/sprout/pkg/history"
)

// ViewHistoryResult captures the output and metadata for history views.
type ViewHistoryResult struct {
	Output   string
	Metadata map[string]interface{}
}

// RollbackResult captures the output, metadata, and success state for rollback operations.
type RollbackResult struct {
	Output   string
	Metadata map[string]interface{}
	Success  bool
}

// ViewHistory returns a formatted history view based on the provided filters.
func ViewHistory(limit int, fileFilter string, since *time.Time, showContent bool) (ViewHistoryResult, error) {
	if limit <= 0 {
		limit = 10
	}

	var (
		changes []history.ChangeLog
		err     error
	)

	if since != nil {
		changes, err = history.GetChangesSince(*since)
	} else {
		changes, err = history.GetAllChanges()
	}
	if err != nil {
		return ViewHistoryResult{}, fmt.Errorf("retrieve change history: %w", err)
	}

	fileFilter = strings.TrimSpace(fileFilter)
	if fileFilter != "" {
		filtered := make([]history.ChangeLog, 0, len(changes))
		for _, change := range changes {
			if strings.Contains(strings.ToLower(change.Filename), strings.ToLower(fileFilter)) {
				filtered = append(filtered, change)
			}
		}
		changes = filtered
	}

	if len(changes) > limit {
		changes = changes[:limit]
	}

	// Collect the distinct revision IDs the view touched so the caller
	// can bump activity timestamps (cold→warm promotion under the
	// compaction policy).
	seen := make(map[string]bool, len(changes))
	revisionIDs := make([]string, 0, len(changes))
	for _, c := range changes {
		if c.RequestHash == "" || seen[c.RequestHash] {
			continue
		}
		seen[c.RequestHash] = true
		revisionIDs = append(revisionIDs, c.RequestHash)
	}

	metadata := map[string]interface{}{
		"limit":         limit,
		"file_filter":   fileFilter,
		"show_content":  showContent,
		"entry_count":   len(changes),
		"revision_ids":  revisionIDs,
	}
	if since != nil {
		metadata["since"] = since.Format(time.RFC3339)
	}

	if len(changes) == 0 {
		return ViewHistoryResult{
			Output:   "No changes found matching the specified criteria.",
			Metadata: metadata,
		}, nil
	}

	return ViewHistoryResult{
		Output:   formatHistoryView(changes, showContent),
		Metadata: metadata,
	}, nil
}

// RollbackChanges previews or performs a rollback for a revision or file.
func RollbackChanges(revisionID string, filePath string, confirm bool) (RollbackResult, error) {
	revisionID = strings.TrimSpace(revisionID)
	filePath = strings.TrimSpace(filePath)

	if revisionID == "" {
		return listAvailableRevisions()
	}

	if filePath != "" {
		if !confirm {
			return RollbackResult{
				Output: fmt.Sprintf("Would rollback file '%s' from revision '%s'.\nTo confirm, call again with confirm=true.",
					filePath, revisionID),
				Metadata: map[string]interface{}{
					"action":      "preview_file_rollback",
					"revision_id": revisionID,
					"file_path":   filePath,
				},
				Success: true,
			}, nil
		}

		changes, err := history.GetAllChanges()
		if err != nil {
			return RollbackResult{}, fmt.Errorf("retrieve changes: %w", err)
		}

		var targetChange *history.ChangeLog
		for i := range changes {
			change := &changes[i]
			if change.RequestHash == revisionID && change.Filename == filePath && change.Status == "active" {
				targetChange = change
				break
			}
		}

		if targetChange == nil {
			return RollbackResult{}, fmt.Errorf("no active change found for file '%s' in revision '%s'", filePath, revisionID)
		}

		// Staleness guard: skip if the file was modified after this snapshot,
		// OR if the file's content has been committed to git (the work is now
		// version-controlled and reverting would undo it).
		//
		// IsRevertSafe layers the content-identity check (disk != NewCode →
		// stale) with git-awareness (disk == NewCode but matches HEAD →
		// committed, refuse). Outside a git repo or for untracked files, the
		// content check alone decides.
		if !history.IsRevertSafeWithOriginal(targetChange.Filename, targetChange.NewCode, targetChange.OriginalCode) {
			history.AuditRevertSkip("RollbackChanges", targetChange.Filename, "stale or committed")
			return RollbackResult{
				Output: fmt.Sprintf("Skipped rollback of '%s': file was modified after the snapshot (content differs from recorded state) or the content is now committed to git. The file may have been committed or edited intentionally.", filePath),
				Metadata: map[string]interface{}{
					"action":      "stale_skip",
					"revision_id": revisionID,
					"file_path":   filePath,
				},
				Success: true,
			}, nil
		}

		history.AuditRevertWrite("RollbackChanges", targetChange.Filename, "OriginalCode")
		if err := filesystem.SaveFile(targetChange.Filename, targetChange.OriginalCode); err != nil {
			return RollbackResult{}, fmt.Errorf("restore file content: %w", err)
		}

		return RollbackResult{
			Output: fmt.Sprintf("Successfully rolled back file '%s' from revision '%s'", filePath, revisionID),
			Metadata: map[string]interface{}{
				"action":      "file_rollback",
				"revision_id": revisionID,
				"file_path":   filePath,
				"file_paths":  []string{filePath},
			},
			Success: true,
		}, nil
	}

	if !confirm {
		return RollbackResult{
			Output: fmt.Sprintf("Would rollback revision '%s'.\nTo confirm, call again with confirm=true.", revisionID),
			Metadata: map[string]interface{}{
				"action":      "preview_revision_rollback",
				"revision_id": revisionID,
			},
			Success: true,
		}, nil
	}

	// Collect the file paths that will be reverted so we can report them
	revisionFiles, _ := history.GetFilesForRevision(revisionID)

	if err := history.RevertChangeByRevisionID(revisionID); err != nil {
		return RollbackResult{}, fmt.Errorf("rollback revision: %w", err)
	}

	metadata := map[string]interface{}{
		"action":      "revision_rollback",
		"revision_id": revisionID,
	}
	if len(revisionFiles) > 0 {
		metadata["file_paths"] = revisionFiles
	}

	return RollbackResult{
		Output:   fmt.Sprintf("Successfully rolled back revision '%s'", revisionID),
		Metadata: metadata,
		Success:  true,
	}, nil
}

// revisionGroup holds parsed data for a single revision block.
type revisionGroup struct {
	ID      string
	Changes []history.ChangeLog
}

// formatRevisionOpts controls the output format for a single revision block.
type formatRevisionOpts struct {
	ShowContent      bool
	ShowStatus       bool
	ShowInstructions bool
	TimeFormat       string
	TitlePrefix      string
	FilesLabel       string
}

// groupChangesByRevision groups changes by RequestHash and returns them sorted
// by timestamp (newest first). The result order is deterministic because of the
// timestamp-based sort.
func groupChangesByRevision(changes []history.ChangeLog) []revisionGroup {
	if len(changes) == 0 {
		return []revisionGroup{}
	}

	groups := make(map[string][]history.ChangeLog)

	for _, change := range changes {
		groups[change.RequestHash] = append(groups[change.RequestHash], change)
	}

	result := make([]revisionGroup, 0, len(groups))
	for id, revChanges := range groups {
		result = append(result, revisionGroup{ID: id, Changes: revChanges})
	}

	sort.SliceStable(result, func(i, j int) bool {
		return result[i].Changes[0].Timestamp.After(result[j].Changes[0].Timestamp)
	})

	return result
}

// formatRevision formats a single revision group into a string using the provided options.
func formatRevision(group revisionGroup, opts formatRevisionOpts) string {
	if len(group.Changes) == 0 {
		return ""
	}

	var b strings.Builder
	changes := group.Changes

	b.WriteString(fmt.Sprintf("%s%s\n", opts.TitlePrefix, group.ID))
	b.WriteString(fmt.Sprintf("**Model:** %s\n", changes[0].AgentModel))
	b.WriteString(fmt.Sprintf("**Time:** %s\n", changes[0].Timestamp.Format(opts.TimeFormat)))
	b.WriteString(fmt.Sprintf("%s%d\n", opts.FilesLabel, len(changes)))

	// Surface tier so the LLM knows when conversation context isn't
	// available for an old revision. All other data is intact for any
	// surfaced entry (compacted revisions either keep diffs or are
	// dropped entirely — there's no middle tier where diffs vanish).
	if changes[0].Tier != "" && changes[0].Tier != "hot" {
		b.WriteString(fmt.Sprintf("**Storage:** %s (conversation context unavailable)\n", changes[0].Tier))
	}

	if opts.ShowInstructions && changes[0].Instructions != "" {
		b.WriteString(fmt.Sprintf("**Instructions:** %s\n", changes[0].Instructions))
	}

	if opts.ShowStatus {
		b.WriteString("\n**Files:**\n")
		for _, change := range changes {
			b.WriteString(fmt.Sprintf("- **%s** (%s)\n", change.Filename, change.Status))
			if change.Description != "" {
				b.WriteString(fmt.Sprintf("  *%s*\n", change.Description))
			}

			if opts.ShowContent {
				b.WriteString("  ```diff\n")
				b.WriteString(fmt.Sprintf("  Content changed (%d chars → %d chars)\n",
					len(change.OriginalCode), len(change.NewCode)))
				b.WriteString("  ```\n")
			}
		}
	} else {
		for _, change := range changes {
			b.WriteString(fmt.Sprintf("  - %s\n", change.Filename))
		}
	}

	return b.String()
}

func listAvailableRevisions() (RollbackResult, error) {
	changes, err := history.GetAllChanges()
	if err != nil {
		return RollbackResult{}, fmt.Errorf("retrieve changes: %w", err)
	}

	if len(changes) == 0 {
		return RollbackResult{
			Output:   "No changes found to rollback.",
			Metadata: map[string]interface{}{"action": "list_revisions", "available_count": 0},
			Success:  true,
		}, nil
	}

	// Filter to only active changes
	var active []history.ChangeLog
	for _, change := range changes {
		if change.Status == "active" {
			active = append(active, change)
		}
	}

	if len(active) == 0 {
		return RollbackResult{
			Output:   "No active changes found to rollback.",
			Metadata: map[string]interface{}{"action": "list_revisions", "available_count": 0},
			Success:  true,
		}, nil
	}

	groups := groupChangesByRevision(active)

	opts := formatRevisionOpts{
		ShowContent:  false,
		ShowStatus:   false,
		ShowInstructions: false,
		TimeFormat:   time.RFC3339,
		TitlePrefix:  "**Revision ID:** ",
		FilesLabel:   "**Files changed:** ",
	}

	var builder strings.Builder
	builder.WriteString("Available revisions to rollback:\n\n")

	for _, group := range groups {
		builder.WriteString(formatRevision(group, opts))
		builder.WriteString("\n")
	}

	builder.WriteString("To rollback a revision, call this tool again with:\n")
	builder.WriteString("- `revision_id`: The revision ID to rollback\n")
	builder.WriteString("- `confirm`: true (to actually perform the rollback)\n")
	builder.WriteString("- `file_path`: Optional, to rollback only a specific file\n")

	return RollbackResult{
		Output: builder.String(),
		Metadata: map[string]interface{}{
			"action":          "list_revisions",
			"available_count": len(groups),
		},
		Success: true,
	}, nil
}

// formatHistoryView formats change history for display.
func formatHistoryView(changes []history.ChangeLog, showContent bool) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("## Change History (%d entries)\n\n", len(changes)))

	groups := groupChangesByRevision(changes)

	for _, group := range groups {
		revChanges := group.Changes
		if len(revChanges) == 0 {
			continue
		}

		opts := formatRevisionOpts{
			ShowContent:      showContent,
			ShowStatus:       true,
			ShowInstructions: true,
			TimeFormat:       "2006-01-02 15:04:05",
			TitlePrefix:      "### Revision: ",
			FilesLabel:       "**Files Changed:** ",
		}
		result.WriteString(formatRevision(group, opts))
		result.WriteString("\n---\n\n")
	}

	return result.String()
}
