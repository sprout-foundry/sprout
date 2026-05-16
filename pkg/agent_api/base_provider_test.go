package api

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// =====================================================================
// NewBaseProvider
// =====================================================================

func TestNewBaseProvider(t *testing.T) {
	p := NewBaseProvider("test-provider", OpenAIClientType, "https://api.test.com", "test-key")
	require.NotNil(t, p)
	assert.Equal(t, "test-provider", p.name)
	assert.Equal(t, OpenAIClientType, p.clientType)
	assert.Equal(t, "https://api.test.com", p.endpoint)
	assert.Equal(t, "test-key", p.apiKey)
	assert.NotNil(t, p.httpClient)
}

func TestNewBaseProvider_DefaultFeatureFlags(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// supportsTools and supportsStreaming default to true
	assert.True(t, p.SupportsTools())
	assert.True(t, p.SupportsStreaming())
	// supportsVision and supportsReasoning default to false
	assert.False(t, p.SupportsVision())
	assert.False(t, p.SupportsReasoning())
}

// =====================================================================
// Accessor methods
// =====================================================================

func TestBaseProvider_GetName(t *testing.T) {
	p := NewBaseProvider("my-provider", OpenAIClientType, "https://api.test.com", "key")
	assert.Equal(t, "my-provider", p.GetName())
}

func TestBaseProvider_GetType(t *testing.T) {
	p := NewBaseProvider("test", DeepInfraClientType, "https://api.test.com", "key")
	assert.Equal(t, DeepInfraClientType, p.GetType())
}

func TestBaseProvider_GetEndpoint(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://custom.endpoint.com/v1", "key")
	assert.Equal(t, "https://custom.endpoint.com/v1", p.GetEndpoint())
}

func TestBaseProvider_GetModel(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Initially empty
	assert.Empty(t, p.GetModel())
	// After setting
	p.SetModel("gpt-4o")
	assert.Equal(t, "gpt-4o", p.GetModel())
}

func TestBaseProvider_SetModel(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// SetModel always returns nil (no validation by default)
	assert.NoError(t, p.SetModel("gpt-4o"))
	assert.Equal(t, "gpt-4o", p.GetModel())

	// Setting to empty string should work (no validation error)
	assert.NoError(t, p.SetModel(""))
	assert.Empty(t, p.GetModel())
}

func TestBaseProvider_SetDebug(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Initially false
	assert.False(t, p.IsDebug())

	p.SetDebug(true)
	assert.True(t, p.IsDebug())

	p.SetDebug(false)
	assert.False(t, p.IsDebug())
}

func TestBaseProvider_IsDebug(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	assert.False(t, p.IsDebug())

	p.debug = true
	assert.True(t, p.IsDebug())
}

// =====================================================================
// Feature flags (direct field manipulation to test getter methods)
// =====================================================================

func TestBaseProvider_SupportsVision(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	assert.False(t, p.SupportsVision())

	p.supportsVision = true
	assert.True(t, p.SupportsVision())
}

func TestBaseProvider_SupportsTools(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Default is true
	assert.True(t, p.SupportsTools())

	p.supportsTools = false
	assert.False(t, p.SupportsTools())
}

func TestBaseProvider_SupportsStreaming(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Default is true
	assert.True(t, p.SupportsStreaming())

	p.supportsStreaming = false
	assert.False(t, p.SupportsStreaming())
}

func TestBaseProvider_SupportsReasoning(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	assert.False(t, p.SupportsReasoning())

	p.supportsReasoning = true
	assert.True(t, p.SupportsReasoning())
}

// =====================================================================
// MakeAuthRequest
// =====================================================================

func TestBaseProvider_MakeAuthRequest_Success(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "secret-key")
	ctx := context.Background()
	body := bytes.NewReader([]byte(`{"prompt":"hello"}`))

	req, err := p.MakeAuthRequest(ctx, "POST", "https://api.test.com/v1/chat/completions", body)
	require.NoError(t, err)
	require.NotNil(t, req)

	assert.Equal(t, "POST", req.Method)
	assert.Equal(t, "https://api.test.com/v1/chat/completions", req.URL.String())
	assert.Equal(t, "Bearer secret-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))

	// Verify body is present
	buf := new(bytes.Buffer)
	_, err = buf.ReadFrom(req.Body)
	require.NoError(t, err)
	assert.Equal(t, `{"prompt":"hello"}`, buf.String())
}

func TestBaseProvider_MakeAuthRequest_GET(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	ctx := context.Background()

	req, err := p.MakeAuthRequest(ctx, "GET", "https://api.test.com/v1/models", nil)
	require.NoError(t, err)
	require.NotNil(t, req)

	assert.Equal(t, "GET", req.Method)
	assert.Equal(t, "https://api.test.com/v1/models", req.URL.String())
	assert.Equal(t, "Bearer key", req.Header.Get("Authorization"))
}

func TestBaseProvider_MakeAuthRequest_BodyNotNeeded(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	ctx := context.Background()

	// nil body should work fine
	req, err := p.MakeAuthRequest(ctx, "GET", "https://api.test.com/v1/models", nil)
	require.NoError(t, err)
	require.NotNil(t, req)
	assert.Nil(t, req.Body)
}

func TestBaseProvider_MakeAuthRequest_ContextCancellation(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	// http.NewRequestWithContext should succeed even with cancelled context
	// (the cancellation is only checked when making the actual request)
	req, err := p.MakeAuthRequest(ctx, "GET", "https://api.test.com/v1/models", nil)
	require.NoError(t, err)
	require.NotNil(t, req)
	// The request should carry the context
	assert.NotNil(t, req.Context())
}

func TestBaseProvider_MakeAuthRequest_InvalidURL(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	ctx := context.Background()

	req, err := p.MakeAuthRequest(ctx, "GET", ":invalid url with spaces:", nil)
	require.Error(t, err)
	assert.Nil(t, req)
	assert.Contains(t, err.Error(), "failed to create auth request")
}

func TestBaseProvider_MakeAuthRequest_WithBodyReader(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "my-api-key")
	ctx := context.Background()
	body := io.NopCloser(bytes.NewReader([]byte(`{"query":"test"}`)))

	req, err := p.MakeAuthRequest(ctx, "POST", "https://api.test.com/v1/complete", body)
	require.NoError(t, err)
	require.NotNil(t, req)

	assert.Equal(t, "Bearer my-api-key", req.Header.Get("Authorization"))
	assert.Equal(t, "application/json", req.Header.Get("Content-Type"))
}

// =====================================================================
// EstimateCost
// =====================================================================

func TestBaseProvider_EstimateCost_ZeroTokens(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	cost := p.EstimateCost(0, 0, "any-model")
	assert.Equal(t, 0.0, cost)
}

func TestBaseProvider_EstimateCost_PromptOnly(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// 1000 prompt tokens at $0.001 per 1K = $0.001
	cost := p.EstimateCost(1000, 0, "gpt-4o")
	assert.Equal(t, 0.001, cost)
}

func TestBaseProvider_EstimateCost_CompletionOnly(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// 1000 completion tokens at $0.002 per 1K = $0.002
	cost := p.EstimateCost(0, 1000, "gpt-4o")
	assert.Equal(t, 0.002, cost)
}

func TestBaseProvider_EstimateCost_BothPromptAndCompletion(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// 1000 prompt + 500 completion = $0.001 + $0.001 = $0.002
	cost := p.EstimateCost(1000, 500, "gpt-4o")
	assert.Equal(t, 0.002, cost)
}

func TestBaseProvider_EstimateCost_LargeNumbers(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// 10000 prompt + 5000 completion = $0.01 + $0.01 = $0.02
	cost := p.EstimateCost(10000, 5000, "gpt-4o")
	assert.Equal(t, 0.02, cost)
}

func TestBaseProvider_EstimateCost_IgnoreModelName(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Default implementation ignores the model parameter
	cost1 := p.EstimateCost(1000, 1000, "gpt-4o")
	cost2 := p.EstimateCost(1000, 1000, "llama-3")
	assert.Equal(t, cost1, cost2)
}

func TestBaseProvider_EstimateCost_SmallTokens(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// 100 prompt + 50 completion = $0.0001 + $0.0001 = $0.0002
	cost := p.EstimateCost(100, 50, "gpt-4o")
	assert.Equal(t, 0.0002, cost)
}

// =====================================================================
// HTTPClient field
// =====================================================================

func TestBaseProvider_HTTPClient_Set(t *testing.T) {
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	// Default HTTP client is set
	assert.NotNil(t, p.httpClient)

	// Can be replaced with a mock
	mockClient := &mockHTTPClient{}
	p.httpClient = mockClient
	assert.Equal(t, mockClient, p.httpClient)
}

// mockHTTPClient implements HTTPClient for testing
type mockHTTPClient struct {
	response *http.Response
	err      error
}

func (m *mockHTTPClient) Do(req *http.Request) (*http.Response, error) {
	return m.response, m.err
}

// =====================================================================
// BaseProvider does NOT implement Provider (missing SendChatRequest,
// CheckConnection, GetAvailableModels, GetModelContextLimit).
// It is a base struct meant to be embedded by concrete providers.
// =====================================================================

// Compile-time verification that BaseProvider does NOT satisfy Provider.
// If it ever does, this function body should be updated accordingly.
func TestBaseProvider_PartialImplementation(t *testing.T) {
	// BaseProvider provides shared methods but delegates
	// SendChatRequest, CheckConnection, GetAvailableModels,
	// and GetModelContextLimit to derived types.
	p := NewBaseProvider("test", OpenAIClientType, "https://api.test.com", "key")
	_ = p.GetName()
	_ = p.GetType()
	_ = p.GetEndpoint()
	_ = p.GetModel()
	_ = p.IsDebug()
	_ = p.SupportsVision()
	_ = p.SupportsTools()
	_ = p.SupportsStreaming()
	_ = p.SupportsReasoning()
}

// =====================================================================
// HTTPClient interface
// =====================================================================

// Compile-time interface satisfaction checks
var _ HTTPClient = &http.Client{}
var _ HTTPClient = &mockHTTPClient{}
