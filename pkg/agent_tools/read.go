package tools

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/filesystem"
)

// File size constants for read operations
const (
	// defaultMaxFileSize is the maximum file size (in bytes) for reading files
	// Files larger than this will be truncated with head+tail and a warning
	// ~8,000 tokens at 4 chars/token heuristic (~6% of 128K context window)
	defaultMaxFileSize = 32 * 1024 // 32KB default

	// lineRangeMaxSize is the maximum file size (in bytes) when a line range is requested
	// This is larger to ensure we can read enough content for accurate line counts
	lineRangeMaxSize = 2 * 1024 * 1024 // 2MB for line range requests
)

func getFileReadMaxSize() int {
	if raw := configuration.GetEnvSimple("READ_FILE_MAX_BYTES"); raw != "" {
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
		return "", fmt.Errorf("resolve file path: %w", err)
	}

	// Security check passed - now check if file exists
	// Note: os.Stat uses blocking syscalls. On network filesystems, this can hang.
	// The symlink resolution above already has a timeout; stat gets a short timeout too.
	info, err := statWithTimeout(ctx, cleanPath)
	if os.IsNotExist(err) {
		return "", fmt.Errorf("file does not exist: %s", cleanPath)
	}
	if err != nil {
		return "", fmt.Errorf("access file %s: %w", cleanPath, err)
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
		return "", fmt.Errorf("open file %s: %w", cleanPath, err)
	}
	defer file.Close()

	var content []byte
	var truncated bool
	var headLines, tailLines int

	if startLine > 0 || endLine > 0 {
		// For line-range reads, just read up to maxFileSize (could be lineRangeMaxSize)
		content, err = readAllWithContext(ctx, cleanPath, maxFileSize)
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", cleanPath, err)
		}
		if int64(len(content)) > int64(maxFileSize) {
			content = content[:maxFileSize]
			// Trim to last complete line to avoid mid-line split
			if idx := bytes.LastIndex(content, []byte("\n")); idx > 0 {
				content = content[:idx]
			}
			truncated = true
		}
	} else if info.Size() > int64(maxFileSize) {
		// Head+tail truncation: read 60% from start, 40% from end
		headSize := maxFileSize * 60 / 100
		tailSize := maxFileSize - headSize

		head := make([]byte, headSize)
		n, err := fileReadWithContext(ctx, cleanPath, 0, head)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read head %s: %w", cleanPath, err)
		}
		head = head[:n]
		headLines = strings.Count(string(head), "\n")

		// Seek to position for tail
		tailOffset := info.Size() - int64(tailSize)
		if tailOffset < 0 {
			tailOffset = 0
		}

		tail := make([]byte, tailSize)
		n, err = fileReadWithContext(ctx, cleanPath, tailOffset, tail)
		if err != nil && err != io.EOF {
			return "", fmt.Errorf("read tail %s: %w", cleanPath, err)
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
		// For smaller files, read all content with context cancellation support
		content, err = readAllWithContext(ctx, cleanPath, int(maxFileSize))
		if err != nil {
			return "", fmt.Errorf("read file %s: %w", cleanPath, err)
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
			fileContent = fmt.Sprintf("[WARN] File was truncated to 2MB limit. Requested lines %d-%d, but only %d lines were available in the truncated content. Try a smaller line range to target specific sections.\n\n%s",
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

// statWithTimeout wraps os.Stat with a timeout to guard against hangs
// on unresponsive network filesystems (NFS, cloud mounts, Docker volumes).
// If the context has no Done channel, os.Stat is called directly.
func statWithTimeout(ctx context.Context, path string) (os.FileInfo, error) {
	if ctx.Done() == nil {
		return os.Stat(path)
	}

	type result struct {
		info os.FileInfo
		err  error
	}
	resultCh := make(chan result, 1)
	go func() {
		info, err := os.Stat(path)
		resultCh <- result{info, err}
	}()

	select {
	case res := <-resultCh:
		return res.info, res.err
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("stat timed out after 5s for: %s", path)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// readAllWithContext reads an entire file with context cancellation support.
// The read runs in a goroutine so the caller can return immediately on context
// cancellation rather than blocking on a stuck filesystem. maxSize limits the
// bytes read. If the context has no Done channel (e.g., context.Background()),
// the syscall is invoked directly to avoid goroutine overhead.
func readAllWithContext(ctx context.Context, path string, maxSize int) ([]byte, error) {
	// For non-cancellable contexts, skip the goroutine entirely.
	if ctx.Done() == nil {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open file: %w", err)
		}
		defer file.Close()
		return io.ReadAll(io.LimitReader(file, int64(maxSize)))
	}

	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		file, err := os.Open(path)
		if err != nil {
			resultCh <- readResult{nil, fmt.Errorf("open file: %w", err)}
			return
		}
		defer file.Close()
		data, err := io.ReadAll(io.LimitReader(file, int64(maxSize)))
		resultCh <- readResult{data, err}
	}()

	select {
	case res := <-resultCh:
		return res.data, res.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// fileReadWithContext reads a specific portion of a file using read(2) syscalls.
// This is used for head+tail reads where we need precise positioning.
// If the context has no Done channel, the syscall is invoked directly.
func fileReadWithContext(ctx context.Context, path string, offset int64, buf []byte) (int, error) {
	if ctx.Done() == nil {
		file, err := os.Open(path)
		if err != nil {
			return 0, fmt.Errorf("open file: %w", err)
		}
		defer file.Close()
		if offset > 0 {
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				return 0, fmt.Errorf("seek: %w", err)
			}
		}
		return file.Read(buf)
	}

	type readResult struct {
		n   int
		err error
	}
	resultCh := make(chan readResult, 1)

	go func() {
		file, err := os.Open(path)
		if err != nil {
			resultCh <- readResult{0, fmt.Errorf("open file: %w", err)}
			return
		}
		defer file.Close()
		if offset > 0 {
			if _, err := file.Seek(offset, io.SeekStart); err != nil {
				resultCh <- readResult{0, fmt.Errorf("seek: %w", err)}
				return
			}
		}
		n, err := file.Read(buf)
		resultCh <- readResult{n, err}
	}()

	select {
	case res := <-resultCh:
		return res.n, res.err
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
