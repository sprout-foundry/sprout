package providers

import (
	"encoding/json"
	"testing"
)

func TestLMStudioEmbeddedConfigUsesRuntimeModelMetadata(t *testing.T) {
	data, err := embeddedConfigs.ReadFile("configs/lmstudio.json")
	if err != nil {
		t.Fatalf("failed to read embedded LM Studio config: %v", err)
	}

	// LM Studio users control both the loaded model and its runtime context
	// window. The live /api/v0/models response is authoritative, so the embedded
	// config must not claim model-specific limits that can contradict it.
	var raw struct {
		Defaults map[string]json.RawMessage `json:"defaults"`
		Models   map[string]json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("failed to parse embedded LM Studio config: %v", err)
	}

	for _, key := range []string{"available_models", "default_model", "model_info", "model_overrides", "pattern_overrides", "vision_model"} {
		if _, exists := raw.Models[key]; exists {
			t.Errorf("models.%s must be omitted from the LM Studio config", key)
		}
	}
	defaultModelJSON, exists := raw.Defaults["model"]
	if !exists {
		t.Fatal("defaults.model must be present and empty so runtime discovery is used")
	}
	var defaultModel string
	if err := json.Unmarshal(defaultModelJSON, &defaultModel); err != nil {
		t.Fatalf("failed to decode defaults.model: %v", err)
	}
	if defaultModel != "" {
		t.Errorf("expected defaults.model to be empty, got %q", defaultModel)
	}

	var config ProviderConfig
	if err := json.Unmarshal(data, &config); err != nil {
		t.Fatalf("failed to decode embedded LM Studio config: %v", err)
	}
	if err := config.Validate(); err != nil {
		t.Fatalf("embedded LM Studio config is invalid: %v", err)
	}
	if config.Defaults.Model != "" {
		t.Errorf("expected no seeded LM Studio model, got %q", config.Defaults.Model)
	}
	provider, err := NewGenericProvider(&config)
	if err != nil {
		t.Fatalf("failed to create provider from embedded LM Studio config: %v", err)
	}
	if got := provider.GetModel(); got != "" {
		t.Errorf("expected provider to start without a seeded model, got %q", got)
	}
	if !config.Models.SupportsVision {
		t.Error("expected LM Studio vision support to remain enabled")
	}

	for _, model := range []string{
		"anything-else",
		"llama-3-70b-instruct",
		"llama-3.1-70b-instruct",
		"mistral-7b-instruct",
		"qwen2.5-coder-32b-instruct",
		"qwen3-coder:30b",
		"yi-34b-chat",
	} {
		if got := config.GetContextLimit(model); got != 32768 {
			t.Errorf("GetContextLimit(%q) = %d, want fallback 32768", model, got)
		}
	}
}

func TestConfigurationBasedContextLimits(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-provider",
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
		},
		Defaults: RequestDefaults{
			Model: "default-model",
		},
		Models: ModelConfig{
			DefaultContextLimit:        32000,
			DefaultMaxCompletionTokens: 16000,
			ModelOverrides: map[string]int{
				"special-model": 64000,
				"ultra-model":   128000,
			},
			MaxCompletionOverrides: map[string]int{
				"special-model": 20000,
			},
			PatternOverrides: []PatternOverride{
				{
					Pattern:      "gpt-.*",
					ContextLimit: 128000,
				},
				{
					Pattern:      "claude-.*",
					ContextLimit: 200000,
				},
			},
			CompletionPatternOverrides: []PatternOverride{
				{
					Pattern:      "gpt-.*",
					ContextLimit: 64000,
				},
			},
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Test various model names and their expected context limits
	testCases := []struct {
		modelName     string
		expectedLimit int
		description   string
	}{
		{
			modelName:     "special-model",
			expectedLimit: 64000,
			description:   "Exact model match override",
		},
		{
			modelName:     "ultra-model",
			expectedLimit: 128000,
			description:   "Another exact model match override",
		},
		{
			modelName:     "gpt-4-turbo",
			expectedLimit: 128000,
			description:   "Pattern match for GPT models",
		},
		{
			modelName:     "claude-3-sonnet",
			expectedLimit: 200000,
			description:   "Pattern match for Claude models",
		},
		{
			modelName:     "unknown-model",
			expectedLimit: 32000,
			description:   "Fallback to default context limit",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.modelName, func(t *testing.T) {
			provider.SetModel(tc.modelName)
			limit, err := provider.GetModelContextLimit()
			if err != nil {
				t.Fatalf("Failed to get model context limit: %v", err)
			}
			if limit != tc.expectedLimit {
				t.Errorf("Expected context limit %d for %s (%s), got %d",
					tc.expectedLimit, tc.modelName, tc.description, limit)
			}
		})
	}
}

// TestModelInfoContextLimits verifies that GetContextLimit falls back to
// model_info entries when no override or pattern matches.
// This is critical for provider/model names like "MiniMaxAI/MiniMax-M2.7"
// that need to resolve against model_info ID "MiniMax-M2.7".
func TestModelInfoContextLimits(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-deepinfra",
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
		},
		Defaults: RequestDefaults{
			Model: "default-model",
		},
		Models: ModelConfig{
			// Provider default (would apply without model_info fallback)
			DefaultContextLimit: 131072,
			ModelInfo: []ModelInfo{
				{ID: "MiniMax-M2.5", ContextLength: 196608},
				{ID: "MiniMax-M2.7", ContextLength: 196608},
				{ID: "MiniMax-M2.5-Lightning", ContextLength: 1000000},
				{ID: "gpt-4o", ContextLength: 128000},
			},
		},
	}

	testCases := []struct {
		modelName     string
		expectedLimit int
		description   string
	}{
		{
			modelName:     "MiniMaxAI/MiniMax-M2.7",
			expectedLimit: 196608,
			description:   "Full provider/model name suffix matches model_info ID",
		},
		{
			modelName:     "deepinfra/MiniMax-M2.5",
			expectedLimit: 196608,
			description:   "Different provider prefix still matches via /suffix",
		},
		{
			modelName:     "MiniMax-M2.5",
			expectedLimit: 196608,
			description:   "Exact ID match",
		},
		{
			modelName:     "MiniMax-M2.5-Lightning",
			expectedLimit: 1000000,
			description:   "1M context from model_info",
		},
		{
			modelName:     "unknown-model",
			expectedLimit: 131072,
			description:   "No match in model_info, falls back to default_context_limit",
		},
		{
			modelName:     "gpt-4o",
			expectedLimit: 128000,
			description:   "model_info entry with exact match",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.modelName, func(t *testing.T) {
			limit := config.GetContextLimit(tc.modelName)
			if limit != tc.expectedLimit {
				t.Errorf("Expected context limit %d for %s (%s), got %d",
					tc.expectedLimit, tc.modelName, tc.description, limit)
			}
		})
	}
}

// TestPatternOverrideBeatsModelInfo verifies that pattern_overrides take
// priority over model_info entries (step 2 > step 3 in the resolution order).
func TestPatternOverrideBeatsModelInfo(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-provider",
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
		},
		Defaults: RequestDefaults{
			Model: "default-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 131072,
			PatternOverrides: []PatternOverride{
				{
					Pattern:      "MiniMax-.*",
					ContextLimit: 999999, // Intentional admin override — larger than catalog
				},
			},
			ModelInfo: []ModelInfo{
				{ID: "MiniMax-M2.5", ContextLength: 196608},
				{ID: "MiniMax-M2.7", ContextLength: 196608},
			},
		},
	}

	// Pattern override should win over model_info for the same model
	limit := config.GetContextLimit("MiniMaxAI/MiniMax-M2.7")
	if limit != 999999 {
		t.Errorf("Expected pattern override 999999 to beat model_info 196608, got %d", limit)
	}
}

func TestConfigurationBasedCompletionLimits(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-provider",
		Endpoint: "https://api.example.com",
		Auth: AuthConfig{
			Type: "bearer",
		},
		Defaults: RequestDefaults{
			Model: "default-model",
		},
		Models: ModelConfig{
			DefaultContextLimit:        32000,
			DefaultMaxCompletionTokens: 16000,
			MaxCompletionOverrides: map[string]int{
				"special-model": 20000,
				"gpt-4o-mini":   16384,
			},
			CompletionPatternOverrides: []PatternOverride{
				{
					Pattern:      "gpt-.*",
					ContextLimit: 64000,
				},
			},
		},
	}

	testCases := []struct {
		modelName     string
		expectedLimit int
	}{
		{"special-model", 20000},
		{"gpt-4o-mini", 16384},
		{"gpt-4-turbo", 64000},
		{"unknown-model", 16000},
	}

	for _, tc := range testCases {
		t.Run(tc.modelName, func(t *testing.T) {
			limit := config.GetMaxCompletionLimit(tc.modelName)
			if limit != tc.expectedLimit {
				t.Errorf("expected completion limit %d for %s, got %d", tc.expectedLimit, tc.modelName, limit)
			}
		})
	}
}

func TestConfigurationValidation(t *testing.T) {
	testCases := []struct {
		name          string
		config        *ProviderConfig
		expectError   bool
		errorContains string
	}{
		{
			name: "Valid config with default context limit",
			config: &ProviderConfig{
				Name:     "test",
				Endpoint: "https://api.example.com",
				Auth: AuthConfig{
					Type: "bearer",
				},
				Defaults: RequestDefaults{
					Model: "test-model",
				},
				Models: ModelConfig{
					DefaultContextLimit: 32000,
				},
			},
			expectError: false,
		},
		{
			name: "Valid config with legacy context limit",
			config: &ProviderConfig{
				Name:     "test",
				Endpoint: "https://api.example.com",
				Auth: AuthConfig{
					Type: "bearer",
				},
				Defaults: RequestDefaults{
					Model: "test-model",
				},
				Models: ModelConfig{
					ContextLimit: 32000,
				},
			},
			expectError: false,
		},
		{
			name: "Invalid config - no context limit specified",
			config: &ProviderConfig{
				Name:     "test",
				Endpoint: "https://api.example.com",
				Auth: AuthConfig{
					Type: "bearer",
				},
				Defaults: RequestDefaults{
					Model: "test-model",
				},
				Models: ModelConfig{},
			},
			expectError:   true,
			errorContains: "invalid model configuration",
		},
		{
			name: "Invalid config - negative context limit in model override",
			config: &ProviderConfig{
				Name:     "test",
				Endpoint: "https://api.example.com",
				Auth: AuthConfig{
					Type: "bearer",
				},
				Defaults: RequestDefaults{
					Model: "test-model",
				},
				Models: ModelConfig{
					DefaultContextLimit: 32000,
					ModelOverrides: map[string]int{
						"bad-model": -1000,
					},
				},
			},
			expectError:   true,
			errorContains: "invalid model configuration",
		},
		{
			name: "Invalid config - invalid regex pattern",
			config: &ProviderConfig{
				Name:     "test",
				Endpoint: "https://api.example.com",
				Auth: AuthConfig{
					Type: "bearer",
				},
				Defaults: RequestDefaults{
					Model: "test-model",
				},
				Models: ModelConfig{
					DefaultContextLimit: 32000,
					PatternOverrides: []PatternOverride{
						{
							Pattern:      "[invalid regex",
							ContextLimit: 128000,
						},
					},
				},
			},
			expectError:   true,
			errorContains: "invalid model configuration",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.config.Validate()
			if tc.expectError {
				if err == nil {
					t.Errorf("Expected error containing '%s', got nil", tc.errorContains)
				} else if tc.errorContains != "" && err.Error()[:len(tc.errorContains)] != tc.errorContains {
					t.Errorf("Expected error containing '%s', got '%s'", tc.errorContains, err.Error())
				}
			} else {
				if err != nil {
					t.Errorf("Expected no error, got: %v", err)
				}
			}
		})
	}
}
