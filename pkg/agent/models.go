package agent

import (
	"fmt"
	"os"

	"github.com/alantheprice/ledit/pkg/agent_api"
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
		api.OpenRouterClientType,  // Check OpenRouter first as it has most models
		api.DeepInfraClientType,
		api.CerebrasClientType,
		api.GroqClientType,
		api.DeepSeekClientType,
		api.OllamaClientType,      // Check Ollama last as it's local
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
		
		// Check if this provider has the model
		for _, model := range models {
			if model.ID == modelID {
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
	
	// For each provider, directly call the appropriate function based on current environment
	// This avoids the complexity of environment manipulation
	switch provider {
	case api.OpenRouterClientType:
		if os.Getenv("OPENROUTER_API_KEY") != "" {
			// Backup all other keys temporarily 
			deepinfraKey := os.Getenv("DEEPINFRA_API_KEY")
			cerebrasKey := os.Getenv("CEREBRAS_API_KEY")
			groqKey := os.Getenv("GROQ_API_KEY")
			deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
			
			// Clear other keys temporarily
			os.Unsetenv("DEEPINFRA_API_KEY")
			os.Unsetenv("CEREBRAS_API_KEY")
			os.Unsetenv("GROQ_API_KEY")
			os.Unsetenv("DEEPSEEK_API_KEY")
			
			// Get OpenRouter models
			models, err := api.GetAvailableModels()
			
			// Restore other keys
			if deepinfraKey != "" {
				os.Setenv("DEEPINFRA_API_KEY", deepinfraKey)
			}
			if cerebrasKey != "" {
				os.Setenv("CEREBRAS_API_KEY", cerebrasKey)
			}
			if groqKey != "" {
				os.Setenv("GROQ_API_KEY", groqKey)
			}
			if deepseekKey != "" {
				os.Setenv("DEEPSEEK_API_KEY", deepseekKey)
			}
			
			return models, err
		}
		return nil, fmt.Errorf("OPENROUTER_API_KEY not set")
		
	case api.DeepInfraClientType:
		if os.Getenv("DEEPINFRA_API_KEY") != "" {
			// Similar approach for DeepInfra
			openrouterKey := os.Getenv("OPENROUTER_API_KEY")
			cerebrasKey := os.Getenv("CEREBRAS_API_KEY")
			groqKey := os.Getenv("GROQ_API_KEY")
			deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
			
			os.Unsetenv("OPENROUTER_API_KEY")
			os.Unsetenv("CEREBRAS_API_KEY")
			os.Unsetenv("GROQ_API_KEY")
			os.Unsetenv("DEEPSEEK_API_KEY")
			
			models, err := api.GetAvailableModels()
			
			if openrouterKey != "" {
				os.Setenv("OPENROUTER_API_KEY", openrouterKey)
			}
			if cerebrasKey != "" {
				os.Setenv("CEREBRAS_API_KEY", cerebrasKey)
			}
			if groqKey != "" {
				os.Setenv("GROQ_API_KEY", groqKey)
			}
			if deepseekKey != "" {
				os.Setenv("DEEPSEEK_API_KEY", deepseekKey)
			}
			
			return models, err
		}
		return nil, fmt.Errorf("DEEPINFRA_API_KEY not set")
		
	case api.CerebrasClientType:
		if os.Getenv("CEREBRAS_API_KEY") != "" {
			openrouterKey := os.Getenv("OPENROUTER_API_KEY")
			deepinfraKey := os.Getenv("DEEPINFRA_API_KEY")
			groqKey := os.Getenv("GROQ_API_KEY")
			deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
			
			os.Unsetenv("OPENROUTER_API_KEY")
			os.Unsetenv("DEEPINFRA_API_KEY")
			os.Unsetenv("GROQ_API_KEY")
			os.Unsetenv("DEEPSEEK_API_KEY")
			
			models, err := api.GetAvailableModels()
			
			if openrouterKey != "" {
				os.Setenv("OPENROUTER_API_KEY", openrouterKey)
			}
			if deepinfraKey != "" {
				os.Setenv("DEEPINFRA_API_KEY", deepinfraKey)
			}
			if groqKey != "" {
				os.Setenv("GROQ_API_KEY", groqKey)
			}
			if deepseekKey != "" {
				os.Setenv("DEEPSEEK_API_KEY", deepseekKey)
			}
			
			return models, err
		}
		return nil, fmt.Errorf("CEREBRAS_API_KEY not set")
		
	case api.GroqClientType:
		if os.Getenv("GROQ_API_KEY") != "" {
			openrouterKey := os.Getenv("OPENROUTER_API_KEY")
			deepinfraKey := os.Getenv("DEEPINFRA_API_KEY")
			cerebrasKey := os.Getenv("CEREBRAS_API_KEY")
			deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
			
			os.Unsetenv("OPENROUTER_API_KEY")
			os.Unsetenv("DEEPINFRA_API_KEY")
			os.Unsetenv("CEREBRAS_API_KEY")
			os.Unsetenv("DEEPSEEK_API_KEY")
			
			models, err := api.GetAvailableModels()
			
			if openrouterKey != "" {
				os.Setenv("OPENROUTER_API_KEY", openrouterKey)
			}
			if deepinfraKey != "" {
				os.Setenv("DEEPINFRA_API_KEY", deepinfraKey)
			}
			if cerebrasKey != "" {
				os.Setenv("CEREBRAS_API_KEY", cerebrasKey)
			}
			if deepseekKey != "" {
				os.Setenv("DEEPSEEK_API_KEY", deepseekKey)
			}
			
			return models, err
		}
		return nil, fmt.Errorf("GROQ_API_KEY not set")
		
	case api.DeepSeekClientType:
		if os.Getenv("DEEPSEEK_API_KEY") != "" {
			openrouterKey := os.Getenv("OPENROUTER_API_KEY")
			deepinfraKey := os.Getenv("DEEPINFRA_API_KEY")
			cerebrasKey := os.Getenv("CEREBRAS_API_KEY")
			groqKey := os.Getenv("GROQ_API_KEY")
			
			os.Unsetenv("OPENROUTER_API_KEY")
			os.Unsetenv("DEEPINFRA_API_KEY")
			os.Unsetenv("CEREBRAS_API_KEY")
			os.Unsetenv("GROQ_API_KEY")
			
			models, err := api.GetAvailableModels()
			
			if openrouterKey != "" {
				os.Setenv("OPENROUTER_API_KEY", openrouterKey)
			}
			if deepinfraKey != "" {
				os.Setenv("DEEPINFRA_API_KEY", deepinfraKey)
			}
			if cerebrasKey != "" {
				os.Setenv("CEREBRAS_API_KEY", cerebrasKey)
			}
			if groqKey != "" {
				os.Setenv("GROQ_API_KEY", groqKey)
			}
			
			return models, err
		}
		return nil, fmt.Errorf("DEEPSEEK_API_KEY not set")
		
	case api.OllamaClientType:
		// For Ollama, we need to clear API keys to ensure it's selected
		openrouterKey := os.Getenv("OPENROUTER_API_KEY")
		deepinfraKey := os.Getenv("DEEPINFRA_API_KEY")
		cerebrasKey := os.Getenv("CEREBRAS_API_KEY")
		groqKey := os.Getenv("GROQ_API_KEY")
		deepseekKey := os.Getenv("DEEPSEEK_API_KEY")
		
		os.Unsetenv("OPENROUTER_API_KEY")
		os.Unsetenv("DEEPINFRA_API_KEY")
		os.Unsetenv("CEREBRAS_API_KEY")
		os.Unsetenv("GROQ_API_KEY")
		os.Unsetenv("DEEPSEEK_API_KEY")
		
		models, err := api.GetAvailableModels()
		
		if openrouterKey != "" {
			os.Setenv("OPENROUTER_API_KEY", openrouterKey)
		}
		if deepinfraKey != "" {
			os.Setenv("DEEPINFRA_API_KEY", deepinfraKey)
		}
		if cerebrasKey != "" {
			os.Setenv("CEREBRAS_API_KEY", cerebrasKey)
		}
		if groqKey != "" {
			os.Setenv("GROQ_API_KEY", groqKey)
		}
		if deepseekKey != "" {
			os.Setenv("DEEPSEEK_API_KEY", deepseekKey)
		}
		
		return models, err
		
	default:
		return nil, fmt.Errorf("unknown provider type: %s", provider)
	}
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