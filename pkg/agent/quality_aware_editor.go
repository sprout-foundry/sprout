package agent

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"github.com/alantheprice/ledit/pkg/editor"
	"strings"
)

// QualityAwareEditor wraps the editor with quality-enhanced prompts
type QualityAwareEditor struct {
	optimizer *QualityOptimizer
}

// NewQualityAwareEditor creates a new quality-aware editor
func NewQualityAwareEditor(optimizer *QualityOptimizer) *QualityAwareEditor {
	return &QualityAwareEditor{
		optimizer: optimizer,
	}
}

// ProcessCodeGenerationWithQuality generates code using quality-appropriate prompts
func (qae *QualityAwareEditor) ProcessCodeGenerationWithQuality(
	filename, instructions string,
	cfg *config.Config,
	qualityLevel QualityLevel,
	imagePath string,
) (string, error) {
	// Enhance instructions with quality guidelines based on level
	enhancedInstructions := qae.enhanceInstructions(instructions, qualityLevel)

	// Set quality level in config for the editor to use
	qualityCfg := *cfg
	qualityCfg.QualityLevel = int(qualityLevel)

	// Call the underlying editor with enhanced instructions and quality config
	return editor.ProcessCodeGeneration(filename, enhancedInstructions, &qualityCfg, imagePath)
}

// ProcessCodeGenerationWithRollbackAndQuality generates code with rollback using quality prompts
func (qae *QualityAwareEditor) ProcessCodeGenerationWithRollbackAndQuality(
	filename, instructions string,
	cfg *config.Config,
	qualityLevel QualityLevel,
	imagePath string,
) (*editor.EditingOperationResult, error) {
	// Enhance instructions with quality guidelines
	enhancedInstructions := qae.enhanceInstructions(instructions, qualityLevel)

	// Set quality level in config for the editor to use
	qualityCfg := *cfg
	qualityCfg.QualityLevel = int(qualityLevel)

	// Call the underlying editor with enhanced instructions and quality config
	return editor.ProcessCodeGenerationWithRollback(filename, enhancedInstructions, &qualityCfg, imagePath)
}

// enhanceInstructions adds quality guidance to the instructions based on quality level
func (qae *QualityAwareEditor) enhanceInstructions(instructions string, qualityLevel QualityLevel) string {
	baseInstructions := instructions

	switch qualityLevel {
	case QualityProduction:
		return fmt.Sprintf(`%s

CRITICAL PRODUCTION REQUIREMENTS:
- Implement comprehensive error handling with specific exception types
- Add input validation for all parameters and edge cases
- Include proper logging for debugging and monitoring
- Ensure thread safety and handle concurrent access
- Implement security best practices (input sanitization, authentication)
- Add detailed documentation and comments for maintainability
- Consider performance optimization and resource efficiency
- Include unit tests or test-friendly interfaces
- Follow industry standards and security guidelines
- Validate all assumptions and handle failure scenarios gracefully`, baseInstructions)

	case QualityEnhanced:
		return fmt.Sprintf(`%s

QUALITY ENHANCEMENT REQUIREMENTS:
- Follow established code patterns and best practices
- Implement proper error handling and input validation
- Use meaningful variable and function names
- Add clear documentation for complex logic
- Consider edge cases and potential failure points
- Ensure code is maintainable and extensible
- Follow SOLID principles and design patterns where appropriate
- Add appropriate comments and documentation`, baseInstructions)

	default: // QualityStandard
		return baseInstructions
	}
}

// GetQualityGuidelines returns quality guidelines for a given level
func (qae *QualityAwareEditor) GetQualityGuidelines(qualityLevel QualityLevel) string {
	switch qualityLevel {
	case QualityProduction:
		return "Production-grade: Comprehensive error handling, security, performance, testing, and documentation"
	case QualityEnhanced:
		return "Enhanced quality: Best practices, proper structure, validation, and maintainability"
	default:
		return "Standard quality: Basic functionality with optimized efficiency"
	}
}

// AssessCodeQuality provides a quality assessment of generated code
func (qae *QualityAwareEditor) AssessCodeQuality(code string, language string) QualityAssessment {
	assessment := QualityAssessment{
		Language:  language,
		Score:     5, // Default middle score
		Issues:    []string{},
		Strengths: []string{},
	}

	codeLower := strings.ToLower(code)

	// Check for quality indicators
	qualityIndicators := []string{
		"error handling", "try", "catch", "exception", "validate",
		"documentation", "comment", "//", "/*", "test", "logging",
		"security", "sanitize", "authenticate", "authorize",
	}

	indicatorCount := 0
	for _, indicator := range qualityIndicators {
		if strings.Contains(codeLower, indicator) {
			indicatorCount++
		}
	}

	// Score based on quality indicators
	if indicatorCount >= 6 {
		assessment.Score = 9
		assessment.Strengths = append(assessment.Strengths, "Comprehensive quality practices")
	} else if indicatorCount >= 4 {
		assessment.Score = 7
		assessment.Strengths = append(assessment.Strengths, "Good quality practices")
	} else if indicatorCount >= 2 {
		assessment.Score = 6
		assessment.Strengths = append(assessment.Strengths, "Basic quality practices")
	} else {
		assessment.Score = 4
		assessment.Issues = append(assessment.Issues, "Limited quality practices detected")
	}

	// Check for potential issues
	issuePatterns := []string{
		"todo", "fixme", "hack", "temp", "temporary",
	}

	for _, pattern := range issuePatterns {
		if strings.Contains(codeLower, pattern) {
			assessment.Issues = append(assessment.Issues, fmt.Sprintf("Contains %s markers", pattern))
			if assessment.Score > 3 {
				assessment.Score--
			}
		}
	}

	return assessment
}

// QualityAssessment represents a code quality assessment
type QualityAssessment struct {
	Language  string
	Score     int // 1-10 scale
	Issues    []string
	Strengths []string
}
