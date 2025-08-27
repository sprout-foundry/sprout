package agent

import (
	"github.com/alantheprice/ledit/pkg/config"
	"strings"
	"time"
)

// GetSmartTimeout returns an optimized timeout based on request complexity and model characteristics
func GetSmartTimeout(cfg *config.Config, modelName string, taskType string) time.Duration {
	baseTimeout := 120 * time.Second // 2 minutes default

	// Adjust base timeout by provider characteristics
	if strings.Contains(strings.ToLower(modelName), "deepinfra") {
		baseTimeout = 180 * time.Second // DeepInfra can be slower
	} else if strings.Contains(strings.ToLower(modelName), "groq") {
		baseTimeout = 60 * time.Second // Groq is typically fast
	} else if strings.Contains(strings.ToLower(modelName), "ollama") {
		baseTimeout = 300 * time.Second // Local models need more time
	} else if strings.Contains(strings.ToLower(modelName), "deepseek-r1") {
		baseTimeout = 300 * time.Second // Reasoning models need more time
	}

	// Adjust by task complexity
	switch taskType {
	case "analysis":
		// Analysis tasks are typically faster
		return time.Duration(float64(baseTimeout) * 0.75)
	case "documentation":
		// Documentation tasks can be slower due to large outputs
		return time.Duration(float64(baseTimeout) * 1.5)
	case "code_generation":
		// Code generation is baseline
		return baseTimeout
	case "refactoring":
		// Refactoring can be complex
		return time.Duration(float64(baseTimeout) * 1.25)
	case "creation":
		// Creation tasks can be complex
		return time.Duration(float64(baseTimeout) * 1.25)
	default:
		return baseTimeout
	}
}

// GetTimeoutForComplexity adjusts timeout based on estimated complexity
func GetTimeoutForComplexity(baseTimeout time.Duration, complexity ComplexityLevel) time.Duration {
	switch complexity {
	case ComplexitySimple:
		return time.Duration(float64(baseTimeout) * 0.6) // 60% of base
	case ComplexityModerate:
		return baseTimeout // 100% of base
	case ComplexityComplex:
		return time.Duration(float64(baseTimeout) * 1.5) // 150% of base
	case ComplexityVeryComplex:
		return time.Duration(float64(baseTimeout) * 2.0) // 200% of base
	default:
		return baseTimeout
	}
}

// ComplexityLevel represents the estimated complexity of a task
type ComplexityLevel int

const (
	ComplexitySimple ComplexityLevel = iota
	ComplexityModerate
	ComplexityComplex
	ComplexityVeryComplex
)

// EstimateComplexity estimates the complexity of a user request
func EstimateComplexity(userIntent string, fileCount int) ComplexityLevel {
	intent := strings.ToLower(userIntent)

	// Simple tasks
	simpleKeywords := []string{"fix typo", "add comment", "update version", "change string"}
	for _, keyword := range simpleKeywords {
		if strings.Contains(intent, keyword) {
			return ComplexitySimple
		}
	}

	// Very complex tasks
	complexKeywords := []string{"refactor", "restructure", "migrate", "overhaul", "comprehensive", "complete rewrite"}
	for _, keyword := range complexKeywords {
		if strings.Contains(intent, keyword) {
			return ComplexityVeryComplex
		}
	}

	// File count based complexity
	if fileCount <= 1 {
		return ComplexitySimple
	} else if fileCount <= 3 {
		return ComplexityModerate
	} else if fileCount <= 7 {
		return ComplexityComplex
	} else {
		return ComplexityVeryComplex
	}
}
