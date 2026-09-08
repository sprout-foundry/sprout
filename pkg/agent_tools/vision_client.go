package tools

import (
	"fmt"
	"strings"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/factory"
	"github.com/sprout-foundry/sprout/pkg/utils"
)

// ============================================================================
// Vision Processor Constructors
// ============================================================================

// NewVisionProcessor creates a vision processor with the given client
func NewVisionProcessor(client api.ClientInterface, logger *utils.Logger, debug bool) *VisionProcessor {
	return &VisionProcessor{
		visionClient: client,
		logger:       logger,
		debug:        debug,
	}
}

// ============================================================================
// Custom Provider Configuration Helpers
// ============================================================================

// GetCustomProviderConfig returns the custom provider configuration for a given type
func GetCustomProviderConfig(providerType api.ClientType) (configuration.CustomProviderConfig, bool) {
	configManager, err := configuration.NewManager()
	if err != nil {
		return configuration.CustomProviderConfig{}, false
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return configuration.CustomProviderConfig{}, false
	}

	customConfig, exists := config.CustomProviders[string(providerType)]
	if !exists {
		return configuration.CustomProviderConfig{}, false
	}
	return customConfig, true
}

// GetCustomVisionProviders returns a list of custom providers that support vision
func GetCustomVisionProviders() []api.ClientType {
	configManager, err := configuration.NewManager()
	if err != nil {
		return nil
	}
	config := configManager.GetConfig()
	if config == nil || config.CustomProviders == nil {
		return nil
	}

	providers := make([]api.ClientType, 0, len(config.CustomProviders))
	for name, custom := range config.CustomProviders {
		if !custom.SupportsVision {
			continue
		}
		providers = append(providers, api.ClientType(name))
	}
	return providers
}

// GetCustomVisionFallback returns the fallback provider and model for vision
func GetCustomVisionFallback(providerType api.ClientType) (api.ClientType, string, bool) {
	customConfig, ok := GetCustomProviderConfig(providerType)
	if !ok {
		return "", "", false
	}

	fallbackProvider := strings.TrimSpace(customConfig.VisionFallbackProvider)
	if fallbackProvider == "" {
		return "", "", false
	}

	configManager, err := configuration.NewManager()
	if err != nil {
		return "", "", false
	}

	fallbackClientType, err := configManager.MapStringToClientType(fallbackProvider)
	if err != nil {
		return "", "", false
	}

	return fallbackClientType, strings.TrimSpace(customConfig.VisionFallbackModel), true
}

// createOCRClient creates a client for the configured OCR fallback model
// (ocr_fallback_model, "provider/model" form) via the normal factory.
// Provider-agnostic: any provider the factory can build works.
func createOCRClient(model string) (api.ClientInterface, error) {
	providerType, modelName := splitProviderQualifiedModel(model)
	if providerType == "" || modelName == "" {
		return nil, fmt.Errorf("invalid OCR fallback model %q: expected provider/model", model)
	}
	client, err := factory.CreateProviderClient(api.ClientType(providerType), modelName)
	if err != nil {
		return nil, fmt.Errorf("create OCR fallback client for %s: %w", model, err)
	}
	return client, nil
}

// splitProviderQualifiedModel splits a "provider/model" string. When there
// is no slash, the model alone is returned with an empty provider.
func splitProviderQualifiedModel(model string) (provider, modelName string) {
	model = strings.TrimSpace(model)
	if model == "" {
		return "", ""
	}
	if idx := strings.Index(model, "/"); idx > 0 {
		return model[:idx], model[idx+1:]
	}
	return "", model
}

// ============================================================================
// Vision Processor Creation
// ============================================================================

// NewVisionProcessorWithMode creates a vision processor for image/OCR workflows.
// Client selection is intentionally deterministic and does not vary by mode:
// Registry-driven client selection (SP-137: provider-neutral).
func NewVisionProcessorWithMode(debug bool, _ string) (*VisionProcessor, error) {
	client, err := CreateVisionClient()
	if err != nil {
		return nil, fmt.Errorf("create vision client: %w", err)
	}

	return &VisionProcessor{
		visionClient: client,
		logger:       nil,
		debug:        debug,
	}, nil
}

// NewVisionProcessorWithProvider creates a vision processor using the specified provider
func NewVisionProcessorWithProvider(debug bool, providerType api.ClientType) (*VisionProcessor, error) {
	client, err := CreateVisionClientWithProvider(providerType)
	if err != nil {
		return nil, fmt.Errorf("create vision client for provider %s: %w", providerType, err)
	}

	return &VisionProcessor{
		visionClient: client,
		logger:       nil,
		debug:        debug,
	}, nil
}

// CreateVisionClientWithProvider creates a vision client using the specified provider
func CreateVisionClientWithProvider(providerType api.ClientType) (api.ClientInterface, error) {
	// Get the vision model for this provider
	visionModel := GetVisionModelForProvider(providerType)
	if visionModel != "" {
		// Create client with the vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err == nil && client.SupportsVision() {
			return client, nil
		}
	}

	// For custom providers, support explicit vision fallback provider/model.
	fallbackProvider, fallbackModel, hasFallback := GetCustomVisionFallback(providerType)
	if hasFallback {
		if fallbackModel == "" {
			fallbackModel = GetVisionModelForProvider(fallbackProvider)
		}
		if fallbackModel != "" {
			client, err := factory.CreateProviderClient(fallbackProvider, fallbackModel)
			if err == nil && client.SupportsVision() {
				return client, nil
			}
		}
	}

	// Deterministic final fallback path shared across scenarios:
	// run the standard registry-driven provider-first list.
	globalClient, globalErr := CreateVisionClient()
	if globalErr == nil && globalClient != nil && globalClient.SupportsVision() {
		return globalClient, nil
	}

	return nil, fmt.Errorf("provider %s does not support vision models and no usable fallback is configured: %w", providerType, globalErr)
}

// GetVisionModelForProvider returns the appropriate vision model for a given provider.
//
// Resolution order (SP-137: provider-neutral):
//  1. Custom providers check their explicit vision_model / model_name config.
//  2. All other providers read from the provider JSON config via a temporary
//     client's GetVisionModel(). No provider is special-cased.
//
// Vision models are configured in the provider JSON config files in
// pkg/agent_providers/configs/*.json under the "vision_model" field.
func GetVisionModelForProvider(providerType api.ClientType) string {
	switch providerType {
	case api.TestClientType:
		return ""
	}

	// Check custom provider config first for explicit vision settings.
	if customConfig, ok := GetCustomProviderConfig(providerType); ok {
		if !customConfig.SupportsVision {
			return ""
		}
		if strings.TrimSpace(customConfig.VisionModel) != "" {
			return strings.TrimSpace(customConfig.VisionModel)
		}
		return strings.TrimSpace(customConfig.ModelName)
	}

	// Registry config first: an explicit vision_model beats everything and
	// works even when defaults.model is unset (SP-137: config-driven, no
	// throwaway client needed for the common case).
	if cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType)); err == nil && cfg != nil {
		if vm := strings.TrimSpace(cfg.Models.VisionModel); vm != "" {
			return vm
		}
	}

	// No explicit vision model: the provider's default model must itself
	// be vision-capable, resolved through a client's GetVisionModel.
	model := GetDefaultModelForProvider(providerType)
	if model == "" {
		return ""
	}

	client, err := factory.CreateProviderClient(providerType, model)
	if err != nil {
		return ""
	}

	// Get vision model from the provider
	return client.GetVisionModel()
}

// GetDefaultModelForProvider returns the provider's configured default
// model from its registry config (SP-137: no per-provider model names in
// the vision tier). Empty when the provider or default is unknown.
func GetDefaultModelForProvider(providerType api.ClientType) string {
	cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType))
	if err != nil || cfg == nil {
		return ""
	}
	return strings.TrimSpace(cfg.Defaults.Model)
}

// visionProviderCandidates returns provider types that may offer a vision
// model, in resolution order: authenticated providers from the registry
// (sorted for determinism), then custom vision providers, then local
// runtimes (which need no credentials). Registry-driven — no provider is
// named here (SP-137 rule).
func visionProviderCandidates() []api.ClientType {
	var providers []api.ClientType
	seen := map[api.ClientType]struct{}{}
	add := func(pt api.ClientType) {
		if _, dup := seen[pt]; dup {
			return
		}
		seen[pt] = struct{}{}
		providers = append(providers, pt)
	}

	names := factory.GlobalFactory().GetAvailableProviders()
	// Usage-tiered priority: recently-used providers first, then
	// credentialed ones, then the rest (shared with onboarding's
	// provider ordering). Explicit and stable — never alphabetical.
	var cfg *configuration.Config
	if cm, err := configuration.NewManager(); err == nil {
		cfg = cm.GetConfig()
	}
	for _, name := range configuration.OrderProvidersByUsage(names, cfg) {
		add(api.ClientType(name))
	}
	for _, pt := range GetCustomVisionProviders() {
		add(pt)
	}
	return providers
}

// CreateVisionClient creates a client capable of vision analysis
func CreateVisionClient() (api.ClientInterface, error) {
	for _, providerType := range visionProviderCandidates() {
		if !configuration.HasProviderAuth(string(providerType)) {
			continue // Skip if API key not set
		}

		// Get vision model from provider config
		visionModel := GetVisionModelForProvider(providerType)
		if visionModel == "" {
			continue // Skip if no vision model configured
		}

		// Try to create client with vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err != nil {
			continue // Try next provider
		}

		// Verify the client supports vision
		if !client.SupportsVision() {
			continue // Try next provider
		}

		return client, nil
	}

	return nil, fmt.Errorf("no vision capability available: no configured provider offers a vision model and native OCR is unavailable")
}

// CreateVisionClientWithModel creates a vision client using a specific model.
// Provider resolution is registry-driven; no vendor namespaces are
// special-cased (SP-137).
func CreateVisionClientWithModel(modelName string) (api.ClientInterface, error) {
	return CreateVisionClient()
}

// isLocalRuntimeProvider reports whether the provider needs no credentials
// (a local model runtime). Derived from the provider config's auth type —
// "none" means no key is ever required — plus the built-in local client
// types that have no config file.
func isLocalRuntimeProvider(providerType api.ClientType) bool {
	// Built-in local client types have no provider config file.
	switch providerType {
	case api.OllamaClientType, api.OllamaLocalClientType, api.LMStudioClientType:
		return true
	}
	// Registry-driven: configs declaring auth.type "none" never need keys.
	cfg, err := factory.GlobalFactory().GetProviderConfig(string(providerType))
	if err != nil || cfg == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(cfg.Auth.Type), "none")
}

// HasVisionCapability checks if vision processing is available
func HasVisionCapability() bool {
	// Native OCR counts: text extraction needs no provider at all (SP-137).
	if nativeOCRAvailable() {
		return true
	}
	// Check if any provider with vision capability is available.
	// Registry-driven candidate list — see visionProviderCandidates.
	for _, providerType := range visionProviderCandidates() {
		// Get the vision model for this provider
		visionModel := GetVisionModelForProvider(providerType)
		if visionModel == "" {
			continue // Skip providers without vision support
		}

		if !configuration.HasProviderAuth(string(providerType)) {
			if !isLocalRuntimeProvider(providerType) {
				continue
			}
		}

		// Try to create client with vision model
		client, err := factory.CreateProviderClient(providerType, visionModel)
		if err != nil {
			continue // Try next provider
		}

		// Verify the client actually supports vision
		if client.SupportsVision() {
			return true
		}
	}

	return false
}
