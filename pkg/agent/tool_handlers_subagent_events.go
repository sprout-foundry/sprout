package agent

import (
	"context"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// flushSubagentBatch publishes buffered lines and clears the buffer
func flushSubagentBatch(buffer *subagentBatchBuffer, a *Agent, toolCallID, toolName string) {
	if len(buffer.lines) == 0 {
		return
	}

	// Publish all buffered lines as a batch
	batchMessage := strings.Join(buffer.lines, "\n")
	details := map[string]interface{}{
		"task_id":     buffer.taskID,
		"persona":     buffer.persona,
		"is_parallel": buffer.isParallel,
		"batch_size":  len(buffer.lines),
	}

	a.publishEvent(events.EventTypeSubagentActivity, events.SubagentActivityEvent(toolCallID, toolName, "output", batchMessage, details))
}

// cleanupSubagentBatch flushes any remaining buffered output for a task
func cleanupSubagentBatch(taskID string, a *Agent, toolCallID, toolName string) {
	bufferMu.Lock()
	defer bufferMu.Unlock()

	if buffer, exists := batchBuffers[taskID]; exists {
		if len(buffer.lines) > 0 {
			flushSubagentBatch(buffer, a, toolCallID, toolName)
		}
		// Remove the buffer to free memory
		delete(batchBuffers, taskID)
	}
}

func publishSubagentActivity(ctx context.Context, a *Agent, phase, message string, details map[string]interface{}) {
	if a == nil {
		return
	}
	message = strings.TrimSpace(stripAnsiCodes(message))
	if message == "" {
		return
	}
	toolCallID, toolName := toolExecutionMetadataFromContext(ctx)

	// Check if this is a milestone phase - publish immediately
	isMilestone := false
	for _, milestone := range MILESTONE_PHASES {
		if phase == milestone {
			isMilestone = true
			break
		}
	}

	// Extract task ID from details for batching
	taskID := ""
	if tid, ok := details["task_id"]; ok {
		if tidStr, ok := tid.(string); ok {
			taskID = tidStr
		}
	}

	// If milestone phase, publish immediately without batching
	if isMilestone {
		// Clean up any pending batch buffers before publishing milestone
		if taskID != "" {
			cleanupSubagentBatch(taskID, a, toolCallID, toolName)
		}
		a.publishEvent(events.EventTypeSubagentActivity, events.SubagentActivityEvent(toolCallID, toolName, phase, message, details))
		return
	}

	// For output lines, use batching
	bufferMu.Lock()

	// Get or create buffer for this task
	if taskID == "" {
		taskID = toolCallID
	}

	buffer, exists := batchBuffers[taskID]
	if !exists {
		// Extract persona and is_parallel safely
		persona := ""
		if p, ok := details["persona"]; ok {
			if pStr, ok := p.(string); ok {
				persona = pStr
			}
		}
		isParallel := false
		if p, ok := details["is_parallel"]; ok {
			if pBool, ok := p.(bool); ok {
				isParallel = pBool
			}
		}

		buffer = &subagentBatchBuffer{
			lines:      make([]string, 0, BATCH_SIZE),
			lineCount:  0,
			taskID:     taskID,
			persona:    persona,
			isParallel: isParallel,
		}
		batchBuffers[taskID] = buffer
	}

	// Add line to buffer
	buffer.lines = append(buffer.lines, message)
	buffer.lineCount++

	// Flush when the batch is full. Snapshot the lines into a separate buffer
	// BEFORE resetting, then publish outside the lock. The previous code reset
	// buffer.lines to length 0 and then passed the same (now-empty) buffer to
	// flushSubagentBatch, which saw len==0 and published nothing — so every
	// full 50-line batch of subagent output was silently dropped and never
	// reached the WebUI. Only the final sub-50 remainder (flushed on the
	// "complete" milestone) ever showed.
	if buffer.lineCount >= BATCH_SIZE {
		toFlush := &subagentBatchBuffer{
			lines:      append([]string(nil), buffer.lines...),
			lineCount:  buffer.lineCount,
			taskID:     buffer.taskID,
			persona:    buffer.persona,
			isParallel: buffer.isParallel,
		}
		buffer.lines = buffer.lines[:0]
		buffer.lineCount = 0
		bufferMu.Unlock()
		flushSubagentBatch(toFlush, a, toolCallID, toolName)
		bufferMu.Lock()
	}

	bufferMu.Unlock()
}

// ---------------------------------------------------------------------------
// Utility helpers shared by subagent handlers
// ---------------------------------------------------------------------------

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// stripAnsiCodes removes ANSI escape codes from a string
func stripAnsiCodes(s string) string {
	// ANSI escape code regex pattern
	ansiEscape := regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)
	return ansiEscape.ReplaceAllString(s, "")
}

// isPathInWorkspace checks if a path is within the workspace directory
func isPathInWorkspace(path, workspaceDir string) bool {
	if path == workspaceDir {
		return true
	}
	return strings.HasPrefix(path, workspaceDir+string(filepath.Separator))
}

// isPathInTmp checks if a path is in /tmp/ for temporary file access
func isPathInTmp(path string) bool {
	// Check for /tmp/ or /var/folders/.../T/ (macOS temp dir) or any path containing tmp
	return strings.Contains(path, "/tmp/") ||
		strings.Contains(path, "/var/folders/.../T/") ||
		strings.Contains(strings.ToLower(path), "/tmp/")
}

// commonParent finds the common parent directory of multiple paths
func commonParent(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	if len(paths) == 1 {
		return filepath.Dir(paths[0])
	}
	result := paths[0]
	for _, p := range paths[1:] {
		for !strings.HasPrefix(p+string(filepath.Separator), result+string(filepath.Separator)) && p != result {
			result = filepath.Dir(result)
			if result == "/" || result == "." {
				return result
			}
		}
	}
	return result
}

// flushAllSubagentBuffers flushes all pending batch buffers
func flushAllSubagentBuffers(a *Agent) {
	bufferMu.Lock()
	defer bufferMu.Unlock()

	for taskID, buffer := range batchBuffers {
		if len(buffer.lines) > 0 {
			toolCallID := taskID
			toolName := "subagent"
			flushSubagentBatch(buffer, a, toolCallID, toolName)
			// Delete buffer immediately after flushing to prevent memory leak
			delete(batchBuffers, taskID)
		}
	}
}
