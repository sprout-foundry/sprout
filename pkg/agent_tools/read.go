package tools

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func ReadFile(ctx context.Context, filePath string) (string, error) {
	return ReadFileWithRange(ctx, filePath, 0, 0)
}

func ReadFileWithRange(ctx context.Context, filePath string, startLine, endLine int) (string, error) {
	if filePath == "" {
		return "", fmt.Errorf("empty file path provided")
	}

	// Clean and validate the path
	cleanPath := filepath.Clean(filePath)

	// Check if file exists
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

	// Open and read the file
	file, err := os.Open(cleanPath)
	if err != nil {
		return "", fmt.Errorf("failed to open file %s: %w", cleanPath, err)
	}
	defer file.Close()

	const maxFileSize = 100 * 1024 // Increased to 100KB
	var content []byte
	var truncated bool
	
	if info.Size() > maxFileSize {
		// For large files, read only the maximum size and truncate
		content = make([]byte, maxFileSize)
		n, err := file.Read(content)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("failed to read file %s: %w", cleanPath, err)
		}
		content = content[:n]
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
		
		// Add line range info to result
		return fmt.Sprintf("Lines %d-%d of %s:\n%s", startLine, endLine, cleanPath, fileContent), nil
	}
	
	// Add truncation warning if file was truncated
	if truncated {
		fileContent = fmt.Sprintf("⚠️ File truncated (>100KB). Showing first %dKB of %s:\n%s\n\n[Content truncated - file is %d bytes total]", 
			maxFileSize/1024, cleanPath, fileContent, info.Size())
	}

	return fileContent, nil
}

// isNonTextFileExtension checks if the file extension indicates a non-text file
func isNonTextFileExtension(filePath string) bool {
	// Common non-text file extensions
	nonTextExtensions := []string{
		".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp", // Images
		".pdf", ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", // Documents
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
