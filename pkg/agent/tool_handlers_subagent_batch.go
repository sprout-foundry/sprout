package agent

import (
	"context"
	"strings"
	"sync"

	"github.com/sprout-foundry/sprout/pkg/events"
)

// MILESTONE_PHASES defines phases that trigger immediate publish without batching
var MILESTONE_PHASES = []string{"spawn", "complete", "step"}

// subagentBatchBuffer holds buffered output for a subagent task
type subagentBatchBuffer struct {
	lines      []string
	lineCount  int
	taskID     string
	persona    string
	isParallel bool
}

// Global batch buffer manager
var (
	batchBuffers = make(map[string]*subagentBatchBuffer)
	bufferMu     sync.Mutex
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

	// Check if batch is full - clear buffer first, then flush
	if buffer.lineCount >= BATCH_SIZE {
		buffer.lines = buffer.lines[:0]
		buffer.lineCount = 0
		bufferMu.Unlock()
		flushSubagentBatch(buffer, a, toolCallID, toolName)
		bufferMu.Lock()
	}

	bufferMu.Unlock()
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
