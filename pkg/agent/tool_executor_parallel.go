// Tool executor: parallel batch execution for safe, independent tools.
package agent

import (
	"fmt"
	"math"
	"strings"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// canExecuteInParallel checks if all tools can be executed in parallel
func (te *ToolExecutor) canExecuteInParallel(toolCalls []api.ToolCall) bool {
	if len(toolCalls) <= 1 {
		return false
	}

	// Disable parallel execution for providers with strict tool call ordering requirements
	provider := te.agent.GetProvider()
	if strings.EqualFold(provider, "deepseek") {
		return false
	}
	if strings.EqualFold(provider, "minimax") {
		return false
	}

	return te.parallelBatchToolName(toolCalls) != ""
}

func (te *ToolExecutor) parallelBatchToolName(toolCalls []api.ToolCall) string {
	if len(toolCalls) == 0 {
		return ""
	}

	first := te.normalizeToolNameForScheduling(toolCalls[0].Function.Name)
	if !isParallelSafeBatchTool(first) {
		return ""
	}

	for i := 1; i < len(toolCalls); i++ {
		name := te.normalizeToolNameForScheduling(toolCalls[i].Function.Name)
		if name != first {
			return ""
		}
	}

	return first
}

func (te *ToolExecutor) normalizeToolNameForScheduling(toolName string) string {
	name := strings.Split(toolName, "<|channel|>")[0]
	if alias := te.agent.suggestCorrectToolName(name); alias != "" {
		return alias
	}
	return name
}

func isParallelSafeBatchTool(toolName string) bool {
	switch toolName {
	case "read_file", "fetch_url", "search_files":
		return true
	default:
		return false
	}
}

func parallelWorkerLimit(toolName string, batchSize int) int {
	if batchSize <= 1 {
		return 1
	}

	var capValue int
	switch toolName {
	case "fetch_url":
		// Keep network fan-out conservative to avoid provider throttling.
		capValue = 4
	case "search_files":
		// Search is CPU/IO-heavy; keep concurrency moderate.
		capValue = 6
	default:
		capValue = 12
	}

	return int(math.Min(float64(batchSize), float64(capValue)))
}

// executeParallel executes a same-tool batch in parallel when safe.
func (te *ToolExecutor) executeParallel(toolCalls []api.ToolCall) []api.Message {
	// Flush any buffered streaming content before parallel tool execution
	// This ensures narrative text appears before tool calls for better flow
	if te.agent.flushCallback != nil {
		te.agent.flushCallback()
	}

	toolName := te.parallelBatchToolName(toolCalls)
	if toolName == "" {
		return te.executeSequential(toolCalls)
	}

	limit := parallelWorkerLimit(toolName, len(toolCalls))
	te.agent.debugLog("[>>] Executing %d %s operations in parallel (workers=%d)\n", len(toolCalls), toolName, limit)

	// Pre-generate tool call IDs for any tool calls that don't have them
	// This ensures each goroutine has its own unique ID before parallel execution
	// Also assign tool indices for trace recording
	for i := range toolCalls {
		if toolCalls[i].ID == "" {
			toolCalls[i].ID = te.GenerateToolCallID(toolCalls[i].Function.Name)
		}
	}

	var wg sync.WaitGroup
	results := make([]api.Message, len(toolCalls))
	resultsMutex := &sync.Mutex{}
	workers := make(chan struct{}, limit)

	for i, tc := range toolCalls {
		wg.Add(1)
		// Pass toolCall by VALUE (create a copy with tc := toolCall)
		// This ensures each goroutine has its own unique data
		tc := tc
		go func(index int, toolCall api.ToolCall) {
			workers <- struct{}{}
			defer func() {
				<-workers
				if r := recover(); r != nil {
					te.agent.debugLog("[WARN] Tool execution panicked: %v\n", r)
					// Create error result
					resultsMutex.Lock()
					results[index] = api.Message{
						Role:    "tool",
						Content: fmt.Sprintf("Tool execution panicked: %v", r),
					}
					resultsMutex.Unlock()
				}
				wg.Done()
			}()

			// Assign tool index for this parallel execution
			// Use atomic increment to ensure unique indices
			resultsMutex.Lock()
			currentToolIndex := te.toolIndex
			te.toolIndex++
			resultsMutex.Unlock()

			// Execute tool with assigned tool index
			result := te.executeSingleToolWithIndex(toolCall, currentToolIndex)

			// Store result
			resultsMutex.Lock()
			results[index] = result
			resultsMutex.Unlock()
		}(i, tc)
	}

	wg.Wait()
	return results
}
