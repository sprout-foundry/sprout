package editor

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/utils"
)

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
		// In pre-apply review phase, provide advisory feedback only to avoid loops
		if cfg.PreapplyReview && !cfg.SkipPrompt {
			logger.LogProcessStep("Pre-apply review: advisory only (no auto-fixes applied)")
			return nil
		}
		logger.LogProcessStep("Applying suggested revisions...")

		// The review gives new instructions. We execute them.
		_, fixErr := ProcessCodeGeneration("", review.Instructions, cfg, "")
		if fixErr != nil {
			return fmt.Errorf("failed to apply review revisions: %w", fixErr)
		}
		// After applying, the next iteration of validation loop will run.
		// We need to signal a failure to re-validate.
		return fmt.Errorf("revisions applied, re-validating. Feedback: %s", review.Feedback)
	case "rejected":
		logger.LogProcessStep("Code review rejected.")
		logger.LogProcessStep(fmt.Sprintf("Feedback: %s", review.Feedback))
		// In pre-apply review phase, provide advisory feedback only to avoid loops
		if cfg.PreapplyReview && !cfg.SkipPrompt {
			logger.LogProcessStep("Pre-apply review: advisory only (no rollback/retry)")
			return nil
		}

		// Interactive: Only rollback if there are active changes recorded under this revision
		if hasActive, _ := changetracker.HasActiveChangesForRevision(revisionID); hasActive {
			if err := changetracker.RevertChangeByRevisionID(revisionID); err != nil {
				logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", revisionID, err))
				return fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s. New prompt suggestion: %s. Rollback error: %w", review.Feedback, review.NewPrompt, err)
			}
		} else {
			logger.LogProcessStep("No active changes recorded for this revision; skipping rollback.")
		}

		// Bounded retries with refined prompts
		maxRetries := 2
		for attempt := 1; attempt <= maxRetries; attempt++ {
			cfg.RetryAttemptCount = attempt
			refined := review.NewPrompt
			if strings.TrimSpace(refined) == "" {
				// Synthesize a refined prompt using feedback + original
				refined = fmt.Sprintf("Refine the previous change. Keep existing functionality intact. Address review feedback: %s. Original intent: %s.", review.Feedback, originalPrompt)
			}
			logger.LogProcessStep(fmt.Sprintf("Retrying code generation (%d/%d) with new prompt: %s", attempt, maxRetries, refined))
			if _, retryErr := ProcessCodeGeneration("", refined, cfg, ""); retryErr != nil {
				logger.LogProcessStep(fmt.Sprintf("Retry %d failed: %v", attempt, retryErr))
				continue
			}
			logger.LogProcessStep("Retry successful.")
			return fmt.Errorf("retry applied, re-validating. Feedback: %s", review.Feedback)
		}

		return fmt.Errorf("changes rejected after %d retries. Feedback: %s. Suggested prompt: %s", maxRetries, review.Feedback, review.NewPrompt)
	default:
		return fmt.Errorf("unknown review status from LLM: %s. Full feedback: %s", review.Status, review.Feedback)
	}
}
