package agent

import (
	"encoding/json"
	"fmt"
	"github.com/alantheprice/ledit/pkg/llm"
	"github.com/alantheprice/ledit/pkg/prompts"
	"strings"
)

// QualityReviewResult represents the result of a quality-focused code review
type QualityReviewResult struct {
	Status           string   `json:"status"`            // "approved", "needs_revision", "rejected"
	Feedback         string   `json:"feedback"`          // Concise review summary
	DetailedGuidance string   `json:"detailed_guidance"` // Specific improvements needed
	SecurityNotes    string   `json:"security_notes"`    // Security concerns
	QualityScore     int      `json:"quality_score"`     // 1-10 scale
	PatchResolution  string   `json:"patch_resolution"`  // Optional complete file content
	Suggestions      []string `json:"suggestions"`       // Quality improvement suggestions
}

// QualityReviewService provides enhanced code review with quality focus
type QualityReviewService struct {
	optimizer *QualityOptimizer
}

// NewQualityReviewService creates a new quality review service
func NewQualityReviewService(optimizer *QualityOptimizer) *QualityReviewService {
	return &QualityReviewService{
		optimizer: optimizer,
	}
}

// ReviewCodeWithQuality performs a quality-focused code review
func (qrs *QualityReviewService) ReviewCodeWithQuality(
	diff, originalPrompt, fullFileContext string,
	qualityLevel QualityLevel,
	ctx *SimplifiedAgentContext,
) (*QualityReviewResult, error) {
	// Get quality-appropriate review prompt
	var reviewPrompt string
	var err error

	switch qualityLevel {
	case QualityProduction, QualityEnhanced:
		reviewPrompt, err = qrs.optimizer.GetPromptForTask("code_review", qualityLevel)
		if err != nil {
			// Fallback to quality-enhanced prompt content
			reviewPrompt = qrs.getQualityEnhancedReviewPrompt()
		}
	default:
		reviewPrompt, err = qrs.optimizer.GetPromptForTask("code_review", QualityStandard)
		if err != nil {
			// Fallback to standard review
			reviewPrompt = "Expert code reviewer. Analyze changes for correctness and basic quality."
		}
	}

	// Build review messages
	userPrompt := fmt.Sprintf(
		"Original request: \"%s\"\n\nCode changes (diff):\n```diff\n%s\n```\n\nFull context:\n```\n%s\n```\n\nProvide quality-focused review with JSON response.",
		originalPrompt,
		diff,
		fullFileContext,
	)

	messages := []prompts.Message{
		{Role: "system", Content: reviewPrompt},
		{Role: "user", Content: userPrompt},
	}

	// Use smart timeout for review
	timeout := GetSmartTimeout(ctx.Config, ctx.Config.OrchestrationModel, "code_review")

	// Call LLM for review
	response, tokenUsage, err := llm.GetLLMResponse(
		ctx.Config.OrchestrationModel,
		messages,
		"",
		ctx.Config,
		timeout,
	)
	if err != nil {
		return nil, fmt.Errorf("quality review failed: %w", err)
	}

	// Track token usage
	if tokenUsage != nil {
		trackTokenUsage(ctx, tokenUsage, ctx.Config.OrchestrationModel)
	}

	// Parse the JSON response
	var result QualityReviewResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(response)), &result); err != nil {
		// Fallback to basic parsing if JSON is malformed
		return &QualityReviewResult{
			Status:       "needs_revision",
			Feedback:     "Review response parsing failed",
			QualityScore: 5,
			Suggestions:  []string{"Manual code review recommended"},
		}, nil
	}

	// Enhance suggestions based on quality level
	result.Suggestions = append(result.Suggestions, qrs.getQualitySuggestions(qualityLevel)...)

	return &result, nil
}

// getQualityEnhancedReviewPrompt returns the enhanced review prompt as fallback
func (qrs *QualityReviewService) getQualityEnhancedReviewPrompt() string {
	return `Expert code reviewer with focus on quality, security, and best practices. Analyze the ENTIRE changeset holistically.

COMPREHENSIVE REVIEW CRITERIA:
- Correctness: Logic errors, edge cases, type mismatches
- Security: Input validation, injection attacks, sensitive data
- Performance: Efficiency, resource usage, bottlenecks
- Maintainability: Clarity, documentation, modularity
- Testing: Testability, coverage potential
- Architecture: SOLID principles, design patterns

JSON response required with quality_score (1-10) and specific feedback.`
}

// getQualitySuggestions returns quality suggestions based on level
func (qrs *QualityReviewService) getQualitySuggestions(qualityLevel QualityLevel) []string {
	switch qualityLevel {
	case QualityProduction:
		return []string{
			"Consider comprehensive error handling",
			"Add security validation for inputs",
			"Implement performance monitoring",
			"Add unit tests for critical paths",
			"Document public APIs",
		}
	case QualityEnhanced:
		return []string{
			"Follow established coding patterns",
			"Add appropriate error handling",
			"Consider edge cases",
			"Use meaningful variable names",
		}
	default:
		return []string{
			"Review for basic correctness",
			"Check for obvious bugs",
		}
	}
}

// ShouldUseQualityReview determines if quality review should be used
func (qrs *QualityReviewService) ShouldUseQualityReview(qualityLevel QualityLevel, complexity ComplexityLevel) bool {
	// Always use quality review for production level
	if qualityLevel == QualityProduction {
		return true
	}

	// Use for enhanced quality with moderate+ complexity
	if qualityLevel == QualityEnhanced && complexity >= ComplexityModerate {
		return true
	}

	// Use for complex tasks even with standard quality
	if complexity >= ComplexityComplex {
		return true
	}

	return false
}

// GenerateQualityReport generates a comprehensive quality report
func (qrs *QualityReviewService) GenerateQualityReport(
	assessment QualityAssessment,
	review *QualityReviewResult,
	qualityLevel QualityLevel,
) string {
	var report strings.Builder

	report.WriteString(fmt.Sprintf("# Code Quality Report\n\n"))
	report.WriteString(fmt.Sprintf("**Target Quality Level:** %s\n", qrs.getQualityLevelName(qualityLevel)))
	report.WriteString(fmt.Sprintf("**Assessment Score:** %d/10\n", assessment.Score))

	if review != nil {
		report.WriteString(fmt.Sprintf("**Review Score:** %d/10\n", review.QualityScore))
		report.WriteString(fmt.Sprintf("**Review Status:** %s\n\n", strings.Title(review.Status)))

		if review.Feedback != "" {
			report.WriteString(fmt.Sprintf("## Review Feedback\n%s\n\n", review.Feedback))
		}

		if len(review.Suggestions) > 0 {
			report.WriteString("## Quality Suggestions\n")
			for _, suggestion := range review.Suggestions {
				report.WriteString(fmt.Sprintf("- %s\n", suggestion))
			}
			report.WriteString("\n")
		}
	}

	if len(assessment.Strengths) > 0 {
		report.WriteString("## Strengths\n")
		for _, strength := range assessment.Strengths {
			report.WriteString(fmt.Sprintf("- %s\n", strength))
		}
		report.WriteString("\n")
	}

	if len(assessment.Issues) > 0 {
		report.WriteString("## Areas for Improvement\n")
		for _, issue := range assessment.Issues {
			report.WriteString(fmt.Sprintf("- %s\n", issue))
		}
	}

	return report.String()
}

// getQualityLevelName returns a human-readable quality level name
func (qrs *QualityReviewService) getQualityLevelName(level QualityLevel) string {
	switch level {
	case QualityProduction:
		return "Production Grade"
	case QualityEnhanced:
		return "Enhanced Quality"
	default:
		return "Standard"
	}
}
