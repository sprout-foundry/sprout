package agent

import (
	"context"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
)

// getToolTimeout returns the timeout duration for tool execution
// Subagents get 30 minutes (for large file operations), other tools get 5 minutes
// Can be overridden via LEDIT_TOOL_TIMEOUT environment variable (in seconds)
func getToolTimeout(toolName string) time.Duration {
	// Check for environment variable override first
	if envTimeout := os.Getenv("LEDIT_TOOL_TIMEOUT"); envTimeout != "" {
		if seconds, err := strconv.Atoi(envTimeout); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}

	// Tool-specific defaults
	// Subagents can take a long time for large file operations
	if isSubagentTool(toolName) {
		return 30 * time.Minute
	}

	// Default timeout for regular tools
	return 5 * time.Minute
}

// isSubagentTool checks if a tool is a subagent that needs extended timeout
func isSubagentTool(toolName string) bool {
	switch toolName {
	case "run_subagent", "run_parallel_subagents":
		return true
	default:
		return false
	}
}

// ToolExecutor handles tool execution logic
type ToolExecutor struct {
	agent       *Agent
	toolIndex   int   // Counter for tool execution order within each turn
	idCounter   int64 // Atomic counter for unique tool call ID generation
	idCounterMu sync.Mutex
}

const maxToolFailureMessageChars = 4000     // ~1000 tokens worst-case (4 chars/token heuristic)
const defaultFetchURLResultMaxChars = 80000 // Raised from 60000 to 80000 (better web content coverage)
const defaultFetchURLArchiveDir = "/tmp/ledit/downloads"
const defaultAnalyzeImageResultExcerptChars = 4000

// NewToolExecutor creates a new tool executor
func NewToolExecutor(agent *Agent) *ToolExecutor {
	return &ToolExecutor{
		agent: agent,
	}
}

// ExecuteTools executes a list of tool calls and returns the results
func (te *ToolExecutor) ExecuteTools(toolCalls []api.ToolCall) []api.Message {
	// Reset tool index counter at the start of each tool execution batch
	te.toolIndex = 0

	// Log tool calls at the beginning of the process
	if te.agent != nil {
		te.agent.debugLog("[tool] Executing %d tool calls\n", len(toolCalls))
		for _, tc := range toolCalls {
			te.agent.LogToolCall(tc, "executing")

			// Extract persona and subagent info from subagent arguments
			args, _, _ := parseToolArgumentsWithRepair(tc.Function.Arguments)
			persona := ""
			isSubagent := isSubagentTool(tc.Function.Name)
			subagentType := "single"
			if isSubagent {
				if p, ok := args["persona"].(string); ok {
					persona = p
				}
				if tc.Function.Name == "run_parallel_subagents" {
					subagentType = "parallel"
				}
			}
			displayName := formatToolCall(tc)
			te.agent.PublishToolStart(
				tc.Function.Name, tc.ID, tc.Function.Arguments,
				displayName, persona, isSubagent, subagentType,
			)
		}
	}

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

	// Optimize parallel execution for independent, side-effect-free batched tools.
	if te.canExecuteInParallel(toolCalls) {
		return te.executeParallel(toolCalls)
	}

	// Sequential execution for other tools
	return te.executeSequential(toolCalls)
}

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
