package agent

import (
	"fmt"
	"os"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/factory"
)

// GetModel gets the current model being used by the agent
func (a *Agent) GetModel() string {
	// Use the interface method to get the model
	return a.client.GetModel()
}

// GetProvider returns the current provider name
func (a *Agent) GetProvider() string {
	return a.client.GetProvider()
}

// GetProviderType returns the current provider type
func (a *Agent) GetProviderType() api.ClientType {
	return a.clientType
}

// selectDefaultModel chooses an appropriate default model from available models
func (a *Agent) selectDefaultModel(models []api.ModelInfo, provider api.ClientType) string {
	// If there are no models, return empty
	if len(models) == 0 {
		return ""
	}

	// Provider-specific logic to select best default model
	switch provider {
	case api.DeepInfraClientType:
		// Prefer DeepSeek models for DeepInfra
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.ID), "deepseek") && strings.Contains(strings.ToLower(model.ID), "instruct") {
				return model.ID
			}
		}

	case api.OpenRouterClientType:
		// Prefer free models for OpenRouter
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.ID), ":free") {
				return model.ID
			}
		}

	case api.OllamaClientType, api.OllamaLocalClientType:
		// Prefer smaller models for local Ollama
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.ID), "llama3.2") || strings.Contains(strings.ToLower(model.ID), "llama3.1") {
				return model.ID
			}
		}

	case api.OllamaTurboClientType:
		// Prefer gpt-oss models for Ollama Turbo
		for _, model := range models {
			if strings.Contains(strings.ToLower(model.ID), "gpt-oss:20b") {
				return model.ID
			}
		}

	case api.LMStudioClientType:
		// Prefer chat models for LM Studio, skip embedding models
		for _, model := range models {
			if !strings.Contains(strings.ToLower(model.ID), "embedding") &&
				!strings.Contains(strings.ToLower(model.ID), "embed") {
				return model.ID
			}
		}
		// If no non-embedding models found, return the first one
		return models[0].ID
	}

	// Default: return the first model
	return models[0].ID
}

// SetProvider switches to a specific provider with its default or current model
func (a *Agent) SetProvider(provider api.ClientType) error {
	// Get the configured model for this provider
	model := a.configManager.GetModelForProvider(provider)
	if model == "" {
		// If no model configured, try to get the first available model from the provider
		models, err := api.GetModelsForProvider(provider)
		if err == nil && len(models) > 0 {
			// Find a suitable default model
			model = a.selectDefaultModel(models, provider)
			if a.debug {
				a.debugLog("🔍 Auto-selected model %s for provider %s\n", model, api.GetProviderName(provider))
			}
		} else {
			// No models available from API and no model specified
			return fmt.Errorf("no models available from provider %v - please specify a model explicitly", api.GetProviderName(provider))
		}
	}

	// Create a new client with the specified provider
	newClient, err := factory.CreateProviderClient(provider, model)
	if err != nil {
		return fmt.Errorf("failed to create client for provider %s: %w", api.GetProviderName(provider), err)
	}

	// Set debug mode on the new client
	newClient.SetDebug(a.debug)

	// Check connection
	if err := newClient.CheckConnection(); err != nil {
		return fmt.Errorf("connection check failed for provider %s: %w", api.GetProviderName(provider), err)
	}

	// Switch to the new client
	a.client = newClient
	a.clientType = provider

	// Get the actual model being used (might be different due to fallback)
	actualModel := newClient.GetModel()

	// Save to configuration
	if err := a.configManager.SetProvider(provider); err != nil {
		return fmt.Errorf("failed to save provider: %w", err)
	}
	if err := a.configManager.SetModelForProvider(provider, actualModel); err != nil {
		return fmt.Errorf("failed to save model: %w", err)
	}

	// Update context limits for the new model
	a.maxContextTokens = a.getModelContextLimit()
	a.currentContextTokens = 0

	// Notify user if model was different due to fallback
	if actualModel != model {
		fmt.Fprintf(os.Stderr, "ℹ️  Using model: %s (requested: %s)\n", actualModel, model)
	}

	if a.debug {
		a.debugLog("✅ Switched to provider %s with model %s\n", api.GetProviderName(provider), actualModel)
	}

	return nil
}

// SetModel changes the current model and persists the choice
func (a *Agent) SetModel(model string) error {
	// Use the current provider - we don't need to determine it
	// The user has already selected the provider via /providers select

	// Verify the model exists for the current provider
	models, err := a.getModelsForProvider(a.clientType)
	if err != nil {
		return fmt.Errorf("failed to get models for current provider %s: %w", api.GetProviderName(a.clientType), err)
	}

	// Check if the model exists (case-insensitive)
	modelFound := false
	for _, m := range models {
		if strings.EqualFold(m.ID, model) {
			modelFound = true
			// Use the exact model ID from the provider's list
			model = m.ID
			break
		}
	}

	if !modelFound {
		return fmt.Errorf("model %s not found for provider %s", model, api.GetProviderName(a.clientType))
	}

	// Update the model on the current client
	if err := a.client.SetModel(model); err != nil {
		return fmt.Errorf("failed to set model on client: %w", err)
	}

	// Save the selection
	if err := a.configManager.SetProvider(a.clientType); err != nil {
		// Log warning but don't fail - this is not critical
		if a.debug {
			a.debugLog("⚠️  Failed to save provider: %v\n", err)
		}
	}
	if err := a.configManager.SetModelForProvider(a.clientType, model); err != nil {
		// Log warning but don't fail - this is not critical
		if a.debug {
			a.debugLog("⚠️  Failed to save model: %v\n", err)
		}
	}

	// Update context limits for the new model
	a.maxContextTokens = a.getModelContextLimit()
	a.currentContextTokens = 0

	return nil
}

// getModelsForProvider gets models for a specific provider without environment manipulation
func (a *Agent) getModelsForProvider(provider api.ClientType) ([]api.ModelInfo, error) {
	// Check if provider is available first
	if !a.isProviderAvailable(provider) {
		return nil, fmt.Errorf("provider %s not available", api.GetProviderName(provider))
	}

	// Use the same logic as the main API to avoid discrepancies
	// This eliminates complex environment variable manipulation
	return api.GetModelsForProvider(provider)
}

// isProviderAvailable checks if a provider is currently available
func (a *Agent) isProviderAvailable(provider api.ClientType) bool {
	// Use the unified IsProviderAvailable function
	return api.IsProviderAvailable(provider)
}
