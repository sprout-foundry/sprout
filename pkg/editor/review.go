package editor

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

func performAutomatedReview(combinedDiff, originalPrompt, processedInstructions string, cfg *config.Config, logger *utils.Logger, revisionID string) error {
	// Create the unified code review service
	service := codereview.NewCodeReviewService(cfg, logger)

	// Create the review context
	ctx := &codereview.ReviewContext{
		Diff:                  combinedDiff,
		OriginalPrompt:        originalPrompt,
		ProcessedInstructions: processedInstructions,
		RevisionID:            revisionID,
		Config:                cfg,
		Logger:                logger,
	}

	// Create review options for automated review
	opts := &codereview.ReviewOptions{
		Type:             codereview.AutomatedReview,
		SkipPrompt:       cfg.SkipPrompt,
		PreapplyReview:   cfg.PreapplyReview,
		MaxRetries:       2,
		AutoApplyFixes:   true,
		RollbackOnReject: true,
	}

	// Perform the review using the unified service
	_, err := service.PerformReview(ctx, opts)
	if err != nil {
		// Check if this is a signal to re-validate (which is expected behavior)
		if strings.Contains(err.Error(), "re-validating") || strings.Contains(err.Error(), "review instructions need to be applied") {
			return err
		}
		return fmt.Errorf("automated code review failed: %w", err)
	}

	return nil
}
