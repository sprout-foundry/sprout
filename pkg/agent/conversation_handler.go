package agent

import (
	"fmt"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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
	missingCompletionReminders int
	conversationStartTime      time.Time
	lastActivityTime           time.Time
	timeoutDuration            time.Duration
	transientMessages          []api.Message
	pendingUserMessage         string
	turnHistory                []TurnEvaluation
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
		timeoutDuration:       7 * time.Minute, // 7-minute timeout
	}
}

// ProcessQuery handles a user query through the complete conversation flow
func (ch *ConversationHandler) ProcessQuery(userQuery string) (string, error) {
	if ch.agent.debug {
		ch.agent.debugLog("DEBUG: ProcessQuery called with: %s\n", userQuery)
	}

	// Initialize timeout tracking
	ch.conversationStartTime = time.Now()
	ch.lastActivityTime = time.Now()

	// Reset streaming buffer for new query
	ch.agent.streamingBuffer.Reset()

	// Enable change tracking
	ch.agent.EnableChangeTracking(userQuery)

	// Enable escape monitoring
	ch.agent.EnableEscMonitoring()
	defer ch.agent.DisableEscMonitoring()

	// Reset circuit breaker history for a fresh query to avoid carrying over
	// repetitive-tool counts from previous requests.
	if ch.agent.circuitBreaker != nil {
		ch.agent.circuitBreaker.Actions = make(map[string]*CircuitBreakerAction)
		if ch.agent.debug {
			ch.agent.debugLog("DEBUG: Reset circuit breaker for new query\n")
		}
	}

	// Process images if present
	processedQuery, err := ch.processImagesInQuery(userQuery)
	if err != nil {
		return "", err
	}



	// Add user message
	userMessage := api.Message{
		Role:    "user",
		Content: processedQuery,
	}
	ch.agent.messages = append(ch.agent.messages, userMessage)

	// Main conversation loop
	for ch.agent.currentIteration = 0; ch.agent.currentIteration < ch.agent.maxIterations; ch.agent.currentIteration++ {
		ch.agent.debugLog("🔄 Iteration %d/%d - Messages: %d\n", ch.agent.currentIteration, ch.agent.maxIterations, len(ch.agent.messages))

		// Check for interrupts with enhanced pause/resume handling
		if ch.checkForInterrupt() {
			interruptResponse := ch.agent.HandleInterrupt()

			switch interruptResponse {
			case "STOP":
				ch.agent.debugLog("⏹️ Conversation stopped by user\n")
			case "CONTINUE_WITH_CLARIFICATION":
				ch.agent.debugLog("🔄 Continuing with user clarification\n")
				// Reset interrupt context and continue
				ch.agent.ClearInterrupt()
				continue
			case "CONTINUE":
				ch.agent.debugLog("🔄 Continuing without changes\n")
				// Reset interrupt context and continue
				ch.agent.ClearInterrupt()
				continue
			default:
				ch.agent.debugLog("⏹️ Conversation interrupted\n")
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
			ch.agent.debugLog("✅ Conversation complete\n")
			break
		} else {
			ch.agent.debugLog("➡️ Continuing conversation...\n")
		}
	}

	ch.agent.debugLog("🏁 Exited conversation loop - Iteration: %d, Messages: %d\n", ch.agent.currentIteration, len(ch.agent.messages))

	// Finalize conversation
	return ch.finalizeConversation()
}

// checkForInterrupt checks for user interrupts or timeouts
func (ch *ConversationHandler) checkForInterrupt() bool {
	// Check for context cancellation (new interrupt system) with blocking select
	select {
	case <-ch.agent.interruptCtx.Done():
		ch.agent.debugLog("⏹️ Context cancelled, interrupt requested\n")
		return true
	case input := <-ch.agent.GetInputInjectionContext():
		// Input injection detected - inject as new user message
		ch.agent.debugLog("💬 Input injection detected: %s\n", input)
		ch.agent.messages = append(ch.agent.messages, api.Message{
			Role:    "user",
			Content: input,
		})
		return false // Continue processing with new input
	default:
		// Check for timeout (5 minutes of inactivity)
		if time.Since(ch.lastActivityTime) > ch.timeoutDuration {
			ch.agent.debugLog("⏰ Conversation timeout after %v of inactivity\n", ch.timeoutDuration)
			ch.agent.interruptCancel() // Cancel context to trigger interrupt
			return true
		}
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

	if len(resp.Choices) == 0 {
		return ch.finalizeTurn(turn, true)
	}

	choice := resp.Choices[0]

	// Determine the content to record and validate. Prefer the streaming buffer if streaming was used
	contentUsed := choice.Message.Content
	if ch.agent.streamingEnabled && len(ch.agent.streamingBuffer.String()) > 0 {
		// Use the fully streamed content if available
		contentUsed = ch.agent.streamingBuffer.String()
		if ch.agent.debug {
			// Debug: Check for ANSI codes in content being added to conversation
			if strings.Contains(contentUsed, "\x1b[") || strings.Contains(contentUsed, "\x1b(") {
				ch.agent.debugLog("🚨 ANSI DETECTED in conversation content: %q\n", contentUsed)
			}
		}
		// Sanitize content to remove ANSI codes that might have leaked in
		contentUsed = ch.sanitizeContent(contentUsed)
	}

	turn.AssistantContent = contentUsed
	turn.FinishReason = choice.FinishReason

	reasoningContent := choice.Message.ReasoningContent
	turn.ReasoningSnippet = abbreviate(reasoningContent, 280)

	// Ensure tool calls always carry IDs so downstream sanitization can keep results
	if len(choice.Message.ToolCalls) > 0 {
		for i := range choice.Message.ToolCalls {
			if choice.Message.ToolCalls[i].ID == "" {
				choice.Message.ToolCalls[i].ID = ch.toolExecutor.GenerateToolCallID(choice.Message.ToolCalls[i].Function.Name)
				ch.agent.debugLog("🔧 Generated missing tool call ID: %s for tool: %s\n",
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
				ch.agent.debugLog("♻️ Skipping duplicate tool call id=%s name=%s args=%s\n",
					tc.ID, tc.Function.Name, sampleArgs)
				continue
			}

			deduped = append(deduped, tc)
		}
		if len(deduped) != len(choice.Message.ToolCalls) {
			ch.agent.debugLog("♻️ Deduplicated tool calls: kept %d of %d\n", len(deduped), len(choice.Message.ToolCalls))
		}
		choice.Message.ToolCalls = deduped
	}

	turn.ToolCalls = append(turn.ToolCalls, choice.Message.ToolCalls...)

	// Add to conversation history
	assistantMsg := api.Message{
		Role:             "assistant",
		Content:          contentUsed,
		ReasoningContent: reasoningContent,
	}

	// Preserve tool calls (with generated IDs if needed) so tool outputs remain linked
	if len(choice.Message.ToolCalls) > 0 {
		toolCalls := make([]api.ToolCall, len(choice.Message.ToolCalls))
		copy(toolCalls, choice.Message.ToolCalls)
		assistantMsg.ToolCalls = toolCalls
	}

	ch.agent.messages = append(ch.agent.messages, assistantMsg)

	// Token tracking is handled by the agent struct fields

	// Execute tools if present
	if len(choice.Message.ToolCalls) > 0 {
		// Log raw tool calls as received from the model for debugging
		for _, tc := range choice.Message.ToolCalls {
			ch.agent.LogToolCall(tc, "received")
		}
		ch.agent.debugLog("🛠️ Executing %d tool calls\n", len(choice.Message.ToolCalls))

		// Flush any buffered streaming content before tool execution
		// This ensures narrative text appears before tool calls for better flow
		if ch.agent.flushCallback != nil {
			ch.agent.flushCallback()
		}

		ch.displayIntermediateResponse(contentUsed)
		toolResults := ch.toolExecutor.ExecuteTools(choice.Message.ToolCalls)

		// Add tool results immediately after the assistant message with tool calls
		ch.agent.messages = append(ch.agent.messages, toolResults...)
		ch.appendToolExecutionSummary(choice.Message.ToolCalls)
		ch.agent.debugLog("✔️ Added %d tool results to conversation\n", len(toolResults))
		ch.missingCompletionReminders = 0

		toolLogs := ch.flushToolLogsToOutput()
		turn.ToolLogs = append(turn.ToolLogs, toolLogs...)
		turn.ToolResults = append(turn.ToolResults, toolResults...)

		return ch.finalizeTurn(turn, false) // Continue conversation
	}

	// If no tool_calls came back but the content suggests attempted tool usage,
	// inject one-time guidance and try again.
	if !ch.responseValidator.ValidateToolCalls(contentUsed) {
		// Guidance disabled for now; rely on ValidateToolCalls alone
		return ch.finalizeTurn(turn, false) // Continue conversation to allow the model to issue proper tool_calls
	}

	// Check for blank iteration (no content and no tool calls)
	isBlankIteration := ch.isBlankIteration(contentUsed, choice.Message.ToolCalls)

	// Check for repetitive content loop
	isRepetitiveContent := ch.isRepetitiveContent(contentUsed)

	if isBlankIteration || isRepetitiveContent {
		ch.consecutiveBlankIterations++
		if isBlankIteration {
			ch.agent.debugLog("⚠️ Blank iteration detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		} else {
			ch.agent.debugLog("⚠️ Repetitive content detected (%d consecutive)\n", ch.consecutiveBlankIterations)
		}

		if ch.consecutiveBlankIterations == 1 {
			// First blank/repetitive iteration - provide explicit, actionable reminder
			ch.agent.debugLog("🔔 Sending reminder about task completion signal and next action\n")
			var reminderContent string
			if isRepetitiveContent {
				reminderContent = "You appear to be stuck in a repetitive loop. Please break out of this pattern and either:\n" +
					"1. If you are finished, reply exactly with [[TASK_COMPLETE]]\n" +
					"2. If not finished, take a concrete action (use tools) or provide a specific result\n" +
					"3. Avoid repeating the same phrases and move forward with the actual task.\n" +
					"- Focus on making actual changes or providing specific findings."
			} else {
				reminderContent = "You provided no content. If you are finished, reply exactly with [[TASK_COMPLETE]]. If not finished, continue now with your next concrete action/output.\n" +
					"- If you intend to use tools, emit valid tool_calls with proper JSON arguments.\n" +
					"- Otherwise, proceed with the actual result (not a plan)."
			}
			ch.enqueueTransientMessage(api.Message{
				Role:    "user",
				Content: reminderContent,
			})

			// Guidance suppressed for now; guardrail already re-enqueues reminders
			turn.GuardrailTrigger = "blank iteration reminder"
			return ch.finalizeTurn(turn, false) // Continue conversation to get a proper response
		} else if ch.consecutiveBlankIterations >= 2 {
			// Two consecutive blank iterations - error out
			ch.agent.debugLog("❌ Too many consecutive blank iterations, stopping with error\n")
			errorMessage := "Error: The agent provided two consecutive blank responses and appears to be stuck. Please try rephrasing your request or break it into smaller tasks."
			ch.displayFinalResponse(errorMessage)
			return ch.finalizeTurn(turn, true) // Stop with error
		}
	} else {
		// Reset blank iteration counter on any non-blank response
		ch.consecutiveBlankIterations = 0
	}

	// Check if the response indicates completion
	if ch.responseValidator.IsComplete(contentUsed) {
		ch.missingCompletionReminders = 0
		// Remove all variations of the completion signal from the content
		cleanContent := contentUsed
		completionSignals := []string{
			"[[TASK_COMPLETE]]",
			"[[TASKCOMPLETE]]",
			"[[TASK COMPLETE]]",
			"[[task_complete]]",
			"[[taskcomplete]]",
			"[[task complete]]",
		}

		for _, signal := range completionSignals {
			cleanContent = strings.ReplaceAll(cleanContent, signal, "")
		}
		cleanContent = strings.TrimSpace(cleanContent)

		// Update the last message to remove the signal
		if len(ch.agent.messages) > 0 {
			ch.agent.messages[len(ch.agent.messages)-1].Content = cleanContent
		}

		// Apply completion context summarization to prevent contamination in follow-up questions
		if ch.agent.completionSummarizer != nil && ch.agent.completionSummarizer.ShouldApplySummarization(ch.agent.messages) {
			ch.agent.messages = ch.agent.completionSummarizer.ApplyCompletionSummarization(ch.agent.messages)
		}

		// Display final response
		ch.displayFinalResponse(cleanContent)
		turn.CompletionReached = true
		return ch.finalizeTurn(turn, true) // Stop - response explicitly indicates completion
	}

	// Otherwise, decide whether this is a final (non-incomplete) response or we need more
	// If the response appears incomplete, ask the model to continue. Otherwise treat it as final.
	if choice.FinishReason == "tool_calls" {
		turn.GuardrailTrigger = "tool_calls finish"
		return ch.finalizeTurn(turn, false)
	}

	if ch.responseValidator.IsIncomplete(contentUsed) {
		ch.agent.debugLog("⚠️ Response appears incomplete, asking model to continue\n")
		ch.missingCompletionReminders = 0
		ch.handleIncompleteResponse()
		turn.GuardrailTrigger = "incomplete response reminder"
		return ch.finalizeTurn(turn, false) // Continue conversation to get a complete response
	}

	// No explicit completion signal and response doesn't look incomplete.
	// Decide based on provider/model policy whether implicit completion is acceptable.
	if ch.agent.shouldAllowImplicitCompletion() {
		ch.agent.debugLog("📝 Treating response as final (implicit completion allowed for provider/model)\n")
		ch.missingCompletionReminders = 0
		ch.displayFinalResponse(contentUsed)
		turn.CompletionReached = true
		return ch.finalizeTurn(turn, true)
	}

	ch.agent.debugLog("⏳ Waiting for explicit completion signal per provider/model policy\n")
	turn.GuardrailTrigger = "missing completion reminder"
	ch.handleMissingCompletionSignal()
	return ch.finalizeTurn(turn, false)
}

// finalizeConversation finalizes the conversation and returns the last assistant message
func (ch *ConversationHandler) finalizeConversation() (string, error) {
	// Commit tracked changes
	if ch.agent.IsChangeTrackingEnabled() && ch.agent.GetChangeCount() > 0 {
		if err := ch.agent.CommitChanges("Task completed"); err != nil {
			ch.agent.debugLog("Warning: Failed to commit changes: %v\n", err)
		}
	}

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