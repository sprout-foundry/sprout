package tools

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

func WriteFile(ctx context.Context, filePath, content string) (string, error) {
	// Off-workspace writes go through the active FilesystemGate (if
	// any) so the user can approve once, allow-list the folder for
	// the session, or elevate — instead of receiving a hard error on
	// the live seed dispatch path. See filesystem_gate.go and the
	// FilesystemGate interface in handler.go for the full contract.
	return withFilesystemApproval(ctx, FilesystemGateFromContext(ctx), "write_file", filePath,
		func(ctx context.Context) (string, error) {
			// SECURITY: Validate parent directory is safe to access (handles new files)
			cleanPath, err := filesystem.SafeResolvePathForWriteWithBypass(ctx, filePath)
			if err != nil {
				return "", fmt.Errorf("resolve file path for write: %w", err)
			}

			// Security check passed - now create directory if it doesn't exist
			dir := filepath.Dir(cleanPath)
			if err := os.MkdirAll(dir, 0755); err != nil {
				return "", fmt.Errorf("create directory %s: %w", dir, err)
			}

			// Preserve existing file permissions before writing
			var filePerm os.FileMode = 0644
			if fi, statErr := os.Stat(cleanPath); statErr == nil {
				filePerm = fi.Mode() & 0777
			}

			// Write the file
			err = os.WriteFile(cleanPath, []byte(content), filePerm)
			if err != nil {
				return "", fmt.Errorf("write file %s: %w", cleanPath, err)
			}

			// Log when permissions differ from default
			if filePerm != 0644 {
				log.Printf("[permissions] Preserving existing file mode %o for %s", filePerm, cleanPath)
			}

			// Read back the file to confirm successful write
			readContent, readErr := os.ReadFile(cleanPath)
			if readErr != nil {
				return "", fmt.Errorf("read back written file for verification: %w", readErr)
			}

			// Get file info for confirmation
			info, err := os.Stat(cleanPath)
			if err != nil {
				return fmt.Sprintf("File %s written successfully", cleanPath), nil
			}

			// Return summary instead of full content to save LLM tokens
			return formatWriteSummary(cleanPath, readContent, info.Size()), nil
		})
}

// formatWriteSummary creates a summary of the written file with line count, byte count, and a preview.
func formatWriteSummary(path string, content []byte, size int64) string {
	lines := countLines(content)

	if lines <= 10 {
		// Short file: return full content
		return fmt.Sprintf("Successfully wrote %s (%d lines, %d bytes)\n\n%s", path, lines, size, string(content))
	}

	// Long file: return first 5 and last 5 lines with truncation notice
	allLines := splitLines(content)
	firstLines := allLines[:5]
	lastLines := allLines[len(allLines)-5:]

	log.Printf("[write] Returning summary for %s (%d lines)", path, lines)

	return fmt.Sprintf("Successfully wrote %s (%d lines, %d bytes)\n--- First 5 lines ---\n%s...--- Last 5 lines ---\n%s",
		path, lines, size, joinLines(firstLines), joinLines(lastLines))
}

// countLines counts the number of lines in the content.
func countLines(content []byte) int {
	if len(content) == 0 {
		return 0
	}
	count := 1
	for _, b := range content {
		if b == '\n' {
			count++
		}
	}
	return count
}

// splitLines splits content into individual lines.
func splitLines(content []byte) []string {
	result := []string{}
	start := 0
	for i, b := range content {
		if b == '\n' {
			result = append(result, string(content[start:i]))
			start = i + 1
		}
	}
	// Add last line (may be empty if file ends with newline)
	if start < len(content) {
		result = append(result, string(content[start:]))
	} else if len(content) > 0 && content[len(content)-1] == '\n' {
		// File ends with newline - add empty last line
		result = append(result, "")
	}
	return result
}

// joinLines joins lines with newlines.
func joinLines(lines []string) string {
	result := ""
	for i, line := range lines {
		if i > 0 {
			result += "\n"
		}
		result += line
	}
	return result
}
