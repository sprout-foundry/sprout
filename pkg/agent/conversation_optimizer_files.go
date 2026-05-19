package agent

import (
	"crypto/md5"
	"fmt"
	"regexp"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// FileReadRecord tracks file reads to detect redundancy
type FileReadRecord struct {
	FilePath     string
	Content      string
	ContentHash  string
	Timestamp    time.Time
	MessageIndex int
}

// isRedundantFileRead checks if this message is a redundant file read
func (co *ConversationOptimizer) isRedundantFileRead(msg api.Message, index int) bool {
	if msg.Role != "tool" {
		return false
	}

	// Check if this is a file read result
	if !strings.Contains(msg.Content, "Tool call result for read_file:") {
		return false
	}

	filePath := co.extractFilePath(msg.Content)
	if filePath == "" {
		return false
	}

	// Check if we have the most recent read of this file
	if record, exists := co.fileReads[filePath]; exists {
		// Extract current content
		currentContent := co.extractFileContent(msg.Content)
		currentHash := co.hashContent(currentContent)

		// Only consider it redundant if:
		// 1. Content hasn't changed AND
		// 2. This is NOT the most recent read (index < record.MessageIndex) AND
		// 3. The gap to the most recent read is at least 15 messages (preserving more context)
		messageGap := record.MessageIndex - index
		if record.ContentHash == currentHash && index < record.MessageIndex && messageGap >= 15 {
			return true
		}
	}

	return false
}

// trackFileRead records a file read for future optimization
func (co *ConversationOptimizer) trackFileRead(msg api.Message, index int) {
	if msg.Role != "tool" || !strings.Contains(msg.Content, "Tool call result for read_file:") {
		return
	}

	filePath := co.extractFilePath(msg.Content)
	if filePath == "" {
		return
	}

	content := co.extractFileContent(msg.Content)
	hash := co.hashContent(content)

	// Always track the MOST RECENT read of each file
	// This ensures we preserve the latest read and optimize older ones
	co.fileReads[filePath] = &FileReadRecord{
		FilePath:     filePath,
		Content:      content,
		ContentHash:  hash,
		Timestamp:    time.Now(),
		MessageIndex: index,
	}
}

// extractFilePath extracts the file path from a tool call result message
func (co *ConversationOptimizer) extractFilePath(content string) string {
	// Pattern: "Tool call result for read_file: <filepath>"
	re := regexp.MustCompile(`Tool call result for read_file:\s*([^\s\n]+)`)
	matches := re.FindStringSubmatch(content)
	if len(matches) > 1 {
		return strings.TrimSpace(matches[1])
	}
	return ""
}

// extractFileContent extracts the file content from a tool call result message
func (co *ConversationOptimizer) extractFileContent(content string) string {
	// Find the content after the file path
	lines := strings.Split(content, "\n")
	if len(lines) < 2 {
		return ""
	}

	// Skip the first line (tool call result header) and join the rest
	return strings.Join(lines[1:], "\n")
}

// hashContent creates a hash of file content for comparison
func (co *ConversationOptimizer) hashContent(content string) string {
	return fmt.Sprintf("%x", md5.Sum([]byte(content)))
}

// createFileReadSummary creates a summary for a redundant file read
func (co *ConversationOptimizer) createFileReadSummary(msg api.Message) string {
	filePath := co.extractFilePath(msg.Content)
	content := co.extractFileContent(msg.Content)

	// Count lines and characters
	lines := strings.Split(strings.TrimSpace(content), "\n")
	lineCount := len(lines)
	charCount := len(content)

	// Determine file type
	fileType := "file"
	if strings.HasSuffix(filePath, ".go") {
		fileType = "Go file"
	} else if strings.HasSuffix(filePath, ".md") {
		fileType = "Markdown file"
	} else if strings.HasSuffix(filePath, ".json") {
		fileType = "JSON file"
	}

	return fmt.Sprintf("Tool call result for read_file: %s\n[OPTIMIZED] Previously read %s (%d lines, %d chars) - content unchanged since last read",
		filePath, fileType, lineCount, charCount)
}

// getTrackedFilePaths returns list of tracked file paths
func (co *ConversationOptimizer) getTrackedFilePaths() []string {
	paths := make([]string, 0, len(co.fileReads))
	for path := range co.fileReads {
		paths = append(paths, path)
	}
	return paths
}

// InvalidateFile clears cached data for a specific file when it's modified
// This ensures stale metadata (like line counts) doesn't mislead the model
func (co *ConversationOptimizer) InvalidateFile(filePath string) {
	if filePath == "" {
		return
	}
	co.mu.Lock()
	defer co.mu.Unlock()

	if co.debug && co.printLine != nil {
		co.printLine(fmt.Sprintf("\n[~] Invalidating cached file data: %s\n", filePath))
	}
	delete(co.fileReads, filePath)
}
