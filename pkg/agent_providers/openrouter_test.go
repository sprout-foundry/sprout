package providers

import (
	"os"
	"strings"
	"testing"
)

func TestListModelsParsing(t *testing.T) {
	// Skip this test if no API key is set
	apiKey := os.Getenv("OPENROUTER_API_KEY")
	if apiKey == "" {
		t.Skip("OPENROUTER_API_KEY not set, skipping test")
	}

	// Create provider using factory
	factory := NewProviderFactory()
	if err := factory.LoadConfigFromFile("configs/openrouter.json"); err != nil {
		t.Fatal("Failed to load openrouter config:", err)
	}

	p, err := factory.CreateProvider("openrouter")
	if err != nil {
		t.Fatal("Failed to create provider:", err)
	}

	// Test that we can list models
	models, err := p.ListModels()
	if err != nil {
		t.Fatal("ListModels failed:", err)
	}

	// We should have many models available
	if len(models) < 10 {
		t.Errorf("Expected at least 10 models, got %d", len(models))
	}

	// Check that models have required fields
	hasClaudeModel := false
	for _, model := range models {
		if model.ID == "" {
			t.Error("Model has empty ID")
		}
		if model.Provider == "" {
			t.Error("Model has empty Provider")
		}

		// Look for a Claude model to test
		if strings.Contains(model.ID, "claude") {
			hasClaudeModel = true
			// Claude models should have reasonable context lengths
			if model.ContextLength < 100000 {
				t.Errorf("Claude model %s has unexpectedly small context: %d", model.ID, model.ContextLength)
			}
		}
	}

	if !hasClaudeModel {
		t.Error("Expected to find at least one Claude model")
	}

	// Test that model caching works
	models2, err := p.ListModels()
	if err != nil {
		t.Fatal("Second ListModels call failed:", err)
	}

	if len(models) != len(models2) {
		t.Error("Model caching doesn't seem to be working")
	}
}

func TestGetModelContextLimitFallback(t *testing.T) {
	// Create provider using factory
	factory := NewProviderFactory()
	if err := factory.LoadConfigFromFile("configs/openrouter.json"); err != nil {
		t.Fatal("Failed to load openrouter config:", err)
	}

	p, err := factory.CreateProviderWithModel("openrouter", "unknown-gpt-3.5")
	if err != nil {
		t.Fatal("Failed to create provider:", err)
	}

	cl, err := p.GetModelContextLimit()
	if err != nil {
		t.Fatal("Unexpected error:", err)
	}
	if cl != 16385 { // Model-aware fallback for gpt-3.5 (16k context)
		t.Errorf("Expected 16385 fallback, got %d", cl)
	}
}
