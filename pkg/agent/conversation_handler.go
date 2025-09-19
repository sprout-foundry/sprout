package agent

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ConversationHandler manages the high-level conversation flow
type ConversationHandler struct {
	agent             *Agent
	apiClient         *APIClient
	toolExecutor      *ToolExecutor
	responseValidator *ResponseValidator
	errorHandler      *ErrorHandler
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
		// Check for interrupts
		if ch.checkForInterrupt() {
			break
		}

		// Send message to LLM
		response, err := ch.sendMessage()
		if err != nil {
			return ch.errorHandler.HandleAPIFailure(err, ch.agent.messages)
		}

		// Process response
		if shouldStop := ch.processResponse(response); shouldStop {
			break
		}
	}

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
	})

	// Update token tracking
	ch.agent.updateTokenUsage(resp.Usage)

	// Execute tools if present
	if len(choice.Message.ToolCalls) > 0 {
		ch.displayIntermediateResponse(choice.Message.Content)
		toolResults := ch.toolExecutor.ExecuteTools(choice.Message.ToolCalls)
		ch.agent.messages = append(ch.agent.messages, toolResults...)
		return false // Continue conversation
	}

	// Validate response completeness
	if ch.responseValidator.IsIncomplete(choice.Message.Content) {
		ch.handleIncompleteResponse()
		return false // Continue to get complete response
	}

	// Display final response
	ch.displayFinalResponse(choice.Message.Content)
	return true // Stop - response is complete
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
			// Add newline after streaming content
			if ch.agent.outputMutex != nil {
				ch.agent.outputMutex.Lock()
				fmt.Println()
				ch.agent.outputMutex.Unlock()
			}
		} else {
			// Display thinking message
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
