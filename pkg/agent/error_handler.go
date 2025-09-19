package agent

import (
	"fmt"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/utils"
)

// ErrorHandler handles API errors and failures
type ErrorHandler struct {
	agent *Agent
}

// NewErrorHandler creates a new error handler
func NewErrorHandler(agent *Agent) *ErrorHandler {
	return &ErrorHandler{
		agent: agent,
	}
}

// HandleAPIFailure handles API failures appropriately based on context
func (eh *ErrorHandler) HandleAPIFailure(apiErr error, messages []api.Message) (string, error) {
	// Count completed work
	toolsExecuted := eh.countToolsExecuted(messages)

	eh.agent.debugLog("⚠️ API request failed after %d tools executed (tokens: %s). Preserving conversation context.\n",
		toolsExecuted, eh.formatTokenCount(eh.agent.totalTokens))

	// In non-interactive mode, fail fast
	if !eh.agent.IsInteractiveMode() || os.Getenv("CI") != "" || os.Getenv("GITHUB_ACTIONS") != "" {
		errorMsg := fmt.Sprintf("API request failed after %d tools executed: %v", toolsExecuted, apiErr)

		if toolsExecuted > 0 {
			errorMsg += fmt.Sprintf(" (Progress: %d tools executed, %s tokens used)",
				toolsExecuted, eh.formatTokenCount(eh.agent.totalTokens))
		}

		return "", fmt.Errorf("%s", errorMsg)
	}

	// In interactive mode, return helpful error message
	return eh.buildInteractiveErrorResponse(apiErr, toolsExecuted), nil
}

// buildInteractiveErrorResponse builds a user-friendly error response
func (eh *ErrorHandler) buildInteractiveErrorResponse(apiErr error, toolsExecuted int) string {
	response := "\n🚨 **API Request Failed - Conversation Preserved**\n\n"

	// Classify error type
	errorMsg := apiErr.Error()
	response += eh.classifyError(errorMsg)

	// Add progress information
	response += "**Progress So Far:**\n"
	response += fmt.Sprintf("- Tools executed: %d\n", toolsExecuted)
	response += fmt.Sprintf("- Total tokens used: %s\n", eh.formatTokenCount(eh.agent.totalTokens))
	response += fmt.Sprintf("- Current iteration: %d/%d\n\n", eh.agent.currentIteration, eh.agent.maxIterations)

	// Add recovery options
	response += "🔄 **Your conversation context is preserved.** You can:\n"
	response += "- Ask me to continue with your original request\n"
	response += "- Ask a more specific question about what you wanted to know\n"
	response += "- Ask me to summarize what I've learned so far\n"
	response += "- Try a different approach to your question\n\n"
	response += "💡 What would you like me to do next?"

	return response
}

// classifyError returns user-friendly error explanation
func (eh *ErrorHandler) classifyError(errorMsg string) string {
	if strings.Contains(errorMsg, "timeout") || strings.Contains(errorMsg, "deadline exceeded") {
		return "The API request timed out, likely due to high server load or a complex request.\n\n"
	}

	if strings.Contains(errorMsg, "rate limit") || strings.Contains(errorMsg, "usage limit") || strings.Contains(errorMsg, "429") {
		// Log rate limit details
		eh.logRateLimit(errorMsg)
		return "Hit API rate limits. Please wait a moment before continuing.\n\n"
	}

	if strings.Contains(strings.ToLower(errorMsg), "model") &&
		(strings.Contains(strings.ToLower(errorMsg), "not exist") ||
			strings.Contains(strings.ToLower(errorMsg), "not found") ||
			strings.Contains(strings.ToLower(errorMsg), "invalid")) {
		return fmt.Sprintf("The model '%s' is not available. Please check:\n"+
			"- Model name is correct\n"+
			"- Your account has access to this model\n"+
			"- The model is available in your region\n\n", eh.agent.GetModel())
	}

	if strings.Contains(errorMsg, "401") || strings.Contains(errorMsg, "unauthorized") ||
		strings.Contains(errorMsg, "authentication") || strings.Contains(errorMsg, "api key") {
		return "The API key was rejected. Please check:\n" +
			"- Your API key is correct and active\n" +
			"- The API key has access to the selected model\n" +
			"- Your account has sufficient credits/quota\n\n"
	}

	return fmt.Sprintf("API error: %s\n\n", errorMsg)
}

// countToolsExecuted counts how many tools were executed
func (eh *ErrorHandler) countToolsExecuted(messages []api.Message) int {
	count := 0
	for _, msg := range messages {
		if msg.Role == "tool" {
			count++
		}
	}
	return count
}

// formatTokenCount formats token count for display
func (eh *ErrorHandler) formatTokenCount(tokens int) string {
	if tokens >= 1000 {
		return fmt.Sprintf("%dk", tokens/1000)
	}
	return fmt.Sprintf("%d", tokens)
}

// logRateLimit logs rate limit information
func (eh *ErrorHandler) logRateLimit(errorMsg string) {
	logger := utils.GetLogger(false)
	logger.LogProcessStep(fmt.Sprintf("🚨 RATE LIMIT HIT: %s | Total tokens: %s | Provider: %s | Model: %s",
		errorMsg, eh.formatTokenCount(eh.agent.totalTokens), eh.agent.GetProvider(), eh.agent.GetModel()))

	if rl := utils.GetRunLogger(); rl != nil {
		rl.LogEvent("rate_limit_hit", map[string]any{
			"provider":       eh.agent.GetProvider(),
			"model":          eh.agent.GetModel(),
			"total_tokens":   eh.agent.totalTokens,
			"error_message":  errorMsg,
			"tools_executed": eh.countToolsExecuted(eh.agent.messages),
			"timestamp":      time.Now().Format(time.RFC3339),
		})
	}
}
