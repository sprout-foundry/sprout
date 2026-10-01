package codereview

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/types"
)

// PerformReview performs a single-call review of the staged change in ctx.
func (s *CodeReviewService) PerformReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	if err := validateReviewRequest(ctx, opts); err != nil {
		return nil, err
	}
	if opts.Type != StagedReview {
		return nil, fmt.Errorf("only staged review type is supported")
	}

	result, err := s.performStagedReview(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to perform code review: %w", err)
	}
	return checkReviewStatus(result)
}

// PerformAgenticReview performs a stricter evidence-focused single-call
// review that requires structured output.
func (s *CodeReviewService) PerformAgenticReview(ctx *ReviewContext, opts *ReviewOptions) (*types.CodeReviewResult, error) {
	if err := validateReviewRequest(ctx, opts); err != nil {
		return nil, err
	}

	result, err := s.performDeepAgentBasedCodeReview(ctx)
	if err != nil {
		return nil, fmt.Errorf("failed to perform deep review: %w", err)
	}
	return checkReviewStatus(result)
}

func validateReviewRequest(ctx *ReviewContext, opts *ReviewOptions) error {
	if ctx == nil {
		return fmt.Errorf("review context cannot be nil")
	}
	if opts == nil {
		return fmt.Errorf("review options cannot be nil")
	}
	if strings.TrimSpace(ctx.Diff) == "" {
		return fmt.Errorf("no diff content provided for review")
	}
	return nil
}

func checkReviewStatus(result *types.CodeReviewResult) (*types.CodeReviewResult, error) {
	switch result.Status {
	case "approved", "needs_revision", "rejected":
		return result, nil
	default:
		return nil, fmt.Errorf("unknown review status: %s", result.Status)
	}
}
