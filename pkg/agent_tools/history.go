package tools

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/filesystem"
	"github.com/alantheprice/ledit/pkg/history"
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
		return ViewHistoryResult{}, fmt.Errorf("failed to retrieve change history: %w", err)
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

	metadata := map[string]interface{}{
		"limit":        limit,
		"file_filter":  fileFilter,
		"show_content": showContent,
		"entry_count":  len(changes),
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
			return RollbackResult{}, fmt.Errorf("failed to retrieve changes: %w", err)
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

		if err := filesystem.SaveFile(targetChange.Filename, targetChange.OriginalCode); err != nil {
			return RollbackResult{}, fmt.Errorf("failed to restore file content: %w", err)
		}

		return RollbackResult{
			Output: fmt.Sprintf("Successfully rolled back file '%s' from revision '%s'", filePath, revisionID),
			Metadata: map[string]interface{}{
				"action":      "file_rollback",
				"revision_id": revisionID,
				"file_path":   filePath,
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

	if err := history.RevertChangeByRevisionID(revisionID); err != nil {
		return RollbackResult{}, fmt.Errorf("failed to rollback revision: %w", err)
	}

	return RollbackResult{
		Output: fmt.Sprintf("Successfully rolled back revision '%s'", revisionID),
		Metadata: map[string]interface{}{
			"action":      "revision_rollback",
			"revision_id": revisionID,
		},
		Success: true,
	}, nil
}

func listAvailableRevisions() (RollbackResult, error) {
	changes, err := history.GetAllChanges()
	if err != nil {
		return RollbackResult{}, fmt.Errorf("failed to retrieve changes: %w", err)
	}

	if len(changes) == 0 {
		return RollbackResult{
			Output:   "No changes found to rollback.",
			Metadata: map[string]interface{}{"action": "list_revisions", "available_count": 0},
			Success:  true,
		}, nil
	}

	// Group active changes by revision ID
	revisions := make(map[string][]history.ChangeLog)
	type revisionInfo struct {
		id      string
		when    time.Time
		changes []history.ChangeLog
	}
	order := make([]revisionInfo, 0)

	for _, change := range changes {
		if change.Status != "active" {
			continue
		}
		if _, ok := revisions[change.RequestHash]; !ok {
			revisions[change.RequestHash] = []history.ChangeLog{}
		}
		revisions[change.RequestHash] = append(revisions[change.RequestHash], change)
	}

	if len(revisions) == 0 {
		return RollbackResult{
			Output:   "No active changes found to rollback.",
			Metadata: map[string]interface{}{"action": "list_revisions", "available_count": 0},
			Success:  true,
		}, nil
	}

	for id, revChanges := range revisions {
		when := revChanges[0].Timestamp
		order = append(order, revisionInfo{id: id, when: when, changes: revChanges})
	}

	sort.Slice(order, func(i, j int) bool {
		return order[i].when.After(order[j].when)
	})

	var builder strings.Builder
	builder.WriteString("Available revisions to rollback:\n\n")

	for _, info := range order {
		revChanges := info.changes
		builder.WriteString(fmt.Sprintf("**Revision ID:** %s\n", info.id))
		builder.WriteString(fmt.Sprintf("**Model:** %s\n", revChanges[0].AgentModel))
		builder.WriteString(fmt.Sprintf("**Time:** %s\n", revChanges[0].Timestamp.Format(time.RFC3339)))
		builder.WriteString(fmt.Sprintf("**Files changed:** %d\n", len(revChanges)))
		for _, change := range revChanges {
			builder.WriteString(fmt.Sprintf("  - %s\n", change.Filename))
		}
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
			"available_count": len(revisions),
		},
		Success: true,
	}, nil
}

// formatHistoryView formats change history for display.
func formatHistoryView(changes []history.ChangeLog, showContent bool) string {
	var result strings.Builder

	result.WriteString(fmt.Sprintf("## Change History (%d entries)\n\n", len(changes)))

	revisions := make(map[string][]history.ChangeLog)
	revisionOrder := make([]string, 0)
	seen := make(map[string]bool)

	for _, change := range changes {
		if !seen[change.RequestHash] {
			revisionOrder = append(revisionOrder, change.RequestHash)
			seen[change.RequestHash] = true
		}
		revisions[change.RequestHash] = append(revisions[change.RequestHash], change)
	}

	for _, revID := range revisionOrder {
		revChanges := revisions[revID]
		if len(revChanges) == 0 {
			continue
		}

		firstChange := revChanges[0]
		result.WriteString(fmt.Sprintf("### Revision: %s\n", revID))
		result.WriteString(fmt.Sprintf("**Model:** %s\n", firstChange.AgentModel))
		result.WriteString(fmt.Sprintf("**Time:** %s\n", firstChange.Timestamp.Format("2006-01-02 15:04:05")))
		result.WriteString(fmt.Sprintf("**Files Changed:** %d\n", len(revChanges)))

		if firstChange.Instructions != "" {
			result.WriteString(fmt.Sprintf("**Instructions:** %s\n", firstChange.Instructions))
		}

		result.WriteString("\n**Files:**\n")
		for _, change := range revChanges {
			result.WriteString(fmt.Sprintf("- **%s** (%s)\n", change.Filename, change.Status))
			if change.Description != "" {
				result.WriteString(fmt.Sprintf("  *%s*\n", change.Description))
			}

			if showContent {
				result.WriteString("  ```diff\n")
				result.WriteString(fmt.Sprintf("  Content changed (%d chars → %d chars)\n",
					len(change.OriginalCode), len(change.NewCode)))
				result.WriteString("  ```\n")
			}
		}
		result.WriteString("\n---\n\n")
	}

	return result.String()
}
