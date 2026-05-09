package agent

import (
	"fmt"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
	"github.com/sprout-foundry/sprout/pkg/trace"
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
	agent.initSubManagers()
	now := time.Now()
	ch := &ConversationHandler{
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

	// Set up callback to re-prepare messages after compaction
	ch.apiClient.prepareMessagesCallback = func(tools []api.Tool) []api.Message {
		return ch.prepareMessages(tools)
	}

	return ch
}

// ProcessQuery handles a user query through the complete conversation flow
func (ch *ConversationHandler) ProcessQuery(userQuery string) (string, error) {
	if ch.agent.debug {
		ch.agent.debugLog("DEBUG: ProcessQuery called with: %s\n", userQuery)
	}
	ch.agent.state.SetLastRunTerminationReason("")

	// Publish query started event
	ch.agent.publishEvent(events.EventTypeQueryStarted, events.QueryStartedEvent(userQuery, ch.agent.GetProvider(), ch.agent.GetModel()))

	// Initialize conversation tracking
	ch.conversationStartTime = time.Now()
	ch.lastActivityTime = time.Now()

	// Reset streaming buffer for new query
	ch.agent.output.GetStreamingBuffer().Reset()
	ch.agent.output.GetReasoningBuffer().Reset()

	// Enable change tracking
	ch.agent.EnableChangeTracking(userQuery)

	// Reset circuit breaker history for a fresh query to avoid carrying over
	// repetitive-tool counts from previous requests.
	if ch.agent.state.GetCircuitBreaker() != nil {
		ch.agent.state.GetCircuitBreaker().mu.Lock()
		// Clear entries instead of replacing map to avoid memory churn and reduce lock hold time
		for key := range ch.agent.state.GetCircuitBreaker().Actions {
			delete(ch.agent.state.GetCircuitBreaker().Actions, key)
		}
		ch.agent.state.GetCircuitBreaker().mu.Unlock()
		if ch.agent.debug {
			ch.agent.debugLog("DEBUG: Reset circuit breaker for new query\n")
		}
	}

	// Process images if present
	images, processedQuery, err := ch.processImagesInQuery(userQuery)
	if err != nil {
		ch.agent.publishEvent(events.EventTypeError, events.ErrorEvent("Image processing failed", err))
		return "", fmt.Errorf("failed to process images in query: %w", err)
	}

	// Add user message with optional multimodal images
	ch.queryStartIndex = len(ch.agent.state.GetMessages())
	userMessage := api.Message{
		Role:    "user",
		Content: ch.prepareUserInputForModel(processedQuery),
		Images:  images,
	}
	ch.agent.state.AddMessage(userMessage)

	// Main conversation loop
	completed := false
	finalIter := 0
	for iter := 0; ch.agent.maxIterations == 0 || iter < ch.agent.maxIterations; iter++ {
		finalIter = iter
		ch.agent.state.SetCurrentIteration(iter)

		if ch.agent.maxIterations > 0 {
			ch.agent.debugLog("[~] Iteration %d/%d - Messages: %d\n", iter, ch.agent.maxIterations, len(ch.agent.state.GetMessages()))
		} else {
			ch.agent.debugLog("[~] Iteration %d/unlimited - Messages: %d\n", iter, len(ch.agent.state.GetMessages()))
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
				ch.agent.state.SetLastRunTerminationReason(RunTerminationInterrupted)
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
			ch.agent.debugLog("DEBUG: ConversationHandler sending message (iteration %d) at %s\n", ch.agent.state.GetCurrentIteration(), time.Now().Format("15:04:05.000"))
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
					ch.agent.state.SetLastRunTerminationReason(RunTerminationInterrupted)
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
			if ch.agent.output.GetFlushCallback() != nil {
				ch.agent.output.GetFlushCallback()()
			}

			// Display user-friendly error message based on error type
			ch.displayUserFriendlyError(err)

			return ch.errorHandler.HandleAPIFailure(err, ch.agent.state.GetMessages())
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
			ch.agent.state.SetLastRunTerminationReason(RunTerminationCompleted)
			break
		} else {
			ch.agent.debugLog("-> Continuing conversation...\n")
		}
	}

	ch.agent.debugLog("[GO] Exited conversation loop - Iteration: %d, Messages: %d\n", ch.agent.state.GetCurrentIteration(), len(ch.agent.state.GetMessages()))
	if !completed && ch.agent.maxIterations > 0 && (finalIter+1) >= ch.agent.maxIterations {
		ch.agent.state.SetCurrentIteration(finalIter + 1)
		ch.agent.state.SetLastRunTerminationReason(RunTerminationMaxIterations)
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
		ch.agent.state.AddMessage(api.Message{
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
	messages := ch.agent.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			return messages[i].Content, true
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
		TurnIndex:          ch.agent.state.GetCurrentIteration(),
		SystemPrompt:       ch.agent.systemPrompt,
		UserPrompt:         processedQuery,    // What model sees (after truncation)
		UserPromptOriginal: originalQuery,     // What user typed (before truncation)
		MessagesSent:       ch.agent.state.GetMessages(), // Messages array as sent to provider
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

// processResponse handles the LLM response including tool execution.
// Delegates to focused sub-methods in conversation_handler_response.go.
func (ch *ConversationHandler) processResponse(resp *api.ChatResponse) bool {
	var ctx responseContext

	// Extract content from response; handle empty choices
	if ch.extractResponseContent(resp, &ctx) {
		ch.agent.debugLog("[WARN] Response had no choices; asking model to continue\n")
		ch.handleIncompleteResponse()
		ctx.turn.GuardrailTrigger = "empty choices response"
		ch.updateTurnRecord("", nil, ctx.parserErrors, ctx.fallbackUsed, ctx.fallbackOutput)
		return ch.finalizeTurn(ctx.turn, false)
	}

	choice := resp.Choices[0]

	ch.agent.debugLog("[search] Finish reason received: '%s' (len=%d)\n", choice.FinishReason, len(choice.FinishReason))
	contentPreview := ctx.contentUsed
	if len(contentPreview) > 200 {
		contentPreview = contentPreview[:200] + "..."
	}
	ch.agent.debugLog("[search] Content length: %d, preview: %q\n", len(ctx.contentUsed), contentPreview)

	ch.prepareToolCalls(choice, &ctx)
	ch.recordAssistantMessage(&ctx)

	if len(ctx.toolCalls) > 0 {
		decision := ch.handleToolCalls(&ctx)
		return ch.finalizeTurn(ctx.turn, decision == responseDecideStop)
	}

	return ch.handleNoToolContent(&ctx, choice)
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
			return "", fmt.Errorf("failed self-review gate: %w", err)
		}
	}

	// Get the final response content
	var finalContent string
	messages := ch.agent.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			finalContent = messages[i].Content
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
	if ch.agent.output.IsStreamingEnabled() && len(ch.agent.output.GetStreamingBuffer().String()) > 0 {
		return "", nil
	}

	// Get last assistant message
	messages = ch.agent.state.GetMessages()
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "assistant" {
			return messages[i].Content, nil
		}
	}

	return "", fmt.Errorf("no assistant response found in %d messages", len(messages))
}

func (ch *ConversationHandler) maybeCheckpointCompletedTurn() {
	if ch == nil || ch.agent == nil {
		return
	}
	messages := ch.agent.state.GetMessages()
	if ch.queryStartIndex < 0 || ch.queryStartIndex >= len(messages) {
		return
	}

	reason := ch.agent.GetLastRunTerminationReason()
	if reason != RunTerminationCompleted && reason != RunTerminationMaxIterations {
		return
	}

	endIndex := len(messages) - 1
	hasAssistant := false
	for i := ch.queryStartIndex; i <= endIndex; i++ {
		if messages[i].Role == "assistant" {
			hasAssistant = true
			break
		}
	}
	if !hasAssistant {
		return
	}

	ch.agent.RecordTurnCheckpointAsync(ch.queryStartIndex, endIndex)
}
