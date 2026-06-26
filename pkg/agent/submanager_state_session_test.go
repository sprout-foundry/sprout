package agent

import (
	"sync"
	"testing"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestAgentStateManager_SessionProvider tests GetSessionProvider and SetSessionProvider
// with mutex protection.
func TestAgentStateManager_SessionProvider(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Test zero values
	if got := sm.GetSessionProvider(); got != "" {
		t.Errorf("expected empty provider for fresh state, got %v", got)
	}

	// Test setting provider
	sm.SetSessionProvider(api.OpenAIClientType)
	if got := sm.GetSessionProvider(); got != api.OpenAIClientType {
		t.Errorf("expected OpenAIClientType, got %v", got)
	}

	// Test setting to another provider
	sm.SetSessionProvider(api.OllamaClientType)
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType, got %v", got)
	}
}

// TestAgentStateManager_SessionModel tests GetSessionModel and SetSessionModel
// with mutex protection.
func TestAgentStateManager_SessionModel(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Test zero values
	if got := sm.GetSessionModel(); got != "" {
		t.Errorf("expected empty model for fresh state, got %q", got)
	}

	// Test setting model
	sm.SetSessionModel("gpt-4o")
	if got := sm.GetSessionModel(); got != "gpt-4o" {
		t.Errorf("expected gpt-4o, got %q", got)
	}

	// Test setting to another model
	sm.SetSessionModel("llama3.2")
	if got := sm.GetSessionModel(); got != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", got)
	}
}

// TestAgentStateManager_SessionProviderRace tests that GetSessionProvider and
// SetSessionProvider are race-free when called concurrently.
func TestAgentStateManager_SessionProviderRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Start multiple goroutines that concurrently set and get the provider
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Use a fixed set of provider types
			providers := []api.ClientType{api.OpenAIClientType, api.OllamaClientType, api.OpenRouterClientType, api.DeepInfraClientType, api.LMStudioClientType}
			provider := providers[id%len(providers)]
			sm.SetSessionProvider(provider)
			got := sm.GetSessionProvider()
			if got != provider {
				t.Errorf("goroutine %d: expected %v, got %v", id, provider, got)
			}
		}(i)
	}

	wg.Wait()
}

// TestAgentStateManager_SessionModelRace tests that GetSessionModel and
// SetSessionModel are race-free when called concurrently.
func TestAgentStateManager_SessionModelRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	var wg sync.WaitGroup
	numGoroutines := 10

	// Start multiple goroutines that concurrently set and get the model
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			// Use a fixed set of model names
			models := []string{"model-a", "model-b", "model-c", "model-d", "model-e"}
			model := models[id%len(models)]
			sm.SetSessionModel(model)
			got := sm.GetSessionModel()
			if got != model {
				t.Errorf("goroutine %d: expected %q, got %q", id, model, got)
			}
		}(i)
	}

	wg.Wait()
}

// TestAgentStateManager_SessionProviderModel tests that both sessionProvider
// and sessionModel can be set independently and retrieved correctly.
func TestAgentStateManager_SessionProviderModel(t *testing.T) {
	sm := NewAgentStateManager(false)

	// Set both provider and model
	sm.SetSessionProvider(api.OpenRouterClientType)
	sm.SetSessionModel("anthropic/claude-3")

	// Verify both are set correctly
	if got := sm.GetSessionProvider(); got != api.OpenRouterClientType {
		t.Errorf("expected OpenRouterClientType, got %v", got)
	}
	if got := sm.GetSessionModel(); got != "anthropic/claude-3" {
		t.Errorf("expected anthropic/claude-3, got %q", got)
	}

	// Change provider only
	sm.SetSessionProvider(api.OllamaClientType)
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType, got %v", got)
	}
	if got := sm.GetSessionModel(); got != "anthropic/claude-3" {
		t.Errorf("expected anthropic/claude-3 (unchanged), got %q", got)
	}

	// Change model only
	sm.SetSessionModel("llama3.2")
	if got := sm.GetSessionProvider(); got != api.OllamaClientType {
		t.Errorf("expected OllamaClientType (unchanged), got %v", got)
	}
	if got := sm.GetSessionModel(); got != "llama3.2" {
		t.Errorf("expected llama3.2, got %q", got)
	}
}
