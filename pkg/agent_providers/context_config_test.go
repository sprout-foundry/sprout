package providers

import (
	"testing"
)

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
			DefaultContextLimit: 32000,
			ModelOverrides: map[string]int{
				"special-model": 64000,
				"ultra-model":   128000,
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
