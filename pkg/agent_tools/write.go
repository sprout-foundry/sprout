package tools

import (
	"fmt"
	"os"
	"path/filepath"
)

func WriteFile(filePath, content string) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}

	// Clean the path
	cleanPath := filepath.Clean(filePath)

	// Create directory if it doesn't exist
	dir := filepath.Dir(cleanPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return "", fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	// Write the file
	err := os.WriteFile(cleanPath, []byte(content), 0644)
	if err != nil {
		return "", fmt.Errorf("failed to write file %s: %w", cleanPath, err)
	}

	// Read back the file to confirm successful write and return content
	readContent, readErr := os.ReadFile(cleanPath)
	if readErr != nil {
		return "", fmt.Errorf("file written but failed to read back for verification: %w", readErr)
	}

	// Get file info for confirmation
	info, err := os.Stat(cleanPath)
	if err != nil {
		return fmt.Sprintf("File %s written successfully. Content:\n\n%s", cleanPath, string(readContent)), nil
	}

	return fmt.Sprintf("File %s written successfully (%d bytes). Content:\n\n%s", cleanPath, info.Size(), string(readContent)), nil
}
