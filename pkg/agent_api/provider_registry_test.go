package api

import (
	"sync"
	"testing"
)

func TestProviderRegistry_ThreadSafety(t *testing.T) {
	// Reset the singleton for testing
	defaultProviderRegistry = nil
	providerRegistryOnce = sync.Once{}

	// Test concurrent access to GetProviderRegistry
	var wg sync.WaitGroup
	numGoroutines := 100
	registries := make([]*ProviderRegistry, numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			registries[idx] = GetProviderRegistry()
		}(i)
	}

	wg.Wait()

	// Verify all goroutines got the same instance
	first := registries[0]
	for i := 1; i < numGoroutines; i++ {
		if registries[i] != first {
			t.Errorf("goroutine %d got different registry instance", i)
		}
	}

	// Verify the registry is properly initialized
	if first == nil {
		t.Error("registry is nil")
	}

	// Test it has providers
	providers := first.GetAvailableProviders()
	if len(providers) == 0 {
		t.Error("no providers registered")
	}
}

func TestProviderRegistry_Methods(t *testing.T) {
	registry := GetProviderRegistry()

	// Test GetProviderName
	name := registry.GetProviderName(OpenAIClientType)
	if name != "OpenAI" {
		t.Errorf("expected OpenAI, got %s", name)
	}

	// Test GetProviderEnvVar
	envVar := registry.GetProviderEnvVar(OpenAIClientType)
	if envVar != "OPENAI_API_KEY" {
		t.Errorf("expected OPENAI_API_KEY, got %s", envVar)
	}

	// Test GetDefaultModel
	model := registry.GetDefaultModel(OpenAIClientType)
	if model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", model)
	}

	// Test GetDefaultVisionModel
	visionModel := registry.GetDefaultVisionModel(OpenAIClientType)
	if visionModel != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %s", visionModel)
	}

	// Test non-existent provider
	unknownName := registry.GetProviderName(ClientType("unknown"))
	if unknownName != "unknown" {
		t.Errorf("expected 'unknown', got %s", unknownName)
	}
}
