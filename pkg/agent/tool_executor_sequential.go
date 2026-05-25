// Tool executor: sequential and single tool call execution.
package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	tools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/security"
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
				ToolCallID: toolCallID,
			})
			return toolResults
		default:
			// Context not cancelled
		}

		// Flush any buffered streaming content before each tool execution
		// This ensures narrative text appears before each tool call for better flow
		if te.agent.output.GetFlushCallback() != nil {
			te.agent.output.GetFlushCallback()()
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
		todoBefore = te.agent.GetTodoManager().Read()
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
			ToolCallID: toolCallID,
		}
	}
	if repairedArgs {
		te.agent.debugLog("[tool] Repaired malformed tool arguments for %s\n", normalizedToolName)
	}

	// Execute with circuit breaker check
	if te.checkCircuitBreaker(normalizedToolName, args) {
		// Record failed tool call to trace session
		err := agenterrors.NewTransientError("circuit breaker triggered", nil)
		te.recordToolExecutionWithIndex(normalizedToolName, toolCall.Function.Arguments, args, "", "", err, toolIndex)
		return api.Message{
			Role:       "tool",
			Content:    "Circuit breaker: This action has been attempted too many times with the same parameters.",
			ToolCallID: toolCallID,
		}
	}

	// Create a context with a timeout for the tool execution
	// Subagents get 30 minutes (for large file operations), other tools get 5 minutes
	// Can be overridden via SPROUT_TOOL_TIMEOUT environment variable
	// Use agent's interrupt context as parent so user Ctrl+C propagates to running tools
	toolTimeout := getToolTimeout(normalizedToolName)
	parentCtx := te.agent.InterruptCtx()
	ctx, cancel := context.WithTimeout(parentCtx, toolTimeout)
	defer cancel()

	// Create a channel to receive the result of the tool execution
	resultChan := make(chan struct {
		images []api.ImageData
		result string
		err    error
	}, 1)

	// Execute the tool in a goroutine
	go func() {
		// SP-038: Check new handler registry first for dual dispatch.
		if registry := te.getHandlerRegistry(); registry != nil {
			if handler, found := registry.Lookup(normalizedToolName); found {
				te.agent.debugLog("[tool] registry dispatch: %s\n", normalizedToolName)
				env := tools.ToolEnv{
					WorkspaceRoot: te.agent.GetWorkspaceRoot(),
					ConfigManager: te.agent.GetConfigManager(),
					EventBus:      te.agent.GetEventBus(),
				}
				// Validate arguments before execution.
				if err := handler.Validate(args); err != nil {
					resultChan <- struct {
						images []api.ImageData
						result string
						err    error
					}{nil, fmt.Sprintf("Validation failed: %v", err), err}
					return
				}
				// Propagate execution metadata for tracing/observability.
				execCtx := withToolExecutionMetadata(ctx, toolCallID, normalizedToolName, te.agent.GetWorkspaceRoot())
				res, err := handler.Execute(execCtx, env, args)
				if err != nil {
					output := res.Output
					resultChan <- struct {
						images []api.ImageData
						result string
						err    error
					}{nil, output, err}
					return
				}
				// Map ToolResult.IsError to legacy error path.
				if res.IsError {
					resultChan <- struct {
						images []api.ImageData
						result string
						err    error
					}{nil, res.Output, errors.New(res.Output)}
					return
				}

				// Convert new-style ImageData to legacy api.ImageData if present.
				var images []api.ImageData
				for _, img := range res.Images {
					images = append(images, api.ImageData{URL: img.URI, Type: img.MIMEType})
				}
				resultChan <- struct {
					images []api.ImageData
					result string
					err    error
				}{images, res.Output, nil}
				return
			}
		}
		te.agent.debugLog("[tool] legacy dispatch: %s\n", normalizedToolName)

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

		// Check for unknown tool using typed error classification first,
		// then fall back to string matching for untyped errors (backward compat).
		// TODO: migrate registry to return InvalidInputError for unknown tools
		// so this string check can be removed entirely.
		if err != nil && (agenterrors.IsInvalidInput(err) || strings.Contains(err.Error(), "unknown tool")) {
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
		err = agenterrors.NewTransientError(fmt.Sprintf("tool execution timed out after %s", toolTimeout), nil)
	case <-te.agent.interruptCtx.Done():
		err = agenterrors.NewTransientError("tool execution interrupted by user", nil)
	}

	// Capture error for trace recording before modifying result
	recordErr := err

	if err != nil {
		safeErr := sanitizeToolFailureMessage(err.Error())
		
		// Use typed error classification to detect security errors, with a
		// fallback to string matching for untyped errors (backward compat).
		//
		// SECURITY BOUNDARY NOTE: The underlying classification in
		// pkg/agent_tools/security_classifier.go is purely string-based heuristics with known
		// limitations (no filesystem access, no symlink resolution, no env variable
		// expansion). This caution flow is a defense-in-depth layer, not a security
		// boundary. Actual enforcement relies on the user's filesystem permissions,
		// interactive confirmation, and operating system controls.
		action := ClassifyError(err)
		if action == ActionEscalate || strings.Contains(err.Error(), "security caution:") {
			// This is a caution-level operation that requires LLM verification
			// Send it back to the LLM as a special message that indicates "verify before proceeding"
			te.agent.PrintLine("")
			te.agent.PrintLine(fmt.Sprintf("[⚠️  SECURITY CAUTION - LLM VERIFICATION REQUIRED] %s", safeErr))
			te.agent.PrintLine("")
			
			// Return a special tool result that signals the LLM to re-verify
			// The LLM will see this and can decide to re-assert safety and retry, or abort
			return api.Message{
				Role:       "tool",
				Content:    fmt.Sprintf("SECURITY_CAUTION_REQUIRED: %s", safeErr),
				ToolCallID: toolCallID,
			}
		}
		
		// Ensure the error is visible to the user immediately
		te.agent.PrintLine("")
		te.agent.PrintLine(fmt.Sprintf("[FAIL] Tool '%s' failed: %s", normalizedToolName, safeErr))
		te.agent.PrintLine("")
		fullResult = fmt.Sprintf("Error: %s", safeErr)
	}

	if err == nil && normalizedToolName == "TodoWrite" {
		te.emitTodoChecklistUpdate(todoBefore, te.agent.GetTodoManager().Read())
	}

	// Apply model-specific constraints (truncation for fetch_url, etc.)
	// fullResult is the actual tool output
	// modelResult is what gets sent to the model (may be truncated)
	modelResult := fullResult
	if err == nil {
		modelResult = constrainToolResultForModel(normalizedToolName, args, fullResult)

		// Universal truncation: cap total result size to prevent blowing up LLM context
		modelResult = truncateToolResult(modelResult)
	}

	// Apply secret redaction to tool output before sending to LLM.
	if err == nil && modelResult != "" && te.agent.security.GetOutputRedactor() != nil &&
		isSecretSensitiveTool(normalizedToolName) {
		redactResult := te.agent.security.GetOutputRedactor().RedactToolOutput(modelResult, normalizedToolName, args)
		if len(redactResult.Secrets) > 0 {
			modelResult = te.applySecretElevation(modelResult, redactResult, normalizedToolName, args, toolCallID)
		}
	}

	// Also redact fullResult for trace recording to avoid storing secrets.
	traceResult := fullResult
	if te.agent.security.GetOutputRedactor() != nil {
		traceRedactResult := te.agent.security.GetOutputRedactor().RedactToolOutput(fullResult, normalizedToolName, args)
		traceResult = traceRedactResult.Content
	}

	if strings.HasPrefix(modelResult, "BLOCKED:") {
		return api.Message{
			Role:       "tool",
			Content:    modelResult,
			ToolCallID: toolCallID,
		}
	}

	// Record tool execution to trace session
	te.recordToolExecutionWithIndex(normalizedToolName, toolCall.Function.Arguments, args, traceResult, modelResult, recordErr, toolIndex)

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
		ToolCallID: toolCallID,
		Images:     images,
	}
}

// applySecretElevation evaluates detected secrets through the elevation gate
// and returns the appropriate content to send to the LLM (redacted, allowed, or blocked).
func (te *ToolExecutor) applySecretElevation(originalResult string, redactResult security.RedactionResult, toolName string, args map[string]interface{}, toolCallID string) string {
	if te.agent.security.GetElevationGate() == nil {
		return redactResult.Content // no gate — default to redaction
	}

	source := toolName
	if path, ok := args["path"].(string); ok && path != "" {
		source = toolName + ": " + path
	}
	if cmd, ok := args["command"].(string); ok && cmd != "" {
		if len(cmd) > 80 {
			cmd = cmd[:77] + "..."
		}
		source = toolName + ": " + cmd
	}

	action, evalErr := te.agent.security.GetElevationGate().Evaluate(redactResult.Secrets, source)
	if evalErr != nil {
		te.agent.debugLog("[security] elevation gate error: %v\n", evalErr)
	}

	switch action {
	case security.SecretAllow:
		te.agent.debugLog("[security] user allowed %d secret(s) in %s\n", len(redactResult.Secrets), toolName)
		return originalResult
	case security.SecretBlock:
		te.agent.PrintLine(fmt.Sprintf("[security] Blocked %s: %d secret(s) detected, user chose to block", source, len(redactResult.Secrets)))
		return fmt.Sprintf("BLOCKED: detected secrets in output. Operation blocked. Found %d secret(s) — user chose to block.", len(redactResult.Secrets))
	default:
		te.agent.debugLog("[security] redacted %d secret(s) from %s\n", len(redactResult.Secrets), toolName)
		return redactResult.Content
	}
}
