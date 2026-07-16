package agent

import (
	"fmt"
	"os"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/agent_providers"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/modelcontract"
)

// GetModel gets the current model being used by the agent
func (a *Agent) GetModel() string {
	// Check session override first
	if a.state != nil && a.state.GetSessionModel() != "" {
		return a.state.GetSessionModel()
	}
	// Use the interface method to get the model
	c := a.getClient()
	if c == nil {
		return "unknown"
	}
	return c.GetModel()
}

// GetProvider returns the current provider name
func (a *Agent) GetProvider() string {
	// Check session override first
	if a.state != nil && a.state.GetSessionProvider() != "" {
		return string(a.state.GetSessionProvider())
	}
	c := a.getClient()
	if c == nil {
		return "unknown"
	}
	return c.GetProvider()
}

// GetProviderType returns the current provider type
func (a *Agent) GetProviderType() api.ClientType {
	// Check session override first
	if a.state != nil && a.state.GetSessionProvider() != "" {
		return a.state.GetSessionProvider()
	}
	return a.getClientType()
}

// selectDefaultModel chooses an appropriate default model from available models.
// It prefers probe-recommended candidates first (primary > subagent), then falls
// back to per-provider string-matching heuristics, and finally to the first model.
func (a *Agent) selectDefaultModel(models []api.ModelInfo, provider api.ClientType) string {
	// If there are no models, return empty
	if len(models) == 0 {
		return ""
	}

	// Probe-first: prefer models with RecommendedRoles from the capability probe.
	// Primary (complex stage passed) is the strongest signal; subagent (gates
	// passed) is the next tier. Only use this path if at least one model has
	// probe-backed recommendations — empty RecommendedRoles means un-probed.
	if probe := selectProbeRecommended(models); probe != "" {
		return probe
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

	case api.OllamaCloudClientType:
		// Prefer gpt-oss models for Ollama Cloud
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

// selectProbeRecommended scans the model list for probe-backed recommendations.
// It returns the first model whose RecommendedRoles contains "primary" (strongest
// signal — complex stage passed). If none have "primary", it returns the first
// model with "subagent" (gates passed). Returns "" if no probe-backed candidate
// exists. An empty RecommendedRoles slice means the model was never probed and
// is ignored by this function.
func selectProbeRecommended(models []api.ModelInfo) string {
	var firstSubagent string
	for _, m := range models {
		if modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RolePrimary) {
			return m.ID
		}
		if firstSubagent == "" && modelcontract.RoleHas(m.RecommendedRoles, modelcontract.RoleSubagent) {
			firstSubagent = m.ID
		}
	}
	return firstSubagent
}

// getDefaultModelFromFactory gets the default model from the provider factory for dynamic providers
func (a *Agent) getDefaultModelFromFactory(provider api.ClientType) string {
	providerName := string(provider)

	// Only check factory for dynamic providers (not built-in ones)
	switch provider {
	case api.OpenAIClientType, api.OllamaClientType, api.OllamaLocalClientType, api.OllamaCloudClientType, api.TestClientType:
		return "" // These are built-in providers, don't check factory
	}

	// Create provider factory and load configs
	providerFactory := providers.NewProviderFactory()
	if err := providerFactory.LoadEmbeddedConfigs(); err != nil {
		if a.debug {
			a.Logger().Debug("[WARN] Failed to load provider factory configs: %v\n", err)
		}
		return ""
	}

	// Get provider config
	config, err := providerFactory.GetProviderConfig(providerName)
	if err != nil {
		if a.debug {
			a.Logger().Debug("[WARN] No factory config found for provider %s: %v\n", providerName, err)
		}
		return ""
	}

	// Return the default model from the config
	if config.Defaults.Model != "" {
		return config.Defaults.Model
	}

	return ""
}

// getModelFromCustomProviderConfig returns the ModelName from the custom
// provider configuration if the provider is registered as a custom provider.
// This handles user-defined providers (e.g. ai-worker) that may not expose
// a models list endpoint.
func (a *Agent) getModelFromCustomProviderConfig(provider api.ClientType) string {
	cfg := a.configManager.GetConfig()
	if cfg.CustomProviders == nil {
		return ""
	}
	if cp, exists := cfg.CustomProviders[string(provider)]; exists && cp.ModelName != "" {
		return cp.ModelName
	}
	return ""
}

// SetProvider switches to a specific provider with its default or current model.
// Session-scoped: changes are not written to config. Use SetProviderPersisted
// when the user explicitly chose the provider (e.g. CLI /provider command).
func (a *Agent) SetProvider(provider api.ClientType) error {
	prevProvider := a.GetProvider()
	prevModel := a.GetModel()
	availableModels, _ := a.getModelsForProvider(provider)

	// Get the configured model for this provider
	model := a.configManager.GetModelForProvider(provider)
	if model == "" {
		// If no model configured, try to get default model from provider factory
		if defaultModel := a.getDefaultModelFromFactory(provider); defaultModel != "" {
			model = defaultModel
			if a.debug {
				a.Logger().Debug("[search] Using default model %s from factory for provider %s\n", model, api.GetProviderName(provider))
			}
		} else if customModel := a.getModelFromCustomProviderConfig(provider); customModel != "" {
			model = customModel
			if a.debug {
				a.Logger().Debug("[search] Using model %s from custom provider config for provider %s\n", model, api.GetProviderName(provider))
			}
		} else {
			// If no factory default, try to get the first available model from the provider API
			if len(availableModels) > 0 {
				// Find a suitable default model
				model = a.selectDefaultModel(availableModels, provider)
				if a.debug {
					a.Logger().Debug("[search] Auto-selected model %s from API for provider %s\n", model, api.GetProviderName(provider))
				}
			} else {
				// No models available from API and no model specified
				return fmt.Errorf("no models available from provider %s; please specify a model explicitly (e.g. /provider %s:<model-name>)", api.GetProviderName(provider), api.GetProviderName(provider))
			}
		}
	} else if resolvedModel, ok := resolveModelIDForProvider(model, availableModels); ok {
		model = resolvedModel
	} else if len(availableModels) > 0 {
		fallbackModel := a.selectDefaultModel(availableModels, provider)
		if fallbackModel == "" {
			fallbackModel = availableModels[0].ID
		}
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[info] Configured model %s is not available for %s. Using %s.\n", model, api.GetProviderName(provider), fallbackModel)))
		model = fallbackModel
	}

	// Create a new client with the specified provider
	newClient, err := factory.CreateProviderClient(provider, model)
	if err != nil {
		return agenterrors.NewConfig(fmt.Sprintf("failed to create client for provider %s", api.GetProviderName(provider)), err)
	}

	// Set debug mode on the new client
	newClient.SetDebug(a.debug)

	// Connection is validated on the first real request — skip the blocking
	// connection check here so provider/model switches feel instant in the UI.

	// Switch to the new client atomically (both fields under the write lock)
	a.setClient(newClient, provider)

	// Get the actual model being used (might be different due to fallback)
	actualModel := newClient.GetModel()

	// Store in session fields (not config) - this allows session-scoped changes
	// without affecting other sessions or persisting to config
	a.state.SetSessionProvider(provider)
	a.state.SetSessionModel(actualModel)

	// Update context limits for the new model
	a.state.SetMaxContextTokens(a.getModelContextLimit())
	a.state.SetCurrentContextTokens(0)
	a.normalizeConversationForCurrentModelSyntax(prevProvider, prevModel)

	// Notify user if model was different due to fallback
	if actualModel != model {
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[info] Using model: %s (requested: %s)\n", actualModel, model)))
	}

	if a.debug {
		a.Logger().Debug("[OK] Switched to provider %s with model %s\n", api.GetProviderName(provider), actualModel)
	}

	return nil
}

// SetProviderPersisted switches to a specific provider and persists the choice to config.
// This is intended for CLI use where the selection should be saved.
// The test/mock provider is rejected since it should never be the persisted default.
func (a *Agent) SetProviderPersisted(provider api.ClientType) error {
	if provider == api.TestClientType {
		return fmt.Errorf("test provider cannot be persisted as the active provider")
	}

	prevProvider := a.GetProvider()
	prevModel := a.GetModel()
	availableModels, _ := a.getModelsForProvider(provider)

	// Get the configured model for this provider
	model := a.configManager.GetModelForProvider(provider)
	if model == "" {
		// If no model configured, try to get default model from provider factory
		if defaultModel := a.getDefaultModelFromFactory(provider); defaultModel != "" {
			model = defaultModel
			if a.debug {
				a.Logger().Debug("[search] Using default model %s from factory for provider %s\n", model, api.GetProviderName(provider))
			}
		} else if customModel := a.getModelFromCustomProviderConfig(provider); customModel != "" {
			model = customModel
			if a.debug {
				a.Logger().Debug("[search] Using model %s from custom provider config for provider %s\n", model, api.GetProviderName(provider))
			}
		} else {
			// If no factory default, try to get the first available model from the provider API
			if len(availableModels) > 0 {
				// Find a suitable default model
				model = a.selectDefaultModel(availableModels, provider)
				if a.debug {
					a.Logger().Debug("[search] Auto-selected model %s from API for provider %s\n", model, api.GetProviderName(provider))
				}
			} else {
				// No models available from API and no model specified
				return fmt.Errorf("no models available from provider %s; please specify a model explicitly (e.g. /provider %s:<model-name>)", api.GetProviderName(provider), api.GetProviderName(provider))
			}
		}
	} else if resolvedModel, ok := resolveModelIDForProvider(model, availableModels); ok {
		model = resolvedModel
	} else if len(availableModels) > 0 {
		fallbackModel := a.selectDefaultModel(availableModels, provider)
		if fallbackModel == "" {
			fallbackModel = availableModels[0].ID
		}
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[info] Configured model %s is not available for %s. Using %s.\n", model, api.GetProviderName(provider), fallbackModel)))
		model = fallbackModel
	}

	// Create a new client with the specified provider
	newClient, err := factory.CreateProviderClient(provider, model)
	if err != nil {
		return agenterrors.NewConfig(fmt.Sprintf("failed to create client for provider %s", api.GetProviderName(provider)), err)
	}

	// Set debug mode on the new client
	newClient.SetDebug(a.debug)

	// Connection is validated on the first real request — skip the blocking
	// connection check here so provider/model switches feel instant in the CLI.

	// Switch to the new client atomically (both fields under the write lock)
	a.setClient(newClient, provider)

	// Get the actual model being used (might be different due to fallback)
	actualModel := newClient.GetModel()

	// Save to configuration (persisted for CLI use)
	if err := a.configManager.SetProvider(provider); err != nil {
		return agenterrors.NewConfig("failed to save provider", err)
	}
	if err := a.configManager.SetModelForProvider(provider, actualModel); err != nil {
		return agenterrors.NewConfig("failed to save model", err)
	}

	// Update context limits for the new model
	a.state.SetMaxContextTokens(a.getModelContextLimit())
	a.state.SetCurrentContextTokens(0)
	a.normalizeConversationForCurrentModelSyntax(prevProvider, prevModel)

	// Notify user if model was different due to fallback
	if actualModel != model {
		_, _ = os.Stderr.Write([]byte(fmt.Sprintf("[info] Using model: %s (requested: %s)\n", actualModel, model)))
	}

	if a.debug {
		a.Logger().Debug("[OK] Switched to provider %s with model %s\n", api.GetProviderName(provider), actualModel)
	}

	return nil
}

func resolveModelIDForProvider(model string, models []api.ModelInfo) (string, bool) {
	trimmed := strings.TrimSpace(model)
	if trimmed == "" {
		return "", false
	}
	for _, candidate := range models {
		if strings.EqualFold(strings.TrimSpace(candidate.ID), trimmed) {
			return candidate.ID, true
		}
	}
	return "", false
}

// SetModel changes the current model for the session.
// This is the session-scoped version that doesn't persist to config.
// For CLI use with persistence, use SetModelPersisted.
func (a *Agent) SetModel(model string) error {
	prevProvider := a.GetProvider()
	prevModel := a.GetModel()

	// Hold the read lock for the entire SetModel operation. SetModel only
	// changes the model name on the existing client — it doesn't swap the
	// client pointer. Holding RLock prevents SetProvider from swapping
	// the client out from under us mid-operation. A concurrent SetModel
	// on the same agent is fine (RLock is shared).
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()

	// Try to set the model directly first - this allows testing unknown models
	// Only validate against known models if the direct setting fails
	err := a.client.SetModel(model)
	if err != nil {
		// If direct setting failed, try to find the model in the known list
		// This provides better error messages and handles case sensitivity
		models, getModelErr := a.getModelsForProvider(a.clientType)
		if getModelErr != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed to set model '%s' on provider %s (also failed to get model list: %v)",
				model, api.GetProviderName(a.clientType), getModelErr), err)
		}

		// Check if the model exists in the known list (case-insensitive)
		modelFound := false
		for _, m := range models {
			if strings.EqualFold(m.ID, model) {
				modelFound = true
				// Use the exact model ID from the provider's list
				model = m.ID
				// Try again with the exact model ID
				if retryErr := a.client.SetModel(model); retryErr != nil {
					return agenterrors.NewConfig(fmt.Sprintf("model '%s' found in list but failed to set", model), retryErr)
				}
				break
			}
		}

		if !modelFound {
			return agenterrors.NewConfig(fmt.Sprintf("model '%s' not found for provider %s and failed to set directly",
				model, api.GetProviderName(a.clientType)), err)
		}
	}

	// Connection is validated on the first real request — skip the blocking
	// connection check here so model switches feel instant in the UI.

	// Store in session fields (not config) - this allows session-scoped changes
	a.state.SetSessionModel(model)

	// Update context limits for the new model
	a.state.SetMaxContextTokens(a.getModelContextLimit())
	a.state.SetCurrentContextTokens(0)
	a.normalizeConversationForCurrentModelSyntax(prevProvider, prevModel)

	return nil
}

// SetModelPersisted changes the current model and persists the choice to config.
// This is intended for CLI use where the selection should be saved.
func (a *Agent) SetModelPersisted(model string) error {
	prevProvider := a.GetProvider()
	prevModel := a.GetModel()

	// Hold the read lock for the entire operation (same rationale as SetModel).
	a.clientMu.RLock()
	defer a.clientMu.RUnlock()

	// Try to set the model directly first - this allows testing unknown models
	// Only validate against known models if the direct setting fails
	err := a.client.SetModel(model)
	if err != nil {
		// If direct setting failed, try to find the model in the known list
		// This provides better error messages and handles case sensitivity
		models, getModelErr := a.getModelsForProvider(a.clientType)
		if getModelErr != nil {
			return agenterrors.NewConfig(fmt.Sprintf("failed to set model '%s' on provider %s (also failed to get model list: %v)",
				model, api.GetProviderName(a.clientType), getModelErr), err)
		}

		// Check if the model exists in the known list (case-insensitive)
		modelFound := false
		for _, m := range models {
			if strings.EqualFold(m.ID, model) {
				modelFound = true
				// Use the exact model ID from the provider's list
				model = m.ID
				// Try again with the exact model ID
				if retryErr := a.client.SetModel(model); retryErr != nil {
					return agenterrors.NewConfig(fmt.Sprintf("model '%s' found in list but failed to set", model), retryErr)
				}
				break
			}
		}

		if !modelFound {
			return agenterrors.NewConfig(fmt.Sprintf("model '%s' not found for provider %s and failed to set directly",
				model, api.GetProviderName(a.clientType)), err)
		}
	}

	// Connection is validated on the first real request — skip the blocking
	// connection check here so model switches feel instant in the CLI.

	// Store in session fields so GetModel() returns the new value immediately.
	a.state.SetSessionModel(model)

	// Save the selection to config (persisted for CLI use)
	if err := a.configManager.SetProvider(a.clientType); err != nil {
		return agenterrors.NewConfig("failed to save provider", err)
	}
	if err := a.configManager.SetModelForProvider(a.clientType, model); err != nil {
		return agenterrors.NewConfig("failed to save model", err)
	}

	// Update context limits for the new model
	a.state.SetMaxContextTokens(a.getModelContextLimit())
	a.state.SetCurrentContextTokens(0)
	a.normalizeConversationForCurrentModelSyntax(prevProvider, prevModel)

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

// ClearSessionOverrides clears any session-scoped provider/model overrides.
// This should be called when a webui session ends to restore config-based behavior.
func (a *Agent) ClearSessionOverrides() {
	a.state.SetSessionProvider("")
	a.state.SetSessionModel("")
	// SP-063: reset per-session computer-use consent so the next session
	// re-prompts. The persistent workspace allowlist survives (it's in
	// config), but the transient session flag does not.
	a.ResetComputerUseSessionApproval()
	// SP-063-4h: clear the per-session app allowlist too.
	a.computerUseMu.Lock()
	a.computerUseAppAllowlist = nil
	a.computerUseMu.Unlock()
}

// ResetComputerUseSessionApproval clears the per-session computer-use opt-in
// flag (SP-063). Called from ClearSessionOverrides when a session ends so
// that the next session re-prompts the user for consent.
func (a *Agent) ResetComputerUseSessionApproval() {
	if a == nil {
		return
	}
	a.computerUseMu.Lock()
	a.computerUseSessionApproved = false
	a.computerUseMu.Unlock()
}

// HasSessionOverrides returns true if there are session-scoped provider/model overrides
func (a *Agent) HasSessionOverrides() bool {
	return a.state.GetSessionProvider() != "" || a.state.GetSessionModel() != ""
}
