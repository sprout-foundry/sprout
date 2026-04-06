package codereview

import (
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/history"
	"github.com/alantheprice/ledit/pkg/types"
)

// PerformReview performs a code review based on the provided context and options
func (s *CodeReviewService) PerformReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {

	// Validate input parameters
	if ctx == nil {
		return nil, fmt.Errorf("review context cannot be nil")
	}
	if opts == nil {
		return nil, fmt.Errorf("review options cannot be nil")
	}
	if ctx.Diff == "" {
		return nil, fmt.Errorf("no diff content provided for review")
	}

	// Try to load existing context if session ID is provided
	var existingCtx *ReviewContext
	if ctx.SessionID != "" {
		if storedCtx, exists := s.getStoredContext(ctx.SessionID); exists {
			existingCtx = storedCtx
		}
	}

	// Merge with existing context or initialize new history
	if existingCtx != nil {
		// Update existing context with new information
		existingCtx.Diff = ctx.Diff
		existingCtx.OriginalPrompt = ctx.OriginalPrompt
		existingCtx.ProcessedInstructions = ctx.ProcessedInstructions
		existingCtx.RevisionID = ctx.RevisionID
		existingCtx.Config = ctx.Config
		existingCtx.Logger = ctx.Logger
		existingCtx.CurrentIteration = ctx.CurrentIteration
		existingCtx.FullFileContext = ctx.FullFileContext
		ctx = existingCtx
	} else {
		// Initialize review history if not provided
		if ctx.History == nil {
			ctx.History = s.initializeReviewHistory(ctx)
		}
	}

	// Check iteration limits
	if s.hasExceededIterationLimit(ctx) {
		return s.handleIterationLimitExceeded(ctx)
	}

	// Check for convergence
	if s.reviewConfig.EnableConvergenceDetection && s.hasConverged(ctx) {
		return s.handleConvergence(ctx)
	}

	var result *types.CodeReviewResult
	var err error

	// Only support staged review now
	if opts.Type != StagedReview {
		return nil, fmt.Errorf("only staged review type is supported")
	}

	result, err = s.performStagedReview(ctx)

	if err != nil {
		return nil, fmt.Errorf("failed to perform code review: %w", err)
	}

	// Record the iteration
	s.recordReviewIteration(ctx, result, ctx.Diff)

	// Store the updated context for future iterations
	s.storeContext(ctx)

	// Handle the review result based on options
	return s.handleReviewResult(result, ctx, opts)
}

// PerformAgenticReview performs a deep evidence-focused code review.
func (s *CodeReviewService) PerformAgenticReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	if ctx == nil {
		return nil, fmt.Errorf("review context cannot be nil")
	}
	if opts == nil {
		return nil, fmt.Errorf("review options cannot be nil")
	}
	if strings.TrimSpace(ctx.Diff) == "" {
		return nil, fmt.Errorf("no diff content provided for deep review")
	}
	if ctx.History == nil {
		ctx.History = s.initializeReviewHistory(ctx)
	}

	result, err := s.performDeepAgentBasedCodeReview(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to perform deep review: %w", err)
	}

	s.recordReviewIteration(ctx, result, ctx.Diff)

	return s.handleReviewResult(result, ctx, opts)
}

// initializeReviewHistory initializes the review history for a new context
func (s *CodeReviewService) initializeReviewHistory(ctx *ReviewContext) *ReviewHistory {
	now := time.Now()
	sessionID := s.generateSessionID(ctx)

	return &ReviewHistory{
		SessionID:       sessionID,
		Iterations:      make([]ReviewIteration, 0),
		OriginalPrompt:  ctx.OriginalPrompt,
		OriginalContent: ctx.Diff,
		StartTime:       now,
		LastUpdate:      now,
		Converged:       false,
		FinalStatus:     "",
	}
}

// generateSessionID generates a unique session ID for the review context
func (s *CodeReviewService) generateSessionID(ctx *ReviewContext) string {
	// Use MD5 hash of key context information
	input := fmt.Sprintf("%s-%s-%d", ctx.OriginalPrompt, ctx.Diff, time.Now().UnixNano())
	hash := md5.Sum([]byte(input))
	return fmt.Sprintf("%x", hash)
}

// hasExceededIterationLimit checks if the review has exceeded the maximum iteration limit
func (s *CodeReviewService) hasExceededIterationLimit(ctx *ReviewContext) bool {
	return len(ctx.History.Iterations) >= s.reviewConfig.MaxIterations
}

// handleIterationLimitExceeded handles the case when iteration limit is exceeded
func (s *CodeReviewService) handleIterationLimitExceeded(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	s.logger.LogProcessStep(fmt.Sprintf("Review iteration limit exceeded (%d/%d). Applying fallback strategy.",
		len(ctx.History.Iterations), s.reviewConfig.MaxIterations))

	ctx.History.Converged = true
	ctx.History.FinalStatus = "fallback"

	// Return the most recent approved result or suggest fallback
	if len(ctx.History.Iterations) > 0 {
		for i := len(ctx.History.Iterations) - 1; i >= 0; i-- {
			iteration := ctx.History.Iterations[i]
			if iteration.ReviewResult.Status == "approved" {
				s.logger.LogProcessStep("Using most recent approved review result as fallback.")
				return iteration.ReviewResult, nil
			}
		}
	}

	// If no approved result found, create a fallback result
	return &types.CodeReviewResult{
		Status:   "needs_revision",
		Feedback: fmt.Sprintf("Review process exceeded maximum iterations (%d). Manual intervention required. Consider simplifying the original request or breaking it into smaller parts.", s.reviewConfig.MaxIterations),
	}, nil
}

// hasConverged checks if the review process has converged (similar iterations)
func (s *CodeReviewService) hasConverged(ctx *ReviewContext) bool {
	if len(ctx.History.Iterations) < 3 {
		return false
	}

	// Check if the last few iterations have similar feedback
	recentIterations := ctx.History.Iterations[len(ctx.History.Iterations)-3:]
	if len(recentIterations) < 2 {
		return false
	}

	// Compare feedback similarity
	for i := 0; i < len(recentIterations)-1; i++ {
		for j := i + 1; j < len(recentIterations); j++ {
			similarity := s.calculateSimilarity(
				recentIterations[i].ReviewResult.Feedback,
				recentIterations[j].ReviewResult.Feedback,
			)
			if similarity >= s.reviewConfig.SimilarityThreshold {
				return true
			}
		}
	}

	return false
}

// handleConvergence handles the case when the review process has converged
func (s *CodeReviewService) handleConvergence(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	s.logger.LogProcessStep("Review process has converged. Similar feedback detected in recent iterations.")

	ctx.History.Converged = true
	ctx.History.FinalStatus = "converged"

	// Return the most recent result
	if len(ctx.History.Iterations) > 0 {
		latest := ctx.History.Iterations[len(ctx.History.Iterations)-1]
		return latest.ReviewResult, nil
	}

	return &types.CodeReviewResult{
		Status:   "needs_revision",
		Feedback: "Review process converged but no valid result found. Manual review required.",
	}, nil
}

// recordReviewIteration records a review iteration in the history
func (s *CodeReviewService) recordReviewIteration(ctx *ReviewContext, result *types.CodeReviewResult, originalDiff string) {
	iteration := ReviewIteration{
		IterationNumber: len(ctx.History.Iterations) + 1,
		OriginalDiff:    originalDiff,
		ReviewResult:    result,
		AppliedChanges:  false, // This would be set when changes are actually applied
		Timestamp:       time.Now(),
		ContentHash:     s.calculateContentHash(originalDiff),
	}

	ctx.History.Iterations = append(ctx.History.Iterations, iteration)
	ctx.History.LastUpdate = time.Now()
	ctx.CurrentIteration = iteration.IterationNumber
}

// handleReviewResult processes the review result based on the review options
func (s *CodeReviewService) handleReviewResult(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	switch result.Status {
	case "approved":
		ctx.History.Converged = true
		ctx.History.FinalStatus = "approved"
		return result, nil

	case "needs_revision":
		return s.handleNeedsRevision(result, ctx, opts)

	case "rejected":
		return s.handleRejected(result, ctx, opts)

	default:
		return nil, fmt.Errorf("unknown review status: %s", result.Status)
	}
}

// handleNeedsRevision handles the case where the code review requires revisions
func (s *CodeReviewService) handleNeedsRevision(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	// Check if we're approaching iteration limits
	if ctx.CurrentIteration >= s.reviewConfig.MaxIterations-1 {
		// If we have a previous approved result, prefer it over continuing
		if s.hasPreviousApprovedResult(ctx) {
			return s.getMostRecentApprovedResult(ctx)
		}
	}

	// For pre-apply review phase, provide advisory feedback only to avoid loops
	if opts.PreapplyReview && !opts.SkipPrompt {
		return result, nil
	}

	// Removed automated review logic - no longer supported
	// Only staged reviews are supported now

	return result, nil
}

// handleRejected handles the case where the code review is rejected
func (s *CodeReviewService) handleRejected(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	// For pre-apply review phase, provide advisory feedback only
	if opts.PreapplyReview && !opts.SkipPrompt {
		return result, nil
	}

	// Rollback changes if enabled and we have a revision ID
	if opts.RollbackOnReject && ctx.RevisionID != "" {
		if hasActive, _ := history.HasActiveChangesForRevision(ctx.RevisionID); hasActive {
			if err := history.RevertChangeByRevisionID(ctx.RevisionID); err != nil {
				rollbackErr := fmt.Errorf("failed to rollback changes for revision %s: %w", ctx.RevisionID, err)
				s.logger.LogError(rollbackErr)
				return nil, fmt.Errorf("changes rejected by automated review, but rollback failed: %w. Feedback: %s", rollbackErr, result.Feedback)
			}
		} else {
			s.logger.LogProcessStep("No active changes recorded for this revision; skipping rollback.")
		}
	}

	// Retries are no longer supported - removed with automated review

	return result, nil
}

// hasPreviousApprovedResult checks if there are any previous approved results in history
func (s *CodeReviewService) hasPreviousApprovedResult(ctx *ReviewContext) bool {
	for _, iteration := range ctx.History.Iterations {
		if iteration.ReviewResult.Status == "approved" {
			return true
		}
	}
	return false
}

// getMostRecentApprovedResult returns the most recent approved result from history
func (s *CodeReviewService) getMostRecentApprovedResult(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	for i := len(ctx.History.Iterations) - 1; i >= 0; i-- {
		iteration := ctx.History.Iterations[i]
		if iteration.ReviewResult.Status == "approved" {
			s.logger.LogProcessStep("Returning to previously approved result.")
			return iteration.ReviewResult, nil
		}
	}
	return nil, fmt.Errorf("no approved result found")
}
