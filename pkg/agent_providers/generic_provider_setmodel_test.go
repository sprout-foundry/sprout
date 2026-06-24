package providers

import (
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestGenericProviderSetModelInvalidatesCache tests that SetModel sets
// modelsCached to false, forcing GetModelContextLimit to do a fresh lookup.
func TestGenericProviderSetModelInvalidatesCache(t *testing.T) {
	// Create a minimal provider with a model that has a context limit
	config := &ProviderConfig{
		Name: "test-provider",
		Endpoint: "https://api.test-provider.com/v1",
		Auth: AuthConfig{
			Type: "none",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
			ModelInfo: []ModelInfo{
				{
					ID:            "test-model",
					Name:          "test-model",
					ContextLength: 128000,
				},
			},
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create generic provider: %v", err)
	}

	// Set the model and verify cache is invalidated
	model := "new-model"
	err = provider.SetModel(model)
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	// Verify modelsCached is now false
	if provider.modelsCached {
		t.Error("expected modelsCached to be false after SetModel")
	}

	// Verify the model was set
	if provider.model != model {
		t.Errorf("expected model to be %q, got %q", model, provider.model)
	}
}

// TestGenericProviderSetModelPreservesCache tests that SetModel does not
// affect the cache when modelsCached is already false.
func TestGenericProviderSetModelPreservesCache(t *testing.T) {
	// Create a minimal provider
	config := &ProviderConfig{
		Name: "test-provider",
		Endpoint: "https://api.test-provider.com/v1",
		Auth: AuthConfig{
			Type: "none",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create generic provider: %v", err)
	}

	// Explicitly set cache to false
	provider.modelsCached = false

	// Set the model and verify cache remains false
	model := "another-model"
	err = provider.SetModel(model)
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	// Verify modelsCached is still false
	if provider.modelsCached {
		t.Error("expected modelsCached to remain false after SetModel")
	}

	// Verify the model was set
	if provider.model != model {
		t.Errorf("expected model to be %q, got %q", model, provider.model)
	}
}

// TestGenericProviderSetModelMultipleTimes tests that SetModel can be called
// multiple times and each call invalidates the cache.
func TestGenericProviderSetModelMultipleTimes(t *testing.T) {
	// Create a minimal provider
	config := &ProviderConfig{
		Name: "test-provider",
		Endpoint: "https://api.test-provider.com/v1",
		Auth: AuthConfig{
			Type: "none",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create generic provider: %v", err)
	}

	// Set the model multiple times
	models := []string{"model-1", "model-2", "model-3"}
	for i, model := range models {
		err := provider.SetModel(model)
		if err != nil {
			t.Fatalf("SetModel failed on iteration %d: %v", i, err)
		}

		// Verify cache is invalidated after each call
		if provider.modelsCached {
			t.Errorf("expected modelsCached to be false after SetModel(%q), iteration %d", model, i)
		}

		// Verify the model was set correctly
		if provider.model != model {
			t.Errorf("iteration %d: expected model to be %q, got %q", i, model, provider.model)
		}
	}
}

// TestGenericProviderSetModelWithModelsCached tests that SetModel works
// correctly when the provider has models cached.
func TestGenericProviderSetModelWithModelsCached(t *testing.T) {
	// Create a minimal provider
	config := &ProviderConfig{
		Name: "test-provider",
		Endpoint: "https://api.test-provider.com/v1",
		Auth: AuthConfig{
			Type: "none",
		},
		Defaults: RequestDefaults{
			Model: "test-model",
		},
		Models: ModelConfig{
			DefaultContextLimit: 128000,
		},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create generic provider: %v", err)
	}

	// Set the model and cache models
	model := "cached-model"
	err = provider.SetModel(model)
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	// Manually set modelsCached to true (simulating a successful ListModels call)
	provider.models = []api.ModelInfo{
		{
			ID:            model,
			Name:          model,
			Provider:      "test-provider",
			ContextLength: 128000,
		},
	}
	provider.modelsCached = true

	// Verify cache is true
	if !provider.modelsCached {
		t.Error("expected modelsCached to be true after manual cache set")
	}

	// Set the model again
	newModel := "new-model"
	err = provider.SetModel(newModel)
	if err != nil {
		t.Fatalf("SetModel failed: %v", err)
	}

	// Verify cache is now false
	if provider.modelsCached {
		t.Error("expected modelsCached to be false after SetModel with cached models")
	}

	// Verify the new model was set
	if provider.model != newModel {
		t.Errorf("expected model to be %q, got %q", newModel, provider.model)
	}
}