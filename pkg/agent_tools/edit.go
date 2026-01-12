package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

func EditFile(ctx context.Context, filePath, oldString, newString string) (string, error) {
	// Step 1: Validate inputs
	if err := validateEditInputs(filePath, oldString, newString); err != nil {
		return "", err
	}

	// Step 2: Resolve and validate file
	cleanPath, originalMode, err := resolveAndValidateFile(filePath)
	if err != nil {
		return "", err
	}

	// Step 3: Read file content
	contentStr, err := readFileContent(cleanPath)
	if err != nil {
		return "", err
	}

	// Step 4: Determine and perform replacement
	newContent, err := determineAndPerformReplacement(contentStr, oldString, newString, cleanPath)
	if err != nil {
		return "", err
	}

	// Step 5: Write file with preserved permissions
	if err := writeFileWithPermissions(cleanPath, []byte(newContent), originalMode.Perm()); err != nil {
		return "", err
	}

	// Step 6: Verify edit was successful
	if err := verifyEdit(cleanPath, newString); err != nil {
		return "", err
	}

	// Return concise confirmation with character counts
	return fmt.Sprintf("Edited %s: replaced %d characters with %d characters", cleanPath, len(oldString), len(newString)), nil
}

// validateEditInputs validates filePath, oldString, newString and checks for suspicious patterns
func validateEditInputs(filePath, oldString, newString string) error {
	if filePath == "" {
		return fmt.Errorf("empty file path provided")
	}
	if oldString == "" {
		return fmt.Errorf("empty old string provided")
	}

	// Sanitize input strings to prevent injection attacks
	// Check for suspicious patterns in oldString and newString
	suspiciousPatterns := []string{
		"../",   // Path traversal attempt
		"..\\",  // Windows path traversal
		"\\0",   // Null byte attempts in string literals
		"\x00",  // Actual null bytes
	}

	checkString := func(s, name string) error {
		for _, pattern := range suspiciousPatterns {
			if strings.Contains(s, pattern) {
				return fmt.Errorf("security violation: %s contains suspicious pattern '%s'", name, pattern)
			}
		}
		return nil
	}

	if err := checkString(oldString, "old string"); err != nil {
		return err
	}

	if err := checkString(newString, "new string"); err != nil {
		return err
	}

	return nil
}

// resolveAndValidateFile resolves path using filesystem.SafeResolvePath and checks file exists
// Returns the resolved path, original file mode, and any error
func resolveAndValidateFile(filePath string) (string, os.FileMode, error) {
	// SECURITY: Validate path is within working directory (handles symlinks properly)
	cleanPath, err := filesystem.SafeResolvePath(filePath)
	if err != nil {
		return "", 0, err
	}

	// Security check passed - now check if file exists
	// This must come AFTER the security check to prevent information disclosure
	fileInfo, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return "", 0, fmt.Errorf("file does not exist: %s", cleanPath)
	}
	if err != nil {
		return "", 0, fmt.Errorf("failed to stat file %s: %w", cleanPath, err)
	}

	// Preserve original file permissions
	originalMode := fileInfo.Mode()

	return cleanPath, originalMode, nil
}

// readFileContent reads file content and returns as string
func readFileContent(cleanPath string) (string, error) {
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}

	return string(content), nil
}

// performExactReplacement performs exact string replacement with count check
// Returns the new content or an error if multiple matches found
func performExactReplacement(content, oldString, newString, cleanPath string) (string, error) {
	// Count occurrences to warn about multiple matches
	count := strings.Count(content, oldString)
	if count > 1 {
		return "", fmt.Errorf("old string appears %d times in file %s - please use a more specific string", count, cleanPath)
	}

	// Use standard exact replacement
	newContent := strings.Replace(content, oldString, newString, 1)
	return newContent, nil
}

// performNormalizedReplacement performs whitespace-normalized replacement
func performNormalizedReplacement(content, oldString, newString string) (string, error) {
	// Use smart replacement with normalization
	normalizedContent, contentMap := normalizeWhitespaceWithMapping(content)
	normalizedOld := normalizeWhitespace(oldString)

	newContent, err := findAndReplaceWithNormalization(content, oldString, newString, normalizedContent, normalizedOld, contentMap)
	if err != nil {
		return "", fmt.Errorf("failed to perform normalized replacement: %w", err)
	}

	return newContent, nil
}

// determineAndPerformReplacement determines if exact match or normalized match is needed
// Returns the new content
func determineAndPerformReplacement(content, oldString, newString, cleanPath string) (string, error) {
	// Track if we need exact match or used normalized match
	usedNormalizedMatch := false

	// Try exact match first (fast path)
	if !strings.Contains(content, oldString) {
		// Only try normalization for reasonably long strings to avoid unnecessary processing
		// Short strings (< 10 chars) are unlikely to benefit from whitespace normalization
		oldStringLen := len(oldString)
		if oldStringLen < 10 {
			lineNum := findLineNumber(content, oldString)
			if lineNum > 0 {
				return "", fmt.Errorf("old string not found in file %s (closest match around line %d) - check for exact spelling and whitespace", cleanPath, lineNum)
			}
			return "", fmt.Errorf("old string not found in file %s", cleanPath)
		}

		// Try with whitespace normalization for longer strings
		normalizedContent, _ := normalizeWhitespaceWithMapping(content)
		normalizedOld := normalizeWhitespace(oldString)

		if strings.Contains(normalizedContent, normalizedOld) {
			// Found with normalized whitespace - use smart replacement
			usedNormalizedMatch = true
		} else {
			// Not found even with normalization - try to find closest match for better error
			lineNum := findLineNumber(content, oldString)
			if lineNum > 0 {
				return "", fmt.Errorf("old string not found in file %s (closest match around line %d) - check for exact spelling and whitespace", cleanPath, lineNum)
			}
			return "", fmt.Errorf("old string not found in file %s", cleanPath)
		}
	}

	var newContent string
	var err error

	if usedNormalizedMatch {
		// Use smart replacement with normalization
		newContent, err = performNormalizedReplacement(content, oldString, newString)
		if err != nil {
			return "", err
		}
	} else {
		// Use standard exact replacement
		newContent, err = performExactReplacement(content, oldString, newString, cleanPath)
		if err != nil {
			return "", err
		}
	}

	return newContent, nil
}

// writeFileWithPermissions writes content preserving permissions
func writeFileWithPermissions(cleanPath string, content []byte, perm os.FileMode) error {
	err := os.WriteFile(cleanPath, content, perm)
	if err != nil {
		return fmt.Errorf("failed to write file %s: %w", cleanPath, err)
	}
	return nil
}

// verifyEdit verifies edit was successful by reading back
func verifyEdit(cleanPath string, newString string) error {
	// Verify the edit was successful
	updatedContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Errorf("failed to verify file edit by reading back %s: %w", cleanPath, err)
	}

	// Check that the replacement actually happened
	if !strings.Contains(string(updatedContent), newString) {
		return fmt.Errorf("edit verification failed - new string not found in file after write")
	}

	return nil
}

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
		return "", fmt.Errorf("position mapping invalid: %w", err)
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
