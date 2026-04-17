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

// TestGetCommitProvider_EmptyFallsBackToLastUsedProvider tests that GetCommitProvider falls back to LastUsedProvider
func TestGetCommitProvider_EmptyFallsBackToLastUsedProvider(t *testing.T) {
	cfg := &Config{
		CommitProvider:    "",
		LastUsedProvider: "openrouter",
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "openrouter", result)
}

// TestGetCommitProvider_EmptyFallsBackToProviderPriority tests that GetCommitProvider falls back to ProviderPriority
func TestGetCommitProvider_EmptyFallsBackToProviderPriority(t *testing.T) {
	cfg := &Config{
		CommitProvider:    "",
		LastUsedProvider: "",
		ProviderPriority: []string{"ollama-local", "openrouter"},
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "ollama-local", result)
}

// TestGetCommitProvider_AllEmptyReturnsDefault tests that GetCommitProvider returns ultimate fallback
func TestGetCommitProvider_AllEmptyReturnsDefault(t *testing.T) {
	cfg := &Config{
		CommitProvider:    "",
		LastUsedProvider: "",
		ProviderPriority: []string{},
	}
	result := cfg.GetCommitProvider()
	assert.Equal(t, "ollama-local", result)
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
		CommitModel:     "",
		CommitProvider:  "openai",
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

// TestGetReviewProvider_EmptyFallsBackToLastUsedProvider tests that GetReviewProvider falls back to LastUsedProvider
func TestGetReviewProvider_EmptyFallsBackToLastUsedProvider(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "openrouter",
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "openrouter", result)
}

// TestGetReviewProvider_EmptyFallsBackToProviderPriority tests that GetReviewProvider falls back to ProviderPriority
func TestGetReviewProvider_EmptyFallsBackToProviderPriority(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{"ollama-local", "openrouter"},
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "ollama-local", result)
}

// TestGetReviewProvider_AllEmptyReturnsDefault tests that GetReviewProvider returns ultimate fallback
func TestGetReviewProvider_AllEmptyReturnsDefault(t *testing.T) {
	cfg := &Config{
		ReviewProvider:   "",
		LastUsedProvider: "",
		ProviderPriority: []string{},
	}
	result := cfg.GetReviewProvider()
	assert.Equal(t, "ollama-local", result)
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
	cfg.SetReviewProvider("ollama-turbo")
	assert.Equal(t, "ollama-turbo", cfg.ReviewProvider)
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
		CommitProvider:  "openai",
		CommitModel:     "gpt-4",
		ReviewProvider:  "ollama-local",
		ReviewModel:     "qwen3-coder:30b",
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
		name              string
		commitProvider    string
		lastUsedProvider  string
		providerPriority  []string
		expectedProvider  string
	}{
		{
			name:              "explicit commit provider",
			commitProvider:    "zai",
			lastUsedProvider:  "openrouter",
			providerPriority:  []string{"ollama-local"},
			expectedProvider:  "zai",
		},
		{
			name:              "fallback to last used",
			commitProvider:    "",
			lastUsedProvider:  "deepinfra",
			providerPriority:  []string{"ollama-local"},
			expectedProvider:  "deepinfra",
		},
		{
			name:              "fallback to provider priority",
			commitProvider:    "",
			lastUsedProvider:  "",
			providerPriority:  []string{"openai", "ollama-local"},
			expectedProvider:  "openai",
		},
		{
			name:              "fallback to ultimate default",
			commitProvider:    "",
			lastUsedProvider:  "",
			providerPriority:  []string{},
			expectedProvider:  "ollama-local",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &Config{
				CommitProvider:    tt.commitProvider,
				LastUsedProvider:  tt.lastUsedProvider,
				ProviderPriority:  tt.providerPriority,
			}
			result := cfg.GetCommitProvider()
			assert.Equal(t, tt.expectedProvider, result)
		})
	}
}

// TestReviewConfigFallbackChain tests the complete fallback chain for review config
func TestReviewConfigFallbackChain(t *testing.T) {
	tests := []struct {
		name              string
		reviewProvider    string
		lastUsedProvider  string
		providerPriority  []string
		expectedProvider  string
	}{
		{
			name:              "explicit review provider",
			reviewProvider:    "ollama-turbo",
			lastUsedProvider:  "openrouter",
			providerPriority:  []string{"ollama-local"},
			expectedProvider:  "ollama-turbo",
		},
		{
			name:              "fallback to last used",
			reviewProvider:    "",
			lastUsedProvider:  "openrouter",
			providerPriority:  []string{"ollama-local"},
			expectedProvider:  "openrouter",
		},
		{
			name:              "fallback to provider priority",
			reviewProvider:    "",
			lastUsedProvider:  "",
			providerPriority:  []string{"deepinfra", "zai"},
			expectedProvider:  "deepinfra",
		},
		{
			name:              "fallback to ultimate default",
			reviewProvider:    "",
			lastUsedProvider:  "",
			providerPriority:  []string{},
			expectedProvider:  "ollama-local",
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
		CommitProvider:  "openai",
		CommitModel:     "gpt-4",
		ReviewProvider:  "ollama-local",
		ReviewModel:     "qwen3-coder:30b",
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

	// Getters should now use fallback
	assert.Equal(t, "openrouter", cfg.GetCommitProvider())
	assert.Equal(t, "openrouter", cfg.GetReviewProvider())
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
		ReviewProvider: "ollama-turbo",
		ReviewModel:    "",
		ProviderModels: map[string]string{
			"openrouter":   "openai/gpt-5",
			"ollama-turbo": "deepseek-v3.1:671b",
		},
		LastUsedProvider: "openrouter",
	}

	result := cfg.GetReviewModel()
	assert.Equal(t, "deepseek-v3.1:671b", result)
}
