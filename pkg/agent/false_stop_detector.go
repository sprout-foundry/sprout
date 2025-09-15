package agent

import (
	"fmt"
	"strings"

	"github.com/alantheprice/ledit/pkg/agent_api"
)

// shouldCheckFalseStop determines if we should check for a false stop
func (a *Agent) shouldCheckFalseStop(response string) bool {
	// Check if feature is enabled
	if !a.falseStopDetectionEnabled {
		return false
	}

	// Only check if:
	// 1. Response is short (under 150 chars)
	// 2. We're early in the conversation (iteration < 10)
	// 3. Not an error message
	// 4. Contains certain indicator phrases

	if len(response) > 150 || a.currentIteration >= 10 {
		return false
	}

	if strings.Contains(strings.ToLower(response), "error") {
		return false
	}

	// Check for indicator phrases that suggest incomplete action
	indicators := []string{
		"i'll examine",
		"i'll analyze",
		"i'll check",
		"i'll look at",
		"let me examine",
		"let me check",
		"let me look at",
		"i'll read",
		"let me read",
	}

	responseLower := strings.ToLower(response)
	for _, indicator := range indicators {
		if strings.Contains(responseLower, indicator) {
			return true
		}
	}

	return false
}

// getFastModelForProvider returns the appropriate fast model for a given provider
func (a *Agent) getFastModelForProvider() (string, api.ClientType) {
	// Get current provider type
	providerType := a.GetProviderType()

	// Return provider-specific fast models
	switch providerType {
	case api.OpenAIClientType:
		return "gpt-4o-mini", api.OpenAIClientType
	case api.OpenRouterClientType:
		return "google/gemini-2.5-flash", api.OpenRouterClientType
	case api.DeepInfraClientType:
		return "google/gemini-2.5-flash", api.DeepInfraClientType
	case api.GroqClientType:
		return "gemma2-9b-it", api.GroqClientType
	case api.DeepSeekClientType:
		return "deepseek-chat", api.DeepSeekClientType
	case api.CerebrasClientType:
		return "llama-3.3-70b", api.CerebrasClientType
	case api.OllamaClientType:
		// For Ollama, use whatever model is configured locally
		return a.GetModel(), api.OllamaClientType
	default:
		// Fallback to OpenAI's fast model
		return "gpt-4o-mini", api.OpenAIClientType
	}
}

// checkFalseStop uses a fast model to determine if the response is a false stop
func (a *Agent) checkFalseStop(response string) (bool, float64) {
	// Create a simple, focused prompt for the fast model
	prompt := fmt.Sprintf(`Analyze this assistant response and determine if it's incomplete.

Response: "%s"

An incomplete response:
- Announces an action (like "I'll examine X") but doesn't do it
- Says it will read/analyze something but stops
- Appears to be cut off mid-task

A complete response:
- Provides conclusions or recommendations
- Completes the announced action
- Is a natural stopping point

Reply with only: "INCOMPLETE" or "COMPLETE"`, response)

	// Get provider-specific fast model
	fastModel, clientType := a.getFastModelForProvider()

	// Create fast client with provider-specific model
	fastClient, err := api.NewUnifiedClientWithModel(clientType, fastModel)
	if err != nil {
		// If we can't create fast client, fall back to not checking
		a.debugLog("Failed to create fast model client (%s/%s): %v\n", clientType, fastModel, err)
		return false, 0.0
	}

	// Send request with minimal token usage
	messages := []api.Message{
		{
			Role:    "user",
			Content: prompt,
		},
	}

	resp, err := fastClient.SendChatRequest(messages, nil, "low")
	if err != nil {
		a.debugLog("Fast model check failed: %v\n", err)
		return false, 0.0
	}

	if len(resp.Choices) == 0 {
		return false, 0.0
	}

	result := strings.TrimSpace(resp.Choices[0].Message.Content)

	// Log the check if in debug mode
	if a.debug {
		a.debugLog("🔍 False stop check: Model=%s/%s, Response='%s', Result='%s', Cost=$%.6f\n",
			clientType, fastModel, response, result, resp.Usage.EstimatedCost)
	}

	// Determine confidence based on response
	if result == "INCOMPLETE" {
		return true, 0.9
	} else if strings.Contains(strings.ToUpper(result), "INCOMPLETE") {
		return true, 0.7
	}

	return false, 0.0
}
