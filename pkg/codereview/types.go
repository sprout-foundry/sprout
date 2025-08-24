package codereview

import (
	"time"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
)

// CodeReviewResult represents the result of a code review
type CodeReviewResult = types.CodeReviewResult

// Reviewer defines the interface for a code reviewer
type Reviewer interface {
	Review(cfg *config.Config, combinedDiff, originalPrompt, workspaceContext string) (*types.CodeReviewResult, error)
}

// NoReviewer is a no-op reviewer
type NoReviewer struct{}

// Review performs a no-op review
func (r *NoReviewer) Review(cfg *config.Config, combinedDiff, originalPrompt, workspaceContext string) (*types.CodeReviewResult, error) {
	return &types.CodeReviewResult{
		Status:   "approved",
		Feedback: "No reviewer configured.",
	}, nil
}

// ReviewResult represents the result of a code review operation
// This extends the base types.CodeReviewResult with additional context
type ReviewResult struct {
	*types.CodeReviewResult
	AppliedFixes   bool    // Whether fixes were automatically applied
	RolledBack     bool    // Whether changes were rolled back
	RetryAttempted bool    // Whether a retry was attempted
	RetryCount     int     // Number of retry attempts made
	ProcessingTime float64 // Time taken to process the review in seconds
}

// ReviewMetrics contains metrics about the code review process
type ReviewMetrics struct {
	TotalReviews    int           `json:"total_reviews"`
	ApprovedReviews int           `json:"approved_reviews"`
	RejectedReviews int           `json:"rejected_reviews"`
	RetryAttempts   int           `json:"retry_attempts"`
	AverageTime     time.Duration `json:"average_time"`
	SuccessRate     float64       `json:"success_rate"` // Percentage of approved reviews
}

// ReviewConfiguration contains configuration for the code review service
type ReviewConfiguration struct {
	EnableAutoFix              bool    `json:"enable_auto_fix"`
	EnableRollback             bool    `json:"enable_rollback"`
	MaxRetries                 int     `json:"max_retries"`
	ReviewTimeout              int     `json:"review_timeout_seconds"`
	StrictMode                 bool    `json:"strict_mode"`      // Reject on any issues found
	RequireApproval            bool    `json:"require_approval"` // Require explicit approval for all changes
	CollectMetrics             bool    `json:"collect_metrics"`
	MaxIterations              int     `json:"max_iterations"`               // Maximum review iterations before fallback
	EnableConvergenceDetection bool    `json:"enable_convergence_detection"` // Enable detection of oscillating reviews
	SimilarityThreshold        float64 `json:"similarity_threshold"`         // Threshold for detecting similar review feedback
}

// DefaultReviewConfiguration returns the default configuration for code reviews
func DefaultReviewConfiguration() *ReviewConfiguration {
	return &ReviewConfiguration{
		EnableAutoFix:              true,
		EnableRollback:             true,
		MaxRetries:                 2,
		ReviewTimeout:              300, // 5 minutes
		StrictMode:                 false,
		RequireApproval:            false,
		CollectMetrics:             true,
		MaxIterations:              5, // Allow up to 5 review iterations before fallback
		EnableConvergenceDetection: true,
		SimilarityThreshold:        0.7, // 70% similarity threshold for convergence detection
	}
}

// ReviewIteration represents a single iteration in the review process
type ReviewIteration struct {
	IterationNumber int                     `json:"iteration_number"`
	OriginalDiff    string                  `json:"original_diff"`
	ReviewResult    *types.CodeReviewResult `json:"review_result"`
	AppliedChanges  bool                    `json:"applied_changes"`
	Timestamp       time.Time               `json:"timestamp"`
	ContentHash     string                  `json:"content_hash"` // Hash of the content being reviewed
}

// ReviewHistory tracks the history of review iterations for a given context
type ReviewHistory struct {
	SessionID       string            `json:"session_id"`
	Iterations      []ReviewIteration `json:"iterations"`
	OriginalPrompt  string            `json:"original_prompt"`
	OriginalContent string            `json:"original_content"`
	StartTime       time.Time         `json:"start_time"`
	LastUpdate      time.Time         `json:"last_update"`
	Converged       bool              `json:"converged"`    // Whether the review process has converged
	FinalStatus     string            `json:"final_status"` // Final status: approved, rejected, fallback
}

// ConvergenceStatus represents the convergence analysis of review iterations
type ConvergenceStatus struct {
	IsConverged       bool    `json:"is_converged"`
	Reason            string  `json:"reason"`
	SimilarIterations []int   `json:"similar_iterations"` // Indices of similar iterations
	AverageSimilarity float64 `json:"average_similarity"`
	MaxSimilarity     float64 `json:"max_similarity"`
	Recommendation    string  `json:"recommendation"` // Suggested action
}
