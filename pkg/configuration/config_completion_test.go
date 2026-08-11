package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetCompletionProvider_ExplicitValue_ReturnsValue tests that GetCompletionProvider returns the explicitly set value
func TestGetCompletionProvider_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		CompletionProvider: "ollama-local",
	}
	result := cfg.GetCompletionProvider()
	assert.Equal(t, "ollama-local", result)
}

// TestGetCompletionProvider_EmptyReturnsEmpty tests that GetCompletionProvider returns empty when not explicitly set
func TestGetCompletionProvider_EmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CompletionProvider: "",
		LastUsedProvider:   "openrouter",
	}
	result := cfg.GetCompletionProvider()
	assert.Equal(t, "", result)
}

// TestGetCompletionProvider_OnlyProviderPriorityReturnsEmpty tests that GetCompletionProvider does not fall back to ProviderPriority
func TestGetCompletionProvider_OnlyProviderPriorityReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CompletionProvider: "",
		LastUsedProvider:   "",
		ProviderPriority:   []string{"ollama-local", "openrouter"},
	}
	result := cfg.GetCompletionProvider()
	assert.Equal(t, "", result)
}

// TestGetCompletionProvider_AllEmptyReturnsEmpty tests that GetCompletionProvider returns empty with no explicit config
func TestGetCompletionProvider_AllEmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CompletionProvider: "",
		LastUsedProvider:   "",
		ProviderPriority:   []string{},
	}
	result := cfg.GetCompletionProvider()
	assert.Equal(t, "", result)
}

// TestGetCompletionModel_ExplicitValue_ReturnsValue tests that GetCompletionModel returns the explicitly set model
func TestGetCompletionModel_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		CompletionModel: "qwen2.5-coder:0.5b",
	}
	result := cfg.GetCompletionModel()
	assert.Equal(t, "qwen2.5-coder:0.5b", result)
}

// TestGetCompletionModel_EmptyFallsBackToProviderModel tests that GetCompletionModel falls back to provider's default model
func TestGetCompletionModel_EmptyFallsBackToProviderModel(t *testing.T) {
	cfg := &Config{
		CompletionModel:    "",
		CompletionProvider: "ollama-local",
		ProviderModels: map[string]string{
			"ollama-local": "qwen3-coder:30b",
		},
	}
	result := cfg.GetCompletionModel()
	assert.Equal(t, "qwen3-coder:30b", result)
}

// TestSetCompletionProvider_SetsValue tests that SetCompletionProvider sets the value
func TestSetCompletionProvider_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetCompletionProvider("ollama-local")
	assert.Equal(t, "ollama-local", cfg.CompletionProvider)
}

// TestSetCompletionModel_SetsValue tests that SetCompletionModel sets the value
func TestSetCompletionModel_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetCompletionModel("qwen2.5-coder:0.5b")
	assert.Equal(t, "qwen2.5-coder:0.5b", cfg.CompletionModel)
}

// TestCompletionConfigIndependence tests that completion config is independent from commit/review config
func TestCompletionConfigIndependence(t *testing.T) {
	cfg := &Config{
		CommitProvider:     "openai",
		CommitModel:        "gpt-4",
		ReviewProvider:     "ollama-local",
		ReviewModel:        "qwen3-coder:30b",
		CompletionProvider: "ollama-cloud",
		CompletionModel:    "deepseek-v3.1:671b",
		LastUsedProvider:   "openrouter",
	}

	assert.Equal(t, "openai", cfg.GetCommitProvider())
	assert.Equal(t, "gpt-4", cfg.GetCommitModel())
	assert.Equal(t, "ollama-local", cfg.GetReviewProvider())
	assert.Equal(t, "qwen3-coder:30b", cfg.GetReviewModel())
	assert.Equal(t, "ollama-cloud", cfg.GetCompletionProvider())
	assert.Equal(t, "deepseek-v3.1:671b", cfg.GetCompletionModel())
}

// TestCompletionConfigFallbackChain tests the complete fallback chain for completion config
func TestCompletionConfigFallbackChain(t *testing.T) {
	tests := []struct {
		name               string
		completionProvider string
		lastUsedProvider   string
		providerPriority   []string
		expectedProvider   string
	}{
		{
			name:               "explicit completion provider",
			completionProvider: "ollama-local",
			lastUsedProvider:   "openrouter",
			providerPriority:   []string{"openai"},
			expectedProvider:   "ollama-local",
		},
		{
			name:               "empty returns empty (no fallback to last used)",
			completionProvider: "",
			lastUsedProvider:   "deepinfra",
			providerPriority:   []string{"openai"},
			expectedProvider:   "",
		},
		{
			name:               "empty returns empty (no fallback to provider priority)",
			completionProvider: "",
			lastUsedProvider:   "",
			providerPriority:   []string{"openai", "ollama-local"},
			expectedProvider:   "",
		},
		{
			name:               "all empty returns empty (no ultimate default)",
			completionProvider: "",
			lastUsedProvider:   "",
			providerPriority:   []string{},
			expectedProvider:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				CompletionProvider: tt.completionProvider,
				LastUsedProvider:   tt.lastUsedProvider,
				ProviderPriority:   tt.providerPriority,
			}
			result := cfg.GetCompletionProvider()
			assert.Equal(t, tt.expectedProvider, result)
		})
	}
}

// TestNewConfigIncludesCompletionFields tests that NewConfig initializes completion fields empty by default
func TestNewConfigIncludesCompletionFields(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg)
	// Fields should be empty by default (will fall back to LastUsedProvider)
	assert.Empty(t, cfg.CompletionProvider)
	assert.Empty(t, cfg.CompletionModel)
}

// TestCompletionModelFallbackUsesCompletionProvider tests that completion model uses completion provider, not last used
func TestCompletionModelFallbackUsesCompletionProvider(t *testing.T) {
	cfg := &Config{
		CompletionProvider: "ollama-local",
		CompletionModel:    "",
		ProviderModels: map[string]string{
			"openrouter":   "openai/gpt-5",
			"ollama-local": "qwen2.5-coder:0.5b",
		},
		LastUsedProvider: "openrouter",
	}

	result := cfg.GetCompletionModel()
	assert.Equal(t, "qwen2.5-coder:0.5b", result)
}
