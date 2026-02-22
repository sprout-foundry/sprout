package spec

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/history"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ChangeReviewResult represents the result of reviewing tracked changes
type ChangeReviewResult struct {
	SpecResult   *SpecExtractionResult `json:"spec_result"`
	ScopeResult  *ScopeReviewResult    `json:"scope_result"`
	FilesChanged int                   `json:"files_changed"`
	TotalChanges int                   `json:"total_changes"`
	RevisionID   string                `json:"revision_id"`
	Summary      string                `json:"summary"`
}

// ReviewTrackedChanges reviews changes tracked in the current revision against a canonical spec
// This is used by the agent for self-review during a session
func ReviewTrackedChanges(
	revisionID string,
	cfg *configuration.Config,
	logger *utils.Logger,
) (*ChangeReviewResult, error) {

	// Get revision groups to find our changes
	revisionGroups, err := history.GetRevisionGroups()
	if err != nil {
		return nil, fmt.Errorf("failed to get revision groups: %w", err)
	}

	// Find the current revision
	var currentRevision *history.RevisionGroup
	for i := range revisionGroups {
		if revisionGroups[i].RevisionID == revisionID {
			currentRevision = &revisionGroups[i]
			break
		}
	}

	if currentRevision == nil {
		return nil, fmt.Errorf("revision ID '%s' not found in change tracking", revisionID)
	}

	// Extract changes as a diff
	diff, err := changesToDiff(currentRevision)
	if err != nil {
		return nil, fmt.Errorf("failed to convert changes to diff: %w", err)
	}

	if diff == "" {
		return &ChangeReviewResult{
			RevisionID:   revisionID,
			Summary:      "No changes to review",
			FilesChanged: 0,
			TotalChanges: 0,
		}, nil
	}

	// Build conversation from revision context
	conversation := buildConversationFromRevision(currentRevision)
	userIntent := currentRevision.Instructions

	// Create spec review service
	specService, err := NewSpecReviewService(cfg, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create spec service: %w", err)
	}

	// Extract spec from conversation
	logger.LogProcessStep("Extracting canonical specification from session conversation...")
	specResult, err := specService.GetExtractor().ExtractSpec(conversation, userIntent)
	if err != nil {
		return nil, fmt.Errorf("failed to extract spec: %w", err)
	}

	spec := specResult.Spec

	// Validate changes against spec
	logger.LogProcessStep("Validating tracked changes against specification...")
	scopeResult, err := specService.GetValidator().ValidateScope(diff, spec)
	if err != nil {
		return nil, fmt.Errorf("failed to validate scope: %w", err)
	}

	// Build result
	result := &ChangeReviewResult{
		SpecResult:   specResult,
		ScopeResult:  scopeResult,
		RevisionID:   revisionID,
		FilesChanged: len(currentRevision.Changes),
		TotalChanges: countTotalChanges(currentRevision),
	}

	// Build summary
	result.Summary = buildReviewSummary(specResult, scopeResult, result.FilesChanged, result.TotalChanges)

	return result, nil
}

// changesToDiff converts tracked changes to a unified diff format
// Uses actual line numbers from file content instead of fake hunk headers
func changesToDiff(revision *history.RevisionGroup) (string, error) {
	if len(revision.Changes) == 0 {
		return "", nil
	}

	var diff strings.Builder

	for _, change := range revision.Changes {
		// Skip reverted changes
		if change.Status != "active" {
			continue
		}

		filename := change.Filename

		// Split original and new code into lines
		originalLines := strings.Split(change.OriginalCode, "\n")
		newLines := strings.Split(change.NewCode, "\n")

		// Build diff header
		diff.WriteString(fmt.Sprintf("diff --git a/%s b/%s\n", filename, filename))
		diff.WriteString(fmt.Sprintf("--- a/%s\n", filename))
		diff.WriteString(fmt.Sprintf("+++ b/%s\n", filename))

		// Find common prefix and suffix to minimize diff size
		commonPrefix := 0
		for commonPrefix < len(originalLines) && commonPrefix < len(newLines) {
			if originalLines[commonPrefix] == newLines[commonPrefix] {
				commonPrefix++
			} else {
				break
			}
		}

		commonSuffix := 0
		for commonSuffix < len(originalLines)-commonPrefix && commonSuffix < len(newLines)-commonPrefix {
			if originalLines[len(originalLines)-1-commonSuffix] == newLines[len(newLines)-1-commonSuffix] {
				commonSuffix++
			} else {
				break
			}
		}

		// Calculate line numbers for hunk header
		// Original file: starts at line 1, shows commonPrefix context + changed lines
		originalStart := commonPrefix + 1
		originalCount := (len(originalLines) - commonPrefix - commonSuffix)
		if originalCount < 0 {
			originalCount = 0
		}

		// New file: starts at line 1, shows commonPrefix context + changed lines
		newStart := commonPrefix + 1
		newCount := (len(newLines) - commonPrefix - commonSuffix)
		if newCount < 0 {
			newCount = 0
		}

		// If entire file is different or file is new, use simple format
		if originalStart == 1 && newStart == 1 {
			originalStart = 1
			newStart = 1
			originalCount = len(originalLines)
			newCount = len(newLines)
		}

		// Write hunk header with real line numbers
		diff.WriteString(fmt.Sprintf("@@ -%d,%d +%d,%d @@\n",
			originalStart, originalCount, newStart, newCount))

		// Write context lines before changes (common prefix)
		for i := 0; i < commonPrefix; i++ {
			diff.WriteString(fmt.Sprintf(" %s\n", originalLines[i]))
		}

		// Write removed lines
		for i := commonPrefix; i < len(originalLines)-commonSuffix; i++ {
			diff.WriteString(fmt.Sprintf("-%s\n", originalLines[i]))
		}

		// Write added lines
		for i := commonPrefix; i < len(newLines)-commonSuffix; i++ {
			diff.WriteString(fmt.Sprintf("+%s\n", newLines[i]))
		}

		// Write context lines after changes (common suffix)
		for i := len(newLines) - commonSuffix; i < len(newLines); i++ {
			diff.WriteString(fmt.Sprintf(" %s\n", newLines[i]))
		}

		diff.WriteString("\n")
	}

	return diff.String(), nil
}

// buildConversationFromRevision builds a conversation from revision context
// Returns the full conversation history including all user/assistant/tool messages
func buildConversationFromRevision(revision *history.RevisionGroup) []Message {
	// If we have the full conversation stored, use it
	if revision.Conversation != nil && len(revision.Conversation) > 0 {
		conversation := make([]Message, len(revision.Conversation))
		for i, msg := range revision.Conversation {
			conversation[i] = Message{
				Role:    msg.Role,
				Content: msg.Content,
			}
		}
		return conversation
	}

	// Fallback for revisions created before conversation tracking:
	// Create a simple 2-message conversation (instructions + response)
	return []Message{
		{
			Role:    "user",
			Content: revision.Instructions,
		},
		{
			Role:    "assistant",
			Content: revision.Response,
		},
	}
}

// countTotalChanges counts the total number of active changes
func countTotalChanges(revision *history.RevisionGroup) int {
	count := 0
	for _, change := range revision.Changes {
		if change.Status == "active" {
			count++
		}
	}
	return count
}

// buildReviewSummary builds a human-readable summary of the review
func buildReviewSummary(specResult *SpecExtractionResult, scopeResult *ScopeReviewResult, filesChanged, totalChanges int) string {
	var summary strings.Builder

	summary.WriteString(fmt.Sprintf("Reviewed %d file(s) with %d change(s)\n", filesChanged, totalChanges))
	summary.WriteString(fmt.Sprintf("Spec confidence: %.0f%%\n", specResult.Confidence*100))

	if scopeResult.InScope {
		summary.WriteString("✓ All changes are within scope\n")
	} else {
		summary.WriteString(fmt.Sprintf("⚠ %d scope violation(s) found\n", len(scopeResult.Violations)))
	}

	return summary.String()
}
