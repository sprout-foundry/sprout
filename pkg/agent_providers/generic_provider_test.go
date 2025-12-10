package providers

import (
	api "github.com/alantheprice/ledit/pkg/agent_api"
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
	expectedProviders := []string{"chutes", "openrouter", "deepinfra", "zai", "lmstudio"}

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
