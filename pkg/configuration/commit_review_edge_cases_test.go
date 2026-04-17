package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCommitProviderEdgeCases tests edge cases for commit provider configuration
func TestCommitProviderEdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		config            *Config
		expectedProvider  string
		expectedModel     string
	}{
		{
			name: "whitespace provider returns as-is",
			config: &Config{
				CommitProvider:   "  ",
				LastUsedProvider: "openai",
			},
			expectedProvider: "  ", // Whitespace is not empty, so it's returned as-is
			expectedModel:    "",
		},
		{
			name: "whitespace model returns as-is",
			config: &Config{
				CommitProvider:  "openai",
				CommitModel:     "  ",
				ProviderModels:  map[string]string{"openai": "gpt-4"},
			},
			expectedProvider: "openai",
			expectedModel:    "  ", // Whitespace is not empty, so it's returned as-is
		},
		{
			name: "provider not in provider models map",
			config: &Config{
				CommitProvider: "nonexistent",
				CommitModel:    "",
				ProviderModels: map[string]string{"openai": "gpt-4"},
			},
			expectedProvider: "nonexistent",
			expectedModel:    "", // Empty when provider not found
		},
		{
			name: "all fallback levels exhausted returns default",
			config: &Config{
				CommitProvider:    "",
				LastUsedProvider: "",
				ProviderPriority:  []string{},
				ProviderModels:   map[string]string{"ollama-local": "qwen3-coder:30b"}, // Ultimate default has this model
			},
			expectedProvider: "ollama-local", // Ultimate default
			expectedModel:    "qwen3-coder:30b", // Model from the default provider
		},
		{
			name: "provider priority first is empty string",
			config: &Config{
				CommitProvider:    "",
				LastUsedProvider: "",
				ProviderPriority:  []string{"", "openai"},
			},
			expectedProvider: "", // Empty string is first in priority
			expectedModel:    "",
		},
		{
			name: "multiple fallback levels",
			config: &Config{
				CommitProvider:    "",
				LastUsedProvider: "",
				ProviderPriority:  []string{"deepinfra", "zai", "openai"},
				ProviderModels:   map[string]string{"deepinfra": "deepseek-ai/DeepSeek-V3.1-Terminus"},
			},
			expectedProvider: "deepinfra",
			expectedModel:    "deepseek-ai/DeepSeek-V3.1-Terminus", // Model from ProviderModels
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.config.GetCommitProvider()
			model := tt.config.GetCommitModel()

			assert.Equal(t, tt.expectedProvider, provider, "provider mismatch")
			assert.Equal(t, tt.expectedModel, model, "model mismatch")
		})
	}
}

// TestReviewProviderEdgeCases tests edge cases for review provider configuration
func TestReviewProviderEdgeCases(t *testing.T) {
	tests := []struct {
		name              string
		config            *Config
		expectedProvider  string
		expectedModel     string
	}{
		{
			name: "whitespace provider returns as-is",
			config: &Config{
				ReviewProvider:   "  ",
				LastUsedProvider: "ollama-local",
			},
			expectedProvider: "  ", // Whitespace is not empty, so it's returned as-is
			expectedModel:    "",
		},
		{
			name: "whitespace model returns as-is",
			config: &Config{
				ReviewProvider:  "openai",
				ReviewModel:     "  ",
				ProviderModels:  map[string]string{"openai": "gpt-4-turbo"},
			},
			expectedProvider: "openai",
			expectedModel:    "  ", // Whitespace is not empty, so it's returned as-is
		},
		{
			name: "provider not in provider models map",
			config: &Config{
				ReviewProvider: "unknown-provider",
				ReviewModel:    "",
				ProviderModels: map[string]string{"openai": "gpt-4"},
			},
			expectedProvider: "unknown-provider",
			expectedModel:    "",
		},
		{
			name: "all fallback levels exhausted returns default",
			config: &Config{
				ReviewProvider:   "",
				LastUsedProvider: "",
				ProviderPriority: []string{},
				ProviderModels:   map[string]string{"ollama-local": "qwen3-coder:30b"}, // Ultimate default has this model
			},
			expectedProvider: "ollama-local", // Ultimate default
			expectedModel:    "qwen3-coder:30b", // Model from the default provider
		},
		{
			name: "provider priority with special characters - model fetched from map",
			config: &Config{
				ReviewProvider:   "",
				LastUsedProvider: "",
				ProviderPriority:  []string{"ollama-turbo", "openrouter"},
				ProviderModels:   map[string]string{"ollama-turbo": "deepseek-v3.1:671b", "openrouter": "openai/gpt-5"},
			},
			expectedProvider: "ollama-turbo",
			expectedModel:    "deepseek-v3.1:671b", // Model is fetched from ProviderModels
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider := tt.config.GetReviewProvider()
			model := tt.config.GetReviewModel()

			assert.Equal(t, tt.expectedProvider, provider, "provider mismatch")
			assert.Equal(t, tt.expectedModel, model, "model mismatch")
		})
	}
}

// TestCommitReviewConfigMutability tests that config fields can be changed
func TestCommitReviewConfigMutability(t *testing.T) {
	cfg := NewConfig()

	// Initially empty
	assert.Empty(t, cfg.CommitProvider)
	assert.Empty(t, cfg.CommitModel)
	assert.Empty(t, cfg.ReviewProvider)
	assert.Empty(t, cfg.ReviewModel)

	// Set values
	cfg.SetCommitProvider("openai")
	cfg.SetCommitModel("gpt-4")
	cfg.SetReviewProvider("ollama-local")
	cfg.SetReviewModel("qwen3-coder:30b")

	// Verify they're set
	assert.Equal(t, "openai", cfg.CommitProvider)
	assert.Equal(t, "gpt-4", cfg.CommitModel)
	assert.Equal(t, "ollama-local", cfg.ReviewProvider)
	assert.Equal(t, "qwen3-coder:30b", cfg.ReviewModel)

	// Change values
	cfg.SetCommitProvider("zai")
	cfg.SetCommitModel("GLM-4.6")
	cfg.SetReviewProvider("deepinfra")
	cfg.SetReviewModel("deepseek-v3")

	// Verify they changed
	assert.Equal(t, "zai", cfg.CommitProvider)
	assert.Equal(t, "GLM-4.6", cfg.CommitModel)
	assert.Equal(t, "deepinfra", cfg.ReviewProvider)
	assert.Equal(t, "deepseek-v3", cfg.ReviewModel)
}

// TestCommitReviewConfigConsistency tests that getters return consistent values
func TestCommitReviewConfigConsistency(t *testing.T) {
	cfg := &Config{
		CommitProvider:  "openai",
		CommitModel:     "gpt-4",
		ReviewProvider:  "ollama-local",
		ReviewModel:     "qwen3-coder:30b",
		ProviderModels: map[string]string{
			"openai":       "gpt-4",
			"ollama-local": "qwen3-coder:30b",
		},
		LastUsedProvider: "openrouter",
	}

	// Call getters multiple times to ensure consistency
	for i := 0; i < 5; i++ {
		assert.Equal(t, "openai", cfg.GetCommitProvider(), "iteration %d", i)
		assert.Equal(t, "gpt-4", cfg.GetCommitModel(), "iteration %d", i)
		assert.Equal(t, "ollama-local", cfg.GetReviewProvider(), "iteration %d", i)
		assert.Equal(t, "qwen3-coder:30b", cfg.GetReviewModel(), "iteration %d", i)
	}
}

// TestCommitReviewConfigEmptyStringHandling tests empty string handling
func TestCommitReviewConfigEmptyStringHandling(t *testing.T) {
	cfg := &Config{
		CommitProvider:  "",
		CommitModel:     "",
		ReviewProvider:  "",
		ReviewModel:     "",
		LastUsedProvider: "openrouter",
		ProviderModels: map[string]string{
			"openrouter": "openai/gpt-5",
		},
	}

	// Should fall back to LastUsedProvider
	assert.Equal(t, "openrouter", cfg.GetCommitProvider())
	assert.Equal(t, "openrouter", cfg.GetReviewProvider())
	assert.Equal(t, "openai/gpt-5", cfg.GetCommitModel())
	assert.Equal(t, "openai/gpt-5", cfg.GetReviewModel())
}

// TestCommitReviewConfigModelProviderMismatch tests when model doesn't match provider
func TestCommitReviewConfigModelProviderMismatch(t *testing.T) {
	cfg := &Config{
		CommitProvider:  "openai",
		CommitModel:     "gpt-4", // Model matches provider
		ReviewProvider:  "ollama-local",
		ReviewModel:     "deepseek-v3", // This is an ollama-turbo model, not ollama-local
		ProviderModels: map[string]string{
			"openai":       "gpt-4",
			"ollama-local": "qwen3-coder:30b",
		},
	}

	// Commit model should match configured provider
	assert.Equal(t, "openai", cfg.GetCommitProvider())
	assert.Equal(t, "gpt-4", cfg.GetCommitModel())

	// Review model is explicit and doesn't fall back even if it doesn't match provider's default
	assert.Equal(t, "ollama-local", cfg.GetReviewProvider())
	assert.Equal(t, "deepseek-v3", cfg.GetReviewModel())
}

// TestCommitReviewConfigWithNilConfig tests nil config safety
func TestCommitReviewConfigWithNilConfig(t *testing.T) {
	// This test documents that calling methods on nil config would panic
	// In production, code should check for nil config before calling getters
	// This test is skipped because calling methods on nil would panic

	t.Skip("calling methods on nil config would panic")
}

// TestCommitReviewConfigSettersEmptyString tests that setters accept empty strings
func TestCommitReviewConfigSettersEmptyString(t *testing.T) {
	cfg := &Config{
		CommitProvider:  "openai",
		CommitModel:     "gpt-4",
		ReviewProvider:  "ollama-local",
		ReviewModel:     "qwen3-coder:30b",
	}

	// Set to empty strings
	cfg.SetCommitProvider("")
	cfg.SetCommitModel("")
	cfg.SetReviewProvider("")
	cfg.SetReviewModel("")

	// Verify they're empty
	assert.Empty(t, cfg.CommitProvider)
	assert.Empty(t, cfg.CommitModel)
	assert.Empty(t, cfg.ReviewProvider)
	assert.Empty(t, cfg.ReviewModel)
}
