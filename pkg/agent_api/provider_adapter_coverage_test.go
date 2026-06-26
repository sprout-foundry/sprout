package api

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	utils "github.com/sprout-foundry/sprout/pkg/utils"
)

// enhancedMockClient extends mockClient with configurable SupportsVision
type enhancedMockClient struct {
	mockClient
	supportsVisionFlag bool
	modelFlag          string
}

func (m *enhancedMockClient) GetModel() string     { return m.modelFlag }
func (m *enhancedMockClient) SupportsVision() bool { return m.supportsVisionFlag }

// =====================================================================
// isVisionModel
// =====================================================================

func TestIsVisionModel_gpt4o(t *testing.T) {
	assert.True(t, isVisionModel("gpt-4o"))
	assert.True(t, isVisionModel("gpt-4o-mini"))
	assert.True(t, isVisionModel("gpt-4o-2024-05-13"))
}

func TestIsVisionModel_gpt4vision(t *testing.T) {
	assert.True(t, isVisionModel("gpt-4-vision-preview"))
	// "gpt-4-turbo" does NOT contain any of the vision patterns
	assert.False(t, isVisionModel("gpt-4-turbo"))
}

func TestIsVisionModel_llava(t *testing.T) {
	assert.True(t, isVisionModel("llava-1.5"))
	assert.True(t, isVisionModel("llava-next"))
}

func TestIsVisionModel_vision(t *testing.T) {
	assert.True(t, isVisionModel("some-vision-model"))
}

func TestIsVisionModel_LlamaVision(t *testing.T) {
	assert.True(t, isVisionModel("Llama-3.2-11B-Vision"))
	assert.True(t, isVisionModel("Llama-3.2-11B-Vision-Instruct"))
}

func TestIsVisionModel_Llama4Scout(t *testing.T) {
	assert.True(t, isVisionModel("Llama-4-Scout"))
}

func TestIsVisionModel_gemma(t *testing.T) {
	assert.True(t, isVisionModel("gemma-3-27b-it"))
}

func TestIsVisionModel_GLMVision(t *testing.T) {
	// GLM vision models use a "-<digit>v" suffix convention
	assert.True(t, isVisionModel("glm-4.5v"))
	assert.True(t, isVisionModel("glm-4.6v"))
	assert.True(t, isVisionModel("glm-5v-turbo"))
	assert.True(t, isVisionModel("GLM-4.6V"))
	assert.True(t, isVisionModel("GLM-5V-Turbo"))
}

func TestIsVisionModel_GLMNonVision(t *testing.T) {
	// GLM text-only models should NOT match
	assert.False(t, isVisionModel("glm-5"))
	assert.False(t, isVisionModel("glm-5-turbo"))
	assert.False(t, isVisionModel("glm-4.7"))
	assert.False(t, isVisionModel("GLM-4.6"))
	assert.False(t, isVisionModel("glm-4.5"))
}

func TestIsVisionModel_caseInsensitive(t *testing.T) {
	assert.True(t, isVisionModel("GPT-4O"))
	assert.True(t, isVisionModel("LLAVA-1.5"))
	assert.True(t, isVisionModel("LLAMA-3.2-11B-VISION"))
}

func TestIsVisionModel_NonVisionModels(t *testing.T) {
	assert.False(t, isVisionModel("gpt-4"))
	assert.False(t, isVisionModel("gpt-3.5-turbo"))
	assert.False(t, isVisionModel("llama-3"))
	assert.False(t, isVisionModel("claude-3"))
	assert.False(t, isVisionModel("o1-preview"))
	assert.False(t, isVisionModel("glm-5"))
	assert.False(t, isVisionModel("glm-5-turbo"))
	assert.False(t, isVisionModel(""))
}

// =====================================================================
// containsReasoningModel
// =====================================================================

func TestContainsReasoningModel_o1(t *testing.T) {
	assert.True(t, containsReasoningModel("o1"))
	assert.True(t, containsReasoningModel("o1-preview"))
	assert.True(t, containsReasoningModel("o1-mini"))
}

func TestContainsReasoningModel_o3(t *testing.T) {
	assert.True(t, containsReasoningModel("o3"))
	assert.True(t, containsReasoningModel("o3-mini"))
}

func TestContainsReasoningModel_o4(t *testing.T) {
	assert.True(t, containsReasoningModel("o4"))
	assert.True(t, containsReasoningModel("o4-mini"))
}

func TestContainsReasoningModel_caseInsensitive(t *testing.T) {
	assert.True(t, containsReasoningModel("O1-Preview"))
	assert.True(t, containsReasoningModel("O3-Mini"))
	assert.True(t, containsReasoningModel("O4"))
}

func TestContainsReasoningModel_NonReasoningModels(t *testing.T) {
	assert.False(t, containsReasoningModel("gpt-4"))
	assert.False(t, containsReasoningModel("gpt-4o")) // Not a reasoning model
	assert.False(t, containsReasoningModel("gpt-3.5-turbo"))
	assert.False(t, containsReasoningModel("llama-3"))
	assert.False(t, containsReasoningModel(""))
}

// =====================================================================
// ProviderAdapter.GetEndpoint
// =====================================================================

func TestProviderAdapter_GetEndpoint_OpenAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.Equal(t, "https://api.openai.com/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_DeepInfra(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.Equal(t, "https://api.deepinfra.com/v1/openai/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_DeepSeek(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepseek")
	defer utils.RemoveProviderRateLimiter("deepseek")
	mock := &mockClient{provider: "deepseek"}
	adapter := NewProviderAdapter(DeepSeekClientType, mock)
	assert.Equal(t, "https://api.deepseek.com/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OpenRouter(t *testing.T) {
	utils.RemoveProviderRateLimiter("openrouter")
	defer utils.RemoveProviderRateLimiter("openrouter")
	mock := &mockClient{provider: "openrouter"}
	adapter := NewProviderAdapter(OpenRouterClientType, mock)
	assert.Equal(t, "https://openrouter.ai/api/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Chutes(t *testing.T) {
	utils.RemoveProviderRateLimiter("chutes")
	defer utils.RemoveProviderRateLimiter("chutes")
	mock := &mockClient{provider: "chutes"}
	adapter := NewProviderAdapter(ChutesClientType, mock)
	assert.Equal(t, "https://chutes.ai/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_ZAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("zai")
	defer utils.RemoveProviderRateLimiter("zai")
	mock := &mockClient{provider: "zai"}
	adapter := NewProviderAdapter(ZAIClientType, mock)
	assert.Equal(t, "https://z.ai/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OllamaLocal(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama-local")
	defer utils.RemoveProviderRateLimiter("ollama-local")
	mock := &mockClient{provider: "ollama-local"}
	adapter := NewProviderAdapter(OllamaLocalClientType, mock)
	assert.Equal(t, "http://localhost:11434/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Ollama(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama")
	defer utils.RemoveProviderRateLimiter("ollama")
	mock := &mockClient{provider: "ollama"}
	adapter := NewProviderAdapter(OllamaClientType, mock)
	assert.Equal(t, "http://localhost:11434/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OllamaCloud(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama-cloud")
	defer utils.RemoveProviderRateLimiter("ollama-cloud")
	mock := &mockClient{provider: "ollama-cloud"}
	adapter := NewProviderAdapter(OllamaCloudClientType, mock)
	assert.Equal(t, "https://turbo.ollama.ai/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_LMStudio(t *testing.T) {
	utils.RemoveProviderRateLimiter("lmstudio")
	defer utils.RemoveProviderRateLimiter("lmstudio")
	mock := &mockClient{provider: "lmstudio"}
	adapter := NewProviderAdapter(LMStudioClientType, mock)
	assert.Equal(t, "http://localhost:1234/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Test(t *testing.T) {
	utils.RemoveProviderRateLimiter("test")
	defer utils.RemoveProviderRateLimiter("test")
	mock := &mockClient{provider: "test"}
	adapter := NewProviderAdapter(TestClientType, mock)
	assert.Equal(t, "https://test.api.example.com/v1/chat/completions", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_UnknownClientType(t *testing.T) {
	utils.RemoveProviderRateLimiter("unknown-custom")
	defer utils.RemoveProviderRateLimiter("unknown-custom")
	mock := &mockClient{provider: "unknown-custom"}
	adapter := NewProviderAdapter(ClientType("unknown-custom"), mock)
	// Unknown types that don't implement GetEndpoint return ""
	assert.Equal(t, "", adapter.GetEndpoint())
}

// =====================================================================
// ProviderAdapter.SupportsTools and SupportsStreaming
// =====================================================================

func TestProviderAdapter_SupportsTools(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	// Always returns true regardless of client
	assert.True(t, adapter.SupportsTools())
}

func TestProviderAdapter_SupportsStreaming(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	// Always returns true regardless of client
	assert.True(t, adapter.SupportsStreaming())
}

// =====================================================================
// ProviderAdapter.GetName
// =====================================================================

func TestProviderAdapter_GetName(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "OpenAI Provider"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.Equal(t, "OpenAI Provider", adapter.GetName())
}

// =====================================================================
// ProviderAdapter.SupportsVision (delegates to client)
// =====================================================================

func TestProviderAdapter_SupportsVision_Delegates(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")

	// Default mock returns false
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.False(t, adapter.SupportsVision())

	// With a vision-capable client
	enhancedMock := &enhancedMockClient{
		mockClient:         mockClient{provider: "openai"},
		supportsVisionFlag: true,
	}
	adapter2 := NewProviderAdapter(OpenAIClientType, enhancedMock)
	assert.True(t, adapter2.SupportsVision())
}

// =====================================================================
// ProviderAdapter.SupportsReasoning
// =====================================================================

func TestProviderAdapter_SupportsReasoning_OpenAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai", model: "gpt-4"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	// OpenAI client type always returns true
	assert.True(t, adapter.SupportsReasoning())
}

func TestProviderAdapter_SupportsReasoning_O1Model(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: "o1-preview"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	// Non-OpenAI with o1 model should return true
	assert.True(t, adapter.SupportsReasoning())
}

func TestProviderAdapter_SupportsReasoning_O3Model(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: "o3-mini"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.True(t, adapter.SupportsReasoning())
}

func TestProviderAdapter_SupportsReasoning_O4Model(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: "o4-mini"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.True(t, adapter.SupportsReasoning())
}

func TestProviderAdapter_SupportsReasoning_NonReasoningNonOpenAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: "llama-3"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.False(t, adapter.SupportsReasoning())
}

func TestProviderAdapter_SupportsReasoning_EmptyModelNonOpenAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: ""}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.False(t, adapter.SupportsReasoning())
}

// =====================================================================
// ProviderAdapter.GetType
// =====================================================================

func TestProviderAdapter_GetType(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.Equal(t, OpenAIClientType, adapter.GetType())
}

// =====================================================================
// ProviderAdapter.GetModel and SetModel
// =====================================================================

func TestProviderAdapter_GetModel(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai", model: "gpt-4o"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.Equal(t, "gpt-4o", adapter.GetModel())
}

func TestProviderAdapter_SetModel(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai", model: "gpt-4o"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.NoError(t, adapter.SetModel("gpt-4o-mini"))
	assert.Equal(t, "gpt-4o-mini", adapter.GetModel())
}

// =====================================================================
// ProviderAdapter.GetModelContextLimit
// =====================================================================

func TestProviderAdapter_GetModelContextLimit(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	limit, err := adapter.GetModelContextLimit()
	require.NoError(t, err)
	assert.Equal(t, 128000, limit)
}

// =====================================================================
// ProviderAdapter.CheckConnection
// =====================================================================

func TestProviderAdapter_CheckConnection(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	assert.NoError(t, adapter.CheckConnection(context.Background()))
}

// =====================================================================
// ProviderAdapter.SetDebug and IsDebug
// =====================================================================

func TestProviderAdapter_SetDebug(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai"}
	adapter := NewProviderAdapter(OpenAIClientType, mock)
	adapter.SetDebug(true)
	// IsDebug always returns false (not exposed by old interface)
	assert.False(t, adapter.IsDebug())
}

// =====================================================================
// ProviderAdapter.getModelFeatures
// =====================================================================

func TestProviderAdapter_getModelFeatures_NonVisionNonReasoning(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	features := adapter.getModelFeatures("llama-3")
	assert.Contains(t, features, "tools")
	assert.NotContains(t, features, "vision")
	assert.NotContains(t, features, "reasoning")
}

func TestProviderAdapter_getModelFeatures_VisionModel(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	enhancedMock := &enhancedMockClient{
		mockClient:         mockClient{provider: "openai"},
		supportsVisionFlag: true,
	}
	adapter := NewProviderAdapter(OpenAIClientType, enhancedMock)
	features := adapter.getModelFeatures("gpt-4o")
	assert.Contains(t, features, "tools")
	assert.Contains(t, features, "vision")
}

func TestProviderAdapter_getModelFeatures_ReasoningModel(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	features := adapter.getModelFeatures("o1-preview")
	assert.Contains(t, features, "tools")
	assert.Contains(t, features, "reasoning")
}

func TestProviderAdapter_getModelFeatures_VisionAndReasoning(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	enhancedMock := &enhancedMockClient{
		mockClient:         mockClient{provider: "openai"},
		supportsVisionFlag: true,
	}
	adapter := NewProviderAdapter(OpenAIClientType, enhancedMock)
	features := adapter.getModelFeatures("gpt-4o") // gpt-4o is vision but not reasoning by our patterns
	assert.Contains(t, features, "tools")
	assert.Contains(t, features, "vision")
	// gpt-4o does NOT match o1/o3/o4 reasoning pattern
	assert.NotContains(t, features, "reasoning")
}

// =====================================================================
// CreateProviderFromClient
// =====================================================================

func TestCreateProviderFromClient(t *testing.T) {
	utils.RemoveProviderRateLimiter("openai")
	defer utils.RemoveProviderRateLimiter("openai")
	mock := &mockClient{provider: "openai", model: "gpt-4o"}
	provider := CreateProviderFromClient(OpenAIClientType, mock)
	require.NotNil(t, provider)
	assert.Equal(t, "openai", provider.GetName())
	assert.Equal(t, OpenAIClientType, provider.GetType())
	assert.Equal(t, "gpt-4o", provider.GetModel())
	// Should return *ProviderAdapter
	_, ok := provider.(*ProviderAdapter)
	assert.True(t, ok)
}

func TestCreateProviderFromClient_DifferentProvider(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra", model: "llama-3"}
	provider := CreateProviderFromClient(DeepInfraClientType, mock)
	require.NotNil(t, provider)
	assert.Equal(t, DeepInfraClientType, provider.GetType())
	assert.Equal(t, "https://api.deepinfra.com/v1/openai/chat/completions", provider.GetEndpoint())
}

// =====================================================================
// ProviderAdapter.SendChatRequest - reasoning/disableThinking extraction
// =====================================================================

func TestProviderAdapter_SendChatRequest_PassesReasoningToClient(t *testing.T) {
	utils.RemoveProviderRateLimiter("test-opts-provider")
	defer utils.RemoveProviderRateLimiter("test-opts-provider")
	utils.SetProviderRate("test-opts-provider", 100.0, 10)

	var receivedReasoning string
	var receivedDisableThinking bool
	mock := &mockClient{
		provider: "test-opts-provider",
		sendChatRequestFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			receivedReasoning = reasoning
			receivedDisableThinking = disableThinking
			return &ChatResponse{Choices: []Choice{{Message: Message{Content: "ok"}}}}, nil
		},
	}
	adapter := NewProviderAdapter(ClientType("test-opts-provider"), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Options: &RequestOptions{
			ReasoningEffort: "high",
			DisableThinking: boolPtr(true),
		},
	}
	_, err := adapter.SendChatRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Equal(t, "high", receivedReasoning)
	assert.True(t, receivedDisableThinking)
}

func TestProviderAdapter_SendChatRequest_NoOptions(t *testing.T) {
	utils.RemoveProviderRateLimiter("test-no-opts-provider")
	defer utils.RemoveProviderRateLimiter("test-no-opts-provider")
	utils.SetProviderRate("test-no-opts-provider", 100.0, 10)

	var receivedReasoning string
	var receivedDisableThinking bool
	mock := &mockClient{
		provider: "test-no-opts-provider",
		sendChatRequestFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			receivedReasoning = reasoning
			receivedDisableThinking = disableThinking
			return &ChatResponse{Choices: []Choice{{Message: Message{Content: "ok"}}}}, nil
		},
	}
	adapter := NewProviderAdapter(ClientType("test-no-opts-provider"), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Options:  nil,
	}
	_, err := adapter.SendChatRequest(context.Background(), req)
	require.NoError(t, err)
	assert.Empty(t, receivedReasoning)
	assert.False(t, receivedDisableThinking)
}

// Helper
func boolPtr(b bool) *bool { return &b }
