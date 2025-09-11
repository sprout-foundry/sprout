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
	
	// Initialize with system prompt and processed user query
	a.messages = []api.Message{
		{Role: "system", Content: a.systemPrompt},
		{Role: "user", Content: processedQuery},
	}

	a.currentIteration = 0

	for a.currentIteration < a.maxIterations {
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
			return "", fmt.Errorf("API request failed: %w", err)
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
			// Execute each tool call
			toolResults := make([]string, 0)
			for _, toolCall := range choice.Message.ToolCalls {
				result, err := a.executeTool(toolCall)
				if err != nil {
					result = fmt.Sprintf("Error executing tool %s: %s", toolCall.Function.Name, err.Error())
				}
				toolResults = append(toolResults, fmt.Sprintf("Tool call result for %s: %s", toolCall.Function.Name, result))
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

			// Check if the response looks incomplete and retry
			if a.isIncompleteResponse(choice.Message.Content) {
				// Add encouragement to continue
				a.messages = append(a.messages, api.Message{
					Role: "user",
					Content: "The previous response appears incomplete. Please continue with the task and use available tools to fully complete the work.",
				})
				continue
			}

			// No tool calls and response seems complete - we're done
			return choice.Message.Content, nil
		}
	}

	return "", fmt.Errorf("maximum iterations (%d) reached without completion", a.maxIterations)
}

// ProcessQueryWithContinuity processes a query with continuity from previous actions
func (a *Agent) ProcessQueryWithContinuity(userQuery string) (string, error) {
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
	
	content = strings.ToLower(content)
	
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
			if strings.Contains(content, pattern) {
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
		if strings.Contains(content, pattern) {
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
	// For now, return all tools but could be optimized to return only relevant ones
	// based on recent conversation context
	allTools := api.GetToolDefinitions()
	
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