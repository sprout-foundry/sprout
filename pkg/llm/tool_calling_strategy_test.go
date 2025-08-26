package llm

import (
	"testing"
)

func TestGetToolCallingStrategy(t *testing.T) {
	tests := []struct {
		modelName          string
		expectedProvider   string
		expectedCapability ToolCallingCapability
		expectedUseNative  bool
	}{
		{
			modelName:          "openai:gpt-4",
			expectedProvider:   "openai",
			expectedCapability: ToolCallingOpenAI,
			expectedUseNative:  true,
		},
		{
			modelName:          "deepinfra:deepseek-ai/DeepSeek-V3-0324",
			expectedProvider:   "deepinfra",
			expectedCapability: ToolCallingOpenAI,
			expectedUseNative:  true,
		},
		{
			modelName:          "deepinfra:meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo",
			expectedProvider:   "deepinfra",
			expectedCapability: ToolCallingOpenAI,
			expectedUseNative:  true,
		},
		{
			modelName:          "gemini:gemini-pro",
			expectedProvider:   "gemini",
			expectedCapability: ToolCallingAdvanced,
			expectedUseNative:  true,
		},
		{
			modelName:          "unknown-provider:some-model",
			expectedProvider:   "unknown-provider",
			expectedCapability: ToolCallingNone,
			expectedUseNative:  false,
		},
	}

	for _, test := range tests {
		t.Run(test.modelName, func(t *testing.T) {
			strategy := GetToolCallingStrategy(test.modelName)

			if strategy.Provider != test.expectedProvider {
				t.Errorf("Expected provider %s, got %s", test.expectedProvider, strategy.Provider)
			}

			if strategy.Capability != test.expectedCapability {
				t.Errorf("Expected capability %d, got %d", test.expectedCapability, strategy.Capability)
			}

			if strategy.UseNative != test.expectedUseNative {
				t.Errorf("Expected UseNative %v, got %v", test.expectedUseNative, strategy.UseNative)
			}
		})
	}
}

func TestPrepareToolsForProvider(t *testing.T) {
	tools := []Tool{
		{
			Type: "function",
			Function: ToolFunction{
				Name:        "test_tool",
				Description: "A test tool",
				Parameters: ToolParameters{
					Type: "object",
					Properties: map[string]ToolProperty{
						"param1": {Type: "string", Description: "Test parameter"},
					},
					Required: []string{"param1"},
				},
			},
		},
	}

	tests := []struct {
		modelName   string
		shouldError bool
	}{
		{"openai:gpt-4", false},
		{"gemini:gemini-pro", false},
		{"deepinfra:deepseek-ai/DeepSeek-V3-0324", false}, // Should return tools for native support
	}

	for _, test := range tests {
		t.Run(test.modelName, func(t *testing.T) {
			strategy := GetToolCallingStrategy(test.modelName)
			result, err := strategy.PrepareToolsForProvider(tools)

			if test.shouldError && err == nil {
				t.Error("Expected error but got none")
			}
			if !test.shouldError && err != nil {
				t.Errorf("Expected no error but got: %v", err)
			}

			if strategy.UseNative && result == nil {
				t.Error("Expected tools result for native provider but got nil")
			}
			if !strategy.UseNative && result != nil {
				t.Error("Expected nil result for non-native provider but got tools")
			}
		})
	}
}

func TestSystemPrompts(t *testing.T) {
	nativeStrategy := &ToolCallingStrategy{UseNative: true, Capability: ToolCallingOpenAI}
	textStrategy := &ToolCallingStrategy{UseNative: false, Capability: ToolCallingNone}

	nativePrompt := nativeStrategy.GetSystemPrompt()
	textPrompt := textStrategy.GetSystemPrompt()

	if len(nativePrompt) == 0 {
		t.Error("Expected non-empty system prompt for native tool calling")
	}

	if len(textPrompt) == 0 {
		t.Error("Expected non-empty system prompt for text tool calling")
	}

	// Text prompt should contain JSON formatting instructions
	if !contains(textPrompt, "JSON") {
		t.Error("Expected text tool calling prompt to contain JSON instructions")
	}

	// Native prompt should be more concise
	if len(nativePrompt) >= len(textPrompt) {
		t.Error("Expected native tool calling prompt to be shorter than text prompt")
	}
}

func contains(s, substr string) bool {
	return len(s) > 0 && len(substr) > 0 &&
		(s == substr || len(s) > len(substr) &&
			(s[:len(substr)] == substr || s[len(s)-len(substr):] == substr ||
				containsSubstring(s, substr)))
}

func containsSubstring(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
