package agent

import (
	"context"
	"fmt"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/history"
	"github.com/alantheprice/ledit/pkg/spec"
	"github.com/alantheprice/ledit/pkg/utils"
)

// handleSelfReview implements the self_review tool
// This tool allows the agent to review its own work against a canonical spec extracted from the conversation
func handleSelfReview(ctx context.Context, a *Agent, args map[string]interface{}) (string, error) {
	// Check if change tracker has uncommitted changes
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		// Check if there are uncommitted changes in memory
		if len(a.changeTracker.changes) > 0 {
			// Changes exist but may not be committed yet
			// Try to commit them first to ensure they're in the filesystem
			// Note: CommitChanges is called by agent at end of task, but we can check if it's done
		}
	}

	// Get the current revision ID from the agent's change tracker
	revisionID := ""
	if a.changeTracker != nil && a.changeTracker.IsEnabled() {
		revisionID = a.changeTracker.GetRevisionID()
	}

	// If no active revision, get most recent from history
	if revisionID == "" {
		revisionGroups, err := history.GetRevisionGroups()
		if err != nil {
			return "", fmt.Errorf("failed to get revision history: %w", err)
		}
		if len(revisionGroups) == 0 {
			return "", fmt.Errorf("no changes found - agent must make changes and commit them before reviewing")
		}
		revisionID = revisionGroups[0].RevisionID
	}

	// Get configuration
	cfg := a.configManager.GetConfig()
	if cfg == nil {
		var err error
		cfg, err = configuration.Load()
		if err != nil {
			return "", fmt.Errorf("failed to load configuration: %w", err)
		}
	}

	// Create logger
	logger := utils.GetLogger(false)

	// Perform the review
	result, err := spec.ReviewTrackedChanges(revisionID, cfg, logger)
	if err != nil {
		return "", fmt.Errorf("self-review failed: %w", err)
	}

	// Format result for agent
	output := formatSelfReviewResult(result)

	return output, nil
}

// formatSelfReviewResult formats the review result for the agent
func formatSelfReviewResult(result *spec.ChangeReviewResult) string {
	var output string

	// Header
	output += fmt.Sprintf("## Self-Review Results\n\n")
	output += fmt.Sprintf("**Revision ID**: %s\n", result.RevisionID)
	output += fmt.Sprintf("**Files Changed**: %d\n", result.FilesChanged)
	output += fmt.Sprintf("**Total Changes**: %d\n\n", result.TotalChanges)

	// Spec extraction summary
	if result.SpecResult != nil {
		output += fmt.Sprintf("### Specification\n")
		output += fmt.Sprintf("**Objective**: %s\n", result.SpecResult.Spec.Objective)
		output += fmt.Sprintf("**Confidence**: %.0f%%\n\n", result.SpecResult.Confidence*100)

		if len(result.SpecResult.Spec.InScope) > 0 {
			output += fmt.Sprintf("**In Scope**:\n")
			for _, item := range result.SpecResult.Spec.InScope {
				output += fmt.Sprintf("  - %s\n", item)
			}
			output += "\n"
		}

		if len(result.SpecResult.Spec.OutOfScope) > 0 {
			output += fmt.Sprintf("**Out of Scope**:\n")
			for _, item := range result.SpecResult.Spec.OutOfScope {
				output += fmt.Sprintf("  - %s\n", item)
			}
			output += "\n"
		}
	}

	// Scope validation results
	if result.ScopeResult != nil {
		output += fmt.Sprintf("### Scope Validation\n\n")

		if result.ScopeResult.InScope {
			output += "✅ **Status**: IN_SCOPE\n\n"
			output += "All changes align with the specification. No scope violations detected.\n\n"
		} else {
			output += "⚠️ **Status**: OUT_OF_SCOPE\n\n"
			output += fmt.Sprintf("%s\n\n", result.ScopeResult.Summary)

			if len(result.ScopeResult.Violations) > 0 {
				output += "**Violations**:\n\n"
				for _, violation := range result.ScopeResult.Violations {
					output += fmt.Sprintf("- **[%s]** [%s:%d]\n", violation.Severity, violation.File, violation.Line)
					output += fmt.Sprintf("  - **What**: %s\n", violation.Description)
					output += fmt.Sprintf("  - **Why**: %s\n\n", violation.Why)
				}
			}

			if len(result.ScopeResult.Suggestions) > 0 {
				output += "**Suggestions**:\n\n"
				for _, suggestion := range result.ScopeResult.Suggestions {
					output += fmt.Sprintf("  - %s\n", suggestion)
				}
				output += "\n"
			}
		}
	}

	// Overall summary
	output += fmt.Sprintf("### Summary\n\n")
	output += result.Summary
	output += "\n"

	// Actionable recommendation
	if result.ScopeResult != nil && !result.ScopeResult.InScope {
		output += "### ⚠️ Recommendation\n\n"
		output += "Scope violations were detected. Consider:\n"
		output += "1. Removing out-of-scope changes\n"
		output += "2. Updating the specification if these changes are intentional\n"
		output += "3. Re-running the review after addressing violations\n\n"
	} else if result.SpecResult != nil && result.SpecResult.Confidence < 0.7 {
		output += "### ⚠️ Recommendation\n\n"
		output += "Spec confidence is low. Consider clarifying the requirements before proceeding.\n\n"
	} else {
		output += "### ✅ Recommendation\n\n"
		output += "Changes are within scope and align with the specification. Ready to proceed.\n\n"
	}

	return output
}
