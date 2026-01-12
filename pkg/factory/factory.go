package factory

import (
	"fmt"

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
	callback("Test response from mock provider")
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
			_ = globalProviderFactory.LoadConfigsFromDirectory("./configs")
		}
	}
}

// CreateGenericProvider creates a generic provider by name
func CreateGenericProvider(providerName, model string) (api.ClientInterface, error) {
	// First try the generic provider system
	provider, err := globalProviderFactory.CreateProviderWithModel(providerName, model)
	if err == nil {
		return provider, nil
	}

	// If not found in generic provider system, try to create from custom provider config
	return CreateCustomProvider(providerName, model)
}

// CreateCustomProvider creates a provider from custom provider configuration
func CreateCustomProvider(providerName, model string) (api.ClientInterface, error) {
	// Load configuration to get custom provider details
	config, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	if config.CustomProviders == nil {
		return nil, fmt.Errorf("no custom providers configured")
	}

	customProvider, exists := config.CustomProviders[providerName]
	if !exists {
		return nil, fmt.Errorf("custom provider not found: %s", providerName)
	}

	// Create a generic provider config from custom provider
	authType := "api_key"
	if !customProvider.RequiresAPIKey {
		authType = "none"
	}

	genericConfig := &providers.ProviderConfig{
		Name:     customProvider.Name,
		Endpoint: customProvider.Endpoint,
		Auth: providers.AuthConfig{
			Type:   authType,
			EnvVar: customProvider.EnvVar,
			Key:    customProvider.APIKey,
		},
		Headers: make(map[string]string),
		Defaults: providers.RequestDefaults{
			Model:     customProvider.ModelName,
			MaxTokens: &customProvider.ContextSize,
		},
		Conversion: func() providers.MessageConversion {
			if customProvider.Conversion.IncludeToolCallId ||
				customProvider.Conversion.ConvertToolRoleToUser ||
				customProvider.Conversion.ReasoningContentField != "" {
				return customProvider.Conversion
			}
			// Default conversion
			return providers.MessageConversion{
				IncludeToolCallId:        true,
				ConvertToolRoleToUser:    false, // Keep tool roles as "tool" - they're exempt from alternation
				ArgumentsAsJSON:          false,
				SkipToolExecutionSummary: true, // Skip summary to avoid breaking role alternation
			}
		}(),
		Streaming: providers.StreamingConfig{
			Format:         "sse",
			ChunkTimeoutMs: 120000, // 2 minutes - reasonable for LLM streaming
			DoneMarker:     "[DONE]",
		},
		Models: providers.ModelConfig{
			DefaultContextLimit: customProvider.ContextSize,
			DefaultModel:        customProvider.ModelName,
			SupportsVision:      false,
		},
		Retry: providers.RetryConfig{
			MaxAttempts:       3,
			BaseDelayMs:       1000,
			BackoffMultiplier: 2.0,
			MaxDelayMs:        10000,
			RetryableErrors:   []string{"timeout", "connection", "rate_limit"},
		},
		Cost: providers.CostConfig{
			InputTokenCost:  0.001,
			OutputTokenCost: 0.002,
		},
	}

	// Create the provider using the generic config
	return providers.NewGenericProvider(genericConfig)
}

// CreateProviderClient is a factory function that creates providers
func CreateProviderClient(clientType api.ClientType, model string) (api.ClientInterface, error) {
	switch clientType {
	case api.OpenAIClientType:
		return api.NewOpenAIClientWrapper(model)
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
		return api.NewOllamaTurboClient(model)
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
