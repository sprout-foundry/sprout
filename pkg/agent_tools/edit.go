package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

func EditFile(ctx context.Context, filePath, oldString, newString string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}
	if oldString == "" {
		return "", fmt.Errorf("empty old string provided")
	}

	// Sanitize input strings to prevent injection attacks
	// Check for suspicious patterns in oldString and newString
	suspiciousPatterns := []string{
		"../",       // Path traversal attempt
		"..\\",      // Windows path traversal
		"\\0",       // Null byte attempts in string literals
		"\x00",      // Actual null bytes
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
		return "", err
	}

	if err := checkString(newString, "new string"); err != nil {
		return "", err
	}

	// SECURITY: Validate path is within working directory (handles symlinks properly)
	cleanPath, err := filesystem.SafeResolvePath(filePath)
	if err != nil {
		return "", err
	}

	// Security check passed - now check if file exists
	// This must come AFTER the security check to prevent information disclosure
	fileInfo, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", cleanPath)
	}
	if err != nil {
		return "", fmt.Errorf("failed to stat file %s: %w", cleanPath, err)
	}

	// Preserve original file permissions
	originalMode := fileInfo.Mode()

	// Read current content
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}

	contentStr := string(content)

	// Try exact match first
	if !strings.Contains(contentStr, oldString) {
		// Try with whitespace normalization
		normalizedContent := normalizeWhitespace(contentStr)
		normalizedOld := normalizeWhitespace(oldString)

		if strings.Contains(normalizedContent, normalizedOld) {
			// Found with normalized whitespace - provide helpful error
			lineNum := findLineNumber(contentStr, oldString)
			if lineNum > 0 {
				return "", fmt.Errorf("old string found at line %d but with different whitespace - please ensure exact match including spaces/tabs", lineNum)
			}
			return "", fmt.Errorf("old string found but with different whitespace - please ensure exact match including spaces/tabs")
		}

		// Not found even with normalization - try to find closest match for better error
		lineNum := findLineNumber(contentStr, oldString)
		if lineNum > 0 {
			return "", fmt.Errorf("old string not found in file %s (closest match around line %d) - check for exact spelling and whitespace", cleanPath, lineNum)
		}
		return "", fmt.Errorf("old string not found in file %s", cleanPath)
	}

	// Count occurrences to warn about multiple matches
	count := strings.Count(contentStr, oldString)
	if count > 1 {
		return "", fmt.Errorf("old string appears %d times in file %s - please use a more specific string", count, cleanPath)
	}

	// Replace the string
	newContent := strings.Replace(contentStr, oldString, newString, 1)

	// Write back to file with preserved permissions
	err = os.WriteFile(cleanPath, []byte(newContent), originalMode.Perm())
	if err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", cleanPath, err)
	}

	// Verify the edit was successful
	updatedContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to verify file edit by reading back %s: %w", cleanPath, err)
	}

	// Check that the replacement actually happened
	if !strings.Contains(string(updatedContent), newString) {
		return "", fmt.Errorf("edit verification failed - new string not found in file after write")
	}

	// Return concise confirmation with character counts
	return fmt.Sprintf("Edited %s: replaced %d characters with %d characters", cleanPath, len(oldString), len(newString)), nil
}

// normalizeWhitespace normalizes whitespace for comparison
// Collapses multiple spaces/tabs to single space and trims leading/trailing
func normalizeWhitespace(s string) string {
	// Replace tabs and multiple spaces with single space
	words := strings.Fields(s)
	return strings.Join(words, " ")
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
