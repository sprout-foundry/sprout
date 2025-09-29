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
			results = append(results, api.Message{
				Role:       "tool",
				Content:    "Execution interrupted by user",
				ToolCallId: tc.ID,
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
			defer wg.Done()

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
			toolResults = append(toolResults, api.Message{
				Role:       "tool",
				Content:    "Execution interrupted by user",
				ToolCallId: tc.ID,
			})
			break
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
	}
	// Parse arguments
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(toolCall.Function.Arguments), &args); err != nil {
		return api.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("Error parsing arguments: %v", err),
			ToolCallId: toolCall.ID,
		}
	}

	// Check for MCP tools
	if te.isMCPTool(toolCall.Function.Name) {
		return te.executeMCPTool(toolCall, args)
	}

	// Execute with circuit breaker check
	if te.checkCircuitBreaker(toolCall.Function.Name, args) {
		return api.Message{
			Role:       "tool",
			Content:    "Circuit breaker: This action has been attempted too many times with the same parameters.",
			ToolCallId: toolCall.ID,
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
		registry := GetToolRegistry()
		result, err := registry.ExecuteTool(ctx, toolCall.Function.Name, args, te.agent)
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

	return api.Message{
		Role:       "tool",
		Content:    result,
		ToolCallId: toolCall.ID,
	}
}

// isMCPTool checks if a tool is an MCP tool
func (te *ToolExecutor) isMCPTool(toolName string) bool {
	return strings.Contains(toolName, ":")
}

// executeMCPTool executes an MCP tool
func (te *ToolExecutor) executeMCPTool(toolCall api.ToolCall, args map[string]interface{}) api.Message {
	parts := strings.SplitN(toolCall.Function.Name, ":", 2)
	if len(parts) != 2 {
		return api.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("Invalid MCP tool name format: %s", toolCall.Function.Name),
			ToolCallId: toolCall.ID,
		}
	}

	serverName := parts[0]
	toolName := parts[1]

	te.agent.debugLog("🔧 Executing MCP tool: %s on server: %s\n", toolName, serverName)

	ctx := context.Background()
	mcpResult, err := te.agent.mcpManager.CallTool(ctx, serverName, toolName, args)
	if err != nil {
		return api.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("Error calling MCP tool: %v", err),
			ToolCallId: toolCall.ID,
		}
	}

	// Convert MCP result to string
	var resultContent string
	if mcpResult.IsError {
		resultContent = "MCP Error: "
	}
	for _, content := range mcpResult.Content {
		if content.Type == "text" {
			resultContent += content.Text
		} else if content.Type == "resource" {
			resultContent += fmt.Sprintf("[Resource: %s]", content.Data)
		}
	}

	return api.Message{
		Role:       "tool",
		Content:    resultContent,
		ToolCallId: toolCall.ID,
	}
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

// getCurrentTime returns the current time (abstracted for testing)
func getCurrentTime() int64 {
	return time.Now().Unix()
}
