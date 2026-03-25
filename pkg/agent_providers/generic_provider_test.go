package providers

import (
	"encoding/json"
	api "github.com/alantheprice/ledit/pkg/agent_api"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
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
	expectedProviders := []string{"cerebras", "chutes", "openrouter", "deepinfra", "deepseek", "zai", "lmstudio", "minimax", "mistral", "ollama-turbo", "openai"}

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

	_, err = provider.SendChatRequest([]api.Message{{Role: "user", Content: "hello"}}, nil, "")
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

	_, err = provider.SendChatRequest([]api.Message{{Role: "user", Content: "hello"}}, nil, "")
	if err == nil {
		t.Fatal("expected error when no model can be discovered")
	}
	if got := err.Error(); got == "" || !strings.Contains(got, "did not return any models") {
		t.Fatalf("unexpected error: %v", err)
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
		Name:     "zai",
		Endpoint: "https://example.com",
		Auth:     AuthConfig{Type: "bearer", EnvVar: "API_KEY"},
		Defaults: RequestDefaults{Model: "GLM-4.6"},
		Conversion: MessageConversion{
			ReasoningContentField: "reasoning_content",
		},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
			DefaultModel:        "GLM-4.6",
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
