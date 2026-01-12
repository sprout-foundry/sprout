package utils

import (
	"fmt"
	"path/filepath"
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
			"dist/", "build/", "node_modules/", "vendor/",
			".git/", ".svn/", ".hg/",
		},
	}
}

// OptimizedDiffResult represents the result of diff optimization
type OptimizedDiffResult struct {
	OptimizedContent string            // The optimized diff content
	FileSummaries    map[string]string // Summary for each optimized file
	OriginalLines    int               // Original number of lines
	OptimizedLines   int               // Optimized number of lines
	BytesSaved       int               // Estimated bytes saved
}

// OptimizeDiff optimizes a git diff by replacing large files with summaries
func (do *DiffOptimizer) OptimizeDiff(diff string) *OptimizedDiffResult {
	if diff == "" {
		return &OptimizedDiffResult{
			OptimizedContent: diff,
			FileSummaries:    make(map[string]string),
		}
	}

	lines := strings.Split(diff, "\n")
	originalLines := len(lines)

	result := &OptimizedDiffResult{
		FileSummaries: make(map[string]string),
		OriginalLines: originalLines,
	}

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

// processFileDiff processes a single file's diff and returns optimized content and summary
func (do *DiffOptimizer) processFileDiff(filename string, fileLines []string, changes *FileChangeSummary) ([]string, string) {
	if do.shouldOptimizeFile(filename, fileLines) {
		// Create summary for large/lock files
		summary := do.createFileSummary(filename, changes)

		// Return just the file header with summary
		headerLines := do.extractFileHeaders(fileLines)
		summaryLines := []string{
			"", // Empty line
			fmt.Sprintf("# %s", filename),
			fmt.Sprintf("# Large file optimized: %s", summary),
			"# Full diff omitted for brevity",
			"",
		}

		return append(headerLines, summaryLines...), summary
	}

	// Return full diff for normal files
	return fileLines, ""
}

// shouldOptimizeFile determines if a file should be optimized
func (do *DiffOptimizer) shouldOptimizeFile(filename string, fileLines []string) bool {
	// Check if it's a lock file
	if do.isLockFile(filename) {
		return true
	}

	// Check if it's a generated file
	if do.isGeneratedFile(filename) {
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
	basename := filepath.Base(filename)
	for _, pattern := range do.LockFilePatterns {
		if matched, _ := filepath.Match(pattern, basename); matched {
			return true
		}
		// Also check exact match
		if basename == pattern {
			return true
		}
	}
	return false
}

// isGeneratedFile checks if the file is a generated file
func (do *DiffOptimizer) isGeneratedFile(filename string) bool {
	for _, pattern := range do.GeneratedFilePatterns {
		if matched, _ := filepath.Match(pattern, filename); matched {
			return true
		}
		if strings.Contains(filename, strings.TrimSuffix(pattern, "*")) {
			return true
		}
	}
	return false
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

	if do.isLockFile(filename) {
		summaryParts = append(summaryParts, "lock file")
	}

	if do.isGeneratedFile(filename) {
		summaryParts = append(summaryParts, "generated file")
	}

	if changes.AddedLines > 0 || changes.DeletedLines > 0 {
		summaryParts = append(summaryParts,
			fmt.Sprintf("%d additions, %d deletions", changes.AddedLines, changes.DeletedLines))
	}

	if len(summaryParts) == 0 {
		summaryParts = append(summaryParts,
			fmt.Sprintf("large file with %d lines", changes.TotalLines))
	}

	return strings.Join(summaryParts, ", ")
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
