package api

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	utils "github.com/sprout-foundry/sprout/pkg/utils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockClient implements ClientInterface for testing
type mockClient struct {
	sendChatRequestFunc func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error)
	provider            string
	model               string
}

func (m *mockClient) SendChatRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	if m.sendChatRequestFunc != nil {
		return m.sendChatRequestFunc(messages, tools, reasoning, disableThinking)
	}
	return &ChatResponse{
		Choices: []Choice{{
			Message: Message{
				Content: "test response",
			},
		}},
	}, nil
}

func (m *mockClient) SendChatRequestStream(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool, callback StreamCallback) (*ChatResponse, error) {
	return m.SendChatRequest(context.Background(), messages, tools, reasoning, disableThinking)
}

func (m *mockClient) CheckConnection() error                              { return nil }
func (m *mockClient) SetDebug(debug bool)                                 {}
func (m *mockClient) SetModel(model string) error                         { m.model = model; return nil }
func (m *mockClient) GetModel() string                                    { return m.model }
func (m *mockClient) GetProvider() string                                 { return m.provider }
func (m *mockClient) GetModelContextLimit() (int, error)                  { return 128000, nil }
func (m *mockClient) ListModels(ctx context.Context) ([]ModelInfo, error) { return nil, nil }
func (m *mockClient) SupportsVision() bool                                { return false }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Defaults to false; overridden per client.
func (m *mockClient) SupportsConversationalVision() bool {
	return false
}
func (m *mockClient) GetVisionModel() string { return "" }
func (m *mockClient) SendVisionRequest(ctx context.Context, messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
	return &ChatResponse{
		Choices: []Choice{{
			Message: Message{
				Content: "vision",
			},
		}},
	}, nil
}
func (m *mockClient) GetLastTPS() float64             { return 0 }
func (m *mockClient) GetAverageTPS() float64          { return 0 }
func (m *mockClient) GetTPSStats() map[string]float64 { return nil }
func (m *mockClient) ResetTPSStats()                  {}

// VisionCapabilities returns an empty struct; mockClient is the minimal
// stand-in used by provider adapter tests and intentionally does not
// implement per-provider vision tuning. Tests that exercise capability
// delegation use enhancedMockClient / dedicated fixtures.
// SP-103-D3 / AUDIT-GAP-2.
func (m *mockClient) VisionCapabilities() VisionCapabilities {
	return VisionCapabilities{}
}

func TestProviderAdapterRateLimiter_AcquiresToken(t *testing.T) {
	provider := "test-rate-limit-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Set a restrictive rate: 10 tps, burst 1
	utils.SetProviderRate(provider, 10.0, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	// First request should succeed immediately (burst=1)
	resp, err := adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("Expected no error, got %v", err)
	}
	if len(resp.Choices) == 0 {
		t.Fatal("Expected at least one choice in response")
	}
	if resp.Choices[0].Message.Content != "test response" {
		t.Errorf("Expected 'test response', got %s", resp.Choices[0].Message.Content)
	}
}

func TestProviderAdapterRateLimiter_ContextCancellation(t *testing.T) {
	provider := "test-cancel-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Set a very restrictive rate: 0.1 tps, burst 1
	utils.SetProviderRate(provider, 0.1, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	// Drain the single burst token
	limiter := utils.GetProviderRateLimiter(provider)
	limiter.TryWait() // Use the burst token

	// Now the bucket is empty; next request will need to wait ~10 seconds
	// Cancel the context after 50ms to test cancellation
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	_, err := adapter.SendChatRequest(ctx, req)
	if err == nil {
		t.Fatal("Expected context cancellation error")
	}
	if !strings.Contains(err.Error(), "rate limit wait canceled") {
		t.Errorf("Expected rate limit wait canceled error, got: %v", err)
	}
}

func TestProviderAdapterRateLimiter_ConcurrentRequests(t *testing.T) {
	provider := "test-concurrent-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Allow burst=10 to let multiple concurrent requests through quickly
	utils.SetProviderRate(provider, 100.0, 10)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	const numRequests = 10
	var wg sync.WaitGroup
	wg.Add(numRequests)
	errs := make([]error, numRequests)

	for i := 0; i < numRequests; i++ {
		go func(idx int) {
			defer wg.Done()
			req := &ProviderChatRequest{
				Messages: []Message{{Role: "user", Content: "hello"}},
				Tools:    []Tool{},
			}
			_, err := adapter.SendChatRequest(context.Background(), req)
			errs[idx] = err
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("Request %d failed: %v", i, err)
		}
	}
}

func TestProviderAdapterRateLimiter_TokenReplenishment(t *testing.T) {
	provider := "test-replenish-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// burst=1, rate=100 (fast refill, ~10ms per token)
	utils.SetProviderRate(provider, 100.0, 1)

	mock := &mockClient{provider: provider}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	// First request uses the burst token
	resp, err := adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("First request failed: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "test response" {
		t.Fatal("First request returned unexpected content")
	}

	// Wait briefly for token replenishment (at 100 tps, ~10ms per token)
	time.Sleep(20 * time.Millisecond)

	// Second request should succeed after replenishment
	resp, err = adapter.SendChatRequest(context.Background(), req)
	if err != nil {
		t.Fatalf("Second request (after replenishment) failed: %v", err)
	}
	if len(resp.Choices) == 0 || resp.Choices[0].Message.Content != "test response" {
		t.Fatal("Second request returned unexpected content")
	}
}

func TestProviderAdapterRateLimiter_PropagatesErrors(t *testing.T) {
	provider := "test-err-prop-provider"
	utils.RemoveProviderRateLimiter(provider)
	defer utils.RemoveProviderRateLimiter(provider)

	// Generous rate so the limiter doesn't block
	utils.SetProviderRate(provider, 100.0, 10)

	expectedErr := errors.New("simulated API error")
	mock := &mockClient{
		provider: provider,
		sendChatRequestFunc: func(messages []Message, tools []Tool, reasoning string, disableThinking bool) (*ChatResponse, error) {
			return nil, expectedErr
		},
	}
	adapter := NewProviderAdapter(ClientType(provider), mock)

	req := &ProviderChatRequest{
		Messages: []Message{{Role: "user", Content: "hello"}},
		Tools:    []Tool{},
	}

	_, err := adapter.SendChatRequest(context.Background(), req)
	if err == nil {
		t.Fatal("Expected error from mock client")
	}
	if !errors.Is(err, expectedErr) {
		t.Errorf("Expected wrapped error, got: %v", err)
	}
}

func TestProviderAdapterRateLimiter_ClientTypeMapping(t *testing.T) {
	// Verify that real ClientType constants produce the expected rate limiter defaults.
	// This catches mismatches between ClientType string values and getDefaultRateForProvider.
	testCases := []struct {
		clientType    ClientType
		expectedRate  float64
		expectedBurst int
	}{
		{OpenAIClientType, 1.0, 5},
		{OpenRouterClientType, 2.0, 10},
		{DeepInfraClientType, 1.0, 5},
		{DeepSeekClientType, 0.5, 3},
		{OllamaLocalClientType, 10.0, 20},
		{OllamaCloudClientType, 10.0, 20},
		{ZAIClientType, 2.0, 10},
		{ChutesClientType, 2.0, 10},
		{LMStudioClientType, 10.0, 20},
		{MistralClientType, 1.0, 5},
		{CerebrasClientType, 2.0, 10},
	}

	for _, tc := range testCases {
		t.Run(string(tc.clientType), func(t *testing.T) {
			providerKey := string(tc.clientType)
			utils.RemoveProviderRateLimiter(providerKey)
			defer utils.RemoveProviderRateLimiter(providerKey)

			limiter := utils.GetProviderRateLimiter(providerKey)
			if limiter.GetRate() != tc.expectedRate {
				t.Errorf("Provider %s: expected rate %f, got %f",
					tc.clientType, tc.expectedRate, limiter.GetRate())
			}
			if limiter.GetBurst() != tc.expectedBurst {
				t.Errorf("Provider %s: expected burst %d, got %d",
					tc.clientType, tc.expectedBurst, limiter.GetBurst())
			}
		})
	}
}

// enhancedMockClient extends mockClient with configurable SupportsVision
type enhancedMockClient struct {
	mockClient
	supportsVisionFlag bool
	modelFlag          string
	// visionCaps is returned by VisionCapabilities() in delegation tests.
	// Left zero-valued by default; set it to a populated struct to test
	// adapter pass-through. SP-103-D3 / AUDIT-GAP-2.
	visionCaps VisionCapabilities
}

func (m *enhancedMockClient) GetModel() string     { return m.modelFlag }
func (m *enhancedMockClient) SupportsVision() bool { return m.supportsVisionFlag }

// SupportsConversationalVision reports whether inline multimodal turns
// should embed the image. Falls back to SupportsVision() for test mocks
// that don't need the OCR-only distinction.
func (m *enhancedMockClient) SupportsConversationalVision() bool {
	return m.supportsVisionFlag
}

// VisionCapabilities returns a configurable value so per-provider delegation
// tests can verify the provider adapter forwards the underlying table
// intact. Zero value (default) means "no override — fall through to the
// embedded mockClient's empty caps". SP-103-D3 / AUDIT-GAP-2.
func (m *enhancedMockClient) VisionCapabilities() VisionCapabilities {
	return m.visionCaps
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
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_DeepInfra(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepinfra")
	defer utils.RemoveProviderRateLimiter("deepinfra")
	mock := &mockClient{provider: "deepinfra"}
	adapter := NewProviderAdapter(DeepInfraClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_DeepSeek(t *testing.T) {
	utils.RemoveProviderRateLimiter("deepseek")
	defer utils.RemoveProviderRateLimiter("deepseek")
	mock := &mockClient{provider: "deepseek"}
	adapter := NewProviderAdapter(DeepSeekClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OpenRouter(t *testing.T) {
	utils.RemoveProviderRateLimiter("openrouter")
	defer utils.RemoveProviderRateLimiter("openrouter")
	mock := &mockClient{provider: "openrouter"}
	adapter := NewProviderAdapter(OpenRouterClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Chutes(t *testing.T) {
	utils.RemoveProviderRateLimiter("chutes")
	defer utils.RemoveProviderRateLimiter("chutes")
	mock := &mockClient{provider: "chutes"}
	adapter := NewProviderAdapter(ChutesClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_ZAI(t *testing.T) {
	utils.RemoveProviderRateLimiter("zai")
	defer utils.RemoveProviderRateLimiter("zai")
	mock := &mockClient{provider: "zai"}
	adapter := NewProviderAdapter(ZAIClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OllamaLocal(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama-local")
	defer utils.RemoveProviderRateLimiter("ollama-local")
	mock := &mockClient{provider: "ollama-local"}
	adapter := NewProviderAdapter(OllamaLocalClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Ollama(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama")
	defer utils.RemoveProviderRateLimiter("ollama")
	mock := &mockClient{provider: "ollama"}
	adapter := NewProviderAdapter(OllamaClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_OllamaCloud(t *testing.T) {
	utils.RemoveProviderRateLimiter("ollama-cloud")
	defer utils.RemoveProviderRateLimiter("ollama-cloud")
	mock := &mockClient{provider: "ollama-cloud"}
	adapter := NewProviderAdapter(OllamaCloudClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_LMStudio(t *testing.T) {
	utils.RemoveProviderRateLimiter("lmstudio")
	defer utils.RemoveProviderRateLimiter("lmstudio")
	mock := &mockClient{provider: "lmstudio"}
	adapter := NewProviderAdapter(LMStudioClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
}

func TestProviderAdapter_GetEndpoint_Test(t *testing.T) {
	utils.RemoveProviderRateLimiter("test")
	defer utils.RemoveProviderRateLimiter("test")
	mock := &mockClient{provider: "test"}
	adapter := NewProviderAdapter(TestClientType, mock)
	assert.Equal(t, "", adapter.GetEndpoint())
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
	// GetEndpoint delegates to the underlying client. The mock doesn't
	// implement GetEndpoint, so it returns empty — real GenericProvider
	// instances return the config endpoint.
	assert.Equal(t, "", provider.GetEndpoint())
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
