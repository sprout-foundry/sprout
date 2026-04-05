package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/alantheprice/ledit/pkg/filesystem"
)

// File size constants for read operations
const (
	// defaultMaxFileSize is the maximum file size (in bytes) for reading files
	// Files larger than this will be truncated with head+tail and a warning
	// ~20,000 tokens at 4 chars/token heuristic (~15% of 128K context window)
	defaultMaxFileSize = 80 * 1024 // 80KB default

	// lineRangeMaxSize is the maximum file size (in bytes) when a line range is requested
	// This is larger to ensure we can read enough content for accurate line counts
	lineRangeMaxSize = 10 * 1024 * 1024 // 10MB for line range requests
)

func getFileReadMaxSize() int {
	if raw := os.Getenv("LEDIT_READ_FILE_MAX_BYTES"); raw != "" {
		if size, err := strconv.Atoi(raw); err == nil && size > 0 {
			return size
		}
	}
	return defaultMaxFileSize
}

func ReadFile(ctx context.Context, filePath string) (string, error) {
	return ReadFileWithRange(ctx, filePath, 0, 0)
}

func ReadFileWithRange(ctx context.Context, filePath string, startLine, endLine int) (string, error) {
	// SECURITY: Validate path is within working directory (handles symlinks properly)
	cleanPath, err := filesystem.SafeResolvePathWithBypass(ctx, filePath)
	if err != nil {
		return "", fmt.Errorf("failed to resolve file path: %w", err)
	}

	// Security check passed - now check if file exists
	info, err := os.Stat(cleanPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", cleanPath)
	}
	if err != nil {
		return "", fmt.Errorf("failed to access file %s: %w", cleanPath, err)
	}

	// Check if it's a directory
	if info.IsDir() {
		return "", fmt.Errorf("path is a directory, not a file: %s", cleanPath)
	}

	// Check file extension for common non-text file types
	if isNonTextFileExtension(cleanPath) {
		return "", fmt.Errorf("only text content files can be read. %s appears to be a non-text file", cleanPath)
	}

	// When a line range is requested, we need to read the full file to ensure accuracy
	// Otherwise, truncation can cause incorrect line counts (e.g., 700-line file truncated to 295 lines)

	maxFileSize := getFileReadMaxSize()
	if startLine > 0 || endLine > 0 {
		// For line range requests, read much more to ensure we get the requested lines
		maxFileSize = lineRangeMaxSize
	}

	// Open and read the file
	file, err := os.Open(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", cleanPath, err)
	}
	defer file.Close()

	var content []byte
	var truncated bool
	var headLines, tailLines int

	if startLine > 0 || endLine > 0 {
		// For line-range reads, just read up to maxFileSize (could be lineRangeMaxSize)
		content, err = io.ReadAll(file)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
		}
		if int64(len(content)) > int64(maxFileSize) {
			content = content[:maxFileSize]
			truncated = true
		}
	} else if info.Size() > int64(maxFileSize) {
		// Head+tail truncation: read 60% from start, 40% from end
		headSize := maxFileSize * 60 / 100
		tailSize := maxFileSize - headSize

		head := make([]byte, headSize)
		n, err := file.Read(head)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
		}
		head = head[:n]
		headLines = strings.Count(string(head), "\n")

		// Seek to position for tail
		tailOffset := info.Size() - int64(tailSize)
		if tailOffset < 0 {
			tailOffset = 0
		}
		if _, err := file.Seek(tailOffset, io.SeekStart); err != nil {
			return "", fmt.Errorf("failed to seek in file %s: %w", cleanPath, err)
		}

		tail := make([]byte, tailSize)
		n, err = file.Read(tail)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
		}
		tail = tail[:n]
		tailLines = strings.Count(string(tail), "\n")

		omittedKB := (info.Size() - int64(headSize) - int64(tailSize)) / 1024
		if omittedKB < 1 {
			omittedKB = 1
		}

		content = []byte(string(head) + "\n\n... [~" + fmt.Sprintf("%d", omittedKB) + "KB omitted] ...\n\n" + string(tail))
		truncated = true
	} else {
		// For smaller files, read all content
		content, err = io.ReadAll(file)
		if err != nil {
			return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
		}
	}

	// Check if content appears to be binary/non-text
	if isBinaryContent(content) {
		return "", fmt.Errorf("only text content files can be read. %s appears to contain binary/non-text content", cleanPath)
	}

	fileContent := string(content)

	// If line range is specified, extract only those lines
	if startLine > 0 || endLine > 0 {
		lines := strings.Split(fileContent, "\n")
		totalLines := len(lines)

		// Validate line ranges
		if startLine < 1 {
			startLine = 1
		}
		if endLine < 1 || endLine > totalLines {
			endLine = totalLines
		}
		if startLine > totalLines {
			return "", fmt.Errorf("start line %d exceeds file length %d", startLine, totalLines)
		}
		if startLine > endLine {
			return "", fmt.Errorf("start line %d is greater than end line %d", startLine, endLine)
		}

		// Extract the specified range (convert to 0-based indexing)
		selectedLines := lines[startLine-1 : endLine]
		fileContent = strings.Join(selectedLines, "\n")

		// Warn if file was truncated during line range request
		if truncated {
			fileContent = fmt.Sprintf("[WARN] Warning: File was truncated due to size. Requested lines %d-%d, but only %d lines were available in the truncated content.\n\n%s",
				startLine, endLine, totalLines, fileContent)
		}

		// Add line range info to result
		return fmt.Sprintf("Lines %d-%d of %s:\n%s", startLine, endLine, cleanPath, fileContent), nil
	}

	// Add truncation warning if file was truncated (head+tail mode only)
	if truncated && (headLines > 0 || tailLines > 0) {
		shownKB := maxFileSize / 1024
		omittedKB := (info.Size() - int64(maxFileSize)) / 1024
		if omittedKB < 1 {
			omittedKB = 1
		}
		fileContent = fmt.Sprintf("[WARN] File truncated — total %d bytes (%dKB). Showing ~%dKB: first %d lines and last %d lines, ~%dKB omitted. Use view_range parameter to read specific line ranges, e.g. view_range=[1,50]",
			info.Size(), info.Size()/1024+1, shownKB, headLines, tailLines, omittedKB)
		fileContent += "\n" + string(content)
	}

	return fileContent, nil
}

// isNonTextFileExtension checks if the file extension indicates a non-text file
func isNonTextFileExtension(filePath string) bool {
	// Common non-text file extensions
	nonTextExtensions := []string{
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp", // Images
		// .pdf removed — handled by handleReadFileWithImages before reaching here
		".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", // Documents
		".zip", ".tar", ".gz", ".rar", ".7z", // Archives
		".mp3", ".wav", ".ogg", ".flac", ".aac", // Audio
		".mp4", ".avi", ".mov", ".wmv", ".mkv", // Video
		".exe", ".dll", ".so", ".dylib", ".bin", // Executables
		".db", ".sqlite", ".mdb", // Databases
		".ico", ".woff", ".woff2", ".ttf", // Fonts/icons
		".psd", ".ai", ".eps", // Design files
		".class", ".jar", // Java
		".pyc", ".pyd", // Python compiled
		".o", ".obj", // Compiled objects
	}

	ext := strings.ToLower(filepath.Ext(filePath))
	for _, nonTextExt := range nonTextExtensions {
		if ext == nonTextExt {
			return true
		}
	}
	return false
}

// isBinaryContent checks if the content appears to be binary data
func isBinaryContent(content []byte) bool {
	// If file is empty, it's not binary
	if len(content) == 0 {
		return false
	}

	// Check for null bytes (common in binary files)
	for _, b := range content {
		if b == 0 {
			return true
		}
	}

	// Check for high percentage of non-printable characters
	nonPrintableCount := 0
	totalBytes := len(content)

	// Sample first 1KB for efficiency with large files
	sampleSize := totalBytes
	if sampleSize > 1024 {
		sampleSize = 1024
	}

	for i := 0; i < sampleSize; i++ {
		if content[i] < 32 && content[i] != 9 && content[i] != 10 && content[i] != 13 { // Not tab, LF, CR
			nonPrintableCount++
		}
	}

	// If more than 30% of sampled bytes are non-printable, consider it binary
	if float64(nonPrintableCount)/float64(sampleSize) > 0.3 {
		return true
	}

	return false
}
