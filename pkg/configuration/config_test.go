package configuration

import (
	"testing"
)

func TestGetSubagentProvider(t *testing.T) {
	tests := []struct {
		name             string
		config           *Config
		expectedProvider string
	}{
		{
			name: "explicit subagent provider",
			config: &Config{
				SubagentProvider: "openrouter",
				LastUsedProvider: "openai",
			},
			expectedProvider: "openrouter",
		},
		{
			name: "fallback to last used provider",
			config: &Config{
				LastUsedProvider: "deepinfra",
				ProviderPriority: []string{"openai", "zai"},
			},
			expectedProvider: "deepinfra",
		},
		{
			name: "fallback to provider priority",
			config: &Config{
				ProviderPriority: []string{"zai", "openrouter"},
			},
			expectedProvider: "zai",
		},
		{
			name: "ultimate fallback to openai",
			config: &Config{
				ProviderPriority: []string{},
			},
			expectedProvider: "openai",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentProvider()
			if result != tt.expectedProvider {
				t.Errorf("GetSubagentProvider() = %s, want %s", result, tt.expectedProvider)
			}
		})
	}
}

func TestGetSubagentModel(t *testing.T) {
	tests := []struct {
		name          string
		config        *Config
		expectedModel string
	}{
		{
			name: "explicit subagent model",
			config: &Config{
				SubagentModel: "custom-model",
				ProviderModels: map[string]string{
					"openai": "gpt-4",
				},
			},
			expectedModel: "custom-model",
		},
		{
			name: "fallback to provider model",
			config: &Config{
				SubagentProvider: "openai",
				ProviderModels: map[string]string{
					"openai": "gpt-4-turbo",
				},
			},
			expectedModel: "gpt-4-turbo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.config.GetSubagentModel()
			if result != tt.expectedModel {
				t.Errorf("GetSubagentModel() = %s, want %s", result, tt.expectedModel)
			}
		})
	}
}

func TestSetSubagentProvider(t *testing.T) {
	config := &Config{}
	config.SetSubagentProvider("openrouter")

	if config.SubagentProvider != "openrouter" {
		t.Errorf("SetSubagentProvider() failed, got %s, want openrouter", config.SubagentProvider)
	}
}

func TestSetSubagentModel(t *testing.T) {
	config := &Config{}
	config.SetSubagentModel("qwen/qwen-coder-32b")

	if config.SubagentModel != "qwen/qwen-coder-32b" {
		t.Errorf("SetSubagentModel() failed, got %s, want qwen/qwen-coder-32b", config.SubagentModel)
	}
}
