package text

import (
	"strings"

	"github.com/alantheprice/ledit/pkg/config"
)

// GetSummary returns a summary, exports, and references for the given content.
func GetSummary(content, path string, cfg *config.Config) (string, string, string, error) {
	// Simple heuristic-based summarization for now.
	summary := "File summary"
	exports := "File exports"
	references := "File references"
	return summary, exports, references, nil
}

// ExtractKeywords extracts keywords from a string.
func ExtractKeywords(text string) []string {
	return strings.Fields(text)
}

