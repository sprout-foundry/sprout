package providers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

func TestProviderFactory(t *testing.T) {
	factory := NewProviderFactory()

	// Test loading configs from directory
	err := factory.LoadConfigsFromDirectory("./configs")
	if err != nil {
		t.Fatalf("Failed to load configs: %v", err)
	}

	// Test that providers were loaded
	providers := factory.GetAvailableProviders()
	expectedProviders := []string{"cerebras", "chutes", "openrouter", "deepinfra", "deepseek", "zai", "zai-coding", "lmstudio", "minimax", "mistral", "ollama-cloud", "openai"}

	// Debug: print actual providers
	t.Logf("Actual providers loaded (%d): %v", len(providers), providers)

	if len(providers) != len(expectedProviders) {
		t.Fatalf("Expected %d providers, got %d", len(expectedProviders), len(providers))
	}

	for _, expected := range expectedProviders {
		found := false
		for _, actual := range providers {
			if actual == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected provider %s not found", expected)
		}
	}

	// Test creating OpenRouter provider
	provider, err := factory.CreateProvider("openrouter")
	if err != nil {
		t.Fatalf("Failed to create OpenRouter provider: %v", err)
	}

	if provider.GetProvider() != "openrouter" {
		t.Errorf("Expected provider name 'openrouter', got '%s'", provider.GetProvider())
	}

	// Test provider config
	config, err := factory.GetProviderConfig("openrouter")
	if err != nil {
		t.Fatalf("Failed to get OpenRouter config: %v", err)
	}

	if config.Defaults.Model != "openai/gpt-5" {
		t.Errorf("Expected default model 'openai/gpt-5', got '%s'", config.Defaults.Model)
	}
}

func TestGenericProviderValidation(t *testing.T) {
	// Test invalid config
	invalidConfig := &ProviderConfig{
		Name:     "", // Missing name
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type:   "bearer",
			EnvVar: "API_KEY",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
	}

	_, err := NewGenericProvider(invalidConfig)
	if err == nil {
		t.Error("Expected error for invalid config, got nil")
	}

	// Test valid config
	validConfig := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type:   "bearer",
			EnvVar: "API_KEY",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 32000,
		},
	}

	provider, err := NewGenericProvider(validConfig)
	if err != nil {
		t.Fatalf("Failed to create provider with valid config: %v", err)
	}

	if provider.GetProvider() != "test" {
		t.Errorf("Expected provider name 'test', got '%s'", provider.GetProvider())
	}
}

func TestGenericProviderSupportsVisionUsesActiveModel(t *testing.T) {
	config, err := LoadProviderConfig("./configs/zai.json")
	if err != nil {
		t.Fatalf("failed to load zai config: %v", err)
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	if err := provider.SetModel("glm-5-turbo"); err != nil {
		t.Fatalf("failed to set model: %v", err)
	}
	if provider.SupportsVision() {
		t.Fatalf("glm-5-turbo should not be treated as a vision model")
	}

	if err := provider.SetModel("glm-4.6v"); err != nil {
		t.Fatalf("failed to set model: %v", err)
	}
	if !provider.SupportsVision() {
		t.Fatalf("glm-4.6v should be treated as a vision model")
	}
}

func TestProviderFactoryValidation(t *testing.T) {
	factory := NewProviderFactory()

	// Load test configs
	err := factory.LoadConfigsFromDirectory("./configs")
	if err != nil {
		t.Fatalf("Failed to load configs: %v", err)
	}

	// Test valid provider/model combinations
	testCases := []struct {
		provider string
		model    string
		valid    bool
	}{
		{"openrouter", "openai/gpt-5", true},
		{"deepinfra", "meta-llama/Llama-3.3-70B-Instruct", true},
		{"zai", "GLM-4.6", true},
		{"nonexistent", "any-model", false},
		{"openrouter", "nonexistent-model", true}, // Won't fail since available models is empty
	}

	for _, tc := range testCases {
		err := factory.ValidateProvider(tc.provider, tc.model)
		if tc.valid && err != nil {
			t.Errorf("Expected valid combination %s/%s, got error: %v", tc.provider, tc.model, err)
		}
		if !tc.valid && err == nil {
			t.Errorf("Expected invalid combination %s/%s, got no error", tc.provider, tc.model)
		}
	}
}

func TestProviderModelContextLimits(t *testing.T) {
	factory := NewProviderFactory()

	// Load test configs
	err := factory.LoadConfigsFromDirectory("./configs")
	if err != nil {
		t.Fatalf("Failed to load configs: %v", err)
	}

	// Test OpenRouter provider
	provider, err := factory.CreateProviderWithModel("openrouter", "openai/gpt-4")
	if err != nil {
		t.Fatalf("Failed to create OpenRouter provider: %v", err)
	}

	contextLimit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("Failed to get context limit: %v", err)
	}

	// Should return 128000 for GPT-4 based on our fallback logic
	if contextLimit != 128000 {
		t.Errorf("Expected context limit 128000 for GPT-4, got %d", contextLimit)
	}
}

func TestGenericProviderGetModelContextLimitUsesCachedModel(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "fallback-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	provider.model = "cached-model"
	provider.models = []api.ModelInfo{
		{
			ID:            "cached-model",
			Name:          "cached-model",
			Provider:      "test",
			ContextLength: 128000,
		},
	}
	provider.modelsCached = true

	contextLimit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("failed to get context limit: %v", err)
	}

	if contextLimit != 128000 {
		t.Fatalf("expected cached model context length 128000, got %d", contextLimit)
	}
}

func TestGenericProviderGetModelContextLimitFallsBackWhenCachedEntryHasNoContext(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "fallback-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	provider.model = "cached-model"
	provider.models = []api.ModelInfo{
		{
			ID:            "cached-model",
			Name:          "cached-model",
			Provider:      "test",
			ContextLength: 0,
		},
	}
	provider.modelsCached = true

	contextLimit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("failed to get context limit: %v", err)
	}

	if contextLimit != 4096 {
		t.Fatalf("expected fallback context limit 4096, got %d", contextLimit)
	}
}

func TestConvertToolCallsArgumentsAsJSON(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "test-model"},
		Conversion: MessageConversion{
			ArgumentsAsJSON: true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
			SupportsVision:      false,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	toolCalls := []api.ToolCall{
		{
			ID:   "call_1",
			Type: "function",
		},
	}
	toolCalls[0].Function.Name = "shell_command"
	toolCalls[0].Function.Arguments = "{\"command\":\"ls\"}"

	converted := provider.convertToolCalls(toolCalls)
	list, ok := converted.([]map[string]interface{})
	if !ok {
		t.Fatalf("expected converted tool calls to be []map[string]interface{}")
	}
	function, ok := list[0]["function"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected function to be map")
	}
	args, ok := function["arguments"].(map[string]interface{})
	if !ok {
		t.Fatalf("expected arguments to be map after JSON conversion")
	}
	if args["command"] != "ls" {
		t.Fatalf("unexpected arguments content: %#v", args)
	}
}

func TestApplyModelSpecificSettingsRemovesUnsupportedFields(t *testing.T) {
	request := map[string]interface{}{
		"temperature": 0.7,
		"top_p":       1.0,
	}

	applyModelSpecificSettings("openai/gpt-5", request)

	if _, ok := request["temperature"]; ok {
		t.Fatalf("expected temperature to be removed for gpt-5")
	}
	if _, ok := request["top_p"]; ok {
		t.Fatalf("expected top_p to be removed for gpt-5")
	}
}

func TestApplyModelSpecificSettingsDoesNotForceGptOssReasoningEffort(t *testing.T) {
	request := map[string]interface{}{
		"temperature": 0.7,
	}

	applyModelSpecificSettings("openai/gpt-oss-20b", request)

	if _, exists := request["reasoning_effort"]; exists {
		t.Fatalf("expected no model-settings reasoning_effort injection for gpt-oss")
	}
}

func TestApplyReasoningEffortAddsGptOssReasoningEffort(t *testing.T) {
	request := map[string]interface{}{}
	applyReasoningEffort("openai/gpt-oss-20b", "medium", request)
	if request["reasoning_effort"] != "medium" {
		t.Fatalf("expected reasoning_effort=medium for gpt-oss model, got %#v", request["reasoning_effort"])
	}
}

func TestConvertMessagesDoesNotInjectReasoningEffort(t *testing.T) {
	config := &ProviderConfig{
		Name:     "openrouter",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "test-model"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{Role: "user", Content: "hello"},
	}

	converted := provider.convertMessages(messages, "high")
	if _, exists := converted[0]["reasoning_content"]; exists {
		t.Fatalf("did not expect reasoning effort to be injected into message payload")
	}
}

func TestGenericProviderAllowsEmptyDefaultModelAndDiscoversModelOnDemand(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[{"id":"discovered-model","context_length":64000}]}`))
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"resp","object":"chat.completion","created":1,"model":"discovered-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"estimated_cost":0}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "dynamic-test",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{},
		Models: ModelConfig{
			DefaultContextLimit: 64000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err != nil {
		t.Fatalf("expected provider to discover model and send request, got error: %v", err)
	}

	if provider.GetModel() != "discovered-model" {
		t.Fatalf("expected discovered model to be selected, got %q", provider.GetModel())
	}
}

func TestGenericProviderErrorsWhenNoModelConfiguredOrDiscoverable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"data":[]}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "dynamic-test",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{},
		Models: ModelConfig{
			DefaultContextLimit: 64000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err == nil {
		t.Fatal("expected error when no model can be discovered")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "did not return any models") {
		t.Fatalf("unexpected error: %v", got)
	}
}

func TestConvertMessagesSkipsMinimaxReasoningDetailsHistory(t *testing.T) {
	config := &ProviderConfig{
		Name:     "minimax",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "MiniMax-M2.5"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_details",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "MiniMax-M2.5",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{
			Role:             "assistant",
			Content:          "answer",
			ReasoningContent: "chain of thought from previous provider",
		},
	}

	converted := provider.convertMessages(messages, "")
	if _, exists := converted[0]["reasoning_details"]; exists {
		t.Fatalf("did not expect reasoning_details to be sent as string history for minimax")
	}
}

func TestConvertMessagesPreservesReasoningContentForCompatibleProviders(t *testing.T) {
	config := &ProviderConfig{
		Name:     "openai", // OpenAI-compatible provider preserves reasoning_content
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "gpt-4"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "gpt-4",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{
			Role:             "assistant",
			Content:          "answer",
			ReasoningContent: "preserve me",
		},
	}

	converted := provider.convertMessages(messages, "")
	value, exists := converted[0]["reasoning_content"]
	if !exists {
		t.Fatalf("expected reasoning_content to be preserved for compatible provider")
	}
	if value != "preserve me" {
		t.Fatalf("unexpected reasoning_content value: %v", value)
	}
}

func TestConvertMessagesSkipsReasoningContentForZAI(t *testing.T) {
	config := &ProviderConfig{
		Name:     "zai",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "glm-5.1"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 200000,
			DefaultModel:        "glm-5.1",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{
			Role:             "assistant",
			Content:          "answer",
			ReasoningContent: "stale reasoning that should be stripped",
		},
	}

	converted := provider.convertMessages(messages, "")
	_, exists := converted[0]["reasoning_content"]
	if exists {
		t.Fatalf("expected reasoning_content to be stripped for ZAI provider, but it was present: %v", converted[0])
	}
}

func TestGenericProviderSummarizesCloudflareHTMLTimeouts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(524)
		_, _ = w.Write([]byte(`<!DOCTYPE html>
<html lang="en-US">
<head><title>local-aprice.dev | 524: A timeout occurred</title></head>
<body>
<div>Cloudflare</div>
<div>Error code 524</div>
</body>
</html>`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(&ProviderConfig{
		Name:     "ai-worker",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err == nil {
		t.Fatal("expected timeout error")
	}

	got := err.Error()
	if !strings.Contains(got, "HTTP 524: upstream timeout (Cloudflare 524 HTML error page)") {
		t.Fatalf("unexpected error: %s", got)
	}
	if strings.Contains(strings.ToLower(got), "<!doctype html") || strings.Contains(got, "<html") {
		t.Fatalf("expected HTML body to be suppressed, got: %s", got)
	}
}

func TestGenericProviderExtractsJSONErrorMessage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"model not available for this account"}}`))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(&ProviderConfig{
		Name:     "json-error-test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err == nil {
		t.Fatal("expected JSON error")
	}

	got := err.Error()
	if !strings.Contains(got, "HTTP 400: model not available for this account") {
		t.Fatalf("unexpected error: %s", got)
	}
	if strings.Contains(got, "{\"error\"") {
		t.Fatalf("expected JSON body to be summarized, got: %s", got)
	}
}

func TestGenericProviderTruncatesLargePlainTextErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(strings.Repeat("backend overload ", 40)))
	}))
	defer server.Close()

	provider, err := NewGenericProvider(&ProviderConfig{
		Name:     "plain-error-test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	})
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	_, err = provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err == nil {
		t.Fatal("expected plain text error")
	}

	got := err.Error()
	if !strings.HasPrefix(got, "HTTP "+strconv.Itoa(http.StatusBadGateway)+": ") {
		t.Fatalf("unexpected error prefix: %s", got)
	}
	if len(got) > len("HTTP 502: ")+maxProviderErrorBodyPreview+10 {
		t.Fatalf("expected truncated error, got len=%d: %s", len(got), got)
	}
	if !strings.HasSuffix(got, "...") {
		t.Fatalf("expected truncated error to end with ellipsis, got: %s", got)
	}
}

func TestShouldRetryWithMaxCompletionTokens(t *testing.T) {
	body := []byte(`{"error":{"message":"Unsupported parameter: 'max_tokens' is not supported with this model. Use 'max_completion_tokens' instead."}}`)
	if !shouldRetryWithMaxCompletionTokens(body) {
		t.Fatalf("expected retry detection to be true")
	}
}

func TestRewriteMaxTokensToMaxCompletionTokens(t *testing.T) {
	request := []byte(`{"model":"openai/gpt-oss-20b","messages":[],"max_tokens":1234,"stream":false}`)

	updated, changed, err := rewriteMaxTokensToMaxCompletionTokens(request)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !changed {
		t.Fatalf("expected payload to be rewritten")
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(updated, &payload); err != nil {
		t.Fatalf("failed to decode updated payload: %v", err)
	}

	if _, exists := payload["max_tokens"]; exists {
		t.Fatalf("expected max_tokens to be removed")
	}
	if payload["max_completion_tokens"] != float64(1234) {
		t.Fatalf("expected max_completion_tokens=1234, got %#v", payload["max_completion_tokens"])
	}
}

// --- Tests for SetHTTPClient / SetStreamingClient (WASM bridge support) ---

func TestSetHTTPClient_ReplacesClient(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// The default client created by NewGenericProvider has a non-nil Timeout.
	oldClient := provider.httpClient

	customClient := &http.Client{Timeout: 42}
	provider.SetHTTPClient(customClient)

	if provider.httpClient != customClient {
		t.Fatal("SetHTTPClient did not replace the internal httpClient")
	}
	if provider.httpClient == oldClient {
		t.Fatal("SetHTTPClient still references the old client")
	}
}

func TestSetStreamingClient_ReplacesClient(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	oldClient := provider.streamingClient

	customClient := &http.Client{Timeout: 99}
	provider.SetStreamingClient(customClient)

	if provider.streamingClient != customClient {
		t.Fatal("SetStreamingClient did not replace the internal streamingClient")
	}
	if provider.streamingClient == oldClient {
		t.Fatal("SetStreamingClient still references the old client")
	}
}

func TestSetHTTPClient_ClientActuallyUsed(t *testing.T) {
	// Create a test server that responds with a known marker.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"resp","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}]}`))
	}))
	defer server.Close()

	// Point the provider at our test server.
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Swap in a custom client that uses a different transport — this proves
	// the provider actually uses the injected client.
	customTransport := &roundTripFunc{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header:     http.Header{"Content-Type": []string{"application/json"}},
			Body:       io.NopCloser(strings.NewReader(`{"id":"resp","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"custom"},"finish_reason":"stop"}]}`)),
		}, nil
	}}
	customClient := &http.Client{Transport: customTransport}
	provider.SetHTTPClient(customClient)

	resp, err := provider.SendChatRequest(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	if resp.Choices[0].Message.Content != "custom" {
		t.Fatalf("expected response from custom client, got %q", resp.Choices[0].Message.Content)
	}
}

func TestSetStreamingClient_ClientActuallyUsed(t *testing.T) {
	// Create a test server that returns SSE-style streaming.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("expected flusher")
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(http.StatusOK)

		for _, chunk := range []string{"hello ", "world"} {
			_, _ = w.Write([]byte("data: " + `{"choices":[{"delta":{"content":"` + chunk + `"}}]}` + "\n\n"))
			flusher.Flush()
		}
		_, _ = w.Write([]byte("data: [DONE]\n\n"))
		flusher.Flush()
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test",
		Endpoint: server.URL,
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Swap in a custom streaming client that overrides the transport.
	customTransport := &roundTripFunc{handler: func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Status:     "200 OK",
			Header: http.Header{
				"Content-Type":  []string{"text/event-stream"},
				"Cache-Control": []string{"no-cache"},
			},
			Body: io.NopCloser(strings.NewReader(
				"data: {\"choices\":[{\"delta\":{\"content\":\"custom-stream\"}}]}\n\ndata: [DONE]\n\n",
			)),
		}, nil
	}}
	customClient := &http.Client{Transport: customTransport}
	provider.SetStreamingClient(customClient)

	cb := api.StreamCallback(func(content, contentType string) {})
	resp, err := provider.SendChatRequestStream(context.Background(), []api.Message{{Role: "user", Content: "hello"}}, nil, "", false, cb)
	if err != nil {
		t.Fatalf("streaming request failed: %v", err)
	}
	if resp.Choices[0].Message.Content != "custom-stream" {
		t.Fatalf("expected response from custom streaming client, got %q", resp.Choices[0].Message.Content)
	}
}

// roundTripFunc is a minimal http.RoundTripper implementation for testing.
type roundTripFunc struct {
	handler func(req *http.Request) (*http.Response, error)
}

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f.handler(req)
}

func TestConvertMessagesMergesConsecutiveUserMessages(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "test-model"},
		Conversion: MessageConversion{
			IncludeToolCallID: true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Simulate: user sent "continue", API errored (no response), user sent "continue" again
	messages := []api.Message{
		{Role: "user", Content: "fix the bug"},
		{Role: "assistant", Content: "I'll fix it", ToolCalls: []api.ToolCall{
			{ID: "call_1", Type: "function"},
		}},
		{Role: "tool", Content: "fix applied", ToolCallID: "call_1"},
		{Role: "user", Content: "continue"},     // first retry
		{Role: "user", Content: "continue"},     // second retry (duplicate due to API error)
	}

	converted := provider.convertMessages(messages, "")

	// Should have 4 messages: user, assistant(+tool_calls), tool, user(merged)
	if len(converted) != 4 {
		t.Fatalf("expected 4 messages after merge, got %d", len(converted))
	}

	// Last message should be a single merged user message
	last := converted[3]
	if last["role"] != "user" {
		t.Fatalf("expected last message role=user, got %v", last["role"])
	}
	content, ok := last["content"].(string)
	if !ok {
		t.Fatalf("expected string content, got %T", last["content"])
	}
	if content != "continue\ncontinue" {
		t.Fatalf("expected merged content 'continue\\ncontinue', got %q", content)
	}

	// Tool message should still have tool_call_id (not merged)
	toolMsg := converted[2]
	if toolMsg["role"] != "tool" {
		t.Fatalf("expected tool message, got role=%v", toolMsg["role"])
	}
	if toolMsg["tool_call_id"] != "call_1" {
		t.Fatalf("expected tool_call_id=call_1, got %v", toolMsg["tool_call_id"])
	}
}

func TestConvertMessagesDoesNotMergeToolMessagesWithSameRole(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "test-model"},
		Conversion: MessageConversion{
			IncludeToolCallID: true,
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Parallel tool calls produce consecutive tool messages — these must NOT be merged
	messages := []api.Message{
		{Role: "user", Content: "run two commands"},
		{Role: "assistant", Content: "", ToolCalls: []api.ToolCall{
			{ID: "call_a", Type: "function"},
			{ID: "call_b", Type: "function"},
		}},
		{Role: "tool", Content: "result a", ToolCallID: "call_a"},
		{Role: "tool", Content: "result b", ToolCallID: "call_b"},
	}

	converted := provider.convertMessages(messages, "")

	// All 4 messages should be preserved (no merging of tool messages)
	if len(converted) != 4 {
		t.Fatalf("expected 4 messages (tool messages must not merge), got %d", len(converted))
	}

	if converted[2]["tool_call_id"] != "call_a" {
		t.Fatalf("first tool message lost tool_call_id")
	}
	if converted[3]["tool_call_id"] != "call_b" {
		t.Fatalf("second tool message lost tool_call_id")
	}
}

func TestConvertMessagesMergesConsecutiveAssistantMessagesWithoutToolCalls(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "test-model"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "test-model",
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	messages := []api.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "Let me", ReasoningContent: "thinking step 1"},
		{Role: "assistant", Content: "think about this", ReasoningContent: "thinking step 2"},
	}

	converted := provider.convertMessages(messages, "")

	// Should have 2 messages: user, assistant(merged)
	if len(converted) != 2 {
		t.Fatalf("expected 2 messages after merge, got %d", len(converted))
	}

	merged := converted[1]
	content, _ := merged["content"].(string)
	if content != "Let me\nthink about this" {
		t.Fatalf("expected merged content, got %q", content)
	}

	// Reasoning from first message should be preserved
	if merged["reasoning_content"] != "thinking step 1" {
		t.Fatalf("expected first reasoning preserved, got %v", merged["reasoning_content"])
	}
}
