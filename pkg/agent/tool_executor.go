package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ToolExecutor handles tool execution logic
type ToolExecutor struct {
	agent *Agent
}

// NewToolExecutor creates a new tool executor
func NewToolExecutor(agent *Agent) *ToolExecutor {
	return &ToolExecutor{
		agent: agent,
	}
}

// ExecuteTools executes a list of tool calls and returns the results
func (te *ToolExecutor) ExecuteTools(toolCalls []api.ToolCall) []api.Message {
	// Check for interrupt before executing
	select {
	case <-te.agent.interruptCtx.Done():
		// Context cancelled, interrupt requested
		var results []api.Message
		for _, tc := range toolCalls {
			toolCallID := tc.ID
			if toolCallID == "" {
				toolCallID = te.GenerateToolCallID(tc.Function.Name)
			}
			results = append(results, api.Message{
				Role:       "tool",
				Content:    "Execution interrupted by user",
				ToolCallId: toolCallID,
			})
		}
		return results
	default:
		// Context not cancelled
	}

	// Optimize parallel execution for read_file operations
	if te.canExecuteInParallel(toolCalls) {
		return te.executeParallel(toolCalls)
	}

	// Sequential execution for other tools
	return te.executeSequential(toolCalls)
}

// canExecuteInParallel checks if all tools can be executed in parallel
func (te *ToolExecutor) canExecuteInParallel(toolCalls []api.ToolCall) bool {
	for _, tc := range toolCalls {
		if tc.Function.Name != "read_file" {
			return false
		}
	}
	return len(toolCalls) > 1
}

// executeParallel executes tools in parallel (for read_file operations)
func (te *ToolExecutor) executeParallel(toolCalls []api.ToolCall) []api.Message {
	// Flush any buffered streaming content before parallel tool execution
	// This ensures narrative text appears before tool calls for better flow
	if te.agent.flushCallback != nil {
		te.agent.flushCallback()
	}

	te.agent.debugLog("🚀 Executing %d read_file operations in parallel\n", len(toolCalls))

	var wg sync.WaitGroup
	results := make([]api.Message, len(toolCalls))
	resultsMutex := &sync.Mutex{}

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(index int, toolCall api.ToolCall) {
			defer func() {
				if r := recover(); r != nil {
					te.agent.debugLog("⚠️ Tool execution panicked: %v\n", r)
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

			// Execute tool
			result := te.executeSingleTool(toolCall)

			// Store result
			resultsMutex.Lock()
			results[index] = result
			resultsMutex.Unlock()
		}(i, tc)
	}

	wg.Wait()
	return results
}

// executeSequential executes tools one by one
func (te *ToolExecutor) executeSequential(toolCalls []api.ToolCall) []api.Message {
	var toolResults []api.Message

	for i, tc := range toolCalls {
		// Check for interrupt between tool executions
		select {
		case <-te.agent.interruptCtx.Done():
			// Context cancelled, interrupt requested
			toolCallID := tc.ID
			if toolCallID == "" {
				toolCallID = te.GenerateToolCallID(tc.Function.Name)
			}
			toolResults = append(toolResults, api.Message{
				Role:       "tool",
				Content:    "Execution interrupted by user",
				ToolCallId: toolCallID,
			})
			return toolResults
		default:
			// Context not cancelled
		}

		// Flush any buffered streaming content before each tool execution
		// This ensures narrative text appears before each tool call for better flow
		if te.agent.flushCallback != nil {
			te.agent.flushCallback()
		}

		// Show progress
		if len(toolCalls) > 1 {
			te.agent.debugLog("🔧 Executing tool %d/%d: %s\n", i+1, len(toolCalls), tc.Function.Name)
		}

		// Execute tool
		result := te.executeSingleTool(tc)
		toolResults = append(toolResults, result)

		// Check if execution should stop
		if te.shouldStopExecution(tc.Function.Name, result.Content) {
			break
		}
	}

	return toolResults
}

// executeSingleTool executes a single tool call
func (te *ToolExecutor) executeSingleTool(toolCall api.ToolCall) api.Message {
	// Log prior to execution for diagnostics
	if te.agent != nil {
		te.agent.LogToolCall(toolCall, "executing")
		// Publish tool execution start event for real-time UI updates
		te.agent.PublishToolExecution(toolCall.Function.Name, "starting", map[string]interface{}{
			"tool_call_id": toolCall.ID,
			"arguments":    toolCall.Function.Arguments,
		})
	}

	// Generate a tool call ID if empty to prevent sanitization from dropping the result
	toolCallID := toolCall.ID
	if toolCallID == "" {
		toolCallID = te.GenerateToolCallID(toolCall.Function.Name)
		te.agent.debugLog("🔧 Generated missing tool call ID: %s for tool: %s\n", toolCallID, toolCall.Function.Name)
	}

	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return api.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("Error parsing arguments: %v", err),
			ToolCallId: toolCallID,
		}
	}

	// Execute with circuit breaker check
	if te.checkCircuitBreaker(toolCall.Function.Name, args) {
		return api.Message{
			Role:       "tool",
			Content:    "Circuit breaker: This action has been attempted too many times with the same parameters.",
			ToolCallId: toolCallID,
		}
	}

	// Create a context with a timeout for the tool execution
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	// Create a channel to receive the result of the tool execution
	resultChan := make(chan struct {
		result string
		err    error
	}, 1)

	// Execute the tool in a goroutine
	go func() {
		var result string
		var err error

		if toolCall.Function.Name == "mcp_tools" {
			result, err = te.agent.handleMCPToolsCommand(args)
		} else {
			registry := GetToolRegistry()
			result, err = registry.ExecuteTool(ctx, toolCall.Function.Name, args, te.agent)

			if err != nil && strings.Contains(err.Error(), "unknown tool") {
				if fallbackResult, fallbackErr, handled := te.tryExecuteMCPTool(toolCall.Function.Name, args); handled {
					result = fallbackResult
					err = fallbackErr
				}
			}
		}

		resultChan <- struct {
			result string
			err    error
		}{result, err}
	}()

	var result string
	var err error

	// Wait for the tool to complete, timeout, or interrupt
	select {
	case res := <-resultChan:
		result = res.result
		err = res.err
	case <-ctx.Done():
		err = fmt.Errorf("tool execution timed out after 5 minutes")
	case <-te.agent.interruptCtx.Done():
		err = fmt.Errorf("tool execution interrupted by user")
	}

	if err != nil {
		// Ensure the error is visible to the user immediately
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("❌ Tool '%s' failed: %v", toolCall.Function.Name, err))
		te.agent.PrintLine("")
		result = fmt.Sprintf("Error: %v", err)
	}

	// Update circuit breaker
	te.updateCircuitBreaker(toolCall.Function.Name, args)

	// Publish tool execution completion event for real-time UI updates
	if te.agent != nil {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		te.agent.PublishToolExecution(toolCall.Function.Name, status, map[string]interface{}{
			"tool_call_id": toolCallID,
			"result":       result,
			"error":        err,
		})
	}

	return api.Message{
		Role:       "tool",
		Content:    result,
		ToolCallId: toolCallID,
	}
}

// tryExecuteMCPTool attempts to execute an MCP tool name using the agent's MCP manager.
// Returns handled=false when the tool name doesn't correspond to an MCP tool.
func (te *ToolExecutor) tryExecuteMCPTool(toolName string, args map[string]interface{}) (string, error, bool) {
	if te.agent == nil {
		return "", fmt.Errorf("agent not initialized"), true
	}

	if strings.HasPrefix(toolName, "mcp_") {
		result, err := te.agent.executeMCPTool(toolName, args)
		return result, err, true
	}

	return "", nil, false
}

// shouldStopExecution checks if execution should stop after a tool
func (te *ToolExecutor) shouldStopExecution(toolName, result string) bool {
	// Stop on ask_user to wait for response
	if toolName == "ask_user" {
		return true
	}

	// Stop on critical errors
	if strings.Contains(result, "CRITICAL ERROR") ||
		strings.Contains(result, "FATAL ERROR") {
		return true
	}

	return false
}

// checkCircuitBreaker checks if an action should be blocked
func (te *ToolExecutor) checkCircuitBreaker(toolName string, args map[string]interface{}) bool {
	if te.agent.circuitBreaker == nil {
		return false
	}

	key := te.generateActionKey(toolName, args)
	action, exists := te.agent.circuitBreaker.Actions[key]
	if !exists {
		return false
	}

	// Higher threshold for troubleshooting operations
	threshold := 3

	// Increase threshold for common troubleshooting operations
	switch toolName {
	case "read_file":
		// Reading files is often repeated during troubleshooting
		threshold = 5
		// But be more aggressive for ZAI to prevent loops
		if te.agent.GetProvider() == "zai" {
			threshold = 3
		}
	case "shell_command":
		// Shell commands are frequently repeated during troubleshooting and debugging
		threshold = 8
	case "edit_file":
		// Editing the same file multiple times might be needed for complex fixes
		threshold = 4
	}

	// Block if attempted too many times
	return action.Count >= threshold
}

// updateCircuitBreaker updates the circuit breaker state
func (te *ToolExecutor) updateCircuitBreaker(toolName string, args map[string]interface{}) {
	if te.agent.circuitBreaker == nil {
		return
	}

	key := te.generateActionKey(toolName, args)
	action, exists := te.agent.circuitBreaker.Actions[key]
	if !exists {
		action = &CircuitBreakerAction{
			ActionType: toolName,
			Target:     key,
			Count:      0,
		}
		te.agent.circuitBreaker.Actions[key] = action
	}

	action.Count++
	action.LastUsed = getCurrentTime()

	// Clean up old entries (older than 5 minutes) to prevent memory leaks
	te.cleanupOldCircuitBreakerEntries()
}

// cleanupOldCircuitBreakerEntries removes entries older than 5 minutes
func (te *ToolExecutor) cleanupOldCircuitBreakerEntries() {
	if te.agent.circuitBreaker == nil {
		return
	}

	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300 // 5 minutes in seconds

	for key, action := range te.agent.circuitBreaker.Actions {
		if action.LastUsed < fiveMinutesAgo {
			delete(te.agent.circuitBreaker.Actions, key)
		}
	}
}

// generateActionKey creates a unique key for an action
func (te *ToolExecutor) generateActionKey(toolName string, args map[string]interface{}) string {
	// Create a deterministic key from tool name and arguments
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("%s:%s", toolName, string(argsJSON))
}

// GenerateToolCallID creates a unique tool call ID if one is missing
func (te *ToolExecutor) GenerateToolCallID(toolName string) string {
	// Use a simple timestamp + tool name pattern to create a unique ID
	timestamp := getCurrentTime()
	sanitizedName := strings.ReplaceAll(toolName, "_", "")
	return fmt.Sprintf("call_%s_%d", sanitizedName, timestamp)
}

// getCurrentTime returns the current time (abstracted for testing)
func getCurrentTime() int64 {
	return time.Now().Unix()
}
