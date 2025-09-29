package tools

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func EditFile(ctx context.Context, filePath, oldString, newString string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}
	if oldString == "" {
		return "", fmt.Errorf("empty old string provided")
	}

	// Clean the path
	cleanPath := filepath.Clean(filePath)

	// Check if file exists
	if _, err := os.Stat(cleanPath); os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", cleanPath)
	}

	// Read current content
	content, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
	}

	contentStr := string(content)

	// Check if old string exists
	if !strings.Contains(contentStr, oldString) {
		return "", fmt.Errorf("old string not found in file %s", cleanPath)
	}

	// Count occurrences to warn about multiple matches
	count := strings.Count(contentStr, oldString)
	if count > 1 {
		return "", fmt.Errorf("old string appears %d times in file %s - please use a more specific string", count, cleanPath)
	}

	// Replace the string
	newContent := strings.Replace(contentStr, oldString, newString, 1)

	// Write back to file
	err = os.WriteFile(cleanPath, []byte(newContent), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", cleanPath, err)
	}

	// Read back the updated file to confirm success and return content
	updatedContent, err := os.ReadFile(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to verify file edit by reading back %s: %w", cleanPath, err)
	}

	return fmt.Sprintf("File %s edited successfully - replaced %d characters with %d characters. Updated file content:\n\n%s",
		cleanPath, len(oldString), len(newString), string(updatedContent)), nil
}
