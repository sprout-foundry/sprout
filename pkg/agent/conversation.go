package agent

import (
	"fmt"
	"math/rand"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	// Enable change tracking for this conversation
	a.EnableChangeTracking(userQuery)

	// Enable Esc monitoring during query processing
	a.EnableEscMonitoring()
	defer a.DisableEscMonitoring() // Disable when done

	// Process any images in the user query first
	processedQuery, err := a.processImagesInQuery(userQuery)
	if err != nil {
		a.debugLog("⚠️ Vision processing failed: %v\n", err)
		// Continue with original query if vision processing fails
		processedQuery = userQuery
	}

	a.currentIteration = 0

	// Print startup message when running from agent command
	if os.Getenv("LEDIT_FROM_AGENT") == "1" {
		providerType := a.GetProviderType()
		providerName := api.GetProviderName(providerType)
		model := a.GetModel()
		fmt.Printf("🤖 Agent: %s/%s\n", providerName, model)
		fmt.Printf("📋 Task: %s\n", userQuery)
	}

	// Set a reasonable iteration limit to prevent infinite loops
	// The agent should complete tasks naturally, but we need a safety limit
	maxIterationsForThisQuery := a.maxIterations

	// Check if this is a new conversation or continuing an existing one
	if len(a.messages) == 0 {
		// First query - initialize with system prompt
		a.messages = []api.Message{
			{Role: "system", Content: a.systemPrompt},
		}
	}

	// Add the new user query to the existing conversation
	a.messages = append(a.messages, api.Message{
		Role:    "user",
		Content: processedQuery,
	})

	for a.currentIteration < maxIterationsForThisQuery {
		iterationStart := time.Now()
		a.currentIteration++

		// Check for interrupt signal at the start of each iteration
		if a.CheckForInterrupt() {
			interruptMessage := a.HandleInterrupt()
			if interruptMessage == "STOP" {
				// User wants to stop current processing
				a.debugLog("🛑 User requested stop\n")
				a.ClearInterrupt()
				break // Exit the iteration loop
			} else if interruptMessage != "" {
				// Inject user message into conversation
				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: fmt.Sprintf("🛑 INTERRUPT: %s", interruptMessage),
				})
				a.debugLog("🛑 Interrupt processed, continuing with: %s\n", interruptMessage)
			}
			// Clear interrupt state and continue
			a.ClearInterrupt()
		}

		a.debugLog("Iteration %d/%d\n", a.currentIteration, a.maxIterations)

		// Print compact progress indicator in non-interactive mode
		if os.Getenv("LEDIT_FROM_AGENT") == "1" && !a.IsInteractiveMode() {
			a.PrintCompactProgress()
		}

		// Optimize conversation before sending to API
		optimizedMessages := a.optimizer.OptimizeConversation(a.messages)

		if a.debug && len(optimizedMessages) < len(a.messages) {
			saved := len(a.messages) - len(optimizedMessages)
			a.debugLog("🔄 Conversation optimized: %d messages → %d messages (saved %d)\n",
				len(a.messages), len(optimizedMessages), saved)
		}

		// Check context size and manage if approaching limit
		contextTokens := a.estimateContextTokens(optimizedMessages)
		a.currentContextTokens = contextTokens

		// Apply automatic pruning if needed
		if a.conversationPruner != nil && a.conversationPruner.ShouldPrune(contextTokens, a.maxContextTokens) {
			// Apply automatic pruning
			prunedMessages := a.conversationPruner.PruneConversation(optimizedMessages, contextTokens, a.maxContextTokens, a.optimizer)

			// Update the stored messages to reflect pruning
			if len(prunedMessages) < len(optimizedMessages) {
				a.messages = prunedMessages
				optimizedMessages = prunedMessages
				contextTokens = a.estimateContextTokens(optimizedMessages)
				a.currentContextTokens = contextTokens
			}
		}

		// Check if we're still approaching the context limit after pruning
		contextThreshold := int(float64(a.maxContextTokens) * 0.8)
		if contextTokens > contextThreshold {
			if !a.contextWarningIssued {
				a.debugLog("⚠️  Context approaching limit: %s/%s (%.1f%%)\n",
					a.formatTokenCount(contextTokens),
					a.formatTokenCount(a.maxContextTokens),
					float64(contextTokens)/float64(a.maxContextTokens)*100)
				a.contextWarningIssued = true
			}

			// Perform aggressive optimization as last resort
			optimizedMessages = a.optimizer.AggressiveOptimization(optimizedMessages)
			contextTokens = a.estimateContextTokens(optimizedMessages)
			a.currentContextTokens = contextTokens

			if a.debug {
				a.debugLog("🔄 Aggressive optimization applied: %s context tokens\n",
					a.formatTokenCount(contextTokens))
			}
		}

		// Send request to API using dynamic reasoning effort and cached tools
		reasoningEffort := a.determineReasoningEffort(optimizedMessages)
		tools := a.getOptimizedToolDefinitions(optimizedMessages)

		// Retry logic for transient errors
		var resp *api.ChatResponse
		var err error
		maxRetries := 3
		retryDelay := time.Second

		// Reset streaming buffer
		a.streamingBuffer.Reset()

		for retry := 0; retry <= maxRetries; retry++ {
			if a.streamingEnabled {
				// Don't show indicator here - let the streaming formatter handle it

				// Use streaming API
				streamCallback := func(content string) {
					// Accumulate content in buffer
					a.streamingBuffer.WriteString(content)

					// Call user callback if provided
					if a.streamingCallback != nil {
						a.streamingCallback(content)
					} else if a.outputMutex != nil {
						// Default streaming output
						a.outputMutex.Lock()
						fmt.Print(content)
						a.outputMutex.Unlock()
					}
				}

				resp, err = a.client.SendChatRequestStream(optimizedMessages, tools, reasoningEffort, streamCallback)
			} else {
				// Use regular API
				resp, err = a.client.SendChatRequest(optimizedMessages, tools, reasoningEffort)
			}

			if err == nil {
				break // Success
			}

			// Check if this is a retryable error
			errStr := err.Error()
			isRetryable := strings.Contains(errStr, "stream error") ||
				strings.Contains(errStr, "INTERNAL_ERROR") ||
				strings.Contains(errStr, "connection reset") ||
				strings.Contains(errStr, "EOF") ||
				strings.Contains(errStr, "timeout")

			if !isRetryable || retry == maxRetries {
				// Not retryable or max retries reached
				return a.handleAPIFailure(err, optimizedMessages)
			}

			// Log retry attempt
			a.debugLog("⚠️ Retrying API request (attempt %d/%d) after error: %v\n", retry+1, maxRetries, err)

			// Exponential backoff with jitter
			jitter := time.Duration(rand.Float64() * float64(retryDelay/2))
			sleepTime := retryDelay + jitter
			time.Sleep(sleepTime)
			retryDelay *= 2 // Double the delay for next retry
		}

		if len(resp.Choices) == 0 {
			return "", fmt.Errorf("no response choices returned")
		}

		// Track token usage and cost
		cachedTokens := resp.Usage.PromptTokensDetails.CachedTokens

		// Use actual cost from API (already accounts for cached tokens)
		a.totalCost += resp.Usage.EstimatedCost
		a.totalTokens += resp.Usage.TotalTokens
		a.promptTokens += resp.Usage.PromptTokens
		a.completionTokens += resp.Usage.CompletionTokens
		a.cachedTokens += cachedTokens

		// Debug log token update
		if os.Getenv("DEBUG") == "1" {
			fmt.Fprintf(os.Stderr, "\n[DEBUG] Token update: usage.Total=%d, agent.Total=%d\n", resp.Usage.TotalTokens, a.totalTokens)
		}

		// Calculate cost savings for display purposes only
		cachedCostSavings := a.calculateCachedCost(cachedTokens)
		a.cachedCostSavings += cachedCostSavings

		// Call stats update callback if set
		if a.statsUpdateCallback != nil {
			// Debug log when callback is invoked
			if os.Getenv("DEBUG") == "1" {
				fmt.Fprintf(os.Stderr, "\n[DEBUG] Invoking stats callback: total=%d, cost=%.4f\n", a.totalTokens, a.totalCost)
			}
			a.statsUpdateCallback(a.totalTokens, a.totalCost)
		}

		// Only show context information in debug mode
		// Calculate iteration timing
		iterationDuration := time.Since(iterationStart)

		if a.debug {
			a.debugLog("💰 Response: %d prompt + %d completion | Cost: $%.6f | Context: %s/%s | Time: %v\n",
				resp.Usage.PromptTokens,
				resp.Usage.CompletionTokens,
				resp.Usage.EstimatedCost,
				a.formatTokenCount(a.currentContextTokens),
				a.formatTokenCount(a.maxContextTokens),
				iterationDuration)

			if cachedTokens > 0 {
				a.debugLog("📋 Cached tokens: %d | Savings: $%.6f\n",
					cachedTokens, cachedCostSavings)
			}
		}

		choice := resp.Choices[0]

		// Add assistant's message to history
		a.messages = append(a.messages, api.Message{
			Role:             "assistant",
			Content:          choice.Message.Content,
			ReasoningContent: choice.Message.ReasoningContent,
		})

		// Display intermediate response if it contains substantial content and has tool calls
		// This shows the user the agent's reasoning before executing tools
		if len(choice.Message.ToolCalls) > 0 {
			content := strings.TrimSpace(choice.Message.Content)
			// If streaming was enabled, content was already shown in real-time
			if !a.streamingEnabled && len(content) > 0 {
				// Use mutex if available for synchronized output
				if a.outputMutex != nil {
					a.outputMutex.Lock()
					fmt.Print("\r\033[K") // Clear line
					fmt.Printf("💭 %s\n", content)
					a.outputMutex.Unlock()
				} else {
					fmt.Print("\r\033[K") // Clear line
					fmt.Printf("💭 %s\n", content)
				}
			} else if a.streamingEnabled && len(content) > 0 {
				// Add newline after streaming content if there are tool calls coming
				if a.outputMutex != nil {
					a.outputMutex.Lock()
					fmt.Println() // Add newline after streamed content
					a.outputMutex.Unlock()
				} else {
					fmt.Println()
				}
			}
			// If no content but has tool calls, we'll let the tool execution logs speak for themselves
		}

		// Check if there are tool calls to execute
		if len(choice.Message.ToolCalls) > 0 {
			// Optimization: Check if all tool calls are read_file operations for parallel execution
			allReadFile := true
			for _, tc := range choice.Message.ToolCalls {
				if tc.Function.Name != "read_file" {
					allReadFile = false
					break
				}
			}

			toolResults := make([]string, len(choice.Message.ToolCalls))

			if allReadFile && len(choice.Message.ToolCalls) > 1 {
				// Execute read_file operations in parallel
				a.debugLog("🚀 Executing %d read_file operations in parallel for optimal performance\n", len(choice.Message.ToolCalls))

				type readResult struct {
					index  int
					result string
				}

				resultChan := make(chan readResult, len(choice.Message.ToolCalls))

				// Launch parallel goroutines for each read_file
				for i, toolCall := range choice.Message.ToolCalls {
					go func(idx int, tc api.ToolCall) {
						result, err := a.executeTool(tc)
						if err != nil {
							result = fmt.Sprintf("Error executing tool %s: %s", tc.Function.Name, err.Error())
						}
						resultChan <- readResult{
							index:  idx,
							result: fmt.Sprintf("Tool call result for %s: %s", tc.Function.Name, result),
						}
					}(i, toolCall)
				}

				// Collect results in order
				for i := 0; i < len(choice.Message.ToolCalls); i++ {
					res := <-resultChan
					toolResults[res.index] = res.result
				}
			} else {
				// Execute tool calls sequentially for non-read operations or single calls
				for i, toolCall := range choice.Message.ToolCalls {
					result, err := a.executeTool(toolCall)
					if err != nil {
						result = fmt.Sprintf("Error executing tool %s: %s", toolCall.Function.Name, err.Error())
					}
					toolResults[i] = fmt.Sprintf("Tool call result for %s: %s", toolCall.Function.Name, result)
				}
			}

			// Add tool results to conversation
			a.messages = append(a.messages, api.Message{
				Role:    "user",
				Content: strings.Join(toolResults, "\n\n"),
			})

			continue
		} else {
			// Check if content or reasoning_content contains tool calls that weren't properly parsed
			toolCalls := a.extractToolCallsFromContent(choice.Message.Content)
			if len(toolCalls) == 0 {
				// Also check reasoning_content
				toolCalls = a.extractToolCallsFromContent(choice.Message.ReasoningContent)
			}

			if len(toolCalls) > 0 {
				// Check if these are XML-style tool calls
				if strings.Contains(choice.Message.Content, "<function=") || strings.Contains(choice.Message.ReasoningContent, "<function=") {
					a.debugLog("🔧 Found %d XML-style tool calls - executing them for compatibility\n", len(toolCalls))
				} else {
					a.debugLog("⚠️  Found %d malformed tool calls in content - executing them for compatibility\n", len(toolCalls))
				}

				toolResults := make([]string, 0)
				for _, toolCall := range toolCalls {
					a.debugLog("  - Executing tool call: %s\n", toolCall.Function.Name)
					result, err := a.executeTool(toolCall)
					if err != nil {
						result = fmt.Sprintf("Error executing tool %s: %s", toolCall.Function.Name, err.Error())
					}
					toolResults = append(toolResults, fmt.Sprintf("Tool call result for %s: %s", toolCall.Function.Name, result))
				}

				// Add tool results to conversation without feedback
				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: strings.Join(toolResults, "\n\n"),
				})

				continue
			}

			// Check if this looks like attempted tool calls that we couldn't parse
			if a.containsAttemptedToolCalls(choice.Message.Content) || a.containsAttemptedToolCalls(choice.Message.ReasoningContent) {
				a.debugLog("⚠️  Detected attempted but unparseable tool calls - asking for retry\n")

				// Simple retry message without lecturing
				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: "I couldn't parse your tool calls. Please retry using the correct tool calling format.",
				})

				continue
			}

			// Check if the response looks incomplete and retry
			// But first check if this is a simple question that shouldn't trigger continuation
			queryLower := strings.ToLower(userQuery)
			isSimpleQuestion := false

			// Check for simple arithmetic
			if strings.Contains(queryLower, "what is") &&
				(strings.Contains(queryLower, "+") || strings.Contains(queryLower, "-") ||
					strings.Contains(queryLower, "*") || strings.Contains(queryLower, "/") ||
					strings.Contains(queryLower, "plus") || strings.Contains(queryLower, "minus")) {
				isSimpleQuestion = true
			}

			// Check for brief/tell me questions with "already have" or "information you have"
			if (strings.Contains(queryLower, "briefly") || strings.Contains(queryLower, "tell me")) &&
				(strings.Contains(queryLower, "already have") || strings.Contains(queryLower, "information you have") ||
					strings.Contains(queryLower, "with the information")) {
				isSimpleQuestion = true
			}

			// Check for simple tell/say commands
			if strings.HasPrefix(queryLower, "say ") || strings.HasPrefix(queryLower, "tell me a joke") {
				isSimpleQuestion = true
			}

			if !isSimpleQuestion && a.isIncompleteResponse(choice.Message.Content) {
				// Add encouragement to continue
				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: "The previous response appears incomplete. Please continue with the task and use available tools to fully complete the work.",
				})
				continue
			}

			// Check for potential false stops before returning
			if a.shouldCheckFalseStop(choice.Message.Content) {
				if isFalseStop, confidence := a.checkFalseStop(choice.Message.Content); isFalseStop {
					a.debugLog("🔄 Detected possible false stop (confidence: %.2f), continuing...\n", confidence)

					// Always show a message when false stop detection triggers
					fmt.Printf("🔄 False stop detected (confidence: %.2f) - Response length: %d chars\n", confidence, len(choice.Message.Content))
					if a.debug {
						fmt.Printf("   Response preview: %q\n", truncateString(choice.Message.Content, 100))
					}

					// Add a gentle continuation prompt
					a.messages = append(a.messages, api.Message{
						Role:    "user",
						Content: "Please continue with your analysis.",
					})
					continue
				}
			}

			// No tool calls and response seems complete - we're done
			// Commit any tracked changes before returning
			if commitErr := a.CommitChanges(choice.Message.Content); commitErr != nil {
				a.debugLog("Warning: Failed to commit tracked changes: %v\n", commitErr)
			}

			// Debug: Log the response content to see formatting
			if a.debug {
				a.debugLog("Final response content (%d chars):\n%s\n", len(choice.Message.Content), choice.Message.Content)
			}

			// If streaming was enabled, the content was already displayed
			// Return empty string to avoid duplication
			if a.streamingEnabled {
				if a.debug {
					a.debugLog("Streaming was enabled, returning empty string (content length: %d)\n", len(choice.Message.Content))
				}
				return "", nil
			}
			return choice.Message.Content, nil
		}
	}

	// Commit any tracked changes even if we hit max iterations
	if commitErr := a.CommitChanges("Maximum iterations reached"); commitErr != nil {
		a.debugLog("Warning: Failed to commit tracked changes: %v\n", commitErr)
	}

	return "", fmt.Errorf("iteration limit (%d) reached - task may be incomplete", maxIterationsForThisQuery)
}

// ProcessQueryWithContinuity processes a query with continuity from previous actions
func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
	// Ensure changes are committed even if there are unexpected errors or early termination
	defer func() {
		// Only commit if we have changes and they haven't been committed yet
		if a.IsChangeTrackingEnabled() && a.GetChangeCount() > 0 {
			a.debugLog("DEFER: Attempting to commit %d tracked changes\n", a.GetChangeCount())
			// Check if changes are already committed by trying to commit (it's safe due to committed flag)
			if commitErr := a.CommitChanges("Session cleanup - ensuring changes are not lost"); commitErr != nil {
				a.debugLog("Warning: Failed to commit tracked changes during cleanup: %v\n", commitErr)
			} else {
				a.debugLog("DEFER: Successfully committed tracked changes during cleanup\n")
			}
		} else {
			a.debugLog("DEFER: No changes to commit (enabled: %v, count: %d)\n", a.IsChangeTrackingEnabled(), a.GetChangeCount())
		}
	}()

	// Load previous state if available
	if a.previousSummary != "" {
		continuityPrompt := fmt.Sprintf(`
CONTEXT FROM PREVIOUS SESSION:
%s

CURRENT TASK:
%s

Note: The user cannot see the previous session's responses, so please provide a complete answer without referencing "previous responses" or "as mentioned before". If this task relates to the previous session, build upon that work but present your response as if it's the first time addressing this topic.`,
			a.previousSummary, userQuery)

		return a.ProcessQuery(continuityPrompt)
	}

	// No previous state, process normally
	return a.ProcessQuery(userQuery)
}

// isIncompleteResponse checks if a response looks incomplete or is declining the task prematurely
func (a *Agent) isIncompleteResponse(content string) bool {
	if content == "" {
		return true // Empty responses are definitely incomplete
	}

	// Check if this contains attempted tool calls - if so, it's incomplete
	if a.containsAttemptedToolCalls(content) {
		return true
	}

	originalContent := content // Keep original for case-sensitive checks

	// REMOVED: Overly aggressive intent-to-continue patterns that were causing infinite loops
	// Only check for very specific incomplete indicators

	// Only check for very clear indicators of incomplete responses
	trimmedContent := strings.TrimSpace(originalContent)

	// Check if response ends with ":" followed by nothing (expecting a list or code)
	if strings.HasSuffix(trimmedContent, ":") && !strings.Contains(trimmedContent, "\n") {
		a.debugLog("Detected trailing colon without content\n")
		return true
	}

	// Check if the response is cut off mid-sentence (ends with comma or no punctuation)
	lastChar := ""
	if len(trimmedContent) > 0 {
		lastChar = string(trimmedContent[len(trimmedContent)-1])
	}

	// Only consider it incomplete if it ends with a comma or has no ending punctuation at all
	// and is very short (less than 50 chars)
	if len(trimmedContent) < 50 && (lastChar == "," || (!strings.ContainsAny(lastChar, ".!?\"'`"))) {
		a.debugLog("Detected very short response with no ending punctuation\n")
		return true
	}

	return false
}

// handleAPIFailure preserves conversation context when API calls fail
func (a *Agent) handleAPIFailure(apiErr error, _ []api.Message) (string, error) {
	// Count the tools/work we've already done to show progress
	toolsExecuted := 0
	for _, msg := range a.messages {
		if msg.Role == "tool" {
			toolsExecuted++
		}
	}

	a.debugLog("⚠️ API request failed after %d tools executed (tokens: %s). Preserving conversation context.\n",
		toolsExecuted, a.formatTokenCount(a.totalTokens))

	// Check if we're in a non-interactive environment (e.g., CI/GitHub Actions)
	if !a.IsInteractiveMode() || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		// In non-interactive mode, return an error to fail fast
		errorMsg := fmt.Sprintf("API request failed after %d tools executed: %v", toolsExecuted, apiErr)

		// Include progress information in the error message
		if toolsExecuted > 0 {
			errorMsg += fmt.Sprintf(" (Progress: %d tools executed, %s tokens used)",
				toolsExecuted, a.formatTokenCount(a.totalTokens))
		}

		// Return the error to terminate the process
		return "", fmt.Errorf("%s", errorMsg)
	}

	// Interactive mode - preserve conversation for user to continue
	response := "⚠️ **API Request Failed - Conversation Preserved**\n\n"

	// Classify the error type for better user guidance
	errorMsg := apiErr.Error()
	if strings.Contains(errorMsg, "timeout") || strings.Contains(errorMsg, "deadline exceeded") {
		response += "The API request timed out, likely due to high server load or a complex request.\n\n"
	} else if strings.Contains(errorMsg, "rate limit") {
		response += "Hit API rate limits. Please wait a moment before continuing.\n\n"
	} else if strings.Contains(strings.ToLower(errorMsg), "model") &&
		(strings.Contains(strings.ToLower(errorMsg), "not exist") ||
			strings.Contains(strings.ToLower(errorMsg), "not found") ||
			strings.Contains(strings.ToLower(errorMsg), "does not exist")) {
		response += fmt.Sprintf("❌ **Model Error**: %s\n\n", errorMsg)
		response += "The selected model is not available. You may need to:\n"
		response += "- Check the model name with `/models` command\n"
		response += "- Switch to a different model with `/model` command\n"
		response += "- Verify your API key has access to this model\n\n"
	} else {
		response += fmt.Sprintf("API error: %s\n\n", errorMsg)
	}

	response += "**Progress So Far:**\n"
	response += fmt.Sprintf("- Tools executed: %d\n", toolsExecuted)
	response += fmt.Sprintf("- Total tokens used: %s\n", a.formatTokenCount(a.totalTokens))
	response += fmt.Sprintf("- Current iteration: %d/%d\n\n", a.currentIteration, a.maxIterations)

	// Importantly - keep the conversation state intact so user can continue
	response += "🔄 **Your conversation context is preserved.** You can:\n"
	response += "- Ask me to continue with your original request\n"
	response += "- Ask a more specific question about what you wanted to know\n"
	response += "- Ask me to summarize what I've learned so far\n"
	response += "- Try a different approach to your question\n\n"

	response += "💡 What would you like me to do next?"

	// DON'T return an error - return the preserved conversation response
	// This keeps the conversation alive instead of terminating it
	return response, nil
}

// containsAttemptedToolCalls checks if content contains patterns that suggest attempted tool calls
func (a *Agent) containsAttemptedToolCalls(content string) bool {
	if content == "" {
		return false
	}

	// Patterns that suggest attempted tool calls that we couldn't parse
	attemptedPatterns := []string{
		`"tool_calls"`,
		`"function"`,
		`"arguments"`,
		`"name":`,
		`"id":`,
		`"type": "function"`,
		`shell_command`,
		`read_file`,
		`write_file`,
		`edit_file`,
		`{"id"`,
		`"call_`,
		`<function=`,  // XML-style function calls
		`<parameter=`, // XML-style parameters
		`</function>`, // XML-style closing tag
	}

	contentLower := strings.ToLower(content)
	matchCount := 0

	for _, pattern := range attemptedPatterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			matchCount++
			// If we find multiple patterns, it's very likely attempted tool calls
			if matchCount >= 2 {
				return true
			}
		}
	}

	// Also check for JSON-like structures that might be malformed tool calls
	if strings.Contains(content, `{`) && strings.Contains(content, `}`) && matchCount >= 1 {
		return true
	}

	return false
}

// determineReasoningEffort decides reasoning level based on task complexity
func (a *Agent) determineReasoningEffort(messages []api.Message) string {
	if len(messages) == 0 {
		return "medium"
	}

	lastMessage := messages[len(messages)-1].Content

	// Use low reasoning for simple, repetitive tasks
	simplePatterns := []string{
		"read_file", "ls -", "pwd", "cat ", "echo ",
		"mkdir", "cd ", "mv ", "cp ", "rm ",
		"git status", "git diff", "npm install",
		"go build", "go test", "go run",
	}

	for _, pattern := range simplePatterns {
		if strings.Contains(strings.ToLower(lastMessage), pattern) {
			if a.debug {
				a.debugLog("🚀 Using low reasoning for simple task\n")
			}
			return "low"
		}
	}

	// Use high reasoning for complex tasks
	complexPatterns := []string{
		"analyze", "design", "implement", "create", "plan",
		"debug", "refactor", "optimize", "architecture",
		"vision", "image", "ui", "frontend", "algorithm",
		"error", "fix", "problem", "issue", "troubleshoot",
	}

	for _, pattern := range complexPatterns {
		if strings.Contains(strings.ToLower(lastMessage), pattern) {
			if a.debug {
				a.debugLog("🧠 Using high reasoning for complex task\n")
			}
			return "high"
		}
	}

	// Default to medium for everything else
	if a.debug {
		a.debugLog("⚖️ Using medium reasoning for standard task\n")
	}
	return "medium"
}

// getOptimizedToolDefinitions returns relevant tools based on context
func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	// Get base tools
	allTools := api.GetToolDefinitions()

	if a.debug {
		fmt.Printf("🔧 Base tools count: %d\n", len(allTools))
	}

	// Add MCP tools if available
	if a.mcpManager != nil {
		mcpTools := a.getMCPTools()
		allTools = append(allTools, mcpTools...)
		if a.debug {
			fmt.Printf("🔧 Total tools after adding MCP: %d\n", len(allTools))
		}
	} else if a.debug {
		fmt.Printf("⚠️  MCP manager is nil in getOptimizedToolDefinitions\n")
	}

	if len(messages) == 0 {
		return allTools
	}

	// Quick optimization: if last few messages only used basic tools,
	// prioritize those (but still include all for flexibility)
	return allTools
}

// ClearConversationHistory resets the conversation state
func (a *Agent) ClearConversationHistory() {
	a.messages = []api.Message{}
	a.previousSummary = ""
	a.taskActions = []TaskAction{}
	a.optimizer.Reset()
}

// SetConversationOptimization enables or disables conversation optimization
// Note: Optimization is always enabled by default for optimal performance
func (a *Agent) SetConversationOptimization(enabled bool) {
	a.optimizer.SetEnabled(enabled)
	if a.debug {
		if enabled {
			a.debugLog("🔄 Conversation optimization enabled\n")
		} else {
			a.debugLog("🔄 Conversation optimization disabled\n")
		}
	}
}

// GetOptimizationStats returns optimization statistics
func (a *Agent) GetOptimizationStats() map[string]interface{} {
	return a.optimizer.GetOptimizationStats()
}

// processImagesInQuery detects and processes images in user queries
func (a *Agent) processImagesInQuery(query string) (string, error) {
	// Check if vision processing is available
	if !tools.HasVisionCapability() {
		// No vision capability available, return original query
		return query, nil
	}

	// Determine analysis mode from query context
	var analysisMode string
	if containsFrontendKeywords(query) {
		analysisMode = "frontend"
	} else {
		analysisMode = "general"
	}

	// Create vision processor with appropriate mode
	processor, err := tools.NewVisionProcessorWithMode(a.debug, analysisMode)
	if err != nil {
		return query, fmt.Errorf("failed to create vision processor: %w", err)
	}

	// Process any images found in the text
	enhancedQuery, analyses, err := processor.ProcessImagesInText(query)
	if err != nil {
		return query, fmt.Errorf("failed to process images: %w", err)
	}

	// If images were processed, log the enhancement
	if len(analyses) > 0 {
		a.debugLog("🖼️ Processed %d image(s) and enhanced query with vision analysis\n", len(analyses))
		for _, analysis := range analyses {
			a.debugLog("  - %s: %s\n", analysis.ImagePath, analysis.Description[:min(100, len(analysis.Description))])
		}
	}

	return enhancedQuery, nil
}

// containsFrontendKeywords checks if the query contains frontend-related keywords
func containsFrontendKeywords(query string) bool {
	// High-priority frontend indicators
	highPriorityKeywords := []string{
		"react", "vue", "angular", "nextjs", "next.js", "svelte",
		"app", "website", "webpage", "web app", "web application",
		"frontend", "front-end", "ui", "user interface", "interface",
		"layout", "design", "responsive", "mobile-first",
		"css", "html", "styling", "styles", "stylesheet",
		"component", "components", "widget", "widgets",
		"dashboard", "landing page", "homepage", "navigation",
		"mockup", "wireframe", "prototype", "screenshot",
	}

	// Secondary frontend indicators
	secondaryKeywords := []string{
		"colors", "palette", "theme", "branding",
		"bootstrap", "tailwind", "material", "chakra",
		"sass", "scss", "less", "styled-components",
		"button", "form", "input", "modal", "dropdown",
		"header", "footer", "sidebar", "menu",
		"grid", "flexbox", "margin", "padding", "border",
		"typography", "font", "text", "heading",
		"animation", "transition", "hover", "interactive",
	}

	queryLower := strings.ToLower(query)

	// Check high-priority keywords first (any match = frontend)
	for _, keyword := range highPriorityKeywords {
		if strings.Contains(queryLower, keyword) {
			return true
		}
	}

	// Check for multiple secondary keywords (2+ matches = frontend)
	matches := 0
	for _, keyword := range secondaryKeywords {
		if strings.Contains(queryLower, keyword) {
			matches++
			if matches >= 2 {
				return true
			}
		}
	}

	return false
}
