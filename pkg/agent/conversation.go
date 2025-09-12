package agent

import (
	"fmt"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_api"
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

	// Dynamic iteration approach - natural termination for complex tasks, safety limits for simple questions
	var maxIterationsForThisQuery int
	var isSimpleQuestion bool

	// Check if query is a simple question that needs safety limits
	queryLower := strings.ToLower(processedQuery)
	if strings.Contains(queryLower, "what") || strings.Contains(queryLower, "how") ||
		strings.Contains(queryLower, "where") || strings.Contains(queryLower, "explain") ||
		strings.Contains(queryLower, "show me") || strings.Contains(processedQuery, "?") {
		maxIterationsForThisQuery = 25 // Safety limit for simple questions
		isSimpleQuestion = true
		a.debugLog("❓ Simple question detected - safety limit: %d iterations\n", maxIterationsForThisQuery)
	} else {
		maxIterationsForThisQuery = 1000 // Effectively unlimited for complex work
		isSimpleQuestion = false
		a.debugLog("🎯 Complex task detected - natural termination (up to %d iterations)\n", maxIterationsForThisQuery)
	}

	// Initialize with system prompt (enhanced with iteration context) and processed user query
	contextualSystemPrompt := a.systemPrompt

	// Add specific guidance based on query type
	if isSimpleQuestion {
		// Simple question - emphasize immediate answering with safety limit
		contextualSystemPrompt += fmt.Sprintf(`

## SIMPLE QUESTION MODE - ANSWER EFFICIENTLY (safety limit: %d iterations)
🎯 **PRIMARY GOAL**: Answer the question as efficiently as possible

**CRITICAL RULES:**
1. **ANSWER IMMEDIATELY** when you find relevant information - do NOT continue exploring
2. **STOP AFTER 2-3 iterations** if you have enough to answer the question
3. **PREFER QUICK ANSWERS** over exhaustive research
4. **ONE BATCH READ** after discovery - then answer with what you found

**Process:**
- Use targeted search (grep/find) to locate relevant files
- Read 1-3 most relevant files in ONE batch
- **ANSWER THE QUESTION** immediately with the information found
- Do NOT read additional files unless the answer is incomplete

Current status: Starting iteration 1`, maxIterationsForThisQuery)
	} else {
		// Complex work - natural termination approach
		contextualSystemPrompt += `

## COMPLEX TASK MODE - NATURAL TERMINATION
🎯 **PRIMARY GOAL**: Complete the task thoroughly and correctly

**APPROACH:**
- Work systematically through the task until genuinely complete
- Use structured approach with todo tools for complex multi-step work
- Continue iterating until the task is properly finished
- **NATURAL COMPLETION**: Stop when no more tools are needed and task is done

**TERMINATION CRITERIA:**
- All requirements have been met and verified
- Code compiles/runs successfully (if applicable)
- Tests pass (if applicable)
- No remaining errors or issues to address
- Task is genuinely complete, not just partially done

Current status: Starting iteration 1 (natural termination)`
	}

	a.messages = []api.Message{
		{Role: "system", Content: contextualSystemPrompt},
		{Role: "user", Content: processedQuery},
	}

	for a.currentIteration < maxIterationsForThisQuery {
		iterationStart := time.Now()
		a.currentIteration++

		// Check for interrupt signal at the start of each iteration
		if a.CheckForInterrupt() {
			interruptMessage := a.HandleInterrupt()
			if interruptMessage != "" {
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

		// Check if we're approaching the context limit (80%)
		contextThreshold := int(float64(a.maxContextTokens) * 0.8)
		if contextTokens > contextThreshold {
			if !a.contextWarningIssued {
				a.debugLog("⚠️  Context approaching limit: %s/%s (%.1f%%)\n",
					a.formatTokenCount(contextTokens),
					a.formatTokenCount(a.maxContextTokens),
					float64(contextTokens)/float64(a.maxContextTokens)*100)
				a.contextWarningIssued = true
			}

			// Perform aggressive optimization when near limit
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
		resp, err := a.client.SendChatRequest(optimizedMessages, tools, reasoningEffort)
		if err != nil {
			// IMPROVED: Preserve conversation context on API failures instead of losing everything
			return a.handleAPIFailure(err, optimizedMessages)
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

		// Calculate cost savings for display purposes only
		cachedCostSavings := a.calculateCachedCost(cachedTokens)
		a.cachedCostSavings += cachedCostSavings

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

			// Check if we're using structured workflow tools - if so, remove iteration limits
			// Todo tools indicate the LLM is making progress through a task systematically
			for _, toolCall := range choice.Message.ToolCalls {
				if toolCall.Function.Name == "add_todos" ||
					toolCall.Function.Name == "update_todo_status" ||
					toolCall.Function.Name == "list_todos" {
					if maxIterationsForThisQuery < 1000 {
						maxIterationsForThisQuery = 1000 // Effectively unlimited
						isSimpleQuestion = false         // Treat as complex task - todo usage shows productive work
						a.debugLog("📋 Todo workflow detected - removing iteration limits (up to %d iterations)\n", maxIterationsForThisQuery)
					}
				}
			}

			// Add tool results to conversation with iteration context
			iterationStatus := fmt.Sprintf("\n\n**ITERATION STATUS: %d of %d complete**", a.currentIteration, maxIterationsForThisQuery)
			toolResultsWithContext := strings.Join(toolResults, "\n\n") + iterationStatus
			a.messages = append(a.messages, api.Message{
				Role:    "user",
				Content: toolResultsWithContext,
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
				a.debugLog("Found malformed tool calls in content, executing them and providing feedback\n")

				toolResults := make([]string, 0)
				for _, toolCall := range toolCalls {
					result, err := a.executeTool(toolCall)
					if err != nil {
						result = fmt.Sprintf("Error executing tool %s: %s", toolCall.Function.Name, err.Error())
					}
					toolResults = append(toolResults, fmt.Sprintf("Tool call result for %s: %s", toolCall.Function.Name, result))
				}

				// Add tool results and feedback about proper tool calling to conversation
				feedbackMessage := fmt.Sprintf(`%s

⚠️ IMPORTANT: I detected and executed tool calls from your text response, but you should use proper tool calling format instead of outputting tool calls as text. 

Please use the structured tool calling format for all future tool calls, not text output. Continue with your task using proper tool calls.`, strings.Join(toolResults, "\n\n"))

				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: feedbackMessage,
				})

				continue
			}

			// Check if this looks like attempted tool calls that we couldn't parse
			if a.containsAttemptedToolCalls(choice.Message.Content) || a.containsAttemptedToolCalls(choice.Message.ReasoningContent) {
				a.debugLog("Detected attempted but unparseable tool calls, requesting retry with correct syntax\n")

				retryMessage := `⚠️ I detected what appears to be attempted tool calls in your response, but I couldn't parse them properly. 

Please retry using the correct tool calling format:
- Use proper structured tool calls, not JSON in text
- Each tool call should use the function calling interface
- Do not output tool calls as text or in code blocks

Please continue with your task using the correct tool calling syntax.`

				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: retryMessage,
				})

				continue
			}

			// Check if the response looks incomplete and retry
			if a.isIncompleteResponse(choice.Message.Content) {
				// Add encouragement to continue
				a.messages = append(a.messages, api.Message{
					Role:    "user",
					Content: "The previous response appears incomplete. Please continue with the task and use available tools to fully complete the work.",
				})
				continue
			}

			// No tool calls and response seems complete - we're done
			// Commit any tracked changes before returning
			if commitErr := a.CommitChanges(choice.Message.Content); commitErr != nil {
				a.debugLog("Warning: Failed to commit tracked changes: %v\n", commitErr)
			}
			return choice.Message.Content, nil
		}
	}

	// Commit any tracked changes even if we hit max iterations
	if commitErr := a.CommitChanges("Maximum iterations reached"); commitErr != nil {
		a.debugLog("Warning: Failed to commit tracked changes: %v\n", commitErr)
	}

	if isSimpleQuestion {
		return "", fmt.Errorf("safety limit (%d iterations) reached for simple question - task may need more specific requirements or the question may be too broad", maxIterationsForThisQuery)
	} else {
		return "", fmt.Errorf("iteration limit (%d) reached - task may be incomplete or need manual intervention to continue", maxIterationsForThisQuery)
	}
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
CONTINUITY FROM PREVIOUS SESSION:
%s

CURRENT TASK:
%s

Please continue working on this task chain, building upon the previous actions.`,
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

	contentLower := strings.ToLower(content)
	originalContent := content // Keep original for case-sensitive checks

	// Check for intent-to-continue phrases that indicate the model wants to keep working
	intentToContinuePatterns := []string{
		"let me",
		"i'll",
		"i will",
		"now i",
		"next, i",
		"first, i",
		"i need to",
		"i should",
		"i'm going to",
		"going to",
		"will now",
		"let's",
		"now let's",
		"i can",
		"i'd like to",
		"i want to",
		"time to",
		"ready to",
		"about to",
		"need to",
		"should now",
	}

	for _, pattern := range intentToContinuePatterns {
		if strings.Contains(contentLower, pattern) {
			a.debugLog("Detected intent-to-continue phrase: '%s'\n", pattern)
			return true
		}
	}

	// Check for trailing colons or incomplete sentences that suggest continuation
	trimmedContent := strings.TrimSpace(originalContent)
	if strings.HasSuffix(trimmedContent, ":") || strings.HasSuffix(trimmedContent, "...") {
		a.debugLog("Detected trailing continuation punctuation\n")
		return true
	}

	// Check for sentences that end with action words suggesting more to come
	actionEndingPatterns := []string{
		"file:",
		"files:",
		"code:",
		"function:",
		"method:",
		"class:",
		"component:",
		"module:",
		"directory:",
		"folder:",
		"implementation:",
		"changes:",
		"updates:",
		"modifications:",
	}

	for _, pattern := range actionEndingPatterns {
		if strings.HasSuffix(contentLower, pattern) {
			a.debugLog("Detected action-ending pattern: '%s'\n", pattern)
			return true
		}
	}

	// Common patterns that indicate the agent is giving up too early
	declinePatterns := []string{
		"i'm not able to",
		"i cannot",
		"i can't",
		"not possible to",
		"unable to",
		"can only work with",
		"cannot modify",
		"cannot add",
		"cannot create",
	}

	// If it's a short response with decline language, it's likely incomplete
	if len(content) < 200 {
		for _, pattern := range declinePatterns {
			if strings.Contains(contentLower, pattern) {
				return true
			}
		}
	}

	// If there's no evidence of tool usage or exploration, likely incomplete
	toolEvidencePatterns := []string{
		"ls",
		"read",
		"write",
		"edit",
		"shell",
		"file",
		"directory",
		"explore",
		"implement",
		"create",
	}

	hasToolEvidence := false
	for _, pattern := range toolEvidencePatterns {
		if strings.Contains(contentLower, pattern) {
			hasToolEvidence = true
			break
		}
	}

	// Short response without tool evidence suggests giving up early
	if len(content) < 300 && !hasToolEvidence {
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

	// Create a response that preserves the conversation context and allows the user to continue
	response := "⚠️ **API Request Failed - Conversation Preserved**\n\n"

	// Classify the error type for better user guidance
	errorMsg := apiErr.Error()
	if strings.Contains(errorMsg, "timeout") || strings.Contains(errorMsg, "deadline exceeded") {
		response += "The API request timed out, likely due to high server load or a complex request.\n\n"
	} else if strings.Contains(errorMsg, "rate limit") {
		response += "Hit API rate limits. Please wait a moment before continuing.\n\n"
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

	// Add MCP tools if available
	if a.mcpManager != nil {
		mcpTools := a.getMCPTools()
		allTools = append(allTools, mcpTools...)
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
