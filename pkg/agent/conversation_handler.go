package agent

import (
	"fmt"
	"os"
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
	consecutiveBlankIterations int
	conversationStartTime      time.Time
	lastActivityTime           time.Time
	timeoutDuration            time.Duration
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
		conversationStartTime: now,
		lastActivityTime:      now,
		timeoutDuration:       7 * time.Minute, // 5-minute timeout
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

		// Check for interrupts
		if ch.checkForInterrupt() {
			ch.agent.debugLog("⏹️ Conversation interrupted\n")
			break
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

	// Determine the content to record and validate. Prefer the streaming buffer if streaming was used
	contentUsed := choice.Message.Content
	if ch.agent.streamingEnabled && len(ch.agent.streamingBuffer.String()) > 0 {
		// Use the fully streamed content if available
		contentUsed = ch.agent.streamingBuffer.String()
	}

	// Add to conversation history
	ch.agent.messages = append(ch.agent.messages, api.Message{
		Role:             "assistant",
		Content:          contentUsed,
		ReasoningContent: choice.Message.ReasoningContent,
		ToolCalls:        choice.Message.ToolCalls,
	})

	// Update token tracking
	ch.agent.updateTokenUsage(resp.Usage)

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
		ch.agent.debugLog("✔️ Added %d tool results to conversation\n", len(toolResults))

		// If the tool calls appear malformed (parse/unknown/validation), add guidance after tool results
		if ch.shouldAddToolCallGuidance(toolResults) {
			guidance := ch.buildToolCallGuidance()
			ch.agent.messages = append(ch.agent.messages, api.Message{Role: "system", Content: guidance})
			ch.agent.toolCallGuidanceAdded = true
			ch.agent.debugLog("📝 Added system guidance for proper tool call formatting\n")
		}
		return false // Continue conversation
	}

	// If no tool_calls came back but the content suggests attempted tool usage,
	// inject one-time guidance and try again.
	if !ch.responseValidator.ValidateToolCalls(contentUsed) {
		if !ch.agent.toolCallGuidanceAdded { // avoid spamming repeated hints
			guidance := ch.buildToolCallGuidance()
			ch.agent.messages = append(ch.agent.messages, api.Message{Role: "system", Content: guidance})
			ch.agent.toolCallGuidanceAdded = true
			ch.agent.debugLog("📝 Added system guidance due to attempted tool usage without tool_calls\n")
		}
		return false // Continue conversation to allow the model to issue proper tool_calls
	}

	// Check for blank iteration (no content and no tool calls)
	isBlankIteration := ch.isBlankIteration(contentUsed, choice.Message.ToolCalls)

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
	if ch.responseValidator.IsComplete(contentUsed) {
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

		// Append a concise system summary to mark this task as completed and
		// provide guidance for future messages (treat future user instructions as higher priority)
		summary := "Task completed. Summary: The assistant finished the requested work. Future user instructions or follow-ups should be treated as new actions and take precedence over prior completed tasks."
		ch.agent.messages = append(ch.agent.messages, api.Message{Role: "system", Content: summary})

		// Display final response
		ch.displayFinalResponse(cleanContent)
		return true // Stop - response explicitly indicates completion
	}

	// Otherwise, decide whether this is a final (non-incomplete) response or we need more
	// If the response appears incomplete, ask the model to continue. Otherwise treat it as final.
	if ch.responseValidator.IsIncomplete(contentUsed) {
		ch.agent.debugLog("⚠️ Response appears incomplete, asking model to continue\n")
		ch.handleIncompleteResponse()
		return false // Continue conversation to get a complete response
	}

	// No explicit completion signal and response doesn't look incomplete -> treat as final
	ch.agent.debugLog("📝 Treating response as final (no completion signal but appears complete)\n")
	ch.displayFinalResponse(contentUsed)
	return true // Stop conversation
}

// Helper methods...
// shouldAddToolCallGuidance inspects tool results for common malformed tool call errors
func (ch *ConversationHandler) shouldAddToolCallGuidance(results []api.Message) bool {
	if ch.agent.toolCallGuidanceAdded {
		return false
	}
	for _, r := range results {
		if r.Role != "tool" {
			continue
		}
		c := strings.ToLower(r.Content)
		if strings.Contains(c, "error parsing arguments") ||
			strings.Contains(c, "failed to parse tool arguments") ||
			strings.Contains(c, "unknown tool") ||
			strings.Contains(c, "invalid mcp tool name format") ||
			strings.Contains(c, "parameter validation failed") ||
			(strings.Contains(c, "tool ") && strings.Contains(c, " not found")) {
			return true
		}
	}
	return false
}

// buildToolCallGuidance returns a concise system message instructing correct tool call usage
func (ch *ConversationHandler) buildToolCallGuidance() string {
	return "When you need to use tools, emit structured function calls only (no plain text instructions about tools).\n" +
		"Rules:\n" +
		"- Use the exact tool name from the tools list.\n" +
		"- Arguments must be valid JSON matching the schema keys.\n" +
		"- Do not include extra fields, comments, or trailing commas.\n" +
		"- Include all required parameters.\n\n" +
		"Examples:\n" +
		"1) read_file\n" +
		"   name: read_file\n" +
		"   arguments: {\"file_path\": \"pkg/agent/agent.go\"}\n\n" +
		"2) shell_command\n" +
		"   name: shell_command\n" +
		"   arguments: {\"command\": \"go test ./... -v\"}\n\n" +
		"3) edit_file\n" +
		"   name: edit_file\n" +
		"   arguments: {\"file_path\": \"README.md\", \"old_string\": \"old\", \"new_string\": \"new\"}\n\n" +
		"Notes:\n" +
		"- Do not embed a 'tool_calls' JSON object in your message content.\n" +
		"- If you need multiple tools, emit multiple function calls in order, each with valid JSON arguments."
}
func (ch *ConversationHandler) checkForInterrupt() bool {
	// Check for context cancellation (new interrupt system)
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
		// Context not cancelled, no input injection
	}

	// Check for timeout (5 minutes of inactivity)
	if time.Since(ch.lastActivityTime) > ch.timeoutDuration {
		ch.agent.debugLog("⏰ Conversation timeout after %v of inactivity\n", ch.timeoutDuration)
		ch.agent.interruptCancel() // Cancel context to trigger interrupt
		return true
	}

	return false
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

	// Check context limits and apply pruning if needed
	currentTokens := ch.estimateTokens(allMessages)
	if ch.agent.maxContextTokens > 0 {
		// Create pruner if needed and check if we should prune
		if ch.agent.conversationPruner == nil {
			ch.agent.conversationPruner = NewConversationPruner(ch.agent.debug)
		}

		if ch.agent.conversationPruner.ShouldPrune(currentTokens, ch.agent.maxContextTokens) {
			if ch.agent.debug {
				contextUsage := float64(currentTokens) / float64(ch.agent.maxContextTokens)
				fmt.Printf("🔄 Context pruning triggered: %d/%d tokens (%.1f%%)\n",
					currentTokens, ch.agent.maxContextTokens, contextUsage*100)
			}

			// Apply pruning to optimized messages (excluding system prompt)
			prunedMessages := ch.agent.conversationPruner.PruneConversation(optimizedMessages, currentTokens, ch.agent.maxContextTokens, ch.agent.optimizer)

			// Rebuild with system prompt
			allMessages = []api.Message{{Role: "system", Content: ch.agent.systemPrompt}}
			allMessages = append(allMessages, prunedMessages...)
			// Persist pruned history so future iterations don't re-trigger on stale counts
			ch.agent.messages = prunedMessages

			if ch.agent.debug {
				newTokens := ch.estimateTokens(allMessages)
				fmt.Printf("✅ Context after pruning: %d tokens (%.1f%%)\n",
					newTokens, float64(newTokens)/float64(ch.agent.maxContextTokens)*100)
			}
		}
	}

	allMessages = ch.sanitizeToolMessages(allMessages)

	return allMessages
}

func (ch *ConversationHandler) prepareTools() []api.Tool {
	return ch.agent.getOptimizedToolDefinitions(ch.agent.messages)
}

func (ch *ConversationHandler) sanitizeToolMessages(messages []api.Message) []api.Message {
	if len(messages) == 0 {
		return messages
	}

	sanitized := make([]api.Message, 0, len(messages))
	seenToolCalls := make(map[string]struct{})

	for _, msg := range messages {
		switch msg.Role {
		case "assistant":
			sanitized = append(sanitized, msg)
			if len(msg.ToolCalls) > 0 {
				for _, tc := range msg.ToolCalls {
					if tc.ID != "" {
						seenToolCalls[tc.ID] = struct{}{}
					}
				}
			}
		case "tool":
			if msg.ToolCallId == "" {
				ch.logDroppedToolMessage("missing tool_call_id", msg)
				continue
			}

			if _, ok := seenToolCalls[msg.ToolCallId]; ok {
				sanitized = append(sanitized, msg)
				delete(seenToolCalls, msg.ToolCallId)
			} else {
				ch.logDroppedToolMessage(fmt.Sprintf("no matching assistant for %s", msg.ToolCallId), msg)
			}
		default:
			sanitized = append(sanitized, msg)
		}
	}

	return sanitized
}

func (ch *ConversationHandler) logDroppedToolMessage(reason string, msg api.Message) {
	if ch.agent == nil || !ch.agent.debug {
		return
	}

	snippet := strings.TrimSpace(msg.Content)
	if len(snippet) > 80 {
		snippet = snippet[:77] + "..."
	}

	ch.agent.debugLog("⚠️ Dropping tool message (%s). tool_call_id=%s snippet=%q\n", reason, msg.ToolCallId, snippet)
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
			// In CI mode, don't use cursor control sequences
			if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
				ch.agent.safePrint("💭 %s\n", content)
			} else {
				ch.agent.safePrint("\r\033[K💭 %s\n", content)
			}
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

// estimateTokens provides a rough estimate of token count for messages
func (ch *ConversationHandler) estimateTokens(messages []api.Message) int {
	totalTokens := 0

	for _, msg := range messages {
		// Estimate core content tokens
		totalTokens += EstimateTokens(msg.Content)

		if msg.ReasoningContent != "" {
			totalTokens += EstimateTokens(msg.ReasoningContent)
		}

		// Include tool call metadata (arguments can be sizeable JSON payloads)
		for _, toolCall := range msg.ToolCalls {
			totalTokens += EstimateTokens(toolCall.Function.Name)
			totalTokens += EstimateTokens(toolCall.Function.Arguments)
			// modest overhead for call framing/ids
			totalTokens += 20
		}

		// Role/formatting overhead per message
		totalTokens += 10
	}

	// Apply a small safety buffer but stay close to measured estimate
	buffered := int(float64(totalTokens) * 1.05)
	if buffered < totalTokens {
		return totalTokens
	}
	return buffered
}

// displayUserFriendlyError shows contextual error messages to the user
func (ch *ConversationHandler) displayUserFriendlyError(err error) {
	errStr := err.Error()
	providerName := strings.Title(ch.agent.GetProvider())

	var userMessage string

	// Categorize errors for better user experience
	if strings.Contains(errStr, "timed out") {
		if strings.Contains(errStr, "no response received") {
			userMessage = fmt.Sprintf("⏰ %s is taking longer than usual to respond. This might be due to high load or network issues.\n💡 Try again in a few moments, or use a simpler query if the problem persists.", providerName)
		} else if strings.Contains(errStr, "no data received") {
			userMessage = fmt.Sprintf("⏰ %s stopped sending data. The connection may have been interrupted.\n💡 Please try your request again.", providerName)
		} else {
			userMessage = fmt.Sprintf("⏰ %s request timed out. This usually indicates network issues or high server load.\n💡 Try again in a few moments, or break your request into smaller parts.", providerName)
		}
	} else if strings.Contains(errStr, "connection") || strings.Contains(errStr, "network") {
		userMessage = fmt.Sprintf("🔌 Connection to %s failed. Please check your internet connection and try again.", providerName)
	} else if strings.Contains(errStr, "429") || strings.Contains(errStr, "rate limit") {
		userMessage = fmt.Sprintf("🚦 %s rate limit reached. Please wait a moment before trying again.", providerName)
	} else if strings.Contains(errStr, "401") || strings.Contains(errStr, "unauthorized") {
		userMessage = fmt.Sprintf("🔑 %s API key issue. Please check your authentication.", providerName)
	} else if strings.Contains(errStr, "500") || strings.Contains(errStr, "502") || strings.Contains(errStr, "503") {
		userMessage = fmt.Sprintf("🔧 %s is experiencing server issues. Please try again in a few minutes.", providerName)
	} else {
		userMessage = fmt.Sprintf("❌ %s API error: %v", providerName, err)
	}

	// Display the message in the content area via agent routing
	ch.agent.PrintLine("")
	ch.agent.PrintLine(userMessage)
	ch.agent.PrintLine("")
}
