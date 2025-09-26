package agent

import (
	"fmt"
	"os"
	"strings"
	"sync"

	api "github.com/alantheprice/ledit/pkg/agent_api"
)

// ProcessQueryRefactored is the cleaned up version of ProcessQuery
func (a *Agent) ProcessQueryRefactored(userQuery string) (string, error) {
	// Initialize components
	messageSender := NewMessageSender(a)
	responseValidator := NewResponseValidator(a)
	errorHandler := NewErrorHandler(a)

	// Enable change tracking
	a.EnableChangeTracking(userQuery)

	// Enable escape monitoring
	a.EnableEscMonitoring()
	defer a.DisableEscMonitoring()

	// Process images if present
	processedQuery, err := a.processImagesInQuery(userQuery)
	if err != nil {
		return "", err
	}

	// Add user message
	a.messages = append(a.messages, api.Message{
		Role:    "user",
		Content: processedQuery,
	})

	// Main conversation loop
	for a.currentIteration = 0; a.currentIteration < a.maxIterations; a.currentIteration++ {
		// Check for interrupt
		if a.checkForInterrupt() {
			break
		}

		// Prepare request
		messages := a.prepareMessages()
		tools := a.getOptimizedToolDefinitions(a.messages)
		reasoning := a.determineReasoningEffort(a.messages)

		// Send message to LLM
		resp, err := messageSender.SendMessage(messages, tools, reasoning)
		if err != nil {
			return errorHandler.HandleAPIFailure(err, a.messages)
		}

		// Process response
		shouldContinue, err := a.processResponse(resp, responseValidator)
		if err != nil {
			return "", err
		}

		if !shouldContinue {
			break
		}
	}

	// Finalize and return result
	return a.finalizeConversation()
}

// processResponse handles the LLM response
func (a *Agent) processResponse(resp *api.ChatResponse, validator *ResponseValidator) (bool, error) {
	if len(resp.Choices) == 0 {
		return false, fmt.Errorf("no response choices returned")
	}

	// Update token usage
	a.updateTokenUsage(resp.Usage)

	choice := resp.Choices[0]

	// Add assistant message to history
	a.messages = append(a.messages, api.Message{
		Role:             "assistant",
		Content:          choice.Message.Content,
		ReasoningContent: choice.Message.ReasoningContent,
	})

	// Handle tool calls
	if len(choice.Message.ToolCalls) > 0 {
		a.displayIntermediateResponse(choice.Message.Content)
		toolResults, err := a.executeToolCalls(choice.Message.ToolCalls)
		if err != nil {
			return false, err
		}
		a.messages = append(a.messages, toolResults...)
		return true, nil // Continue conversation
	}

	// Validate response completeness
	if validator.IsIncomplete(choice.Message.Content) {
		a.handleIncompleteResponse()
		return true, nil // Continue to get complete response
	}

	// Display final response
	a.displayFinalResponse(choice.Message.Content)
	return false, nil // Stop - response is complete
}

// prepareMessages prepares messages for sending to LLM
func (a *Agent) prepareMessages() []api.Message {
	var optimizedMessages []api.Message

	// Use conversation optimizer if enabled
	if a.optimizer != nil && a.optimizer.IsEnabled() {
		optimizedMessages = a.optimizer.OptimizeConversation(a.messages)
	} else {
		optimizedMessages = a.messages
	}

	// Always add system prompt at the beginning
	allMessages := []api.Message{{Role: "system", Content: a.systemPrompt}}
	allMessages = append(allMessages, optimizedMessages...)
	return allMessages
}

// updateTokenUsage updates token tracking
func (a *Agent) updateTokenUsage(usage struct {
	PromptTokens        int     `json:"prompt_tokens"`
	CompletionTokens    int     `json:"completion_tokens"`
	TotalTokens         int     `json:"total_tokens"`
	EstimatedCost       float64 `json:"estimated_cost"`
	PromptTokensDetails struct {
		CachedTokens     int  `json:"cached_tokens"`
		CacheWriteTokens *int `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}) {
	cachedTokens := usage.PromptTokensDetails.CachedTokens

	// Update totals
	a.totalCost += usage.EstimatedCost
	a.totalTokens += usage.TotalTokens
	a.promptTokens += usage.PromptTokens
	a.completionTokens += usage.CompletionTokens
	a.cachedTokens += cachedTokens

	// Calculate cached cost savings
	cachedCostSavings := a.calculateCachedCost(cachedTokens)
	a.cachedCostSavings += cachedCostSavings

	// Update stats callback
	if a.statsUpdateCallback != nil {
		a.statsUpdateCallback(a.totalTokens, a.totalCost)
	}

	// Log TPS if available
	a.logTPSInfo()
}

// executeToolCalls executes tool calls efficiently
func (a *Agent) executeToolCalls(toolCalls []api.ToolCall) ([]api.Message, error) {
	// Check for parallel optimization
	if a.canExecuteInParallel(toolCalls) {
		return a.executeToolsInParallel(toolCalls)
	}
	return a.executeToolsSequentially(toolCalls)
}

// canExecuteInParallel checks if tools can be run in parallel
func (a *Agent) canExecuteInParallel(toolCalls []api.ToolCall) bool {
	// Only read_file operations can be parallelized safely
	for _, tc := range toolCalls {
		if tc.Function.Name != "read_file" {
			return false
		}
	}
	return len(toolCalls) > 1
}

// executeToolsInParallel runs tools concurrently
func (a *Agent) executeToolsInParallel(toolCalls []api.ToolCall) ([]api.Message, error) {
	a.debugLog("🚀 Executing %d read_file operations in parallel\n", len(toolCalls))

	var wg sync.WaitGroup
	results := make([]api.Message, len(toolCalls))
	errors := make([]error, len(toolCalls))

	for i, tc := range toolCalls {
		wg.Add(1)
		go func(index int, toolCall api.ToolCall) {
			defer wg.Done()
			result, err := a.executeSingleTool(toolCall)
			results[index] = result
			errors[index] = err
		}(i, tc)
	}

	wg.Wait()

	// Check for errors
	for _, err := range errors {
		if err != nil {
			return results, err
		}
	}

	return results, nil
}

// executeToolsSequentially runs tools one by one
func (a *Agent) executeToolsSequentially(toolCalls []api.ToolCall) ([]api.Message, error) {
	var results []api.Message

	for i, tc := range toolCalls {
		// Check for interrupt
		if a.interruptRequested {
			results = append(results, api.Message{
				Role:    "tool",
				Content: "Execution interrupted by user",
			})
			break
		}

		// Show progress
		if len(toolCalls) > 1 {
			a.debugLog("🔧 Executing tool %d/%d: %s\n", i+1, len(toolCalls), tc.Function.Name)
		}

		// Execute tool
		result, err := a.executeSingleTool(tc)
		if err != nil {
			return results, err
		}
		results = append(results, result)
	}

	return results, nil
}

// executeSingleTool executes one tool call
func (a *Agent) executeSingleTool(toolCall api.ToolCall) (api.Message, error) {
	// Use the tool registry
	registry := GetToolRegistry()
	result, err := registry.ExecuteTool(toolCall.Function.Name,
		map[string]interface{}{}, // Parse args from toolCall.Function.Arguments
		a)

	if err != nil {
		result = fmt.Sprintf("Error: %v", err)
	}

	return api.Message{
		Role:    "tool",
		Content: result,
	}, nil
}

// Helper methods

func (a *Agent) checkForInterrupt() bool {
	select {
	case <-a.escPressed:
		a.interruptRequested = true
		return true
	default:
		return a.interruptRequested
	}
}

func (a *Agent) displayIntermediateResponse(content string) {
	content = strings.TrimSpace(content)
	if len(content) == 0 {
		return
	}

	if a.streamingEnabled {
		// Add newline after streaming
		if a.outputMutex != nil {
			a.outputMutex.Lock()
			fmt.Println()
			a.outputMutex.Unlock()
		}
	} else {
		// Show thinking indicator
		// In CI mode, don't use cursor control sequences
		if os.Getenv("LEDIT_CI_MODE") == "1" || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
			a.safePrint("💭 %s\n", content)
		} else {
			a.safePrint("\r\033[K💭 %s\n", content)
		}
	}
}

func (a *Agent) displayFinalResponse(content string) {
	if !a.streamingEnabled && len(content) > 0 {
		a.safePrint("%s\n", content)
	}
}

func (a *Agent) handleIncompleteResponse() {
	a.messages = append(a.messages, api.Message{
		Role:    "user",
		Content: "Please continue with your response. The previous response appears incomplete.",
	})
}

func (a *Agent) finalizeConversation() (string, error) {
	// Commit tracked changes
	if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
		if err := a.CommitChanges("Task completed"); err != nil {
			a.debugLog("Warning: Failed to commit changes: %v\n", err)
		}
	}

	// Return last assistant message
	for i := len(a.messages) - 1; i >= 0; i-- {
		if a.messages[i].Role == "assistant" {
			return a.messages[i].Content, nil
		}
	}

	return "", fmt.Errorf("no assistant response found")
}

// safePrint safely prints output with mutex protection
func (a *Agent) safePrint(format string, args ...interface{}) {
	content := fmt.Sprintf(format, args...)

	// Filter out completion signals that should not be displayed
	content = a.filterCompletionSignals(content)

	// Only print if there's content left after filtering
	if strings.TrimSpace(content) != "" {
		if a.outputMutex != nil {
			a.outputMutex.Lock()
			fmt.Print(content)
			a.outputMutex.Unlock()
		} else {
			fmt.Print(content)
		}
	}
}

// logTPSInfo logs tokens per second information
func (a *Agent) logTPSInfo() {
	if tpsClient, ok := a.client.(interface{ GetAverageTPS() float64 }); ok {
		averageTPS := tpsClient.GetAverageTPS()
		if averageTPS > 0 && a.debug {
			providerName := ""
			if provider, ok := a.client.(interface{ GetProvider() string }); ok {
				providerName = provider.GetProvider()
			}

			// Skip TPS logging for proxy providers
			if providerName != "openrouter" && providerName != "ollama-turbo" {
				a.debugLog("📊 Average TPS: %.1f tokens/second\n", averageTPS)
			}
		}
	}
}

// filterCompletionSignals removes task completion signals from content
func (a *Agent) filterCompletionSignals(content string) string {
	completionSignals := []string{
		"[[TASK_COMPLETE]]",
		"[[TASKCOMPLETE]]",
		"[[TASK COMPLETE]]",
		"[[task_complete]]",
		"[[taskcomplete]]",
		"[[task complete]]",
	}

	filtered := content
	for _, signal := range completionSignals {
		filtered = strings.ReplaceAll(filtered, signal, "")
	}

	return strings.TrimSpace(filtered)
}
