// Tool executor: sequential and single tool call execution.
package agent

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

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
			te.agent.debugLog("[tool] Executing tool %d/%d [%.0f%%]: %s\n", i+1, len(toolCalls), float64(i+1)/float64(len(toolCalls))*100, tc.Function.Name)
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
	// Use automatic tool index assignment
	currentToolIndex := te.toolIndex
	te.toolIndex++
	return te.executeSingleToolWithIndex(toolCall, currentToolIndex)
}

// executeSingleToolWithIndex executes a single tool call with a specific tool index
func (te *ToolExecutor) executeSingleToolWithIndex(toolCall api.ToolCall, toolIndex int) api.Message {
	// Capture start time for duration tracking
	startTime := time.Now()

	// Single canonical execution log for all tools (including MCP-prefixed tools).
	te.agent.ToolLog("executing tool", formatToolCall(toolCall))
	normalizedToolName := te.normalizeToolNameForScheduling(toolCall.Function.Name)
	if normalizedToolName != toolCall.Function.Name {
		te.agent.debugLog("[~] Normalized tool name: %s -> %s\n", toolCall.Function.Name, normalizedToolName)
	}

	var todoBefore []tools.TodoItem
	if normalizedToolName == "TodoWrite" {
		todoBefore = tools.TodoRead()
	}

	// Generate a tool call ID if empty to prevent sanitization from dropping the result
	toolCallID := toolCall.ID
	if toolCallID == "" {
		toolCallID = te.GenerateToolCallID(toolCall.Function.Name)
		te.agent.debugLog("[tool] Generated missing tool call ID: %s for tool: %s\n", toolCallID, toolCall.Function.Name)
	}

	// Parse arguments
	args, repairedArgs, parseErr := parseToolArgumentsWithRepair(toolCall.Function.Arguments)
	if parseErr != nil {
		// Record failed tool call to trace session
		te.recordToolExecutionWithIndex(normalizedToolName, toolCall.Function.Arguments, args, "", "", parseErr, toolIndex)
		return api.Message{
			Role:       "tool",
			Content:    fmt.Sprintf("Error parsing arguments: %v", parseErr),
			ToolCallId: toolCallID,
		}
	}
	if repairedArgs {
		te.agent.debugLog("[tool] Repaired malformed tool arguments for %s\n", normalizedToolName)
	}

	// Execute with circuit breaker check
	if te.checkCircuitBreaker(normalizedToolName, args) {
		// Record failed tool call to trace session
		err := fmt.Errorf("circuit breaker triggered")
		te.recordToolExecutionWithIndex(normalizedToolName, toolCall.Function.Arguments, args, "", "", err, toolIndex)
		return api.Message{
			Role:       "tool",
			Content:    "Circuit breaker: This action has been attempted too many times with the same parameters.",
			ToolCallId: toolCallID,
		}
	}

	// Create a context with a timeout for the tool execution
	// Subagents get 30 minutes (for large file operations), other tools get 5 minutes
	// Can be overridden via LEDIT_TOOL_TIMEOUT environment variable
	toolTimeout := getToolTimeout(normalizedToolName)
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()

	// Create a channel to receive the result of the tool execution
	resultChan := make(chan struct {
		images []api.ImageData
		result string
		err    error
	}, 1)

	// Execute the tool in a goroutine
	go func() {
		if normalizedToolName == "mcp_tools" {
			result, err := te.agent.handleMCPToolsCommand(args)
			resultChan <- struct {
				images []api.ImageData
				result string
				err    error
			}{nil, result, err}
			return
		}

		registry := GetToolRegistry()
		execCtx := withToolExecutionMetadata(ctx, toolCallID, normalizedToolName, te.agent.GetWorkspaceRoot())
		images, result, err := registry.ExecuteTool(execCtx, normalizedToolName, args, te.agent)

		if err != nil && strings.Contains(err.Error(), "unknown tool") {
			if fallbackResult, fallbackErr, handled := te.tryExecuteMCPTool(normalizedToolName, args); handled {
				resultChan <- struct {
					images []api.ImageData
					result string
					err    error
				}{nil, fallbackResult, fallbackErr}
				return
			}
		}

		resultChan <- struct {
			images []api.ImageData
			result string
			err    error
		}{images, result, err}
	}()

	var fullResult string
	var images []api.ImageData
	var err error

	// Wait for the tool to complete, timeout, or interrupt
	select {
	case res := <-resultChan:
		images = res.images
		fullResult = res.result
		err = res.err
	case <-ctx.Done():
		err = fmt.Errorf("tool execution timed out after %v", toolTimeout)
	case <-te.agent.interruptCtx.Done():
		err = fmt.Errorf("tool execution interrupted by user")
	}

	// Capture error for trace recording before modifying result
	recordErr := err

	if err != nil {
		safeErr := sanitizeToolFailureMessage(err.Error())
		// Ensure the error is visible to the user immediately
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed: %s", normalizedToolName, safeErr))
		te.agent.PrintLine("")
		fullResult = fmt.Sprintf("Error: %s", safeErr)
	}

	if err == nil && normalizedToolName == "TodoWrite" {
		te.emitTodoChecklistUpdate(todoBefore, tools.TodoRead())
	}

	// Apply model-specific constraints (truncation for fetch_url, etc.)
	// fullResult is the actual tool output
	// modelResult is what gets sent to the model (may be truncated)
	modelResult := fullResult
	if err == nil {
		modelResult = constrainToolResultForModel(normalizedToolName, args, fullResult)
	}

	// Record tool execution to trace session
	te.recordToolExecutionWithIndex(normalizedToolName, toolCall.Function.Arguments, args, fullResult, modelResult, recordErr, toolIndex)

	// Update circuit breaker
	te.updateCircuitBreaker(normalizedToolName, args)

	// Publish rich tool end event for real-time UI updates
	if te.agent != nil {
		status := "completed"
		if err != nil {
			status = "failed"
		}
		errorMsg := ""
		if err != nil {
			errorMsg = err.Error()
		}
		te.agent.PublishToolEnd(toolCallID, normalizedToolName, status, modelResult, errorMsg, time.Since(startTime))
	}

	return api.Message{
		Role:       "tool",
		Content:    modelResult,
		ToolCallId: toolCallID,
		Images:     images,
	}
}
