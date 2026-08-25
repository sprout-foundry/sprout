package configuration

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestGetCommitProvider_ExplicitValue_ReturnsValue tests that GetCommitProvider returns the explicitly set value
func TestGetCommitProvider_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		CommitProvider: "openai",
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "openai", result)
}

// TestGetCommitProvider_EmptyReturnsEmpty tests that GetCommitProvider returns empty when not explicitly set
func TestGetCommitProvider_EmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CommitProvider:   "",
		LastUsedProvider: "openrouter",
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "", result)
}

// TestGetCommitProvider_OnlyProviderPriorityReturnsEmpty tests that GetCommitProvider does not fall back to ProviderPriority
func TestGetCommitProvider_OnlyProviderPriorityReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CommitProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{"ollama-local", "openrouter"},
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "", result)
}

// TestGetCommitProvider_AllEmptyReturnsEmpty tests that GetCommitProvider returns empty with no explicit config
func TestGetCommitProvider_AllEmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		CommitProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{},
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "", result)
}

// TestGetCommitModel_ExplicitValue_ReturnsValue tests that GetCommitModel returns the explicitly set model
func TestGetCommitModel_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		CommitModel: "gpt-4",
	}
	result := cfg.GetCommitModel()
	assert.Equal(t, "gpt-4", result)
}

// TestGetCommitModel_EmptyFallsBackToProviderModel tests that GetCommitModel falls back to provider's default model
func TestGetCommitModel_EmptyFallsBackToProviderModel(t *testing.T) {
	cfg := &Config{
		CommitModel:    "",
		CommitProvider: "openai",
		ProviderModels: map[string]string{
			"openai": "gpt-4",
		},
	}
	result := cfg.GetCommitModel()
	assert.Equal(t, "gpt-4", result)
}

// TestSetCommitProvider_SetsValue tests that SetCommitProvider sets the value
func TestSetCommitProvider_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetCommitProvider("deepinfra")
	assert.Equal(t, "deepinfra", cfg.CommitProvider)
}

// TestSetCommitModel_SetsValue tests that SetCommitModel sets the value
func TestSetCommitModel_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetCommitModel("deepseek-v3")
	assert.Equal(t, "deepseek-v3", cfg.CommitModel)
}

// TestGetReviewProvider_ExplicitValue_ReturnsValue tests that GetReviewProvider returns the explicitly set value
func TestGetReviewProvider_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		ReviewProvider: "zai",
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "zai", result)
}

// TestGetReviewProvider_EmptyReturnsEmpty tests that GetReviewProvider returns empty when not explicitly set
func TestGetReviewProvider_EmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "openrouter",
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "", result)
}

// TestGetReviewProvider_OnlyProviderPriorityReturnsEmpty tests that GetReviewProvider does not fall back to ProviderPriority
func TestGetReviewProvider_OnlyProviderPriorityReturnsEmpty(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{"ollama-local", "openrouter"},
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "", result)
}

// TestGetReviewProvider_AllEmptyReturnsEmpty tests that GetReviewProvider returns empty with no explicit config
func TestGetReviewProvider_AllEmptyReturnsEmpty(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{},
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "", result)
}

// TestGetReviewModel_ExplicitValue_ReturnsValue tests that GetReviewModel returns the explicitly set model
func TestGetReviewModel_ExplicitValue_ReturnsValue(t *testing.T) {
	cfg := &Config{
		ReviewModel: "gpt-4-turbo",
	}
	result := cfg.GetReviewModel()
	assert.Equal(t, "gpt-4-turbo", result)
}

// TestGetReviewModel_EmptyFallsBackToProviderModel tests that GetReviewModel falls back to provider's default model
func TestGetReviewModel_EmptyFallsBackToProviderModel(t *testing.T) {
	cfg := &Config{
		ReviewModel:    "",
		ReviewProvider: "openai",
		ProviderModels: map[string]string{
			"openai": "gpt-4-turbo",
		},
	}
	result := cfg.GetReviewModel()
	assert.Equal(t, "gpt-4-turbo", result)
}

// TestSetReviewProvider_SetsValue tests that SetReviewProvider sets the value
func TestSetReviewProvider_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetReviewProvider("ollama-cloud")
	assert.Equal(t, "ollama-cloud", cfg.ReviewProvider)
}

// TestSetReviewModel_SetsValue tests that SetReviewModel sets the value
func TestSetReviewModel_SetsValue(t *testing.T) {
	cfg := &Config{}
	cfg.SetReviewModel("deepseek-v3.1")
	assert.Equal(t, "deepseek-v3.1", cfg.ReviewModel)
}

// TestCommitAndReviewConfigIndependence tests that commit and review configs are independent
func TestCommitAndReviewConfigIndependence(t *testing.T) {
	cfg := &Config{
		CommitProvider:   "openai",
		CommitModel:      "gpt-4",
		ReviewProvider:   "ollama-local",
		ReviewModel:      "qwen3-coder:30b",
		LastUsedProvider: "openrouter",
	}

	assert.Equal(t, "openai", cfg.GetCommitProvider())
	assert.Equal(t, "gpt-4", cfg.GetCommitModel())
	assert.Equal(t, "ollama-local", cfg.GetReviewProvider())
	assert.Equal(t, "qwen3-coder:30b", cfg.GetReviewModel())
}

// TestCommitConfigFallbackChain tests the complete fallback chain for commit config
func TestCommitConfigFallbackChain(t *testing.T) {
	tests := []struct {
		name             string
		commitProvider   string
		lastUsedProvider string
		providerPriority []string
		expectedProvider string
	}{
		{
			name:             "explicit commit provider",
			commitProvider:   "zai",
			lastUsedProvider: "openrouter",
			providerPriority: []string{"ollama-local"},
			expectedProvider: "zai",
		},
		{
			name:             "empty returns empty (no fallback to last used)",
			commitProvider:   "",
			lastUsedProvider: "deepinfra",
			providerPriority: []string{"ollama-local"},
			expectedProvider: "",
		},
		{
			name:             "empty returns empty (no fallback to provider priority)",
			commitProvider:   "",
			lastUsedProvider: "",
			providerPriority: []string{"openai", "ollama-local"},
			expectedProvider: "",
		},
		{
			name:             "all empty returns empty (no ultimate default)",
			commitProvider:   "",
			lastUsedProvider: "",
			providerPriority: []string{},
			expectedProvider: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				CommitProvider:   tt.commitProvider,
				LastUsedProvider: tt.lastUsedProvider,
				ProviderPriority: tt.providerPriority,
			}
			result := cfg.GetCommitProvider()
			assert.Equal(t, tt.expectedProvider, result)
		})
	}
}

// TestReviewConfigFallbackChain tests the complete fallback chain for review config
func TestReviewConfigFallbackChain(t *testing.T) {
	tests := []struct {
		name             string
		reviewProvider   string
		lastUsedProvider string
		providerPriority []string
		expectedProvider string
	}{
		{
			name:             "explicit review provider",
			reviewProvider:   "ollama-cloud",
			lastUsedProvider: "openrouter",
			providerPriority: []string{"ollama-local"},
			expectedProvider: "ollama-cloud",
		},
		{
			name:             "empty returns empty (no fallback to last used)",
			reviewProvider:   "",
			lastUsedProvider: "openrouter",
			providerPriority: []string{"ollama-local"},
			expectedProvider: "",
		},
		{
			name:             "empty returns empty (no fallback to provider priority)",
			reviewProvider:   "",
			lastUsedProvider: "",
			providerPriority: []string{"deepinfra", "zai"},
			expectedProvider: "",
		},
		{
			name:             "all empty returns empty (no ultimate default)",
			reviewProvider:   "",
			lastUsedProvider: "",
			providerPriority: []string{},
			expectedProvider: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				ReviewProvider:   tt.reviewProvider,
				LastUsedProvider: tt.lastUsedProvider,
				ProviderPriority: tt.providerPriority,
			}
			result := cfg.GetReviewProvider()
			assert.Equal(t, tt.expectedProvider, result)
		})
	}
}

// TestNewConfigIncludesCommitReviewFields tests that NewConfig initializes commit and review fields
func TestNewConfigIncludesCommitReviewFields(t *testing.T) {
	cfg := NewConfig()
	assert.NotNil(t, cfg)
	// Fields should be empty by default (will fall back to LastUsedProvider)
	assert.Empty(t, cfg.CommitProvider)
	assert.Empty(t, cfg.CommitModel)
	assert.Empty(t, cfg.ReviewProvider)
	assert.Empty(t, cfg.ReviewModel)
}

// TestCommitReviewConfigCanBeSetToEmpty tests that configs can be explicitly set to empty
func TestCommitReviewConfigCanBeSetToEmpty(t *testing.T) {
	cfg := &Config{
		CommitProvider:   "openai",
		CommitModel:      "gpt-4",
		ReviewProvider:   "ollama-local",
		ReviewModel:      "qwen3-coder:30b",
		LastUsedProvider: "openrouter",
	}

	// Set to empty to trigger fallback
	cfg.SetCommitProvider("")
	cfg.SetCommitModel("")
	cfg.SetReviewProvider("")
	cfg.SetReviewModel("")

	assert.Empty(t, cfg.CommitProvider)
	assert.Empty(t, cfg.CommitModel)
	assert.Empty(t, cfg.ReviewProvider)
	assert.Empty(t, cfg.ReviewModel)

	// Getters return empty — no fallback
	assert.Equal(t, "", cfg.GetCommitProvider())
	assert.Equal(t, "", cfg.GetReviewProvider())
}

// TestCommitModelFallbackUsesCommitProvider tests that commit model uses commit provider, not last used
func TestCommitModelFallbackUsesCommitProvider(t *testing.T) {
	cfg := &Config{
		CommitProvider: "zai",
		CommitModel:    "",
		ProviderModels: map[string]string{
			"openrouter": "openai/gpt-5",
			"zai":        "GLM-4.6",
		},
		LastUsedProvider: "openrouter",
	}

	result := cfg.GetCommitModel()
	assert.Equal(t, "GLM-4.6", result)
}

// TestReviewModelFallbackUsesReviewProvider tests that review model uses review provider, not last used
func TestReviewModelFallbackUsesReviewProvider(t *testing.T) {
	cfg := &Config{
		ReviewProvider: "ollama-cloud",
		ReviewModel:    "",
		ProviderModels: map[string]string{
			"openrouter":   "openai/gpt-5",
			"ollama-cloud": "deepseek-v3.1:671b",
		},
		LastUsedProvider: "openrouter",
	}

	result := cfg.GetReviewModel()
	assert.Equal(t, "deepseek-v3.1:671b", result)
}
