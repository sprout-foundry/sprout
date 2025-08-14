package editor

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/utils"
)

// performAutomatedReview performs an LLM-based code review of the combined diff.
func performAutomatedReview(combinedDiff, originalPrompt, processedInstructions string, cfg *config.Config, logger *utils.Logger, revisionID string) error {
	logger.LogProcessStep("Performing automated code review...")

	review, err := llm.GetCodeReview(cfg, combinedDiff, originalPrompt, processedInstructions)
	if err != nil {
		return fmt.Errorf("failed to get code review from LLM: %w", err)
	}

	switch review.Status {
	case "approved":
		logger.LogProcessStep("Code review approved.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		return nil
	case "needs_revision":
		logger.LogProcessStep("Code review requires revisions.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		logger.LogProcessStep("Applying suggested revisions...")

		// The review gives new instructions. We execute them.
		_, fixErr := ProcessCodeGeneration("", review.Instructions+" #WS", cfg, "")
		if fixErr != nil {
			return fmt.Errorf("failed to apply review revisions: %w", fixErr)
		}
		// After applying, the next iteration of validation loop will run.
		// We need to signal a failure to re-validate.
		return fmt.Errorf("revisions applied, re-validating. Feedback: %s", review.Feedback)
	case "rejected":
		logger.LogProcessStep("Code review rejected.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))

		// Only rollback if there are active changes recorded under this revision
		if hasActive, _ := changetracker.HasActiveChangesForRevision(revisionID); hasActive {
			if err := changetracker.RevertChangeByRevisionID(revisionID); err != nil {
				logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", revisionID, err))
				return fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s. New prompt suggestion: %s. Rollback error: %w", review.Feedback, review.NewPrompt, err)
			}
		} else {
			logger.LogProcessStep("No active changes recorded for this revision; skipping rollback.")
		}

		// Check if we've already retried once
		if cfg.RetryAttemptCount >= 1 {
			return fmt.Errorf("changes rejected by automated review after retry. Feedback: %s. New prompt suggestion: %s", review.Feedback, review.NewPrompt)
		}

		// Increment retry attempt count
		cfg.RetryAttemptCount++

		// Automatically retry with the new prompt
		logger.LogProcessStep(fmt.Sprintf("Retrying code generation with new prompt: %s", review.NewPrompt))
		_, retryErr := ProcessCodeGeneration("", review.NewPrompt, cfg, "")
		if retryErr != nil {
			return fmt.Errorf("retry failed: %w. Original feedback: %s. New prompt: %s", retryErr, review.Feedback, review.NewPrompt)
		}

		// If we get here, the retry was successful
		logger.LogProcessStep("Retry successful.")
		return nil
	default:
		return fmt.Errorf("unknown review status from LLM: %s. Full feedback: %s", review.Status, review.Feedback)
	}
}
