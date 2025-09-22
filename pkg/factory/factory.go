package factory

import (
	"fmt"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/agent_providers"
)

// DeepInfraClientWrapper wraps DeepInfraProvider to implement the full ClientInterface
type DeepInfraClientWrapper struct {
	provider *providers.DeepInfraProvider
}

// Delegate all methods to the provider
func (w *DeepInfraClientWrapper) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendChatRequest(messages, tools, reasoning)
}

func (w *DeepInfraClientWrapper) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return w.provider.SendChatRequestStream(messages, tools, reasoning, callback)
}

func (w *DeepInfraClientWrapper) CheckConnection() error {
	return w.provider.CheckConnection()
}

func (w *DeepInfraClientWrapper) SetDebug(debug bool) {
	w.provider.SetDebug(debug)
}

func (w *DeepInfraClientWrapper) SetModel(model string) error {
	return w.provider.SetModel(model)
}

func (w *DeepInfraClientWrapper) GetModel() string {
	return w.provider.GetModel()
}

func (w *DeepInfraClientWrapper) GetProvider() string {
	return w.provider.GetProvider()
}

func (w *DeepInfraClientWrapper) GetModelContextLimit() (int, error) {
	return w.provider.GetModelContextLimit()
}

func (w *DeepInfraClientWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}

func (w *DeepInfraClientWrapper) GetVisionModel() string {
	return w.provider.GetVisionModel()
}

func (w *DeepInfraClientWrapper) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendVisionRequest(messages, tools, reasoning)
}

func (w *DeepInfraClientWrapper) ListModels() ([]api.ModelInfo, error) {
	return w.provider.ListModels()
}

// TPS methods that the provider doesn't implement
func (w *DeepInfraClientWrapper) GetLastTPS() float64 {
	return 0.0 // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) GetAverageTPS() float64 {
	return 0.0 // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) GetTPSStats() map[string]float64 {
	return map[string]float64{} // Provider doesn't track TPS
}

func (w *DeepInfraClientWrapper) ResetTPSStats() {
	// No-op - provider doesn't track TPS
}

// OpenRouterClientWrapper wraps OpenRouterProvider to implement the full ClientInterface
type OpenRouterClientWrapper struct {
	provider *providers.OpenRouterProvider
}

// Delegate all methods to the provider
func (w *OpenRouterClientWrapper) SendChatRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendChatRequest(messages, tools, reasoning)
}

func (w *OpenRouterClientWrapper) SendChatRequestStream(messages []api.Message, tools []api.Tool, reasoning string, callback api.StreamCallback) (*api.ChatResponse, error) {
	return w.provider.SendChatRequestStream(messages, tools, reasoning, callback)
}

func (w *OpenRouterClientWrapper) CheckConnection() error {
	return w.provider.CheckConnection()
}

func (w *OpenRouterClientWrapper) SetDebug(debug bool) {
	w.provider.SetDebug(debug)
}

func (w *OpenRouterClientWrapper) SetModel(model string) error {
	return w.provider.SetModel(model)
}

func (w *OpenRouterClientWrapper) GetModel() string {
	return w.provider.GetModel()
}

func (w *OpenRouterClientWrapper) GetProvider() string {
	return w.provider.GetProvider()
}

func (w *OpenRouterClientWrapper) GetModelContextLimit() (int, error) {
	return w.provider.GetModelContextLimit()
}

func (w *OpenRouterClientWrapper) SupportsVision() bool {
	return w.provider.SupportsVision()
}

func (w *OpenRouterClientWrapper) GetVisionModel() string {
	return w.provider.GetVisionModel()
}

func (w *OpenRouterClientWrapper) SendVisionRequest(messages []api.Message, tools []api.Tool, reasoning string) (*api.ChatResponse, error) {
	return w.provider.SendVisionRequest(messages, tools, reasoning)
}

func (w *OpenRouterClientWrapper) ListModels() ([]api.ModelInfo, error) {
	return w.provider.ListModels()
}

// TPS methods that the provider now implements
func (w *OpenRouterClientWrapper) GetLastTPS() float64 {
	return w.provider.GetLastTPS()
}

func (w *OpenRouterClientWrapper) GetAverageTPS() float64 {
	return w.provider.GetAverageTPS()
}

func (w *OpenRouterClientWrapper) GetTPSStats() map[string]float64 {
	return w.provider.GetTPSStats()
}

func (w *OpenRouterClientWrapper) ResetTPSStats() {
	w.provider.ResetTPSStats()
}

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

// CreateProviderClient is a factory function that creates providers
func CreateProviderClient(clientType api.ClientType, model string) (api.ClientInterface, error) {
	switch clientType {
	case api.OpenAIClientType:
		return api.NewOpenAIClientWrapper(model)
	case api.DeepInfraClientType:
		// Use the real DeepInfra provider wrapped to implement ClientInterface
		provider, err := providers.NewDeepInfraProviderWithModel(model)
		if err != nil {
			return nil, err
		}
		return &DeepInfraClientWrapper{provider: provider}, nil
	case api.OllamaClientType, api.OllamaLocalClientType:
		return api.NewOllamaLocalClient(model)
	case api.OllamaTurboClientType:
		return api.NewOllamaTurboClient(model)
	case api.OpenRouterClientType:
		// Use the real OpenRouter provider wrapped to implement ClientInterface
		provider, err := providers.NewOpenRouterProviderWithModel(model)
		if err != nil {
			return nil, err
		}
		return &OpenRouterClientWrapper{provider: provider}, nil
	case api.TestClientType:
		// Return test/mock client for CI environments
		testClient := &TestClient{model: model}
		if model != "" {
			testClient.SetModel(model)
		}
		return testClient, nil
	default:
		return nil, fmt.Errorf("unknown client type: %s", clientType)
	}
}
