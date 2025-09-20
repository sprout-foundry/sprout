package agent

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ConversationHandler manages the high-level conversation flow
type ConversationHandler struct {
	agent                      *Agent
	apiClient                  *APIClient
	toolExecutor               *ToolExecutor
	responseValidator          *ResponseValidator
	errorHandler               *ErrorHandler
	consecutiveBlankIterations int
}

// NewConversationHandler creates a new conversation handler
func NewConversationHandler(agent *Agent) *ConversationHandler {
	return &ConversationHandler{
		agent:             agent,
		apiClient:         NewAPIClient(agent),
		toolExecutor:      NewToolExecutor(agent),
		responseValidator: NewResponseValidator(agent),
		errorHandler:      NewErrorHandler(agent),
	}
}

// ProcessQuery handles a user query through the complete conversation flow
func (ch *ConversationHandler) ProcessQuery(userQuery string) (string, error) {
	if ch.agent.debug {
		fmt.Printf("DEBUG: ProcessQuery called with: %s\n", userQuery)
	}

	// Reset streaming buffer for new query
	ch.agent.streamingBuffer.Reset()

	// Enable change tracking
	ch.agent.EnableChangeTracking(userQuery)

	// Enable escape monitoring
	ch.agent.EnableEscMonitoring()
	defer ch.agent.DisableEscMonitoring()

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

		// Check for interrupts
		if ch.checkForInterrupt() {
			ch.agent.debugLog("⏹️ Conversation interrupted\n")
			break
		}

		// Send message to LLM
		if ch.agent.debug {
			fmt.Printf("DEBUG: ConversationHandler sending message (iteration %d)\n", ch.agent.currentIteration)
		}
		response, err := ch.sendMessage()
		if err != nil {
			if ch.agent.debug {
				fmt.Printf("DEBUG: ConversationHandler got error: %v\n", err)
			}
			return ch.errorHandler.HandleAPIFailure(err, ch.agent.messages)
		}

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

// sendMessage handles the API communication with retry logic
func (ch *ConversationHandler) sendMessage() (*api.ChatResponse, error) {
	messages := ch.prepareMessages()
	tools := ch.prepareTools()
	reasoning := ch.determineReasoningEffort()

	return ch.apiClient.SendWithRetry(messages, tools, reasoning)
}

// processResponse handles the LLM response including tool execution
func (ch *ConversationHandler) processResponse(resp *api.ChatResponse) bool {
	if len(resp.Choices) == 0 {
		return true // Stop on empty response
	}

	choice := resp.Choices[0]

	// Add to conversation history
	ch.agent.messages = append(ch.agent.messages, api.Message{
		Role:             "assistant",
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
		ToolCalls:        choice.Message.ToolCalls,
	})

	// Update token tracking
	ch.agent.updateTokenUsage(resp.Usage)

	// Execute tools if present
	if len(choice.Message.ToolCalls) > 0 {
		ch.agent.debugLog("🛠️ Executing %d tool calls\n", len(choice.Message.ToolCalls))

		// Flush any buffered streaming content before tool execution
		// This ensures narrative text appears before tool calls for better flow
		if ch.agent.flushCallback != nil {
			ch.agent.flushCallback()
		}

		ch.displayIntermediateResponse(choice.Message.Content)
		toolResults := ch.toolExecutor.ExecuteTools(choice.Message.ToolCalls)
		ch.agent.messages = append(ch.agent.messages, toolResults...)
		ch.agent.debugLog("✔️ Added %d tool results to conversation\n", len(toolResults))
		return false // Continue conversation
	}

	// Check for blank iteration (no content and no tool calls)
	isBlankIteration := ch.isBlankIteration(choice.Message.Content, choice.Message.ToolCalls)

	if isBlankIteration {
		ch.consecutiveBlankIterations++
		ch.agent.debugLog("⚠️ Blank iteration detected (%d consecutive)\n", ch.consecutiveBlankIterations)

		if ch.consecutiveBlankIterations == 1 {
			// First blank iteration - remind the model
			ch.agent.debugLog("🔔 Sending reminder about task completion signal\n")
			reminderMessage := api.Message{
				Role:    "user",
				Content: "You provided a blank response. If you have completed the task and have no more actions to take, please respond with [[TASK_COMPLETE]] to indicate you are done. If you are not done, please continue with your next action.",
			}
			ch.agent.messages = append(ch.agent.messages, reminderMessage)
			return false // Continue conversation to get a proper response
		} else if ch.consecutiveBlankIterations >= 2 {
			// Two consecutive blank iterations - error out
			ch.agent.debugLog("❌ Too many consecutive blank iterations, stopping with error\n")
			errorMessage := "Error: The agent provided two consecutive blank responses and appears to be stuck. Please try rephrasing your request or break it into smaller tasks."
			ch.displayFinalResponse(errorMessage)
			return true // Stop with error
		}
	} else {
		// Reset blank iteration counter on any non-blank response
		ch.consecutiveBlankIterations = 0
	}

	// Check if the response indicates completion
	if ch.responseValidator.IsComplete(choice.Message.Content) {
		// Remove all variations of the completion signal from the content
		cleanContent := choice.Message.Content
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

		// Display final response
		ch.displayFinalResponse(cleanContent)
		return true // Stop - response explicitly indicates completion
	}

	// Otherwise, continue the conversation
	ch.agent.debugLog("📝 Response doesn't indicate completion, continuing...\n")
	ch.displayIntermediateResponse(choice.Message.Content)
	return false // Continue conversation
}

// Helper methods...
func (ch *ConversationHandler) checkForInterrupt() bool {
	select {
	case <-ch.agent.escPressed:
		ch.agent.interruptRequested = true
		return true
	default:
		return ch.agent.interruptRequested
	}
}

func (ch *ConversationHandler) prepareMessages() []api.Message {
	var optimizedMessages []api.Message

	// Use conversation optimizer if enabled
	if ch.agent.optimizer != nil && ch.agent.optimizer.IsEnabled() {
		optimizedMessages = ch.agent.optimizer.OptimizeConversation(ch.agent.messages)
	} else {
		optimizedMessages = ch.agent.messages
	}

	// Always include system prompt at the beginning
	allMessages := []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
	allMessages = append(allMessages, optimizedMessages...)
	return allMessages
}

func (ch *ConversationHandler) prepareTools() []api.Tool {
	return ch.agent.getOptimizedToolDefinitions(ch.agent.messages)
}

func (ch *ConversationHandler) determineReasoningEffort() string {
	return ch.agent.determineReasoningEffort(ch.agent.messages)
}

func (ch *ConversationHandler) displayIntermediateResponse(content string) {
	content = strings.TrimSpace(content)
	if len(content) > 0 {
		if ch.agent.streamingEnabled {
			// During streaming, content has already been displayed in real-time
			// But we need to ensure proper spacing and formatting after tool calls
			// Add a newline to separate from tool execution output
			ch.agent.safePrint("\n")
		} else {
			// Display thinking message for non-streaming mode
			ch.agent.safePrint("\r\033[K💭 %s\n", content)
		}
	}
}

func (ch *ConversationHandler) displayFinalResponse(content string) {
	if !ch.agent.streamingEnabled {
		ch.agent.safePrint("%s\n", content)
	}
}

func (ch *ConversationHandler) handleIncompleteResponse() {
	ch.agent.messages = append(ch.agent.messages, api.Message{
		Role:    "user",
		Content: "Please continue with your response. The previous response appears incomplete.",
	})
}

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

// processImagesInQuery handles image processing in queries
func (ch *ConversationHandler) processImagesInQuery(query string) (string, error) {
	// Move image processing logic here
	return ch.agent.processImagesInQuery(query)
}

// isBlankIteration checks if an iteration is considered blank (no meaningful content or tool calls)
func (ch *ConversationHandler) isBlankIteration(content string, toolCalls []api.ToolCall) bool {
	// Check if there are tool calls - if yes, not blank
	if len(toolCalls) > 0 {
		return false
	}

	// Check if content is empty or contains only whitespace
	trimmedContent := strings.TrimSpace(content)
	if len(trimmedContent) == 0 {
		return true
	}

	// Check if content is just a very short response that doesn't seem meaningful
	// (e.g., single character, just punctuation, etc.)
	if len(trimmedContent) <= 3 {
		return true
	}

	return false
}
