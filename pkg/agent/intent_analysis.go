package agent

import (
	"strings"
)

// Fallback functions for when LLM analysis fails
func inferCategory(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	if strings.Contains(intentLower, "test") {
		return "test"
	}
	if strings.Contains(intentLower, "fix") || strings.Contains(intentLower, "bug") {
		return "fix"
	}
	if strings.Contains(intentLower, "comment") || strings.Contains(intentLower, "doc") {
		return "docs"
	}
	if strings.Contains(intentLower, "review") {
		return "review"
	}
	return "code"
}

func inferComplexity(userIntent string) string {
	intentLower := strings.ToLower(userIntent)
	complexWords := []string{"refactor", "architect", "multiple", "design"} // Removed "system"
	simpleWords := []string{"add", "comment", "fix typo", "single"}

	for _, word := range complexWords {
		if strings.Contains(intentLower, word) {
			return "complex"
		}
	}

	for _, word := range simpleWords {
		if strings.Contains(intentLower, word) {
			return "simple"
		}
	}

	return "moderate"
}
