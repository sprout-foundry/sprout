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

// EnsureOllamaModelTag ensures the model has a tag suffix
func EnsureOllamaModelTag(model string) string {
	model = strings.TrimSpace(model)
	if model == "" {
		return model
	}
	if strings.Contains(model, ":") {
		return model
	}
	return model + ":latest"
}

// CreateOllamaClient creates an Ollama client with the specified model
func CreateOllamaClient(model string) (api.ClientInterface, error) {
	model = EnsureOllamaModelTag(model)
	client, err := factory.CreateProviderClient(api.OllamaClientType, model)
	if err != nil {
		return nil, fmt.Errorf("create Ollama client: %w", err)
	}
	return client, nil
}

// ============================================================================
// Vision Processor Creation
// ============================================================================

// NewVisionProcessorWithMode creates a vision processor for image/OCR workflows.
// Client selection is intentionally deterministic and does not vary by mode:
// provider-vision list first, local Ollama fallback last.
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
	// run the standard provider-first list with local Ollama last.
	globalClient, globalErr := CreateVisionClient()
	if globalErr == nil && globalClient != nil && globalClient.SupportsVision() {
		return globalClient, nil
	}

	return nil, fmt.Errorf("provider %s does not support vision models and no usable fallback is configured: %w", providerType, globalErr)
}

// GetVisionModelForProvider returns the appropriate vision model for a given provider.
//
// Resolution order:
//  1. Special-cased providers (OpenAI, Ollama) check their specific config
//     paths, falling back to the provider JSON config's vision_model field.
//  2. Custom providers check their explicit vision_model / model_name config.
//  3. All other providers read from the provider JSON config via a temporary
//     client's GetVisionModel().
//
// Vision models are configured in the provider JSON config files in
// pkg/agent_providers/configs/*.json under the "vision_model" field.
func GetVisionModelForProvider(providerType api.ClientType) string {
	switch providerType {
	case api.OpenAIClientType:
		// Read from the provider JSON config (openai.json → vision_model).
		// Falls back to the hardcoded default only if the config is
		// missing or the field is empty.
		if cfg, err := factory.GlobalFactory().GetProviderConfig("openai"); err == nil {
			if vm := strings.TrimSpace(cfg.Models.VisionModel); vm != "" {
				return vm
			}
		}
		return "gpt-4o-mini"
	case api.OllamaClientType, api.OllamaLocalClientType:
		// Prefer the user's configured OCR model, then fall back to
		// a reasonable local default. Local Ollama has no provider
		// JSON config — the model depends on what's installed.
		configManager, err := configuration.NewManager()
		if err == nil {
			config := configManager.GetConfig()
			if strings.TrimSpace(config.PDFOCRModel) != "" {
				return EnsureOllamaModelTag(config.PDFOCRModel)
			}
		}
		return "glm-ocr:latest"
	case api.OllamaCloudClientType:
		// Ollama cloud currently does not support vision.
		return ""
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

	// Try to create a provider to get its vision model
	// Use the default model for this provider
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

// GetDefaultModelForProvider returns the default model for a given provider type
func GetDefaultModelForProvider(providerType api.ClientType) string {
	switch providerType {
	case api.DeepInfraClientType:
		return "meta-llama/Llama-3.3-70B-Instruct"
	case api.OpenRouterClientType:
		return "openai/gpt-5"
	case api.MistralClientType:
		return "devstral-2512"
	case api.DeepSeekClientType:
		return "deepseek-ai/DeepSeek-V3"
	case api.ZAIClientType:
		return "glm-4.6"
	case api.LMStudioClientType:
		return "" // Depends on locally installed models
	case api.ChutesClientType:
		return "" // Depends on chutes service
	default:
		return ""
	}
}

// CreateVisionClient creates a client capable of vision analysis
func CreateVisionClient() (api.ClientInterface, error) {
	// Priority: configured provider vision models first, local Ollama last.
	providers := []api.ClientType{
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.ZAIClientType,
		api.DeepSeekClientType,
	}
	providers = append(providers, GetCustomVisionProviders()...)
	providers = append(providers, api.OllamaClientType)

	for _, providerType := range providers {
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

	return nil, fmt.Errorf("no vision-capable providers available - please configure a provider vision model or local Ollama OCR model")
}

// CreateVisionClientWithModel creates a vision client using a specific model
func CreateVisionClientWithModel(modelName string) (api.ClientInterface, error) {
	// Determine which provider supports this model
	if strings.HasPrefix(modelName, "google/") || strings.HasPrefix(modelName, "meta-llama/") {
		// DeepInfra model - use new generic provider system
		if configuration.HasProviderAuth("deepinfra") {
			provider, err := factory.CreateGenericProvider("deepinfra", modelName)
			if err != nil {
				return nil, fmt.Errorf("create DeepInfra client: %w", err)
			}
			return provider, nil
		}
		return nil, fmt.Errorf("deepinfra credentials not configured for model %s", modelName)
	}

	// Fall back to default client creation
	return CreateVisionClient()
}

// HasVisionCapability checks if vision processing is available
func HasVisionCapability() bool {
	// Check if any provider with vision capability is available
	// Priority: provider vision models first, local providers last.
	providers := []api.ClientType{
		api.DeepInfraClientType,
		api.OpenRouterClientType,
		api.OpenAIClientType,
		api.MistralClientType,
		api.DeepSeekClientType,
		api.ZAIClientType,
	}
	providers = append(providers, GetCustomVisionProviders()...)
	providers = append(providers,
		api.OllamaClientType,
		api.OllamaLocalClientType,
	)

	for _, providerType := range providers {
		// Get the vision model for this provider
		visionModel := GetVisionModelForProvider(providerType)
		if visionModel == "" {
			continue // Skip providers without vision support
		}

		if !configuration.HasProviderAuth(string(providerType)) {
			switch providerType {
			case api.OllamaClientType, api.OllamaLocalClientType:
				// Local providers do not require API keys.
			default:
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
