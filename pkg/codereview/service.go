package codereview

import (
	"crypto/md5"
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/orchestration/types"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ReviewContext represents the context for a code review request
type ReviewContext struct {
	Diff                  string // The code diff to review
	OriginalPrompt        string // The original user prompt (for automated reviews)
	ProcessedInstructions string // Processed instructions (for automated reviews)
	RevisionID            string // Revision ID for change tracking
	Config                *config.Config
	Logger                *utils.Logger
	History               *ReviewHistory // Review history for this context
	SessionID             string         // Unique session identifier
	CurrentIteration      int            // Current iteration number
}

// ReviewType defines the type of code review being performed
type ReviewType int

const (
	AutomatedReview ReviewType = iota // Used during code generation workflow
	StagedReview                      // Used for reviewing Git staged changes
)

// ReviewOptions contains options for the code review
type ReviewOptions struct {
	Type             ReviewType
	SkipPrompt       bool
	PreapplyReview   bool
	MaxRetries       int
	AutoApplyFixes   bool // Whether to automatically apply fixes for automated reviews
	RollbackOnReject bool // Whether to rollback changes when review is rejected
}

// CodeReviewService provides a unified interface for code review operations
type CodeReviewService struct {
	config       *config.Config
	logger       *utils.Logger
	reviewConfig *ReviewConfiguration
}

// NewCodeReviewService creates a new code review service instance
func NewCodeReviewService(cfg *config.Config, logger *utils.Logger) *CodeReviewService {
	return &CodeReviewService{
		config:       cfg,
		logger:       logger,
		reviewConfig: DefaultReviewConfiguration(),
	}
}

// NewCodeReviewServiceWithConfig creates a new code review service instance with custom configuration
func NewCodeReviewServiceWithConfig(cfg *config.Config, logger *utils.Logger, reviewConfig *ReviewConfiguration) *CodeReviewService {
	return &CodeReviewService{
		config:       cfg,
		logger:       logger,
		reviewConfig: reviewConfig,
	}
}

// PerformReview performs a code review based on the provided context and options
func (s *CodeReviewService) PerformReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	s.logger.LogProcessStep("Performing code review...")

	// Initialize review history if not provided
	if ctx.History == nil {
		ctx.History = s.initializeReviewHistory(ctx)
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

	switch opts.Type {
	case AutomatedReview:
		result, err = s.performAutomatedReview(ctx)
	case StagedReview:
		result, err = s.performStagedReview(ctx)
	default:
		return nil, fmt.Errorf("unknown review type: %d", opts.Type)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to perform code review: %w", err)
	}

	// Record the iteration
	s.recordReviewIteration(ctx, result, ctx.Diff)

	// Handle the review result based on options
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
	if len(ctx.History.Iterations) < 2 {
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

// calculateContentHash calculates a hash of the content for change detection
func (s *CodeReviewService) calculateContentHash(content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", hash)
}

// calculateSimilarity calculates the similarity between two strings using Jaccard similarity
func (s *CodeReviewService) calculateSimilarity(str1, str2 string) float64 {
	// Normalize strings by converting to lowercase and splitting into words
	words1 := strings.Fields(strings.ToLower(str1))
	words2 := strings.Fields(strings.ToLower(str2))

	// Handle empty strings
	if len(words1) == 0 && len(words2) == 0 {
		return 1.0
	}
	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// Create sets of unique words
	set1 := make(map[string]bool)
	set2 := make(map[string]bool)

	for _, word := range words1 {
		// Remove punctuation and normalize
		word = strings.Trim(word, ".,!?;:")
		if word != "" {
			set1[word] = true
		}
	}
	for _, word := range words2 {
		word = strings.Trim(word, ".,!?;:")
		if word != "" {
			set2[word] = true
		}
	}

	// Calculate intersection
	intersection := 0
	for word := range set1 {
		if set2[word] {
			intersection++
		}
	}

	// Calculate union
	union := len(set1) + len(set2) - intersection

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// performAutomatedReview handles automated code reviews during code generation workflow
func (s *CodeReviewService) performAutomatedReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	// Use the structured JSON-based review for automated workflow
	return llm.GetCodeReview(s.config, ctx.Diff, ctx.OriginalPrompt, ctx.ProcessedInstructions)
}

// performStagedReview handles reviews of Git staged changes
func (s *CodeReviewService) performStagedReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	// Use the human-readable review for staged changes
	reviewPrompt := prompts.CodeReviewStagedPrompt()
	return llm.GetStagedCodeReview(s.config, ctx.Diff, reviewPrompt, "")
}

// handleReviewResult processes the review result based on the review options
func (s *CodeReviewService) handleReviewResult(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	switch result.Status {
	case "approved":
		s.logger.LogProcessStep("Code review approved.")
		s.logger.LogProcessStep(fmt.Sprintf("Feedback: %s", result.Feedback))
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
	s.logger.LogProcessStep(fmt.Sprintf("Code review requires revisions (iteration %d/%d).",
		ctx.CurrentIteration, s.reviewConfig.MaxIterations))
	s.logger.LogProcessStep(fmt.Sprintf("Feedback: %s", result.Feedback))

	// Check if we're approaching iteration limits
	if ctx.CurrentIteration >= s.reviewConfig.MaxIterations-1 {
		s.logger.LogProcessStep("Approaching iteration limit. Evaluating whether to continue...")

		// If we have a previous approved result, prefer it over continuing
		if s.hasPreviousApprovedResult(ctx) {
			s.logger.LogProcessStep("Previous approved result found. Using that instead of continuing iterations.")
			return s.getMostRecentApprovedResult(ctx)
		}
	}

	// For pre-apply review phase, provide advisory feedback only to avoid loops
	if opts.PreapplyReview && !opts.SkipPrompt {
		s.logger.LogProcessStep("Pre-apply review: advisory only (no auto-fixes applied)")
		return result, nil
	}

	// For automated reviews, apply fixes if enabled and we haven't exceeded limits
	if opts.Type == AutomatedReview && opts.AutoApplyFixes && result.Instructions != "" {
		// Check if the instructions are substantially different from previous attempts
		if s.areInstructionsDifferentFromPrevious(ctx, result.Instructions) {
			s.logger.LogProcessStep("Applying suggested revisions...")
			return nil, s.applyReviewInstructions(result.Instructions, result.Feedback)
		} else {
			s.logger.LogProcessStep("Instructions similar to previous attempts. Avoiding redundant fixes.")
			return result, nil
		}
	}

	return result, nil
}

// handleRejected handles the case where the code review is rejected
func (s *CodeReviewService) handleRejected(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	s.logger.LogProcessStep("Code review rejected.")
	s.logger.LogProcessStep(fmt.Sprintf("Feedback: %s", result.Feedback))

	// For pre-apply review phase, provide advisory feedback only
	if opts.PreapplyReview && !opts.SkipPrompt {
		s.logger.LogProcessStep("Pre-apply review: advisory only (no rollback/retry)")
		return result, nil
	}

	// Rollback changes if enabled and we have a revision ID
	if opts.RollbackOnReject && ctx.RevisionID != "" {
		if hasActive, _ := changetracker.HasActiveChangesForRevision(ctx.RevisionID); hasActive {
			if err := changetracker.RevertChangeByRevisionID(ctx.RevisionID); err != nil {
				s.logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", ctx.RevisionID, err))
				return nil, fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s", result.Feedback)
			}
		} else {
			s.logger.LogProcessStep("No active changes recorded for this revision; skipping rollback.")
		}
	}

	// Attempt retries if enabled
	if opts.MaxRetries > 0 && opts.Type == AutomatedReview {
		return nil, s.attemptRetry(result, ctx, opts)
	}

	return result, nil
}

// applyReviewInstructions applies the review instructions for automated reviews
func (s *CodeReviewService) applyReviewInstructions(instructions, feedback string) error {
	// This function would delegate to the code generation process
	// For now, we'll return an error to signal that revisions need to be applied
	return fmt.Errorf("review instructions need to be applied: %s", instructions)
}

// attemptRetry attempts to retry the code generation with refined prompts
func (s *CodeReviewService) attemptRetry(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) error {
	maxRetries := opts.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 2 // Default to 2 retries
	}

	for attempt := 1; attempt <= maxRetries; attempt++ {
		s.config.RetryAttemptCount = attempt
		refined := result.NewPrompt
		if strings.TrimSpace(refined) == "" {
			// Synthesize a refined prompt using feedback + original
			refined = fmt.Sprintf("Refine the previous change. Keep existing functionality intact. Address review feedback: %s. Original intent: %s.", result.Feedback, ctx.OriginalPrompt)
		}
		s.logger.LogProcessStep(fmt.Sprintf("Retrying code generation (%d/%d) with new prompt: %s", attempt, maxRetries, refined))

		// This would delegate to the code generation process
		// For now, we'll just log the attempt
		s.logger.LogProcessStep("Retry logic would be implemented here")

		// If retry was successful, we'd break here
		// For now, we'll assume it failed and continue to next attempt
	}

	return fmt.Errorf("changes rejected after %d retries. Feedback: %s. Suggested prompt: %s", maxRetries, result.Feedback, result.NewPrompt)
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

// areInstructionsDifferentFromPrevious checks if the current instructions differ from previous ones
func (s *CodeReviewService) areInstructionsDifferentFromPrevious(ctx *ReviewContext, currentInstructions string) bool {
	if len(ctx.History.Iterations) == 0 {
		return true // First iteration, no previous instructions to compare
	}

	// Compare with the last few iterations
	recentIterations := ctx.History.Iterations[max(0, len(ctx.History.Iterations)-3):]

	for _, iteration := range recentIterations {
		if iteration.ReviewResult.Instructions != "" {
			similarity := s.calculateSimilarity(currentInstructions, iteration.ReviewResult.Instructions)
			if similarity >= s.reviewConfig.SimilarityThreshold {
				return false // Instructions are too similar, don't apply again
			}
		}
	}

	return true // Instructions are different enough to try
}

// max returns the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
