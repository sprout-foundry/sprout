package codereview

import (
	"crypto/md5"
	"errors"
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/types"
)

// removeDuplicates removes duplicate entries from a string slice
func (s *CodeReviewService) removeDuplicates(items []string) []string {
	seen := make(map[string]bool)
	result := []string{}

	for _, item := range items {
		if !seen[item] {
			seen[item] = true
			result = append(result, item)
		}
	}

	return result
}

// removeAffectedFiles removes files that are already in the affected files list
func (s *CodeReviewService) removeAffectedFiles(related, affected []string) []string {
	affectedSet := make(map[string]bool)
	for _, file := range affected {
		affectedSet[file] = true
	}

	result := []string{}
	for _, file := range related {
		if !affectedSet[file] {
			result = append(result, file)
		}
	}

	return result
}

// extractAffectedFilesFromDiff parses a diff to find which files are being modified
func (s *CodeReviewService) extractAffectedFilesFromDiff(diff string) []string {
	var files []string
	lines := strings.Split(diff, "\n")

	for _, line := range lines {
		// Look for diff headers that indicate file paths
		if strings.HasPrefix(line, "diff --git") {
			// Parse "diff --git a/file.go b/file.go" format
			parts := strings.Fields(line)
			if len(parts) >= 4 {
				filePath := strings.TrimPrefix(parts[2], "a/")
				files = append(files, filePath)
			}
		} else if strings.HasPrefix(line, "---") || strings.HasPrefix(line, "+++") {
			// Parse "--- a/file.go" or "+++ b/file.go" format
			parts := strings.Fields(line)
			if len(parts) >= 2 && !strings.Contains(parts[1], "/dev/null") {
				filePath := strings.TrimPrefix(parts[1], "a/")
				filePath = strings.TrimPrefix(filePath, "b/")
				files = append(files, filePath)
			}
		}
	}

	return s.removeDuplicates(files)
}

// calculateContentHash calculates a hash of the content for change detection
func (s *CodeReviewService) calculateContentHash(content string) string {
	hash := md5.Sum([]byte(content))
	return fmt.Sprintf("%x", hash)
}

// calculateSimilarity calculates the similarity between two strings using Jaccard similarity
func (s *CodeReviewService) calculateSimilarity(str1, str2 string) float64 {
	// Normalize strings by converting to lowercase and splitting into words
	words1 := strings.Fields(strings.ToLower(str1))
	words2 := strings.Fields(strings.ToLower(str2))

	// Handle empty strings
	if len(words1) == 0 && len(words2) == 0 {
		return 1.0
	}
	if len(words1) == 0 || len(words2) == 0 {
		return 0.0
	}

	// Create sets of unique words
	set1 := make(map[string]bool)
	set2 := make(map[string]bool)

	for _, word := range words1 {
		// Remove punctuation and normalize
		word = strings.Trim(word, ".,!?;:")
		if word != "" {
			set1[word] = true
		}
	}
	for _, word := range words2 {
		word = strings.Trim(word, ".,!?;:")
		if word != "" {
			set2[word] = true
		}
	}

	// Calculate intersection
	intersection := 0
	for word := range set1 {
		if set2[word] {
			intersection++
		}
	}

	// Calculate union
	union := len(set1) + len(set2) - intersection

	if union == 0 {
		return 0.0
	}

	return float64(intersection) / float64(union)
}

// applyPatchToContent applies the patch resolution content directly
func (s *CodeReviewService) applyPatchToContent(patchResolution *types.PatchResolution, feedback string) error {
	if patchResolution == nil {
		return errors.New("patch resolution is nil")
	}

	// Handle multi-file patches
	if len(patchResolution.MultiFile) > 0 {
		s.logger.LogProcessStep(fmt.Sprintf("Applying multi-file patch with %d files", len(patchResolution.MultiFile)))
		for filePath := range patchResolution.MultiFile {
			s.logger.LogProcessStep(fmt.Sprintf("Would apply patch to: %s", filePath))
		}
		// For now, return an error to signal that multi-file patches need to be applied
		return fmt.Errorf("multi-file patch resolution needs to be applied: %d files to update", len(patchResolution.MultiFile))
	}

	// Handle single file patches (backward compatibility)
	if patchResolution.SingleFile != "" {
		s.logger.LogProcessStep("Applying single-file patch")
		// For now, return an error to signal that the patch needs to be applied
		return fmt.Errorf("single-file patch resolution needs to be applied: %d characters", len(patchResolution.SingleFile))
	}

	return errors.New("patch resolution is empty")
}

// validatePatchContent validates the patch resolution content
func (s *CodeReviewService) validatePatchContent(content string) error {
	_ = content // Suppress unused parameter warning for now
	// Check for extremely short content
	if len(strings.TrimSpace(content)) < 5 {
		return fmt.Errorf("patch content is suspiciously short (%d characters)", len(content))
	}

	// Check for content that looks like instructions rather than actual code
	contentLower := strings.ToLower(content)
	if strings.Contains(contentLower, "replace the") && len(content) < 50 {
		return errors.New("patch content appears to be replacement instructions rather than actual code")
	}

	// Check for basic code structure indicators
	hasCodeIndicators := strings.Contains(content, "package") ||
		strings.Contains(content, "func") ||
		strings.Contains(content, "import") ||
		strings.Contains(content, "var") ||
		strings.Contains(content, "type") ||
		strings.Contains(content, "const")

	if !hasCodeIndicators && len(content) > 20 {
		s.logger.LogProcessStep("Warning: Patch content doesn't appear to contain typical Go code structures")
	}

	// Check for balanced braces/brackets
	braceCount := strings.Count(content, "{") - strings.Count(content, "}")
	bracketCount := strings.Count(content, "[") - strings.Count(content, "]")
	parenCount := strings.Count(content, "(") - strings.Count(content, ")")

	if braceCount != 0 || bracketCount != 0 || parenCount != 0 {
		s.logger.LogProcessStep(fmt.Sprintf("Warning: Patch content has unbalanced delimiters (braces: %d, brackets: %d, parens: %d)",
			braceCount, bracketCount, parenCount))
	}

	return nil
}
