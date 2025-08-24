package codereview

import (
	"crypto/md5"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/changetracker"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// RetryRequestError indicates that a retry is needed with a refined prompt
type RetryRequestError struct {
	RefinedPrompt string
	Feedback      string
}

func (e *RetryRequestError) Error() string {
	return fmt.Sprintf("code review requires retry with refined prompt: %s", e.Feedback)
}

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
	FullFileContext       string         // Full file content for patch resolution context
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
	contextStore map[string]*ReviewContext // Store contexts by session ID for persistence
}

// NewCodeReviewService creates a new code review service instance
func NewCodeReviewService(cfg *config.Config, logger *utils.Logger) *CodeReviewService {
	return &CodeReviewService{
		config:       cfg,
		logger:       logger,
		reviewConfig: DefaultReviewConfiguration(),
		contextStore: make(map[string]*ReviewContext),
	}
}

// NewCodeReviewServiceWithConfig creates a new code review service instance with custom configuration
func NewCodeReviewServiceWithConfig(cfg *config.Config, logger *utils.Logger, reviewConfig *ReviewConfiguration) *CodeReviewService {
	return &CodeReviewService{
		config:       cfg,
		logger:       logger,
		reviewConfig: reviewConfig,
		contextStore: make(map[string]*ReviewContext),
	}
}

// storeContext stores a review context for later retrieval
func (s *CodeReviewService) storeContext(ctx *ReviewContext) {
	if ctx.SessionID != "" {
		s.contextStore[ctx.SessionID] = ctx
	}
}

// getStoredContext retrieves a previously stored context by session ID
func (s *CodeReviewService) getStoredContext(sessionID string) (*ReviewContext, bool) {
	ctx, exists := s.contextStore[sessionID]
	return ctx, exists
}

// PerformReview performs a code review based on the provided context and options
func (s *CodeReviewService) PerformReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	s.logger.LogProcessStep("Performing code review...")

	// Try to load existing context if session ID is provided
	var existingCtx *ReviewContext
	if ctx.SessionID != "" {
		if storedCtx, exists := s.getStoredContext(ctx.SessionID); exists {
			existingCtx = storedCtx
			s.logger.LogProcessStep(fmt.Sprintf("Loaded existing review context for session %s", ctx.SessionID))
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

	// Store the updated context for future iterations
	s.storeContext(ctx)

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
	// The GetCodeReview function needs to be updated to accept full file context
	// For now, we'll pass the processed instructions as a substitute
	return llm.GetCodeReview(s.config, ctx.Diff, ctx.OriginalPrompt, ctx.FullFileContext)
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
	if opts.Type == AutomatedReview && opts.AutoApplyFixes && result.PatchResolution != nil && !result.PatchResolution.IsEmpty() {
		s.logger.LogProcessStep("Applying patch resolution...")
		return nil, s.applyPatchToContent(result.PatchResolution, result.Feedback)
	}

	// If detailed guidance is provided but no patch resolution, attempt retry with the feedback
	if opts.Type == AutomatedReview && opts.AutoApplyFixes && (result.DetailedGuidance != "" || result.Feedback != "") {
		s.logger.LogProcessStep("No direct patch resolution provided. Attempting retry with review feedback...")

		// Attempt to retry the code generation with refined prompt based on feedback
		if retryErr := s.attemptRetryForNeedsRevision(result, ctx, opts); retryErr != nil {
			// Check if this is a retry request error
			if retryRequest, ok := retryErr.(*RetryRequestError); ok {
				s.logger.LogProcessStep("Retry requested with refined prompt. Returning to caller for re-generation.")
				// Return the retry request error to signal the caller to retry with the refined prompt
				return nil, retryRequest
			} else {
				s.logger.LogProcessStep(fmt.Sprintf("Unexpected retry error: %v", retryErr))
				// Continue with the original result if retry fails
			}
		} else {
			s.logger.LogProcessStep("Retry completed. The code generation process should use the refined prompt for the next iteration.")
			// Mark that we've attempted to address the issues via retry
			result.Feedback += " (Retry attempted with refined prompt)"
		}
	}

	// If detailed guidance is provided but auto-apply is disabled, log it for manual use
	if opts.Type == AutomatedReview && result.DetailedGuidance != "" && !opts.AutoApplyFixes {
		s.logger.LogProcessStep(fmt.Sprintf("Code review guidance: %s", result.DetailedGuidance))
		s.logger.LogProcessStep("Auto-apply fixes disabled. Guidance provided for manual review.")
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

// createBackup creates a backup of a file before making changes
func (s *CodeReviewService) createBackup(filePath string) (string, error) {
	// Create backup directory if it doesn't exist
	backupDir := filepath.Join(".ledit", "backups")
	if err := os.MkdirAll(backupDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create backup directory: %w", err)
	}

	// Create backup filename with timestamp
	timestamp := time.Now().Format("20060102_150405")
	filename := filepath.Base(filePath)
	backupPath := filepath.Join(backupDir, fmt.Sprintf("%s.%s.backup", filename, timestamp))

	// Copy file to backup
	src, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open source file for backup: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(backupPath)
	if err != nil {
		return "", fmt.Errorf("failed to create backup file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return "", fmt.Errorf("failed to copy file to backup: %w", err)
	}

	s.logger.LogProcessStep(fmt.Sprintf("Created backup: %s -> %s", filePath, backupPath))
	return backupPath, nil
}

// restoreFromBackup restores a file from its backup
func (s *CodeReviewService) restoreFromBackup(backupPath, originalPath string) error {
	// Check if backup exists
	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return fmt.Errorf("backup file does not exist: %s", backupPath)
	}

	// Copy backup back to original location
	src, err := os.Open(backupPath)
	if err != nil {
		return fmt.Errorf("failed to open backup file: %w", err)
	}
	defer src.Close()

	dst, err := os.Create(originalPath)
	if err != nil {
		return fmt.Errorf("failed to create destination file: %w", err)
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return fmt.Errorf("failed to restore from backup: %w", err)
	}

	s.logger.LogProcessStep(fmt.Sprintf("Restored from backup: %s -> %s", backupPath, originalPath))
	return nil
}

// listBackups lists available backups for a file
func (s *CodeReviewService) listBackups(filePath string) ([]string, error) {
	backupDir := filepath.Join(".ledit", "backups")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil, nil // No backup directory exists
	}

	filename := filepath.Base(filePath)
	pattern := fmt.Sprintf("%s.*.backup", filename)

	matches, err := filepath.Glob(filepath.Join(backupDir, pattern))
	if err != nil {
		return nil, fmt.Errorf("failed to list backups: %w", err)
	}

	return matches, nil
}

// cleanupOldBackups removes old backup files to prevent backup directory from growing too large
func (s *CodeReviewService) cleanupOldBackups(maxBackups int) error {
	backupDir := filepath.Join(".ledit", "backups")
	if _, err := os.Stat(backupDir); os.IsNotExist(err) {
		return nil // No backup directory exists
	}

	files, err := os.ReadDir(backupDir)
	if err != nil {
		return fmt.Errorf("failed to read backup directory: %w", err)
	}

	// Sort files by modification time (newest first)
	var backupFiles []os.DirEntry
	for _, file := range files {
		if strings.HasSuffix(file.Name(), ".backup") {
			backupFiles = append(backupFiles, file)
		}
	}

	// If we have more backups than allowed, remove the oldest ones
	if len(backupFiles) > maxBackups {
		toRemove := len(backupFiles) - maxBackups
		for i := len(backupFiles) - 1; i >= len(backupFiles)-toRemove; i-- {
			filePath := filepath.Join(backupDir, backupFiles[i].Name())
			if err := os.Remove(filePath); err != nil {
				s.logger.LogProcessStep(fmt.Sprintf("Warning: Failed to remove old backup %s: %v", filePath, err))
			} else {
				s.logger.LogProcessStep(fmt.Sprintf("Removed old backup: %s", filePath))
			}
		}
	}

	return nil
}

// applyPatchToContent applies the patch resolution content directly
func (s *CodeReviewService) applyPatchToContent(patchResolution *types.PatchResolution, feedback string) error {
	if patchResolution == nil {
		return fmt.Errorf("patch resolution is nil")
	}

	// Handle multi-file patches
	if len(patchResolution.MultiFile) > 0 {
		s.logger.LogProcessStep(fmt.Sprintf("Applying multi-file patch with %d files", len(patchResolution.MultiFile)))
		for filePath, _ := range patchResolution.MultiFile {
			s.logger.LogProcessStep(fmt.Sprintf("Would apply patch to: %s", filePath))
		}
		// For now, return an error to signal that multi-file patches need to be applied
		return fmt.Errorf("multi-file patch resolution needs to be applied: %d files to update", len(patchResolution.MultiFile))
	}

	// Handle single file patches (backward compatibility)
	if patchResolution.SingleFile != "" {
		s.logger.LogProcessStep("Applying single-file patch")
		// For now, return an error to signal that the patch needs to be applied
		return fmt.Errorf("single-file patch resolution needs to be applied: %d characters", len(patchResolution.SingleFile))
	}

	return fmt.Errorf("patch resolution is empty")
}

// validatePatchContent validates the patch resolution content
func (s *CodeReviewService) validatePatchContent(content string) error {
	_ = content // Suppress unused parameter warning for now
	// Check for extremely short content
	if len(strings.TrimSpace(content)) < 5 {
		return fmt.Errorf("patch content is suspiciously short (%d characters)", len(content))
	}

	// Check for content that looks like instructions rather than actual code
	contentLower := strings.ToLower(content)
	if strings.Contains(contentLower, "replace the") && len(content) < 50 {
		return fmt.Errorf("patch content appears to be replacement instructions rather than actual code")
	}

	// Check for basic code structure indicators
	hasCodeIndicators := strings.Contains(content, "package") ||
		strings.Contains(content, "func") ||
		strings.Contains(content, "import") ||
		strings.Contains(content, "var") ||
		strings.Contains(content, "type") ||
		strings.Contains(content, "const")

	if !hasCodeIndicators && len(content) > 20 {
		s.logger.LogProcessStep("Warning: Patch content doesn't appear to contain typical Go code structures")
	}

	// Check for balanced braces/brackets
	braceCount := strings.Count(content, "{") - strings.Count(content, "}")
	bracketCount := strings.Count(content, "[") - strings.Count(content, "]")
	parenCount := strings.Count(content, "(") - strings.Count(content, ")")

	if braceCount != 0 || bracketCount != 0 || parenCount != 0 {
		s.logger.LogProcessStep(fmt.Sprintf("Warning: Patch content has unbalanced delimiters (braces: %d, brackets: %d, parens: %d)",
			braceCount, bracketCount, parenCount))
	}

	return nil
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

// createRefinedPromptForRetry creates a refined prompt for retry attempts when revisions are needed
func (s *CodeReviewService) createRefinedPromptForRetry(result *types.CodeReviewResult, ctx *ReviewContext) string {
	// Use the suggested new prompt if available
	if strings.TrimSpace(result.NewPrompt) != "" {
		return result.NewPrompt
	}

	// Create a refined prompt using feedback and detailed guidance
	var promptParts []string

	// Start with the original intent
	if strings.TrimSpace(ctx.OriginalPrompt) != "" {
		promptParts = append(promptParts, fmt.Sprintf("Original request: %s", ctx.OriginalPrompt))
	}

	// Add feedback
	if strings.TrimSpace(result.Feedback) != "" {
		promptParts = append(promptParts, fmt.Sprintf("Review feedback to address: %s", result.Feedback))
	}

	// Add detailed guidance if available
	if strings.TrimSpace(result.DetailedGuidance) != "" {
		promptParts = append(promptParts, fmt.Sprintf("Detailed guidance: %s", result.DetailedGuidance))
	}

	// Add instruction to fix the issues
	promptParts = append(promptParts, "Please revise the code to address these issues while maintaining existing functionality.")

	return strings.Join(promptParts, "\n\n")
}

// attemptRetryForNeedsRevision attempts to retry code generation when review requires revisions
func (s *CodeReviewService) attemptRetryForNeedsRevision(result *types.CodeReviewResult, ctx *ReviewContext, opts *ReviewOptions) error {
	// Check if we have meaningful feedback to work with
	if strings.TrimSpace(result.Feedback) == "" && strings.TrimSpace(result.DetailedGuidance) == "" {
		s.logger.LogProcessStep("No actionable feedback available for retry. Skipping retry attempt.")
		return nil
	}

	s.logger.LogProcessStep("Code review requires retry with refined prompt based on feedback.")

	// Create the refined prompt for retry
	refinedPrompt := s.createRefinedPromptForRetry(result, ctx)

	s.logger.LogProcessStep("Generated refined prompt for retry attempt.")

	// Return a retry request error to signal to the caller that a retry is needed
	return &RetryRequestError{
		RefinedPrompt: refinedPrompt,
		Feedback:      result.Feedback,
	}
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
