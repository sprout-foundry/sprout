package editor

import (
	"crypto/md5"
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/utils"
)

func performAutomatedReview(combinedDiff, originalPrompt, processedInstructions string, cfg *config.Config, logger *utils.Logger, revisionID string) error {
	// Create the unified code review service
	service := codereview.NewCodeReviewService(cfg, logger)

	// Generate a session ID based on the revision or create a new one
	sessionID := revisionID
	if sessionID == "" {
		// Use a hash of the original prompt and diff as session ID if no revision ID
		hashInput := originalPrompt + combinedDiff
		sessionID = fmt.Sprintf("%x", md5.Sum([]byte(hashInput)))
	}

	// Create the review context with iteration tracking
	ctx := &codereview.ReviewContext{
		Diff:                  combinedDiff,
		OriginalPrompt:        originalPrompt,
		ProcessedInstructions: processedInstructions,
		RevisionID:            revisionID,
		Config:                cfg,
		Logger:                logger,
		CurrentIteration:      1, // Start at iteration 1
		SessionID:             sessionID,
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

	// Implement iteration loop to handle retries
	maxReviewIterations := 5 // Match the service's MaxIterations default
	for iteration := 1; iteration <= maxReviewIterations; iteration++ {
		ctx.CurrentIteration = iteration
		logger.LogProcessStep(fmt.Sprintf("Starting code review iteration %d/%d", iteration, maxReviewIterations))

		// Perform the review using the unified service
		result, err := service.PerformReview(ctx, opts)
		if err != nil {
			// Check if this is a retry request error
			if retryRequest, ok := err.(*codereview.RetryRequestError); ok {
				logger.LogProcessStep(fmt.Sprintf("Code review iteration %d requested retry with refined prompt: %s", iteration, retryRequest.Feedback))

				// If this is the last iteration, return the retry request
				if iteration >= maxReviewIterations {
					logger.LogProcessStep(fmt.Sprintf("Maximum iterations reached (%d). Returning retry request to caller.", maxReviewIterations))
					return retryRequest
				}

				// Update the context with refined prompt for next iteration
				ctx.OriginalPrompt = retryRequest.RefinedPrompt
				ctx.Diff = combinedDiff // Keep the same diff for retry

				logger.LogProcessStep(fmt.Sprintf("Retrying with refined prompt (iteration %d)", iteration+1))
				continue // Continue to next iteration
			}

			// Check if this is a signal to re-validate (which is expected behavior)
			if strings.Contains(err.Error(), "re-validating") || strings.Contains(err.Error(), "review instructions need to be applied") {
				return err
			}

			// Other errors are fatal
			return fmt.Errorf("automated code review failed on iteration %d: %w", iteration, err)
		}

		// Review completed successfully
		if result != nil {
			switch result.Status {
			case "approved":
				logger.LogProcessStep(fmt.Sprintf("Code review approved on iteration %d", iteration))
				return nil

			case "needs_revision":
				logger.LogProcessStep(fmt.Sprintf("Code review iteration %d requires revisions but no retry requested", iteration))
				// If no retry was requested but revisions are needed, we might need to return the result
				if iteration >= maxReviewIterations {
					logger.LogProcessStep(fmt.Sprintf("Maximum iterations reached (%d). Returning revision request.", maxReviewIterations))
					return fmt.Errorf("code review requires revisions after %d iterations: %s", maxReviewIterations, result.Feedback)
				}
				// Continue to next iteration with same context
				continue

			case "rejected":
				logger.LogProcessStep(fmt.Sprintf("Code review rejected on iteration %d", iteration))
				return fmt.Errorf("code review rejected: %s", result.Feedback)

			default:
				logger.LogProcessStep(fmt.Sprintf("Unknown review status on iteration %d: %s", iteration, result.Status))
				return fmt.Errorf("unknown review status: %s", result.Status)
			}
		}

		// If we get here, the result was nil - this shouldn't happen but handle gracefully
		logger.LogProcessStep(fmt.Sprintf("Review iteration %d completed with nil result", iteration))
		// Continue to next iteration instead of returning nil immediately
		continue
	}

	// If we exit the loop, we've exceeded maximum iterations
	return fmt.Errorf("code review exceeded maximum iterations (%d) without resolution", maxReviewIterations)
}
