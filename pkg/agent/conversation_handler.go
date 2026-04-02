package agent

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/events"
	"github.com/alantheprice/ledit/pkg/trace"
)

// ConversationHandler manages the high-level conversation flow
type ConversationHandler struct {
	agent                      *Agent
	apiClient                  *APIClient
	toolExecutor               *ToolExecutor
	responseValidator          *ResponseValidator
	errorHandler               *ErrorHandler
	fallbackParser             *FallbackParser
	consecutiveBlankIterations int
	conversationStartTime      time.Time
	lastActivityTime           time.Time
	transientMessagesMu        sync.Mutex
	transientMessages          []api.Message
	pendingUserMessage         string
	queryStartIndex            int
	turnHistory                []TurnEvaluation
	ocrEnforcementAttempts     int
	tentativeRejectionCount    int
	traceSession               interface{}       // Using interface{} to avoid circular import
	currentTurnRecord          *trace.TurnRecord // Temporary storage for current turn, updated with response data later
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(agent *Agent) *ConversationHandler {
	now := time.Now()
	return &ConversationHandler{
		agent:                 agent,
		apiClient:             NewAPIClient(agent),
		toolExecutor:          NewToolExecutor(agent),
		responseValidator:     NewResponseValidator(agent),
		errorHandler:          NewErrorHandler(agent),
		fallbackParser:        NewFallbackParser(agent),
		conversationStartTime: now,
		lastActivityTime:      now,
		traceSession:          agent.traceSession, // Pass trace session from agent
	}
}

// ProcessQuery handles a user query through the complete conversation flow
func (ch *ConversationHandler) ProcessQuery(userQuery string) (string, error) {
	if ch.agent.debug {
		ch.agent.debugLog("DEBUG: ProcessQuery called with: %s\n", userQuery)
	}
	ch.agent.lastRunTerminationReason = ""

	// Publish query started event
	ch.agent.publishEvent(events.EventTypeQueryStarted, events.QueryStartedEvent(userQuery, ch.agent.GetProvider(), ch.agent.GetModel()))

	// Initialize conversation tracking
	ch.conversationStartTime = time.Now()
	ch.lastActivityTime = time.Now()

	// Reset streaming buffer for new query
	ch.agent.streamingBuffer.Reset()
	ch.agent.reasoningBuffer.Reset()

	// Enable change tracking
	ch.agent.EnableChangeTracking(userQuery)

	// Reset circuit breaker history for a fresh query to avoid carrying over
	// repetitive-tool counts from previous requests.
	if ch.agent.circuitBreaker != nil {
		ch.agent.circuitBreaker.mu.Lock()
		// Clear entries instead of replacing map to avoid memory churn and reduce lock hold time
		for key := range ch.agent.circuitBreaker.Actions {
			delete(ch.agent.circuitBreaker.Actions, key)
		}
		ch.agent.circuitBreaker.mu.Unlock()
		if ch.agent.debug {
			ch.agent.debugLog("DEBUG: Reset circuit breaker for new query\n")
		}
	}

	// Process images if present
	images, processedQuery, err := ch.processImagesInQuery(userQuery)
	if err != nil {
		ch.agent.publishEvent(events.EventTypeError, events.ErrorEvent("Image processing failed", err))
		return "", err
	}

	// Add user message with optional multimodal images
	ch.queryStartIndex = len(ch.agent.messages)
	userMessage := api.Message{
		Role:    "user",
		Content: ch.prepareUserInputForModel(processedQuery),
		Images:  images,
	}
	ch.agent.messages = append(ch.agent.messages, userMessage)

	// Main conversation loop
	completed := false
	for ch.agent.currentIteration = 0; ch.agent.maxIterations == 0 || ch.agent.currentIteration < ch.agent.maxIterations; ch.agent.currentIteration++ {
		if ch.agent.maxIterations > 0 {
			ch.agent.debugLog("[~] Iteration %d/%d - Messages: %d\n", ch.agent.currentIteration, ch.agent.maxIterations, len(ch.agent.messages))
		} else {
			ch.agent.debugLog("[~] Iteration %d/unlimited - Messages: %d\n", ch.agent.currentIteration, len(ch.agent.messages))
		}

		// Record turn data if trace session is enabled
		if ch.traceSession != nil {
			ch.recordTurnStart(userQuery, processedQuery)
		}

		// Check for explicit interrupts
		if ch.checkForInterrupt() {
			interruptResponse := ch.agent.HandleInterrupt()

			switch interruptResponse {
			case "STOP":
				ch.agent.debugLog("[STOP] Conversation stopped by user\n")
				ch.agent.lastRunTerminationReason = RunTerminationInterrupted
				break
			case "CONTINUE":
				ch.agent.debugLog("[~] Continuing without changes\n")
				continue
			default:
				ch.agent.debugLog("[STOP] Conversation interrupted\n")
				break
			}

			// If we reach here, we're breaking out of the loop
			break
		}

		// Track latest user message for this iteration
		if userMsg, ok := ch.lastUserMessage(); ok {
			ch.pendingUserMessage = userMsg
		} else {
			ch.pendingUserMessage = ""
		}

		// Send message to LLM
		if ch.agent.debug {
			ch.agent.debugLog("DEBUG: ConversationHandler sending message (iteration %d) at %s\n", ch.agent.currentIteration, time.Now().Format("15:04:05.000"))
		}
		response, err := ch.sendMessage()
		if err != nil {
			// If this iteration was interrupted, continue the loop based on
			// interrupt handling instead of treating it as an API failure.
			if ch.checkForInterrupt() {
				interruptResponse := ch.agent.HandleInterrupt()
				switch interruptResponse {
				case "STOP":
					ch.agent.debugLog("[STOP] Conversation stopped by user\n")
					ch.agent.lastRunTerminationReason = RunTerminationInterrupted
					break
				case "CONTINUE":
					ch.agent.debugLog("[~] Continuing without changes\n")
					continue
				default:
					ch.agent.debugLog("[STOP] Conversation interrupted\n")
					break
				}
				break
			}

			if ch.agent.debug {
				ch.agent.debugLog("DEBUG: ConversationHandler got error at %s: %v\n", time.Now().Format("15:04:05.000"), err)
			}

			// Ensure any buffered streaming output is flushed before showing the error
			if ch.agent.flushCallback != nil {
				ch.agent.flushCallback()
			}

			// Display user-friendly error message based on error type
			ch.displayUserFriendlyError(err)

			return ch.errorHandler.HandleAPIFailure(err, ch.agent.messages)
		}
		if ch.agent.debug {
			ch.agent.debugLog("DEBUG: ConversationHandler received response at %s\n", time.Now().Format("15:04:05.000"))
		}

		// Update activity time on successful response
		ch.lastActivityTime = time.Now()

		// Process response
		if shouldStop := ch.processResponse(response); shouldStop {
			ch.agent.debugLog("[OK] Conversation complete\n")
			completed = true
			ch.agent.lastRunTerminationReason = RunTerminationCompleted
			break
		} else {
			ch.agent.debugLog("-> Continuing conversation...\n")
		}
	}

	ch.agent.debugLog("[GO] Exited conversation loop - Iteration: %d, Messages: %d\n", ch.agent.currentIteration, len(ch.agent.messages))
	if !completed && ch.agent.maxIterations > 0 && ch.agent.currentIteration >= ch.agent.maxIterations {
		ch.agent.lastRunTerminationReason = RunTerminationMaxIterations
		ch.agent.PrintLineAsync(fmt.Sprintf("[WARN] Reached maximum iterations (%d) before the task completed.", ch.agent.maxIterations))
	}

	// Finalize conversation
	return ch.finalizeConversation()
}

// checkForInterrupt checks for explicit user interrupts or injected input.
func (ch *ConversationHandler) checkForInterrupt() bool {
	// Check for context cancellation (new interrupt system) with blocking select
	select {
	case <-ch.agent.interruptCtx.Done():
		ch.agent.debugLog("[STOP] Context cancelled, interrupt requested\n")
		return true
	case input := <-ch.agent.GetInputInjectionContext():
		// Input injection detected - inject as new user message
		ch.agent.debugLog("[>] Input injection detected: %s\n", input)
		ch.agent.messages = append(ch.agent.messages, api.Message{
			Role:    "user",
			Content: ch.prepareUserInputForModel(input),
		})
		return false // Continue processing with new input
	default:
		return false
	}
}

// lastUserMessage gets the last user message from the conversation
func (ch *ConversationHandler) lastUserMessage() (string, bool) {
	for i := len(ch.agent.messages) - 1; i >= 0; i-- {
		if ch.agent.messages[i].Role == "user" {
			return ch.agent.messages[i].Content, true
		}
	}
	return "", false
}

// recordTurnStart creates and records a turn record at the start of each iteration
func (ch *ConversationHandler) recordTurnStart(originalQuery, processedQuery string) {
	// Type assert to trace session with GetRunID and RecordTurn methods
	type traceSessionInterface interface {
		GetRunID() string
		RecordTurn(record trace.TurnRecord) error
	}

	traceSession, ok := ch.traceSession.(traceSessionInterface)
	if !ok {
		// Not a valid trace session, skip recording
		ch.agent.debugLog("DEBUG: traceSession is not a valid trace session, skipping turn recording\n")
		return
	}

	// Create turn record with initial data
	ch.currentTurnRecord = &trace.TurnRecord{
		RunID:              traceSession.GetRunID(),
		TurnIndex:          ch.agent.currentIteration,
		SystemPrompt:       ch.agent.systemPrompt,
		UserPrompt:         processedQuery,    // What model sees (after truncation)
		UserPromptOriginal: originalQuery,     // What user typed (before truncation)
		MessagesSent:       ch.agent.messages, // Messages array as sent to provider
		RawResponse:        "",                // Will be set later
		ParsedToolCalls:    []api.ToolCall{},  // Will be set later
		ParserErrors:       []string{},        // Will be set later
		FallbackUsed:       false,             // Will be set later
		FallbackOutput:     "",                // Will be set later
		MachineLabels:      []string{},        // Empty initially
		Timestamp:          time.Now().Format(time.RFC3339),
	}

	// Record the turn
	if err := traceSession.RecordTurn(*ch.currentTurnRecord); err != nil {
		ch.agent.debugLog("DEBUG: Failed to record turn: %v\n", err)
	}
}

// processResponse handles the LLM response including tool execution
func (ch *ConversationHandler) processResponse(resp *api.ChatResponse) bool {
	turn := TurnEvaluation{
		Iteration: ch.agent.currentIteration,
		Timestamp: time.Now(),
		UserInput: ch.pendingUserMessage,
	}
	turn.TokenUsage = TokenUsage{
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		TotalTokens:      resp.Usage.TotalTokens,
		EstimatedCost:    resp.Usage.EstimatedCost,
	}

	// Collect parser errors for turn recording
	var parserErrors []string
	fallbackUsed := false
	fallbackOutput := ""

	if len(resp.Choices) == 0 {
		ch.agent.debugLog("[WARN] Response had no choices; asking model to continue\n")
		ch.handleIncompleteResponse()
		turn.GuardrailTrigger = "empty choices response"

		// Update turn record with empty response
		ch.updateTurnRecord("", nil, parserErrors, fallbackUsed, fallbackOutput)
		return ch.finalizeTurn(turn, false)
	}

	choice := resp.Choices[0]

	// Determine the content to record and validate. Prefer the streaming buffer if streaming was used
	contentUsed := choice.Message.Content
	if ch.agent.streamingEnabled && len(ch.agent.streamingBuffer.String()) > 0 {
		// Use the fully streamed content if available
		contentUsed = ch.agent.streamingBuffer.String()
	}

	if ch.agent.debug {
		// Debug: Check for ANSI codes in content being added to conversation
		if strings.Contains(contentUsed, "\x1b[") || strings.Contains(contentUsed, "\x1b(") {
			ch.agent.debugLog("[!!] ANSI DETECTED in conversation content: %q\n", contentUsed)
		}
	}
	// Sanitize content to remove ANSI codes that might have leaked in
	contentUsed = ch.sanitizeContent(contentUsed)

	turn.AssistantContent = contentUsed
	turn.FinishReason = choice.FinishReason

	reasoningContent := choice.Message.ReasoningContent
	if ch.agent.streamingEnabled && len(ch.agent.reasoningBuffer.String()) > 0 {
		reasoningContent = ch.agent.reasoningBuffer.String()
	}
	turn.ReasoningSnippet = abbreviate(reasoningContent, 280)

	// Ensure tool calls always carry IDs so downstream sanitization can keep results
	if len(choice.Message.ToolCalls) > 0 {
		for i := range choice.Message.ToolCalls {
			// Some models (e.g., Harmony/GPT-OSS) append "<|channel|>xxx" suffix to tool names
			// Strip it to get the actual tool name
			choice.Message.ToolCalls[i].Function.Name = strings.Split(choice.Message.ToolCalls[i].Function.Name, "<|channel|>")[0]

			if choice.Message.ToolCalls[i].ID == "" {
				choice.Message.ToolCalls[i].ID = ch.toolExecutor.GenerateToolCallID(choice.Message.ToolCalls[i].Function.Name)
				ch.agent.debugLog("[tool] Generated missing tool call ID: %s for tool: %s\n",
					choice.Message.ToolCalls[i].ID, choice.Message.ToolCalls[i].Function.Name)
			}
		}

		// Some providers stream tool_calls multiple times per chunk. Deduplicate by ID AND arguments.
		deduped := make([]api.ToolCall, 0, len(choice.Message.ToolCalls))
		seenIDs := make(map[string]struct{}, len(choice.Message.ToolCalls))
		seenArgs := make(map[string]struct{}, len(choice.Message.ToolCalls))
		for _, tc := range choice.Message.ToolCalls {
			isDup := false
			if tc.ID != "" {
				if _, exists := seenIDs[tc.ID]; exists {
					isDup = true
				} else {
					seenIDs[tc.ID] = struct{}{}
				}
			}

			argsKey := fmt.Sprintf("%s|%s", tc.Function.Name, strings.TrimSpace(tc.Function.Arguments))
			if !isDup {
				if _, exists := seenArgs[argsKey]; exists {
					isDup = true
				} else {
					seenArgs[argsKey] = struct{}{}
				}
			}

			if isDup {
				sampleArgs := argsKey
				if len(sampleArgs) > 120 {
					sampleArgs = sampleArgs[:117] + "..."
				}
				ch.agent.debugLog("[recycle] Skipping duplicate tool call id=%s name=%s args=%s\n",
					tc.ID, tc.Function.Name, sampleArgs)
				continue
			}

			deduped = append(deduped, tc)
		}
		if len(deduped) != len(choice.Message.ToolCalls) {
			ch.agent.debugLog("[recycle] Deduplicated tool calls: kept %d of %d\n", len(deduped), len(choice.Message.ToolCalls))
		}
		choice.Message.ToolCalls = deduped

		normalizedToolCalls, malformedToolCalls := normalizeToolCallsForExecution(choice.Message.ToolCalls)
		choice.Message.ToolCalls = normalizedToolCalls
		if len(malformedToolCalls) > 0 {
			names := make([]string, 0, len(malformedToolCalls))
			for _, tc := range malformedToolCalls {
				names = append(names, tc.Function.Name)
			}
			ch.agent.debugLog("[WARN] Received %d malformed structured tool call(s): %s\n", len(malformedToolCalls), strings.Join(names, ", "))
			ch.enqueueTransientMessage(api.Message{
				Role: "user",
				Content: "Your previous tool call arguments were incomplete or invalid JSON. " +
					"Re-emit the intended tool call(s) with complete valid JSON arguments only.",
			})
			turn.GuardrailTrigger = "malformed structured tool call"
			choice.Message.ToolCalls = nil

			// Track parser errors for turn recording
			parserErrors = append(parserErrors, fmt.Sprintf("malformed tool calls: %s", strings.Join(names, ", ")))
		}

		for _, tc := range choice.Message.ToolCalls {
			if strings.Split(tc.Function.Name, "<|channel|>")[0] == "analyze_image_content" {
				ch.ocrEnforcementAttempts = 0
				break
			}
		}
	}

	turn.ToolCalls = append(turn.ToolCalls, choice.Message.ToolCalls...)

	// Preserve tool calls (with generated IDs if needed) so tool outputs remain linked
	var toolCalls []api.ToolCall
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls = make([]api.ToolCall, len(choice.Message.ToolCalls))
		copy(toolCalls, choice.Message.ToolCalls)
	}

	// Add to conversation history
	assistantMsg := api.Message{
		Role:             "assistant",
		Content:          contentUsed,
		ReasoningContent: reasoningContent,
		ToolCalls:        toolCalls,
	}

	// Only append if we have a valid message (not a duplicate)
	if assistantMsg.Role != "" {
		ch.agent.messages = append(ch.agent.messages, assistantMsg)
	}

	// Token tracking is handled by the agent struct fields

	// Execute tools if present
	if len(choice.Message.ToolCalls) > 0 {
		// Log raw tool calls as received from the model for debugging
		for _, tc := range choice.Message.ToolCalls {
			ch.agent.LogToolCall(tc, "received")
		}
		ch.agent.debugLog("[tool] Executing %d tool calls\n", len(choice.Message.ToolCalls))

		// Flush any buffered streaming content before tool execution
		// This ensures narrative text appears before tool calls for better flow
		if ch.agent.flushCallback != nil {
			ch.agent.flushCallback()
		}

		ch.displayIntermediateResponse(contentUsed)
		toolResults := ch.toolExecutor.ExecuteTools(choice.Message.ToolCalls)

		// Add tool results immediately after the assistant message with tool calls
		ch.agent.messages = append(ch.agent.messages, toolResults...)
		ch.agent.debugLog("[ok] Added %d tool results to conversation\n", len(toolResults))

		// The model made concrete progress by executing tools, so reset
		// the tentative rejection counter — prior rejections are now stale.
		ch.tentativeRejectionCount = 0

		// Additional debugging for DeepSeek tool call format
		if strings.EqualFold(ch.agent.GetProvider(), "deepseek") {
			ch.agent.debugLog("[search] DeepSeek conversation flow check:\n")
			for i, msg := range ch.agent.messages {
				if msg.Role == "assistant" && len(msg.ToolCalls) > 0 {
					ch.agent.debugLog("  [%d] Assistant with %d tool_calls\n", i, len(msg.ToolCalls))
				} else if msg.Role == "tool" {
					ch.agent.debugLog("  [%d] Tool response for tool_call_id: %s\n", i, msg.ToolCallId)
				}
			}
		}

		turn.ToolResults = append(turn.ToolResults, toolResults...)

		// Update turn record with response data and tool calls
		ch.updateTurnRecord(contentUsed, choice.Message.ToolCalls, parserErrors, fallbackUsed, fallbackOutput)
		return ch.finalizeTurn(turn, false) // Continue conversation
	}

	// If no tool_calls came back but the content suggests attempted tool usage,
	// try to parse and execute them using fallback parser
	if !ch.responseValidator.ValidateToolCalls(contentUsed) {
		return ch.handleMalformedToolCalls(contentUsed, turn, parserErrors)
	}

	// Handle finish reason FIRST to respect model's intent
	// This must happen BEFORE blank/repetitive content checks to avoid forcing continuation
	// when the model has explicitly signaled completion
	ch.agent.debugLog("[search] Finish reason received: '%s' (len=%d)\n", choice.FinishReason, len(choice.FinishReason))
	contentPreview := contentUsed
	if len(contentPreview) > 200 {
		contentPreview = contentPreview[:200] + "..."
	}
	ch.agent.debugLog("[search] Content length: %d, preview: %q\n", len(contentUsed), contentPreview)

	if choice.FinishReason == "" {
		// No finish reason provided - model expects to continue working
		// First check if this is a blank iteration - if so, fall through to blank iteration handling
		// Blank responses should not be treated as "complete" just because they're not "incomplete"
		if ch.isBlankIteration(contentUsed, choice.Message.ToolCalls) {
			ch.agent.debugLog("[search] Blank response with no finish reason - falling through to blank iteration handling\n")
			// Fall through to blank iteration handling below
		} else {
			// Not a blank iteration - check if truly incomplete or just a streaming artifact
			// Some providers don't send finish_reason in every chunk
			// Only continue if the response actually appears incomplete
			isIncomplete := ch.responseValidator.IsIncomplete(contentUsed)
			ch.agent.debugLog("[search] IsIncomplete() result: %v\n", isIncomplete)

			if !isIncomplete {
				// Response looks complete despite no finish_reason - accept it
				if handled, stop := ch.handleOCRCompletionGate(&turn); handled {
					// Update turn record before returning
					ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
					return ch.finalizeTurn(turn, stop)
				}
				ch.agent.debugLog("[OK] No finish_reason but response appears complete - accepting\n")
				ch.displayFinalResponse(contentUsed)
				// Update turn record before returning
				ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
				return ch.finalizeTurn(turn, true)
			}
			ch.agent.debugLog("[~] No finish reason and response appears incomplete - asking model to continue\n")
			// Update turn record before returning
			ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
			return ch.finalizeTurn(turn, false) // Continue conversation
		}
	}

	// Check if model explicitly signaled completion - respect it BEFORE other checks
	if shouldStop, stopReason := ch.handleFinishReason(choice.FinishReason, contentUsed); shouldStop {
		turn.GuardrailTrigger = stopReason
		if stopReason == "completion" || stopReason == "implicit completion" {
			turn.CompletionReached = true
		}
		if handled, stop := ch.handleOCRCompletionGate(&turn); handled {
			// Update turn record before returning
			ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
			return ch.finalizeTurn(turn, stop)
		}
		// Update turn record before returning
		ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
		return ch.finalizeTurn(turn, shouldStop)
	}

	// Only check for blank/repetitive content if finish_reason indicates continuation
	// (e.g., "tool_calls", "length", or other non-stop reasons)
	// Check for blank iteration (no content and no tool calls)
	isBlankIteration := ch.isBlankIteration(contentUsed, choice.Message.ToolCalls)

	// Check for repetitive content loop
	isRepetitiveContent := ch.isRepetitiveContent(contentUsed)

	if isBlankIteration || isRepetitiveContent {
		ch.consecutiveBlankIterations++
		if isBlankIteration {
			ch.agent.debugLog("[WARN] Blank iteration detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		} else {
			ch.agent.debugLog("[WARN] Repetitive content detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		}

		if ch.consecutiveBlankIterations == 1 {
			// First blank/repetitive iteration - provide explicit, actionable reminder
			ch.agent.debugLog("[bell] Sending reminder about next action\n")
			var reminderContent string
			if isRepetitiveContent {
				reminderContent = "You appear to be stuck in a repetitive loop. Please break out of this pattern and either:\n" +
					"1. If you are finished, provide a final summary or result\n" +
					"2. If not finished, take a concrete action (use tools) or provide a specific result\n" +
					"3. Avoid repeating the same phrases and move forward with the actual task.\n" +
					"- Focus on making actual changes or providing specific findings."
			} else {
				reminderContent = "You provided no content. If you are finished, provide a final summary or result. If not finished, continue now with your next concrete action/output.\n" +
					"- If you intend to use tools, emit valid tool_calls with proper JSON arguments.\n" +
					"- Otherwise, proceed with the actual result (not a plan)."
			}
			ch.enqueueTransientMessage(api.Message{
				Role:    "user",
				Content: reminderContent,
			})

			// Guidance suppressed for now; guardrail already re-enqueues reminders
			turn.GuardrailTrigger = "blank iteration reminder"
			// Update turn record before returning
			ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
			return ch.finalizeTurn(turn, false) // Continue conversation to get a proper response
		} else if ch.consecutiveBlankIterations >= 2 {
			// Two consecutive blank iterations - error out
			ch.agent.debugLog("[FAIL] Too many consecutive blank iterations, stopping with error\n")
			errorMessage := "Error: The agent provided two consecutive blank responses and appears to be stuck. Please try rephrasing your request or break it into smaller tasks."
			ch.displayFinalResponse(errorMessage)
			// Update turn record before returning
			ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
			return ch.finalizeTurn(turn, true) // Stop with error
		}
	} else {
		// Reset blank iteration counter on any non-blank response
		ch.consecutiveBlankIterations = 0
	}

	// Final check for incomplete responses (only reached if not stopped and not blank/repetitive)
	if ch.responseValidator.IsIncomplete(contentUsed) {
		ch.agent.debugLog("[WARN] Response appears incomplete, asking model to continue\n")
		ch.handleIncompleteResponse()
		turn.GuardrailTrigger = "incomplete response reminder"
		// Update turn record before returning
		ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
		return ch.finalizeTurn(turn, false) // Continue conversation to get a complete response
	}

	// Response doesn't look incomplete.
	// Respect the model's judgment - continue conversation without reminders
	ch.agent.debugLog("[...] Model response continuing conversation\n")
	// Update turn record before returning
	ch.updateTurnRecord(contentUsed, nil, parserErrors, fallbackUsed, fallbackOutput)
	return ch.finalizeTurn(turn, false)
}

func normalizeToolCallsForExecution(toolCalls []api.ToolCall) ([]api.ToolCall, []api.ToolCall) {
	if len(toolCalls) == 0 {
		return nil, nil
	}

	normalized := make([]api.ToolCall, 0, len(toolCalls))
	malformed := make([]api.ToolCall, 0)

	for _, tc := range toolCalls {
		args, repaired, err := parseToolArgumentsWithRepair(tc.Function.Arguments)
		if err != nil {
			malformed = append(malformed, tc)
			continue
		}
		if repaired {
			if encoded, marshalErr := json.Marshal(args); marshalErr == nil {
				tc.Function.Arguments = string(encoded)
			}
		}
		// Normalize Type field to "function" (required by OpenAI-compatible API schema)
		// Handles cases where Type is empty, missing, or has an invalid value
		if tc.Type != "function" {
			tc.Type = "function"
		}
		normalized = append(normalized, tc)
	}

	return normalized, malformed
}

// finalizeConversation finalizes the conversation and returns the last assistant message
func (ch *ConversationHandler) finalizeConversation() (string, error) {
	hadTrackedChanges := ch.agent.IsChangeTrackingEnabled() && ch.agent.GetChangeCount() > 0

	// Commit tracked changes
	if hadTrackedChanges {
		if err := ch.agent.CommitChanges("Task completed"); err != nil {
			ch.agent.debugLog("Warning: Failed to commit changes: %v\n", err)
		}
	}

	if hadTrackedChanges {
		if err := ch.runSelfReviewGate(); err != nil {
			ch.agent.publishEvent(events.EventTypeError, events.ErrorEvent("Self-review gate failed", err))
			return "", err
		}
	}

	// Get the final response content
	var finalContent string
	for i := len(ch.agent.messages) - 1; i >= 0; i-- {
		if ch.agent.messages[i].Role == "assistant" {
			finalContent = ch.agent.messages[i].Content
			break
		}
	}

	ch.maybeCheckpointCompletedTurn()

	// Publish query completed event
	duration := time.Since(ch.conversationStartTime)
	completedEvent := events.QueryCompletedEvent(
		ch.pendingUserMessage,
		finalContent,
		ch.agent.GetTotalTokens(),
		ch.agent.GetTotalCost(),
		duration,
	)
	if reason := ch.agent.GetLastRunTerminationReason(); reason != "" {
		completedEvent["status"] = reason
	}
	ch.agent.publishEvent(events.EventTypeQueryCompleted, completedEvent)

	// If streaming was enabled and content was streamed, return empty string
	// to avoid duplicate display in the console
	if ch.agent.streamingEnabled && len(ch.agent.streamingBuffer.String()) > 0 {
		return "", nil
	}

	// Get last assistant message
	for i := len(ch.agent.messages) - 1; i >= 0; i-- {
		if ch.agent.messages[i].Role == "assistant" {
			return ch.agent.messages[i].Content, nil
		}
	}

	return "", fmt.Errorf("no assistant response found")
}

func (ch *ConversationHandler) maybeCheckpointCompletedTurn() {
	if ch == nil || ch.agent == nil {
		return
	}
	if ch.queryStartIndex < 0 || ch.queryStartIndex >= len(ch.agent.messages) {
		return
	}

	reason := ch.agent.GetLastRunTerminationReason()
	if reason != RunTerminationCompleted && reason != RunTerminationMaxIterations {
		return
	}

	endIndex := len(ch.agent.messages) - 1
	hasAssistant := false
	for i := ch.queryStartIndex; i <= endIndex; i++ {
		if ch.agent.messages[i].Role == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return
	}

	ch.agent.RecordTurnCheckpointAsync(ch.queryStartIndex, endIndex)
}
