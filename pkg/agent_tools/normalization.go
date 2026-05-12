package tools

import (
	"fmt"
	"strings"
)

func normalizeWhitespace(s string) string {
	// Replace tabs and multiple spaces with single space
	words := strings.Fields(s)
	return strings.Join(words, " ")
}

// normalizeWhitespaceWithMapping normalizes whitespace and returns a mapping
// from normalized positions to original content positions
func normalizeWhitespaceWithMapping(s string) (string, []int) {
	// Build normalized string and position mapping
	normalized := strings.Builder{}
	normalized.Grow(len(s))
	mapping := make([]int, 0, len(s)) // Maps normalized position to original position

	i := 0
	for i < len(s) {
		// Skip whitespace sequences
		if s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r' {
			// Skip all consecutive whitespace
			for i < len(s) && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
				i++
			}
			// Add single space to normalized
			if normalized.Len() > 0 {
				normalized.WriteByte(' ')
				// Map points to the position after the whitespace
				mapping = append(mapping, i)
			}
		} else {
			// Copy non-whitespace character
			for i < len(s) && s[i] != ' ' && s[i] != '\t' && s[i] != '\n' && s[i] != '\r' {
				normalized.WriteByte(s[i])
				mapping = append(mapping, i)
				i++
			}
		}
	}

	return normalized.String(), mapping
}

// validateMapping validates that the position mapping is correct and consistent
// Returns an error if any invalid positions are found
func validateMapping(input string, mapping []int, normalized string) error {
	// Check that all mapped positions are valid (within bounds)
	for i, pos := range mapping {
		if pos < 0 {
			return fmt.Errorf("mapping[%d] contains negative position %d", i, pos)
		}
		if pos > len(input) {
			return fmt.Errorf("mapping[%d] position %d exceeds input length %d", i, pos, len(input))
		}
	}

	// Verify the mapping corresponds to the normalized string
	// The mapping length should match the normalized string length
	if len(mapping) != len(normalized) {
		return fmt.Errorf("mapping length mismatch: mapping has %d entries but normalized string has %d characters", len(mapping), len(normalized))
	}

	// Check that positions are monotonically increasing (optional but helps catch bugs)
	for i := 1; i < len(mapping); i++ {
		if mapping[i] < mapping[i-1] {
			return fmt.Errorf("mapping is not monotonically increasing: mapping[%d]=%d < mapping[%d]=%d", i, mapping[i], i-1, mapping[i-1])
		}
	}

	return nil
}

// findAndReplaceWithNormalization performs smart replacement using normalized matching
// Returns the new content with the replacement applied
func findAndReplaceWithNormalization(content, oldString, newString, normalizedContent, normalizedOld string, contentMap []int) (string, error) {
	// Validate the mapping before using it
	if err := validateMapping(content, contentMap, normalizedContent); err != nil {
		return "", fmt.Errorf("validate position mapping: %w", err)
	}

	// Find position in normalized content
	normPos := strings.Index(normalizedContent, normalizedOld)
	if normPos == -1 {
		return "", fmt.Errorf("normalized string not found in normalized content")
	}

	// Map normalized position back to original content
	if normPos >= len(contentMap) {
		return "", fmt.Errorf("normalized position %d out of bounds for mapping (length %d)", normPos, len(contentMap))
	}

	startPos := contentMap[normPos]

	// Calculate end position in normalized string
	normEndPos := normPos + len(normalizedOld)

	// Determine the end position in original content
	// We need to find where the normalized match actually ends in the original
	var endPos int
	if normEndPos >= len(contentMap) {
		// The match extends to the end of the content
		endPos = len(content)
	} else {
		// Get the mapped position for the end of the normalized match
		candidateEndPos := contentMap[normEndPos]
		// The end position should be at or after the start position
		if candidateEndPos > startPos {
			endPos = candidateEndPos
		} else {
			// Fallback: Find the actual text segment in original content
			// Search forward from startPos to find where the normalized content would end
			endPos = findMatchEndPosition(content, startPos, normalizedOld)
		}
	}

	// Validate the end position
	if endPos <= startPos {
		return "", fmt.Errorf("invalid end position %d (start position %d), mapping may be corrupted", endPos, startPos)
	}
	if endPos > len(content) {
		endPos = len(content)
	}

	// Extract the actual text to replace from original content
	actualOldString := content[startPos:endPos]

	// Verify the extracted text is reasonable (should have similar length when normalized)
	actualNormalized := normalizeWhitespace(actualOldString)
	if actualNormalized != normalizedOld {
		// The mapping didn't work perfectly - try to find the match in original content
		// This can happen when whitespace patterns are complex
		return "", fmt.Errorf("could not accurately map normalized match to original content: expected normalized '%s', got '%s'", normalizedOld, actualNormalized)
	}

	// Perform the replacement on the original content
	newContent := strings.Replace(content, actualOldString, newString, 1)
	if newContent == content {
		return "", fmt.Errorf("replacement did not change content")
	}

	return newContent, nil
}

// findMatchEndPosition finds the end position of a match in the original content
// by searching forward from startPos for text that normalizes to normalizedOld
func findMatchEndPosition(content string, startPos int, normalizedOld string) int {
	// Extract a reasonable portion from startPos forward
	searchWindow := min(len(content)-startPos, len(normalizedOld)*4) // Allow for up to 4x whitespace expansion
	if searchWindow <= 0 {
		return len(content)
	}

	searchArea := content[startPos : startPos+searchWindow]

	// Try to find a segment that normalizes to our expected string
	// This is a heuristic approach for complex whitespace situations
	for end := len(normalizedOld); end <= len(searchArea); end++ {
		candidate := searchArea[:end]
		if normalizeWhitespace(candidate) == normalizedOld {
			return startPos + end
		}
	}

	// If no exact normal match found, use the search window end
	return startPos + searchWindow
}

// findLineNumber attempts to find the line number containing a string
// Returns 0 if not found
func findLineNumber(content, search string) int {
	lines := strings.Split(content, "\n")
	searchLower := strings.ToLower(search)
	searchNormalized := normalizeWhitespace(search)

	for i, line := range lines {
		lineLower := strings.ToLower(line)
		lineNormalized := normalizeWhitespace(line)

		// Try exact match (case-insensitive)
		if strings.Contains(lineLower, searchLower) {
			return i + 1
		}

		// Try normalized match
		if strings.Contains(lineNormalized, searchNormalized) && len(searchNormalized) > 10 {
			// Only return normalized match for longer strings to avoid false positives
			return i + 1
		}
	}

	return 0
}

// performNormalizedReplacement performs whitespace-normalized replacement
func performNormalizedReplacement(content, oldString, newString string) (string, error) {
	// Use smart replacement with normalization
	normalizedContent, contentMap := normalizeWhitespaceWithMapping(content)
	normalizedOld := normalizeWhitespace(oldString)

	newContent, err := findAndReplaceWithNormalization(content, oldString, newString, normalizedContent, normalizedOld, contentMap)
	if err != nil {
		return "", fmt.Errorf("perform normalized replacement: %w", err)
	}

	return newContent, nil
}
