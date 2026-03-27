package utils

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// DiffOptimizer provides utilities for optimizing diff content for API endpoints
type DiffOptimizer struct {
	// Configuration for optimization thresholds
	MaxDiffLines          int      // Maximum lines to include in full diff
	MaxFileSize           int      // Maximum file size in bytes for full content
	LargeFileExtensions   []string // File extensions considered as large files
	LockFilePatterns      []string // Patterns for lock files
	GeneratedFilePatterns []string // Patterns for generated files
	WorkingDir            string   // Working directory for git commands (optional)
}

// NewDiffOptimizer creates a new diff optimizer with default settings
func NewDiffOptimizer() *DiffOptimizer {
	return &DiffOptimizer{
		MaxDiffLines: 500,
		MaxFileSize:  10000, // 10KB
		LargeFileExtensions: []string{
			".lock", ".sum", ".mod", ".json", ".xml", ".yaml", ".yml",
			".min.js", ".min.css", ".bundle.js", ".dist.js",
		},
		LockFilePatterns: []string{
			"package-lock.json", "yarn.lock", "Cargo.lock", "Pipfile.lock",
			"go.sum", "composer.lock", "Gemfile.lock", "poetry.lock",
			"pnpm-lock.yaml", "bun.lockb",
		},
		GeneratedFilePatterns: []string{
			"*.min.*", "*.bundle.*", "*.dist.*", "*.generated.*",
			"bundle.*", "*.bundle.js", "*.bundle.css",
			"dist/", "build/", "node_modules/", "vendor/",
			".git/", ".svn/", ".hg/",
		},
	}
}

// NewDiffOptimizerForReview creates a diff optimizer optimized for code review
// This uses much higher thresholds to ensure reviewers get full context
func NewDiffOptimizerForReview() *DiffOptimizer {
	return &DiffOptimizer{
		MaxDiffLines: 5000,   // 10x higher for code reviews
		MaxFileSize:  100000, // 100KB for code reviews
		LargeFileExtensions: []string{
			// Only optimize lock files and generated content for reviews
			// Note: .mod files (go.mod) are NOT included - they're important for reviews
			".lock", ".sum",
			".min.js", ".min.css", ".bundle.js", ".dist.js",
		},
		LockFilePatterns: []string{
			"package-lock.json", "yarn.lock", "Cargo.lock", "Pipfile.lock",
			"go.sum", "composer.lock", "Gemfile.lock", "poetry.lock",
			"pnpm-lock.yaml", "bun.lockb",
		},
		GeneratedFilePatterns: []string{
			"*.min.*", "*.bundle.*", "*.dist.*", "*.generated.*",
			"bundle.*", "*.bundle.js", "*.bundle.css",
			"*.map",                                // Source maps are fully generated
			"asset-manifest.json", "manifest.json", // Build asset manifests
			"dist/", "build/", "node_modules/", "vendor/",
			".git/", ".svn/", ".hg/",
		},
	}
}

// OptimizedDiffResult represents the result of diff optimization
type OptimizedDiffResult struct {
	OptimizedContent string            // The optimized diff content
	FileSummaries    map[string]string // Summary for each optimized file
	Warnings         []string          // Warnings about suspicious optimized files
	OriginalLines    int               // Original number of lines
	OptimizedLines   int               // Optimized number of lines
	BytesSaved       int               // Estimated bytes saved
}

// addDeduplicatedWarnings adds warnings to the result while deduplicating based on warningSet
func addDeduplicatedWarnings(result *OptimizedDiffResult, warningSet map[string]struct{}, warnings []string) {
	for _, w := range warnings {
		if _, exists := warningSet[w]; exists {
			continue
		}
		warningSet[w] = struct{}{}
		result.Warnings = append(result.Warnings, w)
	}
}

// OptimizeDiff optimizes a git diff by replacing large files with summaries
func (do *DiffOptimizer) OptimizeDiff(diff string) *OptimizedDiffResult {
	if diff == "" {
		return &OptimizedDiffResult{
			OptimizedContent: diff,
			FileSummaries:    make(map[string]string),
			Warnings:         nil,
		}
	}

	lines := strings.Split(diff, "\n")
	originalLines := len(lines)

	result := &OptimizedDiffResult{
		FileSummaries: make(map[string]string),
		Warnings:      make([]string, 0, 2),
		OriginalLines: originalLines,
	}
	warningSet := make(map[string]struct{})

	var optimizedLines []string
	var currentFile string
	var currentFileLines []string
	var currentFileChanges FileChangeSummary

	for _, line := range lines {
		// Detect start of new file diff
		if strings.HasPrefix(line, "diff --git") {
			// Process previous file if exists
			if currentFile != "" {
				optimized, summary := do.processFileDiff(currentFile, currentFileLines, &currentFileChanges)
				optimizedLines = append(optimizedLines, optimized...)
				if summary != "" {
					result.FileSummaries[currentFile] = summary
				}
				addDeduplicatedWarnings(result, warningSet, do.createWarnings(currentFile))
			}

			// Start new file
			currentFile = do.extractFilename(line)
			currentFileLines = []string{line}
			currentFileChanges = FileChangeSummary{}
			continue
		}

		// Add line to current file
		currentFileLines = append(currentFileLines, line)

		// Track changes for summary
		if currentFile != "" {
			do.updateChangeSummary(&currentFileChanges, line)
		}
	}

	// Process last file
	if currentFile != "" {
		optimized, summary := do.processFileDiff(currentFile, currentFileLines, &currentFileChanges)
		optimizedLines = append(optimizedLines, optimized...)
		if summary != "" {
			result.FileSummaries[currentFile] = summary
		}
		addDeduplicatedWarnings(result, warningSet, do.createWarnings(currentFile))
	}

	result.OptimizedContent = strings.Join(optimizedLines, "\n")
	result.OptimizedLines = len(optimizedLines)
	result.BytesSaved = len(diff) - len(result.OptimizedContent)

	return result
}

// FileChangeSummary tracks changes in a file
type FileChangeSummary struct {
	AddedLines   int
	DeletedLines int
	ContextLines int
	TotalLines   int
}

type fileChangeKind string

const (
	fileChangeAdded    fileChangeKind = "added"
	fileChangeDeleted  fileChangeKind = "deleted"
	fileChangeModified fileChangeKind = "modified"
)

// processFileDiff processes a single file's diff and returns optimized content and summary
func (do *DiffOptimizer) processFileDiff(filename string, fileLines []string, changes *FileChangeSummary) ([]string, string) {
	if do.shouldOptimizeFile(filename, fileLines) {
		// For review mode, be smarter about what content to include
		headerLines := do.extractFileHeaders(fileLines)

		// Find first hunk to get some actual changed content
		firstHunkIdx := -1
		for i, line := range fileLines {
			if strings.HasPrefix(line, "@@") {
				firstHunkIdx = i
				break
			}
		}

		var contentLines []string
		var summary string

		if firstHunkIdx >= 0 && do.isReviewMode() {
			// For review mode, distinguish code files from lock files
			if do.isCodeFile(filename) {
				// Code files get more context (up to 500 lines)
				maxContentLines := 500
				contentEnd := firstHunkIdx + maxContentLines
				if contentEnd > len(fileLines) {
					contentEnd = len(fileLines)
				}

				contentLines = fileLines[firstHunkIdx:contentEnd]
				linesIncluded := len(contentLines)
				totalLines := len(fileLines) - firstHunkIdx

				summary = fmt.Sprintf("code file (showing first %d of %d changed lines)",
					linesIncluded, totalLines)
			} else {
				// Lock files get summary only (no need for 500 lines of deps)
				summary = do.createFileSummary(filename, changes)
			}
		} else {
			// For non-review mode, just use summary
			summary = do.createFileSummary(filename, changes)
		}

		summaryLines := []string{
			"", // Empty line
			fmt.Sprintf("# %s", filename),
			fmt.Sprintf("# Optimized: %s", summary),
		}

		if len(contentLines) > 0 {
			summaryLines = append(summaryLines, contentLines...)
			summaryLines = append(summaryLines, "")
			summaryLines = append(summaryLines, fmt.Sprintf("# ... (%d more lines omitted)", len(fileLines)-firstHunkIdx-len(contentLines)))
		}

		return append(headerLines, summaryLines...), summary
	}

	// Return full diff for normal files
	return fileLines, ""
}

// isCodeFile determines if a file is source code (not lock/generated)
// Code files should get more context in review mode
func (do *DiffOptimizer) isCodeFile(filename string) bool {
	// Lock and generated files are NOT code files
	class := ClassifyReviewFile(filename)
	if class.IsLockFile || class.IsGenerated || class.IsVendored || class.IsBinary {
		return false
	}

	// Check file extension against code extensions
	codeExtensions := []string{
		".go", ".js", ".ts", ".jsx", ".tsx",
		".py", ".rb", ".rs", ".java", ".kt",
		".c", ".cpp", ".h", ".cs", ".swift",
		".php", ".scala", ".clj", ".ex", ".exs",
	}

	ext := strings.ToLower(filepath.Ext(filename))
	for _, codeExt := range codeExtensions {
		if ext == codeExt {
			return true
		}
	}

	// Also check for files without extension that are likely code
	// (e.g., Dockerfile, Makefile, etc.)
	if ext == "" {
		// File has no extension - likely a config or build file
		// These are typically useful for review
		return true
	}

	return false
}

// isReviewMode checks if this optimizer is configured for review mode
func (do *DiffOptimizer) isReviewMode() bool {
	// Review mode has higher thresholds (5000 lines, 100KB)
	return do.MaxDiffLines >= 5000
}

// shouldOptimizeFile determines if a file should be optimized
func (do *DiffOptimizer) shouldOptimizeFile(filename string, fileLines []string) bool {
	class := ClassifyReviewFile(filename)
	if class.IsLockFile || class.IsGenerated || class.IsVendored || class.IsBinary {
		return true
	}

	// Check if file has large extension
	ext := filepath.Ext(strings.ToLower(filename))
	for _, largeExt := range do.LargeFileExtensions {
		if ext == largeExt {
			return true
		}
	}

	// Check if diff is too long
	if len(fileLines) > do.MaxDiffLines {
		return true
	}

	// Estimate file size from diff
	totalBytes := 0
	for _, line := range fileLines {
		totalBytes += len(line)
	}
	if totalBytes > do.MaxFileSize {
		return true
	}

	return false
}

// isLockFile checks if the file is a lock file
func (do *DiffOptimizer) isLockFile(filename string) bool {
	return ClassifyReviewFile(filename).IsLockFile
}

// isGeneratedFile checks if the file is a generated file
func (do *DiffOptimizer) isGeneratedFile(filename string) bool {
	return ClassifyReviewFile(filename).IsGenerated
}

// extractFilename extracts filename from git diff header
func (do *DiffOptimizer) extractFilename(line string) string {
	// Parse "diff --git a/file.go b/file.go" format
	parts := strings.Fields(line)
	if len(parts) >= 4 {
		return strings.TrimPrefix(parts[2], "a/")
	}
	return ""
}

// updateChangeSummary updates the change summary based on a diff line
func (do *DiffOptimizer) updateChangeSummary(summary *FileChangeSummary, line string) {
	summary.TotalLines++

	if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "+++") {
		summary.AddedLines++
	} else if strings.HasPrefix(line, "-") && !strings.HasPrefix(line, "---") {
		summary.DeletedLines++
	} else if strings.HasPrefix(line, " ") {
		summary.ContextLines++
	}
}

// createFileSummary creates a human-readable summary for a file
func (do *DiffOptimizer) createFileSummary(filename string, changes *FileChangeSummary) string {
	var summaryParts []string
	changeKind := do.detectFileChangeKind(filename, changes)
	fileSize := do.getFileTotalSize(filename, changeKind)
	class := ClassifyReviewFile(filename)
	isMetadataOnly := do.isLockFile(filename) || do.isGeneratedFile(filename) || class.IsBinary

	if do.isLockFile(filename) {
		summaryParts = append(summaryParts, "lock file")
	}

	if do.isGeneratedFile(filename) {
		summaryParts = append(summaryParts, "generated file")
	}
	if class.IsBinary {
		summaryParts = append(summaryParts, "binary file")
	}

	if changeKind != "" {
		summaryParts = append(summaryParts, string(changeKind))
	}

	if fileSize >= 0 {
		summaryParts = append(summaryParts, fmt.Sprintf("%d bytes", fileSize))
	}

	if !isMetadataOnly && (changes.AddedLines > 0 || changes.DeletedLines > 0) {
		summaryParts = append(summaryParts,
			fmt.Sprintf("%d additions, %d deletions", changes.AddedLines, changes.DeletedLines))
	}

	if len(summaryParts) == 0 {
		summaryParts = append(summaryParts,
			fmt.Sprintf("large file with %d lines", changes.TotalLines))
	}

	return strings.Join(summaryParts, ", ")
}

func (do *DiffOptimizer) createWarnings(filename string) []string {
	class := ClassifyReviewFile(filename)
	if !class.IsBinary {
		return nil
	}

	changeKind := do.detectFileChangeKind(filename, &FileChangeSummary{})
	fileSize := do.getFileTotalSize(filename, changeKind)
	sizeText := "unknown size"
	if fileSize >= 0 {
		sizeText = fmt.Sprintf("%d bytes", fileSize)
	}

	return []string{
		fmt.Sprintf("Binary file staged: %s (%s, %s). Check in binaries only when necessary.", filename, changeKind, sizeText),
	}
}

func (do *DiffOptimizer) detectFileChangeKind(filename string, changes *FileChangeSummary) fileChangeKind {
	if do.stagedBlobSize(filename) >= 0 && do.headBlobSize(filename) < 0 {
		return fileChangeAdded
	}
	if do.stagedBlobSize(filename) < 0 && do.headBlobSize(filename) >= 0 {
		return fileChangeDeleted
	}
	if changes.AddedLines > 0 || changes.DeletedLines > 0 {
		return fileChangeModified
	}
	return fileChangeModified
}

func (do *DiffOptimizer) getFileTotalSize(filename string, kind fileChangeKind) int64 {
	switch kind {
	case fileChangeDeleted:
		return do.headBlobSize(filename)
	case fileChangeAdded, fileChangeModified:
		size := do.stagedBlobSize(filename)
		if size >= 0 {
			return size
		}
		return do.headBlobSize(filename)
	default:
		return do.stagedBlobSize(filename)
	}
}

func (do *DiffOptimizer) stagedBlobSize(filename string) int64 {
	return do.gitBlobSize(":"+filename, "")
}

func (do *DiffOptimizer) headBlobSize(filename string) int64 {
	return do.gitBlobSize("HEAD:"+filename, "")
}

func (do *DiffOptimizer) gitBlobSize(objectSpec string, fallback string) int64 {
	cmd := exec.Command("git", "cat-file", "-s", objectSpec)
	if do.WorkingDir != "" {
		cmd.Dir = do.WorkingDir
	}
	output, err := cmd.Output()
	if err == nil {
		size, parseErr := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
		if parseErr == nil {
			return size
		}
	}
	if fallback != "" {
		cmd = exec.Command("git", "cat-file", "-s", fallback)
		if do.WorkingDir != "" {
			cmd.Dir = do.WorkingDir
		}
		output, err = cmd.Output()
		if err == nil {
			size, parseErr := strconv.ParseInt(strings.TrimSpace(string(output)), 10, 64)
			if parseErr == nil {
				return size
			}
		}
	}
	return -1
}

// extractFileHeaders extracts just the essential header lines from a file diff
func (do *DiffOptimizer) extractFileHeaders(fileLines []string) []string {
	var headers []string

	for _, line := range fileLines {
		// Include diff header, file paths, and index info
		if strings.HasPrefix(line, "diff --git") ||
			strings.HasPrefix(line, "index ") ||
			strings.HasPrefix(line, "--- ") ||
			strings.HasPrefix(line, "+++ ") {
			headers = append(headers, line)
		} else if strings.HasPrefix(line, "@@") {
			// Stop at first hunk header
			break
		}
	}

	return headers
}
