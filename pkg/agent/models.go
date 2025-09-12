package agent

import (
	"fmt"
	"os"
	"strings"

	api "github.com/alantheprice/ledit/pkg/agent_api"
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

// SetModel changes the current model and persists the choice
func (a *Agent) SetModel(model string) error {
	// IMPORTANT: Clear model caches FIRST to ensure fresh model lists when determining provider
	api.ClearModelCaches()

	// Determine which provider this model belongs to
	requiredProvider, err := a.determineProviderForModel(model)
	if err != nil {
		return fmt.Errorf("failed to determine provider for model %s: %w", model, err)
	}

	// Check if we need to switch providers
	if requiredProvider != a.clientType {
		if a.debug {
			a.debugLog("🔄 Switching from %s to %s for model %s\n",
				api.GetProviderName(a.clientType), api.GetProviderName(requiredProvider), model)
		}

		// Create a new client with the required provider
		newClient, err := api.NewUnifiedClientWithModel(requiredProvider, model)
		if err != nil {
			return fmt.Errorf("failed to create client for provider %s: %w", api.GetProviderName(requiredProvider), err)
		}

		// Set debug mode on the new client
		newClient.SetDebug(a.debug)

		// Check connection
		if err := newClient.CheckConnection(); err != nil {
			return fmt.Errorf("connection check failed for provider %s: %w", api.GetProviderName(requiredProvider), err)
		}

		// Switch to the new client
		a.client = newClient
		a.clientType = requiredProvider
	} else {
		// Same provider, just update the model
		if err := a.client.SetModel(model); err != nil {
			return fmt.Errorf("failed to set model on client: %w", err)
		}
	}

	// Save to configuration
	if err := a.configManager.SetProviderAndModel(requiredProvider, model); err != nil {
		return fmt.Errorf("failed to save model selection: %w", err)
	}

	// Update context limits for the new model
	a.maxContextTokens = a.getModelContextLimit()
	a.currentContextTokens = 0

	return nil
}

// determineProviderForModel determines which provider a model belongs to by checking all available models
func (a *Agent) determineProviderForModel(modelID string) (api.ClientType, error) {
	// Get all available models from all providers
	allProviders := []api.ClientType{
		api.OpenRouterClientType, // Check OpenRouter first as it has most models
		api.OpenAIClientType,
		api.DeepInfraClientType,
		api.CerebrasClientType,
		api.GroqClientType,
		api.DeepSeekClientType,
		api.OllamaClientType, // Check Ollama last as it's local
	}

	if a.debug {
		a.debugLog("🔍 Searching for model %s across providers\n", modelID)
	}

	for _, provider := range allProviders {
		if a.debug {
			a.debugLog("🔍 Checking provider: %s\n", api.GetProviderName(provider))
		}

		// Check if this provider is available
		if !a.isProviderAvailable(provider) {
			if a.debug {
				a.debugLog("❌ Provider %s not available\n", api.GetProviderName(provider))
			}
			continue
		}

		if a.debug {
			a.debugLog("✅ Provider %s is available, checking models\n", api.GetProviderName(provider))
		}

		// Get models for this provider
		models, err := a.getModelsForProvider(provider)
		if err != nil {
			if a.debug {
				a.debugLog("❌ Failed to get models for %s: %v\n", api.GetProviderName(provider), err)
			}
			continue
		}

		if a.debug {
			a.debugLog("✅ Got %d models from %s\n", len(models), api.GetProviderName(provider))
		}

		// Check if this provider has the model (case-insensitive matching)
		for _, model := range models {
			if strings.EqualFold(model.ID, modelID) {
				if a.debug {
					a.debugLog("🎉 Found model %s in provider %s\n", modelID, api.GetProviderName(provider))
				}
				return provider, nil
			}
		}

		if a.debug {
			a.debugLog("❌ Model %s not found in provider %s\n", modelID, api.GetProviderName(provider))
		}
	}

	return "", fmt.Errorf("model %s not found in any available provider", modelID)
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
	// For Ollama, check if it's running
	if provider == api.OllamaClientType {
		client, err := api.NewUnifiedClient(api.OllamaClientType)
		if err != nil {
			return false
		}
		return client.CheckConnection() == nil
	}

	// For other providers, check if API key is set
	envVar := a.getProviderEnvVar(provider)
	if envVar == "" {
		return false
	}

	return os.Getenv(envVar) != ""
}
