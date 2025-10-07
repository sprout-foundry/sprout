package api

import (
	"fmt"
	"os"
	"strings"
)

// ClientInterface defines the common interface for all API clients
type ClientInterface interface {
	SendChatRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	SendChatRequestStream(messages []Message, tools []Tool, reasoning string, callback StreamCallback) (*ChatResponse, error)
	CheckConnection() error
	SetDebug(debug bool)
	SetModel(model string) error
	GetModel() string
	GetProvider() string
	GetModelContextLimit() (int, error)
	ListModels() ([]ModelInfo, error)
	SupportsVision() bool
	GetVisionModel() string
	SendVisionRequest(messages []Message, tools []Tool, reasoning string) (*ChatResponse, error)
	// TPS (Tokens Per Second) tracking methods
	GetLastTPS() float64
	GetAverageTPS() float64
	GetTPSStats() map[string]float64
	ResetTPSStats()
}

// ClientType represents the type of client to use
type ClientType string

const (
	DeepInfraClientType   ClientType = "deepinfra"
	DeepSeekClientType    ClientType = "deepseek"
	LMStudioClientType    ClientType = "lmstudio"
	OllamaClientType      ClientType = "ollama" // Maps to local ollama
	OllamaLocalClientType ClientType = "ollama-local"
	OllamaTurboClientType ClientType = "ollama-turbo"
	OpenRouterClientType  ClientType = "openrouter"
	OpenAIClientType      ClientType = "openai"
	TestClientType        ClientType = "test" // Mock provider for CI/testing
)

// NewUnifiedClient creates a client with default model for the provider
func NewUnifiedClient(clientType ClientType) (ClientInterface, error) {
	return NewUnifiedClientWithModel(clientType, "")
}

// NewUnifiedClientWithModel creates a client with a specific model
func NewUnifiedClientWithModel(clientType ClientType, model string) (ClientInterface, error) {
	// Use external factory to avoid import cycles
	return nil, fmt.Errorf("use factory.CreateProviderClient instead to avoid import cycles")
}

// NewDeepInfraClientWrapper creates a DeepInfra client wrapper
func NewDeepInfraClientWrapper(model string) (ClientInterface, error) {
	return nil, fmt.Errorf("DeepInfra client wrapper removed - use providers.NewDeepInfraProviderWithModel instead")
}

// NewOpenRouterClientWrapper is deprecated - use factory.CreateProviderClient instead
func NewOpenRouterClientWrapper(model string) (ClientInterface, error) {
	return nil, fmt.Errorf("OpenRouter client wrapper deprecated - use factory.CreateProviderClient instead")
}

// NewOpenAIClientWrapper creates an OpenAI client wrapper
func NewOpenAIClientWrapper(model string) (ClientInterface, error) {
	client, err := NewOpenAIClient()
	if err != nil {
		return nil, err
	}
	if model != "" {
		if err := client.SetModel(model); err != nil {
			return nil, err
		}
	}
	return client, nil
}

// NewDeepSeekClientWrapper creates a DeepSeek client wrapper
// DeepSeek provider removed (unimplemented)

// DetermineProvider provides unified provider detection with clear precedence:
// 1. Command-line flag (if provided)
// 2. Environment variable (LEDIT_PROVIDER)
// 3. Config file (last_used_provider)
// 4. First available provider based on API keys
// 5. Fallback to Ollama
func DetermineProvider(explicitProvider string, lastUsedProvider ClientType) (ClientType, error) {
	// 1. Command-line flag has highest priority
	if explicitProvider != "" {
		provider, err := ParseProviderName(explicitProvider)
		if err != nil {
			return "", fmt.Errorf("invalid provider '%s': %w", explicitProvider, err)
		}
		if !IsProviderAvailable(provider) {
			return "", fmt.Errorf("provider '%s' is not available (check API key)", explicitProvider)
		}
		return provider, nil
	}

	// 2. Environment variable
	if providerEnv := os.Getenv("LEDIT_PROVIDER"); providerEnv != "" {
		provider, err := ParseProviderName(providerEnv)
		if err == nil && IsProviderAvailable(provider) {
			return provider, nil
		}
	}

	// 3. Last used provider from config
	if lastUsedProvider != "" && IsProviderAvailable(lastUsedProvider) {
		return lastUsedProvider, nil
	}

	// 4. First available provider based on API keys
	priorityOrder := []ClientType{
		OpenAIClientType,
		OpenRouterClientType,
		DeepInfraClientType,
		OllamaTurboClientType,
		LMStudioClientType,
		OllamaLocalClientType,
	}

	for _, provider := range priorityOrder {
		if IsProviderAvailable(provider) {
			return provider, nil
		}
	}

	// 5. Final fallback
	return OllamaLocalClientType, nil
}

// ParseProviderName converts a string provider name to ClientType
func ParseProviderName(name string) (ClientType, error) {
	normalized := strings.ToLower(strings.TrimSpace(name))
	switch normalized {
	case "openai":
		return OpenAIClientType, nil
	case "openrouter":
		return OpenRouterClientType, nil
	case "deepinfra":
		return DeepInfraClientType, nil
	case "ollama":
		// "ollama" maps to local
		return OllamaLocalClientType, nil
	case "ollama-local":
		return OllamaLocalClientType, nil
	case "ollama-turbo":
		return OllamaTurboClientType, nil
	case "lmstudio":
		return LMStudioClientType, nil
	case "test":
		return TestClientType, nil
	default:
		return "", fmt.Errorf("unknown provider: %s", name)
	}
}

// IsProviderAvailable checks if a provider can be used
func IsProviderAvailable(provider ClientType) bool {
	switch provider {
	case OllamaClientType, OllamaLocalClientType:
		// Ollama local is always available (we'll check actual model availability later)
		return true
	case TestClientType:
		// Test provider is always available for CI/testing
		return true
	case LMStudioClientType:
		// LM Studio is a local provider and doesn't require API key
		return true
	case OllamaTurboClientType:
		return os.Getenv("OLLAMA_API_KEY") != ""
	case OpenAIClientType:
		return os.Getenv("OPENAI_API_KEY") != ""
	case OpenRouterClientType:
		return os.Getenv("OPENROUTER_API_KEY") != ""
	case DeepInfraClientType:
		return os.Getenv("DEEPINFRA_API_KEY") != ""
	default:
		return false
	}
}

// GetAvailableProviders returns a list of all available providers
func GetAvailableProviders() []ClientType {
	providers := []ClientType{
		OpenAIClientType,
		DeepInfraClientType,
		OllamaLocalClientType,
		OllamaTurboClientType,
		OpenRouterClientType,
		LMStudioClientType,
	}

	available := make([]ClientType, 0, len(providers))
	for _, provider := range providers {
		if IsProviderAvailable(provider) {
			available = append(available, provider)
		}
	}
	return available
}

// GetProviderName returns the human-readable name for a provider
func GetProviderName(clientType ClientType) string {
	switch clientType {
	case OpenAIClientType:
		return "OpenAI"
	case DeepInfraClientType:
		return "DeepInfra"
	case OllamaClientType, OllamaLocalClientType:
		return "Ollama (Local)"
	case OllamaTurboClientType:
		return "Ollama Turbo"
	case OpenRouterClientType:
		return "OpenRouter"
	case LMStudioClientType:
		return "LM Studio"
	default:
		return string(clientType)
	}
}

// DeepInfraClientWrapper is deprecated - use providers.NewDeepInfraProviderWithModel directly
