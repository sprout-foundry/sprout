package codereview

import (
	"testing"

	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

func TestIterationLimitExceeded(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	ctx := &ReviewContext{
		History: &ReviewHistory{
			Iterations: make([]ReviewIteration, 0),
		},
	}

	// Test with default config (5 max iterations)
	for i := 0; i < 5; i++ {
		ctx.History.Iterations = append(ctx.History.Iterations, ReviewIteration{
			IterationNumber: i + 1,
		})
	}

	if !service.hasExceededIterationLimit(ctx) {
		t.Error("Expected iteration limit to be exceeded")
	}
}

func TestConvergenceDetection(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	ctx := &ReviewContext{
		History: &ReviewHistory{
			Iterations: []ReviewIteration{
				{
					ReviewResult: &types.CodeReviewResult{
						Status:   "needs_revision",
						Feedback: "Please add error handling to the function",
					},
				},
				{
					ReviewResult: &types.CodeReviewResult{
						Status:   "needs_revision",
						Feedback: "Please add error handling to the function and validation",
					},
				},
				{
					ReviewResult: &types.CodeReviewResult{
						Status:   "needs_revision",
						Feedback: "Please add error handling and validation to the function",
					},
				},
			},
		},
	}

	if !service.hasConverged(ctx) {
		t.Error("Expected convergence to be detected")
	}
}

func TestSimilarityCalculation(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	str1 := "Add error handling to the function"
	str2 := "Add error handling and validation"

	similarity := service.calculateSimilarity(str1, str2)

	if similarity <= 0.0 || similarity > 1.0 {
		t.Errorf("Expected similarity between 0.0 and 1.0, got %f", similarity)
	}

	// Test identical strings
	identical := service.calculateSimilarity(str1, str1)
	if identical != 1.0 {
		t.Errorf("Expected identical strings to have similarity 1.0, got %f", identical)
	}

	// Test completely different strings
	different := service.calculateSimilarity("hello world", "goodbye universe")
	if different > 0.2 {
		t.Errorf("Expected very different strings to have low similarity, got %f", different)
	}
}

func TestReviewHistoryInitialization(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	ctx := &ReviewContext{
		OriginalPrompt: "Create a user registration function",
		Diff:           "function registerUser() { ... }",
	}

	history := service.initializeReviewHistory(ctx)

	if history.SessionID == "" {
		t.Error("Expected session ID to be generated")
	}

	if history.OriginalPrompt != ctx.OriginalPrompt {
		t.Error("Expected original prompt to be preserved")
	}

	if history.OriginalContent != ctx.Diff {
		t.Error("Expected original content to be preserved")
	}

	if history.Converged {
		t.Error("Expected new history to not be converged")
	}

	if len(history.Iterations) != 0 {
		t.Error("Expected new history to have no iterations")
	}
}

func TestAttemptRetryForNeedsRevision(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	// Create a mock review result that requires revisions but has no patch
	result := &types.CodeReviewResult{
		Status:           "needs_revision",
		Feedback:         "The code has a bug in the error handling",
		DetailedGuidance: "Add proper error checking before accessing array elements",
	}

	// Create a mock review context
	ctx := &ReviewContext{
		OriginalPrompt: "Implement a function to process user data",
		Diff:           "diff content here",
	}

	// Create review options
	opts := &ReviewOptions{
		Type:           AutomatedReview,
		AutoApplyFixes: true,
		MaxRetries:     2,
	}

	// Test the retry attempt
	err := service.attemptRetryForNeedsRevision(result, ctx, opts)

	// Verify that a RetryRequestError is returned
	if err == nil {
		t.Fatal("Expected RetryRequestError, but got nil")
	}

	retryRequest, ok := err.(*RetryRequestError)
	if !ok {
		t.Fatalf("Expected RetryRequestError, but got %T", err)
	}

	// Verify the retry request contains expected data
	if retryRequest.Feedback != result.Feedback {
		t.Errorf("Expected feedback %q, got %q", result.Feedback, retryRequest.Feedback)
	}

	if retryRequest.RefinedPrompt == "" {
		t.Error("Expected non-empty refined prompt")
	}

	// Verify the refined prompt contains the original prompt and feedback
	if !containsString(retryRequest.RefinedPrompt, ctx.OriginalPrompt) {
		t.Error("Refined prompt should contain original prompt")
	}

	if !containsString(retryRequest.RefinedPrompt, result.Feedback) {
		t.Error("Refined prompt should contain review feedback")
	}

	if !containsString(retryRequest.RefinedPrompt, result.DetailedGuidance) {
		t.Error("Refined prompt should contain detailed guidance")
	}
}

func TestCreateRefinedPromptForRetry(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	// Create a mock review result
	result := &types.CodeReviewResult{
		Status:           "needs_revision",
		Feedback:         "Add input validation",
		DetailedGuidance: "Use proper validation libraries",
	}

	// Create a mock review context
	ctx := &ReviewContext{
		OriginalPrompt: "Create a user registration function",
		Diff:           "diff content here",
	}

	// Test creating refined prompt
	refinedPrompt := service.createRefinedPromptForRetry(result, ctx)

	if refinedPrompt == "" {
		t.Error("Expected non-empty refined prompt")
	}

	// Verify all components are included
	expectedComponents := []string{
		ctx.OriginalPrompt,
		result.Feedback,
		result.DetailedGuidance,
		"Please revise the code to address these issues while maintaining existing functionality.",
	}

	for _, component := range expectedComponents {
		if !containsString(refinedPrompt, component) {
			t.Errorf("Refined prompt should contain: %q", component)
		}
	}
}

func TestAttemptRetryForNeedsRevisionNoFeedback(t *testing.T) {
	cfg := &config.Config{}
	logger := utils.GetLogger(true)
	service := NewCodeReviewService(cfg, logger)

	// Create a mock review result with no feedback or guidance
	result := &types.CodeReviewResult{
		Status:   "needs_revision",
		Feedback: "",
	}

	ctx := &ReviewContext{
		OriginalPrompt: "Implement a function",
	}

	opts := &ReviewOptions{
		Type:           AutomatedReview,
		AutoApplyFixes: true,
	}

	// Test with no actionable feedback
	err := service.attemptRetryForNeedsRevision(result, ctx, opts)

	// Should return nil when no actionable feedback is available
	if err != nil {
		t.Errorf("Expected nil error when no feedback available, got %v", err)
	}
}

// Helper function to check if a string contains a substring
func containsString(str, substr string) bool {
	return len(str) >= len(substr) && (str == substr || len(substr) == 0 ||
		len(str) > len(substr) && (str[:len(substr)] == substr || str[len(str)-len(substr):] == substr ||
			func() bool {
				for i := 1; i <= len(str)-len(substr); i++ {
					if str[i:i+len(substr)] == substr {
						return true
					}
				}
				return false
			}()))
}
