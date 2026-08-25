package tools

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

func EditFile(ctx context.Context, filePath, oldString, newString string) (string, error) {
	// Step 1: Validate inputs
	if err := validateEditInputs(filePath, oldString, newString); err != nil {
		return "", fmt.Errorf("validate edit inputs: %w", err)
	}

	// Step 2: Resolve and validate file. The gate is consulted only
	// here — the rest of the edit operates on an already-resolved
	// path, so a successful resolve means the work happens inside the
	// approved scope (workspace, session-allowed folder, or bypass
	// from a one-shot / elevation approval).
	cleanPath, originalMode, err := resolveAndValidateFileWithGate(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("resolve and validate file %s: %w", filePath, err)
	}

	// Step 3: Read file content
	contentStr, err := readFileContent(cleanPath)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", cleanPath, err)
	}

	// Step 4: Determine and perform replacement
	newContent, err := determineAndPerformReplacement(contentStr, oldString, newString, cleanPath)
	if err != nil {
		return "", fmt.Errorf("perform replacement: %w", err)
	}

	// Step 5: Write file with preserved permissions
	if err := writeFileWithPermissions(cleanPath, []byte(newContent), originalMode.Perm()); err != nil {
		return "", fmt.Errorf("write file %s: %w", cleanPath, err)
	}

	// Step 6: Verify edit was successful
	if err := verifyEdit(cleanPath, newString); err != nil {
		return "", fmt.Errorf("verify edit: %w", err)
	}

	// Return concise confirmation with character counts
	return fmt.Sprintf("Edited %s: replaced %d characters with %d characters", cleanPath, len(oldString), len(newString)), nil
}

// resolveAndValidateFileWithGate resolves and validates the file. PrecheckFileAccess
// should have already set up the bypass context for "allow" paths.
func resolveAndValidateFileWithGate(ctx context.Context, filePath string) (string, os.FileMode, error) {
	return resolveAndValidateFile(ctx, filePath)
}

// validateEditInputs validates filePath, oldString, newString and checks for suspicious patterns
func validateEditInputs(filePath, oldString, newString string) error {
	if filePath == "" {
		return fmt.Errorf("empty file path provided")
	}
	if oldString == "" {
		return fmt.Errorf("empty old string provided")
	}

	// Content validation removed - static classifier in security_classifier.go handles this
	// Pattern matching on content (like "../") was blocking legitimate code edits
	// Path security is handled by SafeResolvePathWithBypass
	// Operation security is handled by the static classifier in security_classifier.go
	// Only check for actual null bytes which could cause issues
	suspiciousPatterns := []string{
		"\x00", // Actual null bytes (can cause issues with string handling)
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
		return fmt.Errorf("validate old string: %w", err)
	}

	if err := checkString(newString, "new string"); err != nil {
		return fmt.Errorf("validate new string: %w", err)
	}

	return nil
}

// resolveAndValidateFile resolves path using filesystem.SafeResolvePath and checks file exists
// Returns the resolved path, original file mode, and any error
func resolveAndValidateFile(ctx context.Context, filePath string) (string, os.FileMode, error) {
	// SECURITY: Validate path is within working directory (handles symlinks properly)
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, filePath)
	if err != nil {
		return "", 0, fmt.Errorf("resolve path %q: %w", filePath, err)
	}

	// Security check passed - now check if file exists
	// This must come AFTER the security check to prevent information disclosure
	fileInfo, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return "", 0, fmt.Errorf("file does not exist: %s", cleanPath)
	}
	if err != nil {
		return "", 0, fmt.Errorf("stat file %s: %w", cleanPath, err)
	}

	// Preserve original file permissions
	originalMode := fileInfo.Mode()

	return cleanPath, originalMode, nil
}

// readFileContent reads file content and returns as string
func readFileContent(cleanPath string) (string, error) {
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("read file %s: %w", cleanPath, err)
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
			return "", fmt.Errorf("perform normalized replacement: %w", err)
		}
	} else {
		// Use standard exact replacement
		newContent, err = performExactReplacement(content, oldString, newString, cleanPath)
		if err != nil {
			return "", fmt.Errorf("perform exact replacement: %w", err)
		}
	}

	return newContent, nil
}

// writeFileWithPermissions writes content preserving permissions
func writeFileWithPermissions(cleanPath string, content []byte, perm os.FileMode) error {
	err := os.WriteFile(cleanPath, content, perm)
	if err != nil {
		return fmt.Errorf("write file %s: %w", cleanPath, err)
	}
	return nil
}

// verifyEdit verifies edit was successful by reading back
func verifyEdit(cleanPath string, newString string) error {
	// Verify the edit was successful
	updatedContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return fmt.Errorf("verify file edit by reading back %s: %w", cleanPath, err)
	}

	// Check that the replacement actually happened
	if !strings.Contains(string(updatedContent), newString) {
		return fmt.Errorf("edit verification failed - new string not found in file after write")
	}

	return nil
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
