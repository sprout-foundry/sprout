package agent

import (
	"fmt"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_tools"
)

// ProcessQuery handles the main conversation loop with the LLM
func (a *Agent) ProcessQuery(userQuery string) (string, error) {
	handler := NewConversationHandler(a)
	return handler.ProcessQuery(userQuery)
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

		// Auto-save memory state after every successful turn
		a.autoSaveState()
		a.debugLog("DEFER: Auto-saved memory state\n")
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

// Helper methods that are still needed by various components

// isIncompleteResponse checks if a response looks incomplete or is declining the task prematurely
func (a *Agent) isIncompleteResponse(content string) bool {
	contentLower := strings.ToLower(content)
	contentLen := len(content)

	// Check if response contains malformed tool calls
	if a.containsAttemptedToolCalls(content) {
		return true
	}

	// Check for very short responses that suggest incompletion
	if contentLen < 100 && (strings.Contains(contentLower, "let me") ||
		strings.Contains(contentLower, "i'll") ||
		strings.Contains(contentLower, "i will")) {
		return true
	}

	// Check for responses that suggest the agent is waiting for confirmation
	incompletePatterns := []string{
		"would you like me to",
		"should i proceed",
		"shall i",
		"do you want me to",
		"before i continue",
		"let me know if",
		"is this what you",
		"does this look",
		"if you'd like",
		"i can help you",
		"i understand you",
		"to get started",
		"to begin",
		"before we proceed",
		"ready to help",
		"happy to assist",
		"glad to help",
		"i'm here to",
	}

	for _, pattern := range incompletePatterns {
		if strings.Contains(contentLower, pattern) {
			// But allow if the response is already substantial
			if contentLen > 500 {
				continue
			}
			return true
		}
	}

	// Check for responses that end with ellipsis or other continuation indicators
	trimmedContent := strings.TrimSpace(content)
	if strings.HasSuffix(trimmedContent, "...") ||
		strings.HasSuffix(trimmedContent, "…") ||
		strings.HasSuffix(trimmedContent, ":") ||
		strings.HasSuffix(trimmedContent, "-") {
		return true
	}

	return false
}

// handleAPIFailure preserves conversation context when API calls fail
func (a *Agent) handleAPIFailure(apiErr error, _ []api.Message) (string, error) {
	// Log the error for debugging
	a.debugLog("🚨 API Error: %v\n", apiErr)

	// Extract error message
	errMsg := apiErr.Error()

	// Check for specific error types and provide helpful context
	if strings.Contains(errMsg, "timeout") {
		return "", fmt.Errorf("Request timed out. The model took too long to respond. Try:\n  - Using a simpler query\n  - Breaking down complex tasks\n  - Checking your internet connection\n\nOriginal error: %v", apiErr)
	}

	if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "quota") {
		return "", fmt.Errorf("API rate limit reached. Try:\n  - Waiting a few moments before retrying\n  - Using a different API key\n  - Checking your usage limits\n\nOriginal error: %v", apiErr)
	}

	if strings.Contains(errMsg, "context length") || strings.Contains(errMsg, "too many tokens") {
		// Try to provide helpful context about what to do
		return "", fmt.Errorf("Context too long for model. The conversation history exceeds the model's limits. Try:\n  - Starting a new conversation with 'ledit agent --fresh'\n  - Using a model with larger context (e.g., claude-3-opus)\n  - Summarizing previous work before continuing\n\nOriginal error: %v", apiErr)
	}

	if strings.Contains(errMsg, "authentication") || strings.Contains(errMsg, "unauthorized") {
		return "", fmt.Errorf("Authentication failed. Check:\n  - Your API key is correctly set\n  - The API key has proper permissions\n  - Your subscription/credits are active\n\nOriginal error: %v", apiErr)
	}

	if strings.Contains(errMsg, "model not found") || strings.Contains(errMsg, "does not exist") {
		return "", fmt.Errorf("Model not available. The requested model may:\n  - Require special access\n  - Be deprecated or renamed\n  - Have a typo in the name\n\nOriginal error: %v", apiErr)
	}

	// Connection errors
	if strings.Contains(errMsg, "connection") || strings.Contains(errMsg, "network") ||
		strings.Contains(errMsg, "dial") || strings.Contains(errMsg, "EOF") {
		return "", fmt.Errorf("Network connection error. Check:\n  - Your internet connection\n  - Any firewall or proxy settings\n  - The API service status\n\nOriginal error: %v", apiErr)
	}

	// Service errors
	if strings.Contains(errMsg, "500") || strings.Contains(errMsg, "502") ||
		strings.Contains(errMsg, "503") || strings.Contains(errMsg, "internal") {
		return "", fmt.Errorf("API service error. The service is experiencing issues. Try:\n  - Waiting a few minutes and retrying\n  - Checking the provider's status page\n  - Using a different model or provider\n\nOriginal error: %v", apiErr)
	}

	// Generic error with conversation preservation advice
	return "", fmt.Errorf("API request failed: %v\n\nYour conversation has been preserved. You can:\n  - Retry the same query\n  - Continue with a different approach\n  - Check logs with --debug flag for more details", apiErr)
}

// containsAttemptedToolCalls checks if content contains patterns that suggest attempted tool calls
func (a *Agent) containsAttemptedToolCalls(content string) bool {
	// Look for common patterns that indicate attempted tool usage
	patterns := []string{
		// JSON-like tool call patterns
		`"tool":`,
		`"function":`,
		`"name":`,
		`"arguments":`,
		`{"tool_calls"`,
		`"tool_calls":`,
		// XML/Claude-style patterns
		`<function=`,
		`</function>`,
		`<tool>`,
		`</tool>`,
		// Code block tool patterns
		"```tool",
		"```function",
		// Direct tool invocation patterns
		"TOOL:",
		"FUNCTION:",
		"Calling:",
		"Executing:",
		"Tool call:",
		"Function call:",
		// Common malformed patterns
		"I'll use the",
		"I'll call the",
		"Let me use",
		"Let me call",
		"Using the.*tool",
		"Calling the.*function",
	}

	contentLower := strings.ToLower(content)
	for _, pattern := range patterns {
		if strings.Contains(contentLower, strings.ToLower(pattern)) {
			// Make sure it's not just discussing tools in general
			if !strings.Contains(contentLower, "tool calls are") &&
				!strings.Contains(contentLower, "tools can be") &&
				!strings.Contains(contentLower, "available tools") &&
				!strings.Contains(contentLower, "the tool is") {
				return true
			}
		}
	}

	// Check for specific tool names being mentioned in action context
	toolNames := []string{"read_file", "write_file", "shell_command", "search_files", "edit_file"}
	for _, tool := range toolNames {
		if strings.Contains(contentLower, tool) &&
			(strings.Contains(contentLower, "i'll") ||
				strings.Contains(contentLower, "i will") ||
				strings.Contains(contentLower, "let me") ||
				strings.Contains(contentLower, "using") ||
				strings.Contains(contentLower, "calling")) {
			return true
		}
	}

	return false
}

// determineReasoningEffort determines the appropriate reasoning effort level based on the query
func (a *Agent) determineReasoningEffort(messages []api.Message) string {
	// Only certain providers support reasoning effort
	if a.GetProvider() != "openai" && a.GetProvider() != "deepseek" {
		return "" // Default - provider will ignore it
	}

	// Get the last user message
	var lastUserMessage string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			lastUserMessage = messages[i].Content
			break
		}
	}

	if lastUserMessage == "" {
		return "medium" // Default
	}

	queryLower := strings.ToLower(lastUserMessage)

	// High reasoning effort indicators
	highEffortKeywords := []string{
		"algorithm", "optimize", "performance", "complexity",
		"architect", "design pattern", "refactor", "security",
		"analyze", "debug", "trace", "investigate",
		"compare", "evaluate", "trade-off", "decision",
		"implement", "integrate", "migrate", "transform",
		"explain why", "explain how", "deep dive", "comprehensive",
		"edge case", "corner case", "test case", "validation",
		"best practice", "recommendation", "strategy",
		"fix", "solve", "resolve", "troubleshoot",
		"create", "build", "develop", "construct",
	}

	// Low reasoning effort indicators
	lowEffortKeywords := []string{
		"what is", "define", "list", "show", "display",
		"tell me", "give me", "provide", "fetch",
		"simple", "basic", "quick", "brief",
		"yes or no", "true or false", "check if",
		"count", "how many", "number of",
		"rename", "move", "copy", "delete",
		"format", "indent", "spacing", "style",
		"typo", "spelling", "grammar",
		"comment", "document", "annotate",
	}

	// Count matches
	highMatches := 0
	lowMatches := 0

	for _, keyword := range highEffortKeywords {
		if strings.Contains(queryLower, keyword) {
			highMatches++
		}
	}

	for _, keyword := range lowEffortKeywords {
		if strings.Contains(queryLower, keyword) {
			lowMatches++
		}
	}

	// Determine effort level based on matches and query characteristics
	if highMatches >= 2 || (highMatches > lowMatches && len(lastUserMessage) > 100) {
		return "high"
	} else if lowMatches >= 2 || (lowMatches > highMatches) {
		return "low"
	}

	// Check query length as additional factor
	if len(lastUserMessage) > 200 {
		return "high" // Complex queries likely need more reasoning
	} else if len(lastUserMessage) < 50 {
		return "low" // Short queries are usually simple
	}

	return "medium" // Default for balanced tasks
}

// getOptimizedToolDefinitions returns tool definitions optimized based on conversation context
func (a *Agent) getOptimizedToolDefinitions(messages []api.Message) []api.Tool {
	// Start with standard tools
	tools := api.GetToolDefinitions()

	// Add MCP tools if available
	mcpTools := a.getMCPTools()
	if mcpTools != nil {
		tools = append(tools, mcpTools...)
	}

	// Future: Could optimize by analyzing conversation context
	// and only returning relevant tools
	return tools
}

// ClearConversationHistory clears the conversation history
func (a *Agent) ClearConversationHistory() {
	a.messages = []api.Message{
		{Role: "system", Content: a.systemPrompt},
	}
	a.currentIteration = 0
	a.previousSummary = ""
	a.debugLog("🧹 Conversation history cleared\n")
}

// SetConversationOptimization enables or disables conversation optimization
func (a *Agent) SetConversationOptimization(enabled bool) {
	if a.optimizer != nil {
		a.optimizer.SetEnabled(enabled)
		if enabled {
			a.debugLog("✨ Conversation optimization enabled\n")
		} else {
			a.debugLog("🔧 Conversation optimization disabled\n")
		}
	}
}

// GetOptimizationStats returns optimization statistics
func (a *Agent) GetOptimizationStats() map[string]interface{} {
	if a.optimizer != nil {
		return a.optimizer.GetOptimizationStats()
	}
	return map[string]interface{}{
		"enabled": false,
		"message": "Optimizer not initialized",
	}
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
