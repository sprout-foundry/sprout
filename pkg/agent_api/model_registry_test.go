package api

import (
	"os"
	"sync"
	"testing"
)

func TestModelRegistry_ThreadSafety(t *testing.T) {
	registry := newDefaultModelRegistry()

	// Test concurrent reads and writes
	var wg sync.WaitGroup
	numGoroutines := 100

	// Start writers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			model := ModelConfig{
				ID:            "test-model-" + string(rune(id)),
				Provider:      "test",
				ContextLength: 1000 + id,
				InputCost:     float64(id),
				OutputCost:    float64(id * 2),
			}
			if err := registry.AddModel(model); err != nil {
				t.Errorf("Failed to add model: %v", err)
			}
		}(i)
	}

	// Start readers
	for i := 0; i < numGoroutines/2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			// Try to read various models
			_, _ = registry.GetModelConfig("gpt-4")
			_, _ = registry.GetModelContextLength("gpt-4o")
			_, _, _ = registry.GetModelPricing("deepseek-chat")
		}()
	}

	wg.Wait()

	// Verify some models exist
	if _, err := registry.GetModelConfig("gpt-4"); err != nil {
		t.Errorf("Failed to get gpt-4: %v", err)
	}
}

func TestModelRegistry_GetModelConfig(t *testing.T) {
	registry := newDefaultModelRegistry()

	tests := []struct {
		name      string
		modelID   string
		wantErr   bool
		checkFunc func(t *testing.T, config ModelConfig)
	}{
		{
			name:    "exact match - gpt-4",
			modelID: "gpt-4",
			wantErr: false,
			checkFunc: func(t *testing.T, config ModelConfig) {
				if config.ID != "gpt-4" {
					t.Errorf("Expected ID gpt-4, got %s", config.ID)
				}
				if config.ContextLength != 8192 {
					t.Errorf("Expected context length 8192, got %d", config.ContextLength)
				}
			},
		},
		{
			name:    "exact match - deepseek-chat",
			modelID: "deepseek-chat",
			wantErr: false,
			checkFunc: func(t *testing.T, config ModelConfig) {
				if config.Provider != "deepseek" {
					t.Errorf("Expected provider deepseek, got %s", config.Provider)
				}
			},
		},
		{
			name:    "pattern match - gpt-5-custom",
			modelID: "gpt-5-custom",
			wantErr: false,
			checkFunc: func(t *testing.T, config ModelConfig) {
				if config.ContextLength != 272000 {
					t.Errorf("Expected context length 272000 for GPT-5 pattern, got %d", config.ContextLength)
				}
			},
		},
		{
			name:    "pattern match - deepseek-r1-distilled",
			modelID: "deepseek-r1-distilled",
			wantErr: false,
			checkFunc: func(t *testing.T, config ModelConfig) {
				if config.ContextLength != 685000 {
					t.Errorf("Expected context length 685000 for DeepSeek R1 pattern, got %d", config.ContextLength)
				}
			},
		},
		{
			name:    "unknown model",
			modelID: "unknown-model-xyz",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config, err := registry.GetModelConfig(tt.modelID)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetModelConfig() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && tt.checkFunc != nil {
				tt.checkFunc(t, config)
			}
		})
	}
}

func TestModelRegistry_ErrorHandling(t *testing.T) {
	registry := newDefaultModelRegistry()

	// Test adding model with empty ID
	err := registry.AddModel(ModelConfig{ID: ""})
	if err == nil {
		t.Error("Expected error when adding model with empty ID")
	}

	// Test getting non-existent model
	_, err = registry.GetModelConfig("non-existent-model")
	if err == nil {
		t.Error("Expected error when getting non-existent model")
	}
	if _, ok := err.(*ModelNotFoundError); !ok {
		t.Errorf("Expected ModelNotFoundError, got %T", err)
	}

	// Test pricing for non-existent model
	_, _, err = registry.GetModelPricing("non-existent-model")
	if err == nil {
		t.Error("Expected error when getting pricing for non-existent model")
	}
}

func TestModelRegistry_PatternMatching(t *testing.T) {
	registry := newDefaultModelRegistry()

	// Add a test pattern
	err := registry.AddPattern(ModelPattern{
		Contains:    []string{"test", "model"},
		NotContains: []string{"skip"},
		Config: ModelConfig{
			Provider:      "test",
			ContextLength: 9999,
		},
		Priority: 100,
	})
	if err != nil {
		t.Fatalf("Failed to add pattern: %v", err)
	}

	// Test matching
	config, err := registry.GetModelConfig("test-model-123")
	if err != nil {
		t.Fatalf("Failed to get config for pattern match: %v", err)
	}
	if config.ContextLength != 9999 {
		t.Errorf("Expected context length 9999, got %d", config.ContextLength)
	}

	// Test non-matching (contains "skip")
	_, err = registry.GetModelConfig("test-model-skip")
	if err == nil {
		t.Error("Expected error for model containing 'skip'")
	}
}

func TestModelRegistry_BackwardCompatibility(t *testing.T) {
	registry := newDefaultModelRegistry()

	// Test the backward-compatible method
	length := registry.GetModelContextLengthWithDefault("unknown-model", 4096)
	if length != 4096 {
		t.Errorf("Expected default length 4096, got %d", length)
	}

	// Test with known model
	length = registry.GetModelContextLengthWithDefault("gpt-4", 4096)
	if length != 8192 {
		t.Errorf("Expected gpt-4 length 8192, got %d", length)
	}
}

func TestDetermineProvider(t *testing.T) {
	// Save and restore environment
	oldProvider := os.Getenv("LEDIT_PROVIDER")
	oldOpenAI := os.Getenv("OPENAI_API_KEY")
	defer func() {
		os.Setenv("LEDIT_PROVIDER", oldProvider)
		os.Setenv("OPENAI_API_KEY", oldOpenAI)
	}()

	tests := []struct {
		name             string
		explicitProvider string
		envProvider      string
		envAPIKeys       map[string]string
		lastUsedProvider ClientType
		want             ClientType
		wantErr          bool
	}{
		{
			name:             "explicit provider",
			explicitProvider: "openai",
			want:             OpenAIClientType,
			envAPIKeys:       map[string]string{"OPENAI_API_KEY": "test"},
			wantErr:          false,
		},
		{
			name:             "explicit provider unavailable",
			explicitProvider: "openai",
			envAPIKeys:       map[string]string{}, // No API key
			wantErr:          true,
		},
		{
			name:        "environment variable",
			envProvider: "openrouter",
			envAPIKeys:  map[string]string{"OPENROUTER_API_KEY": "test"},
			want:        OpenRouterClientType,
			wantErr:     false,
		},
		{
			name:             "last used provider",
			lastUsedProvider: DeepInfraClientType,
			envAPIKeys:       map[string]string{"DEEPINFRA_API_KEY": "test"},
			want:             DeepInfraClientType,
			wantErr:          false,
		},
		{
			name:       "fallback to first available",
			envAPIKeys: map[string]string{"GROQ_API_KEY": "test"},
			want:       GroqClientType,
			wantErr:    false,
		},
		{
			name:    "fallback to Ollama",
			want:    OllamaClientType,
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear environment
			os.Clearenv()

			// Set test environment
			if tt.envProvider != "" {
				os.Setenv("LEDIT_PROVIDER", tt.envProvider)
			}
			for k, v := range tt.envAPIKeys {
				os.Setenv(k, v)
			}

			got, err := DetermineProvider(tt.explicitProvider, tt.lastUsedProvider)
			if (err != nil) != tt.wantErr {
				t.Errorf("DetermineProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("DetermineProvider() = %v, want %v", got, tt.want)
			}
		})
	}
}
