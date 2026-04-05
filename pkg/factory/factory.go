package factory

import (
	"fmt"
	"log"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_providers"
	"github.com/alantheprice/ledit/pkg/configuration"
)

// TestClient implements a mock client for CI/testing environments
type TestClient struct {
	model string
	debug bool
}

// Create test client methods
func (t *TestClient) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return &api.ChatResponse{
		ID:      "test-response-id",
		Object:  "chat.completion",
		Created: 1234567890,
		Model:   t.GetModel(),
		Choices: []api.Choice{
			{
				Index: 0,
				Message: struct {
					Role             string          `json:"role"`
					Content          string          `json:"content"`
					ReasoningContent string          `json:"reasoning_content,omitempty"`
					Images           []api.ImageData `json:"images,omitempty"`
					ToolCalls        []api.ToolCall  `json:"tool_calls,omitempty"`
				}{
					Role:    "assistant",
					Content: "Test response from mock provider",
				},
				FinishReason: "stop",
			},
		},
		Usage: struct {
			PromptTokens        int     `json:"prompt_tokens"`
			CompletionTokens    int     `json:"completion_tokens"`
			TotalTokens         int     `json:"total_tokens"`
			EstimatedCost       float64 `json:"estimated_cost"`
			PromptTokensDetails struct {
				CachedTokens     int  `json:"cached_tokens"`
				CacheWriteTokens *int `json:"cache_write_tokens"`
			} `json:"prompt_tokens_details,omitempty"`
		}{
			PromptTokens:     10,
			CompletionTokens: 5,
			TotalTokens:      15,
			EstimatedCost:    0.0,
		},
	}, nil
}

func (t *TestClient) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	// Simple streaming simulation
	callback("Test response from mock provider", "assistant_text")
	return t.SendChatRequest(messages, tools, reasoning)
}

func (t *TestClient) CheckConnection() error {
	return nil // Test provider always has good connection
}

func (t *TestClient) SetDebug(debug bool) {
	t.debug = debug
}

func (t *TestClient) SetModel(model string) error {
	t.model = model
	return nil
}

func (t *TestClient) GetModel() string {
	if t.model == "" {
		return "test-model"
	}
	return t.model
}

func (t *TestClient) GetProvider() string {
	return "test"
}

func (t *TestClient) GetModelContextLimit() (int, error) {
	return 4096, nil
}

func (t *TestClient) ListModels() ([]api.ModelInfo, error) {
	return []api.ModelInfo{
		{Name: "test-model", ContextLength: 4096},
	}, nil
}

func (t *TestClient) SupportsVision() bool {
	return false
}

func (t *TestClient) GetVisionModel() string {
	return ""
}

func (t *TestClient) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return nil, fmt.Errorf("vision not supported in test provider")
}

func (t *TestClient) GetLastTPS() float64 {
	return 100.0 // Mock TPS
}

func (t *TestClient) GetAverageTPS() float64 {
	return 100.0 // Mock TPS
}

func (t *TestClient) GetTPSStats() map[string]float64 {
	return map[string]float64{"last": 100.0, "average": 100.0}
}

func (t *TestClient) ResetTPSStats() {
	// No-op for test client
}

// Global provider factory instance
var globalProviderFactory *providers.ProviderFactory

// init initializes the global provider factory
func init() {
	globalProviderFactory = providers.NewProviderFactory()

	// First, try to load embedded configs (always available)
	if err := globalProviderFactory.LoadEmbeddedConfigs(); err != nil {
		// Log error but continue with fallback methods
		fmt.Printf("Warning: Failed to load embedded provider configs: %v\n", err)
	}

	// Then try to load from filesystem (allows for customization/overriding)
	if err := globalProviderFactory.LoadConfigsFromDirectory("pkg/agent_providers/configs"); err != nil {
		// Try to load from the binary's location (for installed versions)
		if err := globalProviderFactory.LoadConfigsFromDirectory("configs"); err != nil {
			// As a last resort, try to load from current directory
			if err := globalProviderFactory.LoadConfigsFromDirectory("./configs"); err != nil {
				log.Printf("[debug] failed to load configs from ./configs: %v", err)
			}
		}
	}
}

// CreateGenericProvider creates a generic provider by name
func CreateGenericProvider(providerName, model string) (api.ClientInterface, error) {
	if config, err := globalProviderFactory.GetProviderConfig(providerName); err == nil {
		configCopy := *config
		resolved, resolveErr := configuration.ResolveProviderCredential(providerName, nil)
		if resolveErr == nil && resolved.Value != "" {
			configCopy.Auth.Key = resolved.Value
		}
		provider, providerErr := providers.NewGenericProvider(&configCopy)
		if providerErr == nil {
			if model != "" {
				if err := provider.SetModel(model); err != nil {
					return nil, fmt.Errorf("failed to set model: %w", err)
				}
			}
			return provider, nil
		}
	}

	// If not found in generic provider system, try to create from custom provider config
	return CreateCustomProvider(providerName, model)
}

// CreateCustomProvider creates a provider from custom provider configuration
func CreateCustomProvider(providerName, model string) (api.ClientInterface, error) {
	customProviders, err := configuration.LoadCustomProviders()
	if err != nil {
		return nil, fmt.Errorf("failed to load custom providers: %w", err)
	}

	if len(customProviders) == 0 {
		return nil, fmt.Errorf("no custom providers configured")
	}

	customProvider, exists := customProviders[providerName]
	if !exists {
		return nil, fmt.Errorf("custom provider not found: %s", providerName)
	}

	genericConfig, err := customProvider.ToProviderConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to build provider config: %w", err)
	}
	if resolved, resolveErr := configuration.ResolveProviderCredential(providerName, nil); resolveErr == nil && resolved.Value != "" {
		genericConfig.Auth.Key = resolved.Value
	}

	client, err := providers.NewGenericProvider(genericConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create generic provider: %w", err)
	}
	if model != "" {
		if err := client.SetModel(model); err != nil {
			return nil, fmt.Errorf("failed to set model: %w", err)
		}
	}
	return client, nil
}

// CreateProviderClient is a factory function that creates providers
func CreateProviderClient(clientType api.ClientType, model string) (api.ClientInterface, error) {
	switch clientType {
	case api.OpenAIClientType:
		return CreateGenericProvider("openai", model)
	case api.ChutesClientType:
		// Use the new generic provider system
		return CreateGenericProvider("chutes", model)
	case api.ZAIClientType:
		// Use the new generic provider system
		return CreateGenericProvider("zai", model)
	case api.DeepInfraClientType:
		// Use the new generic provider system
		return CreateGenericProvider("deepinfra", model)
	case api.DeepSeekClientType:
		// Use the new generic provider system
		return CreateGenericProvider("deepseek", model)
	case api.OllamaClientType, api.OllamaLocalClientType:
		return api.NewOllamaLocalClient(model)
	case api.OllamaTurboClientType:
		return CreateGenericProvider("ollama-turbo", model)
	case api.OpenRouterClientType:
		// Use the new generic provider system
		return CreateGenericProvider("openrouter", model)
	case api.LMStudioClientType:
		// Use the new generic provider system
		return CreateGenericProvider("lmstudio", model)
	case api.MistralClientType:
		// Use the new generic provider system
		return CreateGenericProvider("mistral", model)
	case api.TestClientType:
		// Return test/mock client for CI environments
		testClient := &TestClient{model: model}
		if model != "" {
			testClient.SetModel(model)
		}
		return testClient, nil
	default:
		// For custom providers, try to use the generic provider system
		return CreateGenericProvider(string(clientType), model)
	}
}
