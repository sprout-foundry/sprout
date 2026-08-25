package providers

import (
	"encoding/json"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestMaxTokensGlobalCap verifies that all providers have their max_tokens
// capped at 64K to prevent "max_tokens parameter illegal" errors from providers
// with stricter limits (e.g., ZAI's 131072 limit).
func TestMaxTokensGlobalCap(t *testing.T) {
	providers := []struct {
		name   string
		config *ProviderConfig
	}{
		{
			name: "ZAI provider",
			config: &ProviderConfig{
				Name:     "zai",
				Endpoint: "https://api.zai.com/v1/chat/completions",
				Auth: AuthConfig{
					Type: "bearer",
					Key:  "test-key",
				},
				Defaults: RequestDefaults{
					Model: "glm-4.5",
				},
				Conversion: MessageConversion{
					ReasoningContentField: "reasoning_content",
				},
				Models: ModelConfig{
					DefaultContextLimit: 128000,
				},
			},
		},
		{
			name: "GLM provider",
			config: &ProviderConfig{
				Name:     "test-glm-provider",
				Endpoint: "https://api.example.com/v1/chat/completions",
				Auth: AuthConfig{
					Type: "bearer",
					Key:  "test-key",
				},
				Defaults: RequestDefaults{
					Model: "glm-4",
				},
				Models: ModelConfig{
					DefaultContextLimit: 128000,
				},
			},
		},
		{
			name: "OpenAI provider",
			config: &ProviderConfig{
				Name:     "openai",
				Endpoint: "https://api.openai.com/v1/chat/completions",
				Auth: AuthConfig{
					Type: "bearer",
					Key:  "test-key",
				},
				Defaults: RequestDefaults{
					Model: "gpt-4",
				},
				Models: ModelConfig{
					DefaultContextLimit: 128000,
				},
			},
		},
	}

	messages := []api.Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	for _, tc := range providers {
		t.Run(tc.name, func(t *testing.T) {
			provider, err := NewGenericProvider(tc.config)
			if err != nil {
				t.Fatalf("Failed to create provider: %v", err)
			}

			// Set a high hint to simulate a scenario where the budget would exceed 64K
			provider.SetMaxTokensHint(200000)

			requestBody, err := provider.buildChatRequest(messages, nil, "", false, false)
			if err != nil {
				t.Fatalf("Failed to build chat request: %v", err)
			}

			var request map[string]interface{}
			if err := json.Unmarshal(requestBody, &request); err != nil {
				t.Fatalf("Failed to parse request JSON: %v", err)
			}

			maxTokensValue := request["max_tokens"]
			var maxTokens int
			switch v := maxTokensValue.(type) {
			case int:
				maxTokens = v
			case float64:
				maxTokens = int(v)
			default:
				t.Fatalf("max_tokens is unexpected type: %T", maxTokensValue)
			}

			// All providers should be capped at 64K
			if maxTokens > 64000 {
				t.Errorf("max_tokens %d exceeds global cap of 64000", maxTokens)
			}
		})
	}
}

// TestMaxTokensWithConfigOverride verifies that a provider-specific
// MaxTokens config setting overrides the global 64K cap.
func TestMaxTokensWithConfigOverride(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-provider",
		Endpoint: "https://api.example.com/v1/chat/completions",
		Auth: AuthConfig{
			Type: "bearer",
			Key:  "test-key",
		},
		Defaults: RequestDefaults{
			Model:     "test-model",
			MaxTokens: intPtr(32000), // Set a lower limit
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("Failed to create provider: %v", err)
	}

	// Set a high hint to test the cap
	provider.SetMaxTokensHint(200000)

	messages := []api.Message{
		{
			Role:    "user",
			Content: "Hello",
		},
	}

	requestBody, err := provider.buildChatRequest(messages, nil, "", false, false)
	if err != nil {
		t.Fatalf("Failed to build chat request: %v", err)
	}

	var request map[string]interface{}
	if err := json.Unmarshal(requestBody, &request); err != nil {
		t.Fatalf("Failed to parse request JSON: %v", err)
	}

	maxTokensValue := request["max_tokens"]
	var maxTokens int
	switch v := maxTokensValue.(type) {
	case int:
		maxTokens = v
	case float64:
		maxTokens = int(v)
	default:
		t.Fatalf("max_tokens is unexpected type: %T", maxTokensValue)
	}

	// Should be capped at the config's MaxTokens (32000), not 64000
	if maxTokens > 32000 {
		t.Errorf("max_tokens %d exceeds config override of 32000", maxTokens)
	}
}

func intPtr(i int) *int {
	return &i
}
