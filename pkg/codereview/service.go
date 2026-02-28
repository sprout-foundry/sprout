package codereview

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/factory"
	"github.com/alantheprice/ledit/pkg/history"
	"github.com/alantheprice/ledit/pkg/prompts"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ReviewContext represents the context for a code review request
type ReviewContext struct {
	Diff                  string // The code diff to review
	OriginalPrompt        string // The original user prompt (for automated reviews)
	ProcessedInstructions string // Processed instructions (for automated reviews)
	RevisionID            string // Revision ID for change tracking
	Config                *configuration.Config
	Logger                *utils.Logger
	History               *ReviewHistory      // Review history for this context
	SessionID             string              // Unique session identifier
	CurrentIteration      int                 // Current iteration number
	FullFileContext       string              // Full file content for patch resolution context
	RelatedFiles          []string            // Files that might be affected by changes
	AgentClient           api.ClientInterface // Agent API client for LLM calls
	// Metadata for enhanced context
	ProjectType      string // Project type (Go, Node.js, etc.)
	CommitMessage    string // Commit message/intent
	KeyComments      string // Key code comments explaining WHY
	ChangeCategories string // Categorization of changes
}

// ReviewType defines the type of code review being performed
type ReviewType int

const (
	StagedReview ReviewType = iota // Used for reviewing Git staged changes
)

// ReviewOptions contains options for the code review
type ReviewOptions struct {
	Type             ReviewType
	SkipPrompt       bool
	PreapplyReview   bool
	RollbackOnReject bool // Whether to rollback changes when review is rejected
}

// CodeReviewService provides a unified interface for code review operations
type CodeReviewService struct {
	config             *configuration.Config
	logger             *utils.Logger
	reviewConfig       *ReviewConfiguration
	contextStore       map[string]*ReviewContext // Store contexts by session ID for persistence
	defaultAgentClient api.ClientInterface       // NEW: Default agent client for LLM calls
}

// NewCodeReviewService creates a new code review service instance
func NewCodeReviewService(cfg *configuration.Config, logger *utils.Logger) *CodeReviewService {
	agentClient := createReviewAgentClient(cfg, logger)

	return &CodeReviewService{
		config:             cfg,
		logger:             logger,
		reviewConfig:       DefaultReviewConfiguration(),
		contextStore:       make(map[string]*ReviewContext),
		defaultAgentClient: agentClient,
	}
}

// NewCodeReviewServiceWithConfig creates a new code review service instance with custom configuration
func NewCodeReviewServiceWithConfig(cfg *configuration.Config, logger *utils.Logger, reviewConfig *ReviewConfiguration) *CodeReviewService {
	agentClient := createReviewAgentClient(cfg, logger)

	return &CodeReviewService{
		config:             cfg,
		logger:             logger,
		reviewConfig:       reviewConfig,
		contextStore:       make(map[string]*ReviewContext),
		defaultAgentClient: agentClient,
	}
}

func createReviewAgentClient(cfg *configuration.Config, logger *utils.Logger) api.ClientInterface {
	providerName := strings.TrimSpace(os.Getenv("LEDIT_PROVIDER"))
	model := strings.TrimSpace(os.Getenv("LEDIT_MODEL"))

	if providerName == "" && cfg != nil {
		providerName = strings.TrimSpace(cfg.LastUsedProvider)
	}
	if model == "" && cfg != nil && providerName != "" {
		model = strings.TrimSpace(cfg.GetModelForProvider(providerName))
	}
	if providerName == "" {
		if logger != nil {
			logger.LogProcessStep("Warning: no provider configured for code review; review commands require explicit provider selection")
		}
		return nil
	}

	client, err := factory.CreateProviderClient(api.ClientType(providerName), model)
	if err != nil {
		if logger != nil {
			logger.LogProcessStep(fmt.Sprintf("Warning: failed to initialize review provider '%s' (model '%s'): %v", providerName, model, err))
		}
		return nil
	}

	return client
}

// GetDefaultAgentClient returns the default agent client for this service
func (s *CodeReviewService) GetDefaultAgentClient() api.ClientInterface {
	return s.defaultAgentClient
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

// extractAffectedFilesFromDiff parses a diff to find which files are being modified
func (s *CodeReviewService) extractAffectedFilesFromDiff(diff string) []string {
	var files []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		// Look for diff headers that indicate file paths
		if strings.HasPrefix(line, "diff --git") {
			// Parse "diff --git a/file.go b/file.go" format
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				filePath := strings.TrimPrefix(parts[2], "a/")
				files = append(files, filePath)
			}
		} else if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			// Parse "--- a/file.go" or "+++ b/file.go" format
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.Contains(parts[1], "/dev/null") {
				filePath := strings.TrimPrefix(parts[1], "a/")
				filePath = strings.TrimPrefix(filePath, "b/")
				files = append(files, filePath)
			}
		}
	}

	return s.removeDuplicates(files)
}

// removeDuplicates removes duplicate entries from a string slice
func (s *CodeReviewService) removeDuplicates(items []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// removeAffectedFiles removes files that are already in the affected files list
func (s *CodeReviewService) removeAffectedFiles(related, affected []string) []string {
	affectedSet := make(map[string]bool)
	for _, file := range affected {
		affectedSet[file] = true
	}

	result := []string{}
	for _, file := range related {
		if !affectedSet[file] {
			result = append(result, file)
		}
	}

	return result
}

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
		return nil, fmt.Errorf("only staged review type is supported, requested type: %v", opts.Type)
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

// performStagedReview handles reviews of Git staged changes
func (s *CodeReviewService) performStagedReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	// Use enhanced agent-based review with workspace intelligence
	return s.performAgentBasedCodeReview(ctx, false) // human-readable format for staged changes
}

// performAgentBasedCodeReview performs code review using the agent API with enhanced context
func (s *CodeReviewService) performAgentBasedCodeReview(ctx *ReviewContext, structured bool) (*types.CodeReviewResult, error) {
	if ctx.AgentClient == nil {
		return nil, fmt.Errorf("agent client not available for enhanced code review")
	}

	// Build enhanced review prompt with workspace context
	prompt := s.buildEnhancedReviewPrompt(ctx, structured)

	// Create messages for agent API
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	// Make agent API call
	response, err := ctx.AgentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		return nil, fmt.Errorf("agent API call failed: %w", err)
	}

	// Parse response based on format
	if structured {
		return s.parseStructuredReviewResponse(response)
	} else {
		return s.parseHumanReadableReviewResponse(response)
	}
}

// performDeepAgentBasedCodeReview runs a stricter evidence-focused review and requires JSON output.
func (s *CodeReviewService) performDeepAgentBasedCodeReview(ctx *ReviewContext) (*types.CodeReviewResult, error) {
	if ctx.AgentClient == nil {
		return nil, fmt.Errorf("agent client not available for deep code review")
	}

	prompt := s.buildDeepReviewPrompt(ctx)
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	response, err := ctx.AgentClient.SendChatRequest(messages, nil, "")
	if err != nil {
		return nil, fmt.Errorf("agent API call failed: %w", err)
	}

	result, err := s.parseStructuredReviewResponse(response)
	if err != nil {
		return nil, err
	}

	status := strings.ToLower(strings.TrimSpace(result.Status))
	switch status {
	case "approved", "needs_revision", "rejected":
		result.Status = status
	default:
		result.Status = "needs_revision"
		if strings.TrimSpace(result.Feedback) == "" {
			result.Feedback = "Deep review returned an invalid status; manual review recommended."
		}
	}

	return result, nil
}

// buildEnhancedReviewPrompt builds a review prompt with workspace intelligence and context
func (s *CodeReviewService) buildEnhancedReviewPrompt(ctx *ReviewContext, structured bool) string {
	var promptParts []string

	// Add base prompt based on review type
	if structured {
		promptParts = append(promptParts, "Please perform a structured code review of the following changes.")
	} else {
		promptParts = append(promptParts, prompts.CodeReviewStagedPrompt())
	}

	// Add metadata sections FIRST (before the diff) to help LLM understand intent
	// These are CRITICAL for avoiding false positives

	// Project type
	if ctx.ProjectType != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Project Type\n%s", ctx.ProjectType))
	}

	// Commit message/intent
	if ctx.CommitMessage != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Commit Message (Intent)\n%s", ctx.CommitMessage))
	}

	// Key code comments that explain WHY
	if ctx.KeyComments != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Key Code Comments (Context)\n%s", ctx.KeyComments))
	}

	// Change categories
	if ctx.ChangeCategories != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Change Categories\n%s", ctx.ChangeCategories))
	}

	// Add related files context if available
	if len(ctx.RelatedFiles) > 0 {
		promptParts = append(promptParts, fmt.Sprintf("\n## Related Files to Consider\nThe following files may be affected by or related to these changes:\n%s", strings.Join(ctx.RelatedFiles, "\n")))
	}

	// Add original prompt context
	if ctx.OriginalPrompt != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Original Request\n%s", ctx.OriginalPrompt))
	}

	// Add processed instructions if available
	if ctx.ProcessedInstructions != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Processed Instructions\n%s", ctx.ProcessedInstructions))
	}

	// Add full file context if available
	if ctx.FullFileContext != "" {
		promptParts = append(promptParts, fmt.Sprintf("\n## Full File Context\n%s", ctx.FullFileContext))
	}

	// Add the diff to review (LAST, after all context)
	promptParts = append(promptParts, fmt.Sprintf("\n## Code Changes to Review\n```diff\n%s\n```", ctx.Diff))

	return strings.Join(promptParts, "\n")
}

func (s *CodeReviewService) buildDeepReviewPrompt(ctx *ReviewContext) string {
	basePrompt := s.buildEnhancedReviewPrompt(ctx, false)

	return basePrompt + `

## Deep Review Requirements

Perform an evidence-based review focused on reducing false positives.

- Only report an issue when you can point to concrete evidence in the provided diff/context.
- If evidence is incomplete, classify as "verify" in guidance rather than presenting as a confirmed defect.
- Prefer high-signal findings (correctness, security, data loss, concurrency, API contract regressions).
- Ignore stylistic nits unless they create operational risk.
- Include line/file references where possible.

Return ONLY valid JSON using:
{
  "status": "approved|needs_revision|rejected",
  "feedback": "short summary",
  "detailed_guidance": "findings grouped as MUST_FIX and VERIFY, each with evidence and suggested next step"
}`
}

// parseStructuredReviewResponse parses a structured JSON review response from the agent
func (s *CodeReviewService) parseStructuredReviewResponse(response *api.ChatResponse) (*types.CodeReviewResult, error) {
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response choices received from agent")
	}

	content := response.Choices[0].Message.Content
	candidates := extractStructuredReviewCandidates(content)
	for _, candidate := range candidates {
		if parsed, ok := parseStructuredReviewCandidate(candidate); ok {
			return parsed, nil
		}
	}

	// Fail closed for structured reviews to avoid accidental approvals.
	feedback := strings.TrimSpace(content)
	if feedback == "" {
		feedback = "Structured review returned no parseable JSON output."
	}
	return &types.CodeReviewResult{
		Status:   "needs_revision",
		Feedback: feedback,
	}, nil
}

func extractStructuredReviewCandidates(content string) []string {
	candidates := make([]string, 0, 4)
	seen := make(map[string]struct{})

	if jsonStr, err := utils.ExtractJSON(content); err == nil && strings.TrimSpace(jsonStr) != "" {
		jsonStr = strings.TrimSpace(jsonStr)
		seen[jsonStr] = struct{}{}
		candidates = append(candidates, jsonStr)
	}

	for _, part := range utils.SplitTopLevelJSONObjects(content) {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if _, exists := seen[part]; exists {
			continue
		}
		seen[part] = struct{}{}
		candidates = append(candidates, part)
	}

	return candidates
}

func parseStructuredReviewCandidate(candidate string) (*types.CodeReviewResult, bool) {
	var reviewResult types.CodeReviewResult
	if err := json.Unmarshal([]byte(candidate), &reviewResult); err != nil {
		return nil, false
	}

	// Ensure required fields are present
	if reviewResult.Status == "" {
		return nil, false
	}
	if reviewResult.Feedback == "" {
		return nil, false
	}

	return &reviewResult, true
}

// parseHumanReadableReviewResponse parses a human-readable review response from the agent
func (s *CodeReviewService) parseHumanReadableReviewResponse(response *api.ChatResponse) (*types.CodeReviewResult, error) {
	if len(response.Choices) == 0 {
		return nil, fmt.Errorf("no response choices received from agent")
	}

	content := response.Choices[0].Message.Content
	// For staged reviews, we typically return the content as-is with approved status
	// unless there are clear rejection indicators
	status := "approved"
	if strings.Contains(strings.ToLower(content), "reject") || strings.Contains(strings.ToLower(content), "not acceptable") {
		status = "rejected"
	} else if strings.Contains(strings.ToLower(content), "needs") && strings.Contains(strings.ToLower(content), "revision") {
		status = "needs_revision"
	}

	return &types.CodeReviewResult{
		Status:   status,
		Feedback: content,
	}, nil
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
				s.logger.LogError(fmt.Errorf("failed to rollback changes for revision %s: %w", ctx.RevisionID, err))
				return nil, fmt.Errorf("changes rejected by automated review, but rollback failed. Feedback: %s", result.Feedback)
			}
		} else {
			s.logger.LogProcessStep("No active changes recorded for this revision; skipping rollback.")
		}
	}

	// Retries are no longer supported - removed with automated review

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
		for filePath := range patchResolution.MultiFile {
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
