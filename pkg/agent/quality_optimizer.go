package agent

import (
	"fmt"
	"github.com/alantheprice/ledit/pkg/config"
	"strings"
)

// QualityLevel represents the desired quality level for code generation
type QualityLevel int

const (
	QualityStandard   QualityLevel = iota // Use optimized prompts for speed
	QualityEnhanced                       // Use quality-enhanced prompts for better code
	QualityProduction                     // Use production-grade prompts with full validation
)

// QualityOptimizer determines the appropriate quality level and prompts to use
type QualityOptimizer struct {
	cache *PromptCache
}

// NewQualityOptimizer creates a new quality optimizer
func NewQualityOptimizer() *QualityOptimizer {
	return &QualityOptimizer{
		cache: GetPromptCache(),
	}
}

// DetermineQualityLevel analyzes the task and determines appropriate quality level
func (qo *QualityOptimizer) DetermineQualityLevel(userIntent string, taskIntent TaskIntent, cfg *config.Config) QualityLevel {
	intent := strings.ToLower(userIntent)

	// Production-level quality indicators
	productionKeywords := []string{
		"production", "deploy", "release", "security", "authentication",
		"critical", "enterprise", "scalable", "performance", "optimization",
		"api", "database", "payment", "user data", "sensitive",
	}

	for _, keyword := range productionKeywords {
		if strings.Contains(intent, keyword) {
			return QualityProduction
		}
	}

	// Enhanced quality indicators
	enhancedKeywords := []string{
		"implement", "create new", "build", "architecture", "design",
		"service", "class", "module", "component", "system", "algorithm",
		"error handling", "validation", "testing", "complex",
	}

	for _, keyword := range enhancedKeywords {
		if strings.Contains(intent, keyword) {
			return QualityEnhanced
		}
	}

	// Task-based quality determination
	switch taskIntent {
	case TaskIntentCreation, TaskIntentRefactoring:
		return QualityEnhanced
	case TaskIntentDocumentation, TaskIntentAnalysis:
		return QualityStandard
	default:
		// Check if it's a simple modification
		simpleKeywords := []string{"fix typo", "update comment", "change string", "add log"}
		for _, keyword := range simpleKeywords {
			if strings.Contains(intent, keyword) {
				return QualityStandard
			}
		}
		return QualityEnhanced
	}
}

// GetPromptForTask returns the appropriate prompt based on quality level and task type
func (qo *QualityOptimizer) GetPromptForTask(taskType string, qualityLevel QualityLevel) (string, error) {
	var promptFile string

	switch taskType {
	case "code_editing":
		switch qualityLevel {
		case QualityProduction, QualityEnhanced:
			promptFile = "base_code_editing_quality_enhanced.txt"
		default:
			promptFile = "base_code_editing_optimized.txt"
		}
	case "code_generation":
		switch qualityLevel {
		case QualityProduction, QualityEnhanced:
			promptFile = "code_generation_system_quality_enhanced.txt"
		default:
			promptFile = "code_generation_system_optimized.txt"
		}
	case "code_review":
		switch qualityLevel {
		case QualityProduction, QualityEnhanced:
			promptFile = "code_review_system_quality_enhanced.txt"
		default:
			promptFile = "code_review_system_optimized.txt"
		}
	case "interactive_code_generation":
		switch qualityLevel {
		case QualityProduction, QualityEnhanced:
			promptFile = "interactive_code_generation_quality_enhanced.txt"
		default:
			promptFile = "interactive_code_generation_optimized.txt"
		}
	default:
		// Fallback to optimized prompts
		switch taskType {
		case "code_editing":
			promptFile = "base_code_editing_optimized.txt"
		case "code_generation":
			promptFile = "code_generation_system_optimized.txt"
		case "code_review":
			promptFile = "code_review_system_optimized.txt"
		default:
			return "", fmt.Errorf("unknown task type: %s", taskType)
		}
	}

	return qo.cache.GetCachedPrompt(promptFile)
}

// GetQualityPromptWithFallback returns quality prompt with fallback to optimized
func (qo *QualityOptimizer) GetQualityPromptWithFallback(taskType string, qualityLevel QualityLevel, fallback string) string {
	prompt, err := qo.GetPromptForTask(taskType, qualityLevel)
	if err != nil {
		return fallback
	}
	return prompt
}

// EstimateCodeComplexity estimates the complexity of a code change request
func EstimateCodeComplexity(userIntent string, fileCount int) ComplexityLevel {
	intent := strings.ToLower(userIntent)

	// Very complex indicators
	veryComplexKeywords := []string{
		"architecture", "refactor entire", "migrate", "rewrite", "redesign",
		"microservice", "distributed", "concurrent", "parallel", "async",
		"performance optimization", "scaling", "load balancing",
	}

	for _, keyword := range veryComplexKeywords {
		if strings.Contains(intent, keyword) {
			return ComplexityVeryComplex
		}
	}

	// Complex indicators
	complexKeywords := []string{
		"implement", "create new", "build system", "add feature",
		"authentication", "security", "database", "api",
		"algorithm", "data structure", "design pattern",
	}

	for _, keyword := range complexKeywords {
		if strings.Contains(intent, keyword) {
			return ComplexityComplex
		}
	}

	// File count based complexity
	if fileCount > 5 {
		return ComplexityComplex
	} else if fileCount > 2 {
		return ComplexityModerate
	}

	// Simple indicators
	simpleKeywords := []string{
		"fix typo", "update comment", "change string", "add log",
		"update version", "fix formatting", "rename variable",
	}

	for _, keyword := range simpleKeywords {
		if strings.Contains(intent, keyword) {
			return ComplexitySimple
		}
	}

	return ComplexityModerate
}
