package spec

import (
	"fmt"

	"github.com/alantheprice/ledit/pkg/codereview"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/types"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ReviewWithSpec performs standard code review AND scope validation
func ReviewWithSpec(
	diff string,
	conversation []Message,
	userIntent string,
	cfg *configuration.Config,
	logger *utils.Logger,
) (*types.CodeReviewResult, *ScopeReviewResult, *CanonicalSpec, error) {

	// Create spec review service
	specService, err := NewSpecReviewService(cfg, logger)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to create spec service: %w", err)
	}

	// Step 1: Extract canonical spec from conversation
	logger.LogProcessStep("Extracting canonical specification from conversation...")
	extractionResult, err := specService.GetExtractor().ExtractSpec(conversation, userIntent)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("failed to extract spec: %w", err)
	}

	spec := extractionResult.Spec
	logger.LogProcessStep(fmt.Sprintf("✓ Spec extracted: %s", spec.Objective))

	// Step 2: Perform standard code review
	logger.LogProcessStep("Performing standard code review...")
	codeReviewService := codereview.NewCodeReviewService(cfg, logger)

	reviewCtx := &codereview.ReviewContext{
		Diff:          diff,
		Config:        cfg,
		Logger:        logger,
		AgentClient:   specService.extractor.agentClient,
		ProjectType:   "", // Could be detected if needed
		CommitMessage: spec.Objective,
	}

	reviewOpts := &codereview.ReviewOptions{
		Type:       codereview.StagedReview,
		SkipPrompt: true,
	}

	codeReviewResult, err := codeReviewService.PerformReview(reviewCtx, reviewOpts)
	if err != nil {
		return nil, nil, spec, fmt.Errorf("failed to perform code review: %w", err)
	}

	// Step 3: Validate scope compliance
	logger.LogProcessStep("Validating scope compliance...")
	scopeResult, err := specService.GetValidator().ValidateScope(diff, spec)
	if err != nil {
		return codeReviewResult, nil, spec, fmt.Errorf("failed to validate scope: %w", err)
	}

	// Step 4: Merge scope violations into code review result
	if !scopeResult.InScope && len(scopeResult.Violations) > 0 {
		logger.LogProcessStep(fmt.Sprintf("⚠ Found %d scope violations", len(scopeResult.Violations)))

		// Add scope violations as CRITICAL issues in the review
		for _, violation := range scopeResult.Violations {
			violationMsg := fmt.Sprintf(
				"**[CRITICAL] SCOPE VIOLATION** [%s:%d] %s - %s",
				violation.File,
				violation.Line,
				violation.Description,
				violation.Why,
			)

			// Add to issues as string
			codeReviewResult.Issues = append(codeReviewResult.Issues, violationMsg)
		}

		// Update review status if scope violations found
		if codeReviewResult.Status == "approved" {
			codeReviewResult.Status = "needs_revision"
		}

		// Append scope summary to feedback
		scopeFeedback := fmt.Sprintf(
			"\n\n## SCOPE COMPLIANCE\n**Status**: OUT_OF_SCOPE\n\n%s\n\n### Scope Violations\n",
			scopeResult.Summary,
		)

		for _, violation := range scopeResult.Violations {
			scopeFeedback += fmt.Sprintf(
				"- **[%s]** [%s:%d] %s\n  - Why: %s\n",
				violation.Severity,
				violation.File,
				violation.Line,
				violation.Description,
				violation.Why,
			)
		}

		if len(scopeResult.Suggestions) > 0 {
			scopeFeedback += "\n### Suggestions\n"
			for _, suggestion := range scopeResult.Suggestions {
				scopeFeedback += fmt.Sprintf("- %s\n", suggestion)
			}
		}

		codeReviewResult.Feedback += scopeFeedback
	} else {
		logger.LogProcessStep("✓ All changes are within scope")
		codeReviewResult.Feedback += "\n\n## SCOPE COMPLIANCE\n**Status**: IN_SCOPE - All changes align with specification\n"
	}

	return codeReviewResult, scopeResult, spec, nil
}
