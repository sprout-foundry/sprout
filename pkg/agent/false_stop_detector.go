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

	// Skip if we're too late in the conversation
	if a.currentIteration >= 10 {
		return false
	}

	// Skip error messages
	if strings.Contains(strings.ToLower(response), "error") {
		return false
	}

	// Check if response ends with a colon (indicating more to come)
	trimmedResponse := strings.TrimSpace(response)
	if strings.HasSuffix(trimmedResponse, ":") {
		// Extract the last sentence/paragraph to check
		lines := strings.Split(trimmedResponse, "\n")
		lastLine := ""
		for i := len(lines) - 1; i >= 0; i-- {
			if strings.TrimSpace(lines[i]) != "" {
				lastLine = strings.TrimSpace(lines[i])
				break
			}
		}

		// Check if the last line suggests an upcoming action
		lastLineLower := strings.ToLower(lastLine)
		actionIndicators := []string{
			"let me", "i'll", "i need to", "i should", "let's",
			"checking", "looking", "examining", "understanding",
		}

		for _, indicator := range actionIndicators {
			if strings.Contains(lastLineLower, indicator) {
				return true
			}
		}
	}

	// Original check for short responses
	if len(response) <= 150 {
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
	case api.DeepSeekClientType:
		return "deepseek-chat", api.DeepSeekClientType
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
	// For longer responses, focus on the ending
	responseToCheck := response
	if len(response) > 500 {
		// For long responses, check the last 300 characters for context
		startIdx := len(response) - 300
		if startIdx < 0 {
			startIdx = 0
		}
		responseToCheck = "..." + response[startIdx:]
	}

	prompt := fmt.Sprintf(`Analyze this assistant response and determine if it's incomplete.

Response: "%s"

An incomplete response:
- Announces an action (like "I'll examine X") but doesn't do it
- Says it will read/analyze something but stops
- Ends with a colon suggesting more to come
- Appears to be cut off mid-task
- Last sentence indicates an upcoming action that wasn't performed

A complete response:
- Provides conclusions or recommendations
- Completes the announced action
- Is a natural stopping point
- Ends with a complete thought

Pay special attention to responses ending with colons after phrases like "Let me check" or "Let me examine".

Reply with only: "INCOMPLETE" or "COMPLETE"`, responseToCheck)

	// Get provider-specific fast model
	fastModel, clientType := a.getFastModelForProvider()

	// Try fast model first
	fastClient, err := api.NewUnifiedClientWithModel(clientType, fastModel)
	if err != nil {
		// Fall back to main model if fast model fails
		a.debugLog("Failed to create fast model client (%s/%s), falling back to main model: %v\n",
			clientType, fastModel, err)

		// Use the current client's model as fallback
		fastClient = a.client
		fastModel = a.GetModel()
		clientType = a.GetProviderType()
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
		// If fast model fails and we're already using main model, give up
		if fastClient == a.client {
			a.debugLog("False stop check failed with main model fallback: %v\n", err)
			return false, 0.0
		}

		// Try fallback to main model
		a.debugLog("Fast model check failed, trying main model: %v\n", err)
		fastClient = a.client
		fastModel = a.GetModel()

		resp, err = fastClient.SendChatRequest(messages, nil, "low")
		if err != nil {
			a.debugLog("False stop check failed with main model: %v\n", err)
			return false, 0.0
		}
	}

	// Validate response
	if resp == nil || len(resp.Choices) == 0 {
		a.debugLog("False stop check returned empty response\n")
		return false, 0.0
	}

	result := strings.TrimSpace(resp.Choices[0].Message.Content)

	// Validate result format
	resultUpper := strings.ToUpper(result)
	if resultUpper != "INCOMPLETE" && resultUpper != "COMPLETE" &&
		!strings.Contains(resultUpper, "INCOMPLETE") && !strings.Contains(resultUpper, "COMPLETE") {
		a.debugLog("False stop check returned unexpected format: '%s'\n", result)
		return false, 0.0
	}

	// Log the check if in debug mode
	if a.debug {
		cost := 0.0
		if resp.Usage.EstimatedCost > 0 {
			cost = resp.Usage.EstimatedCost
		}
		a.debugLog("🔍 False stop check: Model=%s/%s, Response='%s', Result='%s', Cost=$%.6f\n",
			clientType, fastModel, response, result, cost)
	}

	// Determine confidence based on response
	if resultUpper == "INCOMPLETE" {
		return true, 0.9
	} else if strings.Contains(resultUpper, "INCOMPLETE") {
		return true, 0.7
	}

	return false, 0.0
}
