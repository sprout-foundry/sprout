package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	tools "github.com/alantheprice/ledit/pkg/agent_tools"
	"github.com/alantheprice/ledit/pkg/trace"
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
		images, result, err := registry.ExecuteTool(ctx, normalizedToolName, args, te.agent)

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

func constrainToolResultForModel(toolName string, args map[string]interface{}, result string) string {
	if toolName == "analyze_image_content" {
		return compactAnalyzeImageResultForModel(result)
	}

	if toolName != "fetch_url" {
		return result
	}

	maxChars := defaultFetchURLResultMaxChars
	if raw := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_MAX_CHARS")); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			maxChars = parsed
		}
	}

	if len(result) <= maxChars {
		return result
	}

	headLen := maxChars * 70 / 100
	tailLen := maxChars - headLen
	if tailLen <= 0 {
		tailLen = maxChars / 2
		headLen = maxChars - tailLen
	}

	omitted := len(result) - (headLen + tailLen)
	if omitted < 0 {
		omitted = 0
	}

	archivePath, archiveErr := saveFetchURLOutputToFile(args, result)
	notice := buildFetchURLTruncationNotice(omitted, archivePath, archiveErr)
	return result[:headLen] + notice + result[len(result)-tailLen:]
}

func buildFetchURLTruncationNotice(omitted int, archivePath string, archiveErr error) string {
	if archivePath == "" {
		if archiveErr != nil {
			return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Failed to save full output: %v]\n\n", omitted, archiveErr)
		}
		return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output path unavailable.]\n\n", omitted)
	}
	return fmt.Sprintf("\n\n[FETCH_URL OUTPUT TRUNCATED FOR MODEL CONTEXT: omitted %d characters. Set LEDIT_FETCH_URL_MAX_CHARS to adjust. Full output saved to %s]\n\n", omitted, archivePath)
}

func saveFetchURLOutputToFile(args map[string]interface{}, output string) (string, error) {
	dir := strings.TrimSpace(os.Getenv("LEDIT_FETCH_URL_ARCHIVE_DIR"))
	if dir == "" {
		dir = defaultFetchURLArchiveDir
	}

	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}

	timestamp := time.Now().Format("20060102_150405")
	filename := fmt.Sprintf("fetch_url_%s_%d.txt", timestamp, time.Now().UnixNano()%1_000_000)
	path := filepath.Join(dir, filename)

	header := ""
	if args != nil {
		if rawURL, ok := args["url"].(string); ok && strings.TrimSpace(rawURL) != "" {
			header = fmt.Sprintf("URL: %s\nFetched-At: %s\n\n", strings.TrimSpace(rawURL), time.Now().Format(time.RFC3339))
		}
	}

	fullOutput := output
	if header != "" {
		fullOutput = header + output
	}

	if err := os.WriteFile(path, []byte(fullOutput), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func compactAnalyzeImageResultForModel(result string) string {
	var parsed tools.ImageAnalysisResponse
	if err := json.Unmarshal([]byte(result), &parsed); err != nil {
		return result
	}

	var b strings.Builder
	b.WriteString("analyze_image_content result:\n")
	b.WriteString(fmt.Sprintf("- success: %t\n", parsed.Success))
	if parsed.InputPath != "" {
		b.WriteString(fmt.Sprintf("- input_path: %s\n", parsed.InputPath))
	}
	if parsed.InputType != "" {
		b.WriteString(fmt.Sprintf("- input_type: %s\n", parsed.InputType))
	}
	b.WriteString(fmt.Sprintf("- ocr_attempted: %t\n", parsed.OCRAttempted))

	if parsed.ErrorCode != "" {
		b.WriteString(fmt.Sprintf("- error_code: %s\n", parsed.ErrorCode))
	}
	if parsed.ErrorMessage != "" {
		b.WriteString(fmt.Sprintf("- error_message: %s\n", parsed.ErrorMessage))
	}

	excerpt := strings.TrimSpace(parsed.ExtractedText)
	if excerpt == "" && parsed.Analysis != nil {
		excerpt = strings.TrimSpace(parsed.Analysis.Description)
	}
	if excerpt != "" {
		originalChars := parsed.OriginalChars
		if originalChars == 0 {
			originalChars = len(excerpt)
		}
		returnedChars := parsed.ReturnedChars
		if returnedChars == 0 {
			returnedChars = len(excerpt)
		}
		b.WriteString(fmt.Sprintf("- extracted_chars: %d\n", originalChars))
		b.WriteString(fmt.Sprintf("- returned_chars: %d\n", returnedChars))
	}
	if parsed.OutputTruncated {
		b.WriteString("- tool_output_truncated: true\n")
	}
	if parsed.FullOutputPath != "" {
		b.WriteString(fmt.Sprintf("- full_output_path: %s\n", parsed.FullOutputPath))
	}
	if parsed.Analysis != nil {
		if len(parsed.Analysis.Elements) > 0 {
			b.WriteString(fmt.Sprintf("- detected_elements: %d\n", len(parsed.Analysis.Elements)))
		}
		if len(parsed.Analysis.Issues) > 0 {
			b.WriteString(fmt.Sprintf("- issues: %s\n", strings.Join(parsed.Analysis.Issues, "; ")))
		}
		if len(parsed.Analysis.Suggestions) > 0 {
			b.WriteString(fmt.Sprintf("- suggestions: %s\n", strings.Join(parsed.Analysis.Suggestions, "; ")))
		}
	}
	if excerpt != "" {
		b.WriteString("- extracted_text_excerpt:\n")
		b.WriteString(limitAnalyzeImageExcerpt(excerpt, defaultAnalyzeImageResultExcerptChars))
	}

	return strings.TrimSpace(b.String())
}

func limitAnalyzeImageExcerpt(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if maxChars <= 0 || len(text) <= maxChars {
		return text
	}

	suffix := fmt.Sprintf("\n[EXCERPT TRUNCATED: kept first %d of %d chars]", maxChars, len(text))
	keep := maxChars - len(suffix)
	if keep < 0 {
		keep = maxChars
		suffix = ""
	}
	return strings.TrimSpace(text[:keep]) + suffix
}

// recordToolExecutionWithIndex records tool execution data to the trace session
func (te *ToolExecutor) recordToolExecutionWithIndex(toolName string, rawArgs string, args map[string]interface{}, fullResult, modelResult string, err error, toolIndex int) {
	if te.agent == nil || te.agent.traceSession == nil {
		return // Trace session not enabled
	}

	// Type assert to trace session interface
	type traceSessionInterface interface {
		GetRunID() string
		RecordToolCall(record interface{}) error
	}

	traceSession, ok := te.agent.traceSession.(traceSessionInterface)
	if !ok {
		te.agent.debugLog("DEBUG: traceSession is not a valid trace session, skipping tool call recording\n")
		return
	}

	// Categorize the error
	errorCategory, errorMessage := te.categorizeError(toolName, err)

	// Create normalized arguments
	argsNormalized := te.normalizeArguments(args)

	// Build ToolCallRecord
	toolCallRecord := trace.ToolCallRecord{
		RunID:          traceSession.GetRunID(),
		TurnIndex:      te.agent.currentIteration,
		ToolIndex:      toolIndex,
		ToolName:       toolName,
		Args:           args,
		ArgsNormalized: argsNormalized,
		Success:        err == nil,
		FullResult:     fullResult,
		ModelResult:    modelResult,
		ErrorCategory:  errorCategory,
		ErrorMessage:   errorMessage,
		MachineLabels:  []string{},
		Timestamp:      time.Now().Format(time.RFC3339),
	}

	// Record the tool call
	if err := traceSession.RecordToolCall(toolCallRecord); err != nil {
		te.agent.debugLog("DEBUG: Failed to record tool call: %v\n", err)
	}
}

// normalizeArguments normalizes arguments for consistent representation in traces
func (te *ToolExecutor) normalizeArguments(args map[string]interface{}) map[string]interface{} {
	if args == nil {
		return nil
	}

	normalized := make(map[string]interface{})
	for key, value := range args {
		// Stringify the key for consistency
		stringKey := fmt.Sprintf("%v", key)

		// Normalize numeric values to positive integers where applicable
		switch v := value.(type) {
		case int, int8, int16, int32, int64:
			if normalizedInt := normalizePositiveInt(v); normalizedInt > 0 {
				normalized[stringKey] = normalizedInt
			} else {
				normalized[stringKey] = v
			}
		case uint, uint8, uint16, uint32, uint64:
			if normalizedInt := normalizePositiveInt(v); normalizedInt > 0 {
				normalized[stringKey] = normalizedInt
			} else {
				normalized[stringKey] = v
			}
		case float32, float64:
			// Convert floats to int if they're whole numbers
			var floatValue float64
			if f32, ok := value.(float32); ok {
				floatValue = float64(f32)
			} else {
				floatValue = value.(float64)
			}
			if floatValue == float64(int(floatValue)) {
				if normalizedInt := normalizePositiveInt(int(floatValue)); normalizedInt > 0 {
					normalized[stringKey] = normalizedInt
				} else {
					normalized[stringKey] = int(floatValue)
				}
			} else {
				normalized[stringKey] = floatValue
			}
		default:
			normalized[stringKey] = value
		}
	}
	return normalized
}

// categorizeError categorizes errors for trace recording
func (te *ToolExecutor) categorizeError(toolName string, err error) (string, string) {
	if err == nil {
		return "", ""
	}

	errorMsg := err.Error()

	// Check for unknown tool
	if strings.Contains(errorMsg, "unknown tool") || strings.Contains(errorMsg, "tool not found") {
		return "unknown_tool", errorMsg
	}

	// Check for timeout
	if strings.Contains(errorMsg, "timed out") || strings.Contains(errorMsg, "timeout") {
		return "timeout", errorMsg
	}

	// Check for validation errors (argument parsing, schema validation)
	if strings.Contains(errorMsg, "parsing arguments") || strings.Contains(errorMsg, "invalid arguments") ||
		strings.Contains(errorMsg, "validation") || strings.Contains(errorMsg, "schema") {
		return "validation", errorMsg
	}

	// Check for circuit breaker
	if strings.Contains(errorMsg, "circuit breaker") {
		return "execution_error", errorMsg
	}

	// Default to execution error
	return "execution_error", errorMsg
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

	// Copy action value outside the lock to reduce critical section hold time
	action := func() *CircuitBreakerAction {
		te.agent.circuitBreaker.mu.RLock()
		defer te.agent.circuitBreaker.mu.RUnlock()
		return te.agent.circuitBreaker.Actions[key]
	}()

	if action == nil {
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
// The caller expects this function to be thread-safe with respect to the circuitBreaker map.
func (te *ToolExecutor) updateCircuitBreaker(toolName string, args map[string]interface{}) {
	if te.agent.circuitBreaker == nil {
		return
	}

	key := te.generateActionKey(toolName, args)
	te.agent.circuitBreaker.mu.Lock()
	defer te.agent.circuitBreaker.mu.Unlock()

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
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// cleanupOldCircuitBreakerEntriesLocked removes entries older than 5 minutes
// Precondition: caller must hold te.agent.circuitBreaker.mu.Lock()
func (te *ToolExecutor) cleanupOldCircuitBreakerEntriesLocked() {
	currentTime := getCurrentTime()
	fiveMinutesAgo := currentTime - 300 // 5 minutes in seconds

	for key, action := range te.agent.circuitBreaker.Actions {
		if action.LastUsed < fiveMinutesAgo {
			delete(te.agent.circuitBreaker.Actions, key)
		}
	}
}

// cleanupOldCircuitBreakerEntries removes entries older than 5 minutes
// This function handles locking internally and is safe to call from anywhere.
func (te *ToolExecutor) cleanupOldCircuitBreakerEntries() {
	if te.agent.circuitBreaker == nil {
		return
	}

	te.agent.circuitBreaker.mu.Lock()
	defer te.agent.circuitBreaker.mu.Unlock()
	te.cleanupOldCircuitBreakerEntriesLocked()
}

// generateActionKey creates a unique key for an action
func (te *ToolExecutor) generateActionKey(toolName string, args map[string]interface{}) string {
	// Create a deterministic key from tool name and arguments
	argsJSON, _ := json.Marshal(args)
	return fmt.Sprintf("%s:%s", toolName, string(argsJSON))
}

// GenerateToolCallID creates a unique tool call ID if one is missing
func (te *ToolExecutor) GenerateToolCallID(toolName string) string {
	// Use a monotonic counter to guarantee uniqueness even under parallel execution
	te.idCounterMu.Lock()
	te.idCounter++
	seq := te.idCounter
	te.idCounterMu.Unlock()

	timestamp := getCurrentTime()
	sanitizedName := strings.ReplaceAll(toolName, "_", "")
	return fmt.Sprintf("call_%s_%d_%d", sanitizedName, timestamp, seq)
}

// getCurrentTime returns the current time (abstracted for testing)
func getCurrentTime() int64 {
	return time.Now().Unix()
}

// formatToolCall formats a tool call for display before execution
// Maximum display length for tool call arguments before truncation
const maxToolArgDisplayLength = 50

// formatTruncateString truncates a string to the maximum display length and adds ellipsis if needed,
// then wraps it in quotes for unambiguous display
func formatTruncateString(s string) string {
	if len(s) > maxToolArgDisplayLength {
		s = s[:maxToolArgDisplayLength-3] + "..."
	}
	return fmt.Sprintf("%q", s)
}

func formatToolCall(toolCall api.ToolCall) string {
	// Format: [tool_name]
	// Example: [read_file] "path/to/file.go"
	args, _, err := parseToolArgumentsWithRepair(toolCall.Function.Arguments)
	if err != nil {
		log.Printf("Warning: Failed to parse tool arguments for tool '%s': %v", toolCall.Function.Name, err)
		return fmt.Sprintf("[%s]", toolCall.Function.Name)
	}

	// Extract meaningful arguments for display
	var parts []string
	parts = append(parts, toolCall.Function.Name)

	// Add common parameters consistently with quoting.
	if path, ok := args["path"].(string); ok && path != "" {
		parts = append(parts, formatTruncateString(path))
	} else if filePath, ok := args["file_path"].(string); ok && filePath != "" {
		parts = append(parts, formatTruncateString(filePath))
	}
	if url, ok := args["url"].(string); ok && url != "" {
		parts = append(parts, formatTruncateString(url))
	}
	if imagePath, ok := args["image_path"].(string); ok && imagePath != "" {
		parts = append(parts, formatTruncateString(imagePath))
	}
	if query, ok := args["query"].(string); ok && query != "" {
		parts = append(parts, formatTruncateString(query))
	}
	if command, ok := args["command"].(string); ok && command != "" {
		parts = append(parts, formatTruncateString(command))
	}
	if operation, ok := args["operation"].(string); ok && operation != "" {
		parts = append(parts, formatTruncateString(operation))
	}
	if content, ok := args["content"].(string); ok && len(content) > 0 {
		parts = append(parts, fmt.Sprintf("(%d bytes)", len(content)))
	}
	if pattern, ok := args["pattern"].(string); ok && pattern != "" {
		parts = append(parts, formatTruncateString(pattern))
	}
	if todoSummary := summarizeTodoWriteArgs(args); todoSummary != "" {
		parts = append(parts, todoSummary)
	}

	result := fmt.Sprintf("[%s]", strings.Join(parts, " "))
	return result
}

func summarizeTodoWriteArgs(args map[string]interface{}) string {
	todosRaw, ok := args["todos"].([]interface{})
	if !ok || len(todosRaw) == 0 {
		return ""
	}

	var pending, inProgress, completed, cancelled int
	for _, todoRaw := range todosRaw {
		todoMap, ok := todoRaw.(map[string]interface{})
		if !ok {
			continue
		}
		status, _ := todoMap["status"].(string)
		switch status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}
	}

	return fmt.Sprintf("todos=%d [ ]=%d [~]=%d [x]=%d [-]=%d", len(todosRaw), pending, inProgress, completed, cancelled)
}

func (te *ToolExecutor) emitTodoChecklistUpdate(before, after []tools.TodoItem) {
	if te.agent == nil {
		return
	}

	type todoKey struct {
		ID      string
		Content string
	}
	getKey := func(t tools.TodoItem) todoKey {
		return todoKey{
			ID:      strings.TrimSpace(t.ID),
			Content: strings.TrimSpace(t.Content),
		}
	}
	statusBefore := make(map[todoKey]string, len(before))
	for _, t := range before {
		statusBefore[getKey(t)] = t.Status
	}

	var pending, inProgress, completed, cancelled int
	changed := make([]string, 0, len(after))

	for _, t := range after {
		switch t.Status {
		case "pending":
			pending++
		case "in_progress":
			inProgress++
		case "completed":
			completed++
		case "cancelled":
			cancelled++
		}

		key := getKey(t)
		prevStatus, existed := statusBefore[key]
		if !existed || prevStatus != t.Status {
			statusSymbol := todoStatusSymbol(t.Status)
			label := strings.TrimSpace(t.Content)
			if label == "" {
				label = "<untitled>"
			}
			if existed {
				changed = append(changed, fmt.Sprintf("%s %s (%s -> %s)", statusSymbol, label, prevStatus, t.Status))
			} else {
				changed = append(changed, fmt.Sprintf("%s %s (new)", statusSymbol, label))
			}
		}
	}

	// Publish structured todo update event for WebUI
	var todoItems []map[string]interface{}
	for _, t := range after {
		todoItems = append(todoItems, map[string]interface{}{
			"id":      t.ID,
			"content": t.Content,
			"status":  t.Status,
		})
	}
	te.agent.PublishTodoUpdate(todoItems)

	// In streaming mode, skip text output — the WebUI receives structured
	// todo_update events and does not need the inline text trace.
	if !te.agent.IsStreamingEnabled() {
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("[edit] Todo update: %d total | [ ] %d pending | [~] %d in progress | [x] %d completed | [-] %d cancelled",
			len(after), pending, inProgress, completed, cancelled))

		if len(changed) == 0 {
			te.agent.PrintLine("   No checklist changes detected.")
			te.agent.PrintLine("")
			return
		}

		maxLines := 8
		for i, line := range changed {
			if i >= maxLines {
				te.agent.PrintLine(fmt.Sprintf("   ... and %d more changes", len(changed)-maxLines))
				break
			}
			te.agent.PrintLine("   " + line)
		}
		te.agent.PrintLine("")
	}
}

func todoStatusSymbol(status string) string {
	switch status {
	case "pending":
		return "[ ]"
	case "in_progress":
		return "[~]"
	case "completed":
		return "[x]"
	case "cancelled":
		return "[-]"
	default:
		return "[?]"
	}
}

var toolFailureDataURLPattern = regexp.MustCompile(`data:[^;\s]+;base64,[A-Za-z0-9+/=]+`)
var toolFailureBase64RunPattern = regexp.MustCompile(`[A-Za-z0-9+/=]{512,}`)
var jsonTrailingCommaPattern = regexp.MustCompile(`,(\s*[}\]])`)

func sanitizeToolFailureMessage(msg string) string {
	if strings.TrimSpace(msg) == "" {
		return "unknown tool error"
	}

	msg = toolFailureDataURLPattern.ReplaceAllStringFunc(msg, func(m string) string {
		mime := "application/octet-stream"
		if semi := strings.Index(m, ";"); semi > len("data:") {
			mime = m[len("data:"):semi]
		}
		return "data:" + mime + ";base64,[REDACTED]"
	})

	msg = toolFailureBase64RunPattern.ReplaceAllString(msg, "[BASE64_REDACTED]")

	if len(msg) > maxToolFailureMessageChars {
		msg = msg[:maxToolFailureMessageChars] + "... (truncated)"
	}
	return msg
}

func parseToolArgumentsWithRepair(raw string) (map[string]interface{}, bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false, fmt.Errorf("empty arguments")
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err == nil {
		return args, false, nil
	}

	candidates := repairJSONArgumentCandidates(raw)
	for _, candidate := range candidates {
		if candidate == raw {
			continue
		}
		var repaired map[string]interface{}
		if err := json.Unmarshal([]byte(candidate), &repaired); err == nil {
			return repaired, true, nil
		}
	}

	return nil, false, fmt.Errorf("invalid JSON arguments")
}

func repairJSONArgumentCandidates(raw string) []string {
	seen := make(map[string]struct{})
	candidates := make([]string, 0, 12)
	add := func(s string) {
		s = strings.TrimSpace(s)
		if s == "" {
			return
		}
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		candidates = append(candidates, s)
	}

	add(raw)
	withoutFence := stripMarkdownCodeFence(raw)
	add(withoutFence)

	for _, base := range []string{raw, withoutFence} {
		add(extractFirstBalancedJSONObject(base))
		add(extractOuterJSONObject(base))
	}

	initial := append([]string(nil), candidates...)
	for _, c := range initial {
		noCommas := removeJSONTrailingCommas(c)
		add(noCommas)
		add(closeJSONDelimiters(noCommas))
		add(closeJSONDelimiters(c))
	}

	return candidates
}

func stripMarkdownCodeFence(raw string) string {
	trimmed := strings.TrimSpace(raw)
	if !strings.HasPrefix(trimmed, "```") {
		return trimmed
	}
	lines := strings.Split(trimmed, "\n")
	if len(lines) < 2 {
		return trimmed
	}
	if !strings.HasPrefix(lines[0], "```") {
		return trimmed
	}
	end := len(lines)
	if strings.TrimSpace(lines[end-1]) == "```" {
		end--
	}
	if end <= 1 {
		return trimmed
	}
	return strings.Join(lines[1:end], "\n")
}

func extractOuterJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return ""
	}
	return raw[start : end+1]
}

func extractFirstBalancedJSONObject(raw string) string {
	start := strings.Index(raw, "{")
	if start < 0 {
		return ""
	}

	depth := 0
	inString := false
	escape := false
	for i := start; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		if ch == '"' {
			inString = true
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				return raw[start : i+1]
			}
		}
	}
	return ""
}

func removeJSONTrailingCommas(raw string) string {
	return jsonTrailingCommaPattern.ReplaceAllString(raw, "$1")
}

func closeJSONDelimiters(raw string) string {
	stack := make([]byte, 0, 16)
	inString := false
	escape := false

	for i := 0; i < len(raw); i++ {
		ch := raw[i]
		if inString {
			if escape {
				escape = false
				continue
			}
			if ch == '\\' {
				escape = true
				continue
			}
			if ch == '"' {
				inString = false
			}
			continue
		}

		switch ch {
		case '"':
			inString = true
		case '{', '[':
			stack = append(stack, ch)
		case '}':
			if len(stack) == 0 || stack[len(stack)-1] != '{' {
				return raw
			}
			stack = stack[:len(stack)-1]
		case ']':
			if len(stack) == 0 || stack[len(stack)-1] != '[' {
				return raw
			}
			stack = stack[:len(stack)-1]
		}
	}

	if len(stack) == 0 {
		return raw
	}

	var b strings.Builder
	b.WriteString(raw)
	for i := len(stack) - 1; i >= 0; i-- {
		if stack[i] == '{' {
			b.WriteByte('}')
		} else {
			b.WriteByte(']')
		}
	}
	return b.String()
}
