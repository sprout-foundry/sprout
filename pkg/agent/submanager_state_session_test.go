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
//
// The race detector is what we're verifying here: under `-race`, this
// test catches any data race on the sessionProvider field. We do NOT
// assert that the read after a write on the same goroutine returns the
// value just written — the Go memory model does not guarantee that
// when multiple writers exist (a concurrent writer can land between
// our Set and our Get on this goroutine). What we DO assert is:
//   1. No race detector reports.
//   2. The value read back is always one of the providers we wrote
//      (the providers slice is the only source of truth).
func TestAgentStateManager_SessionProviderRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	providers := []api.ClientType{api.OpenAIClientType, api.OllamaClientType, api.OpenRouterClientType, api.DeepInfraClientType, api.LMStudioClientType}
	valid := map[api.ClientType]bool{}
	for _, p := range providers {
		valid[p] = true
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			provider := providers[id%len(providers)]
			sm.SetSessionProvider(provider)
			got := sm.GetSessionProvider()
			if !valid[got] {
				t.Errorf("goroutine %d: got %v which is not in the providers set", id, got)
			}
		}(i)
	}

	wg.Wait()
}

// TestAgentStateManager_SessionModelRace tests that GetSessionModel and
// SetSessionModel are race-free when called concurrently.
//
// Same reasoning as TestAgentStateManager_SessionProviderRace: under
// `-race` we verify no data race; we do NOT assert the value just
// written is read back (concurrent writers can land between Set and
// Get on the same goroutine). We DO assert the value is always one of
// the models in our write set.
func TestAgentStateManager_SessionModelRace(t *testing.T) {
	sm := NewAgentStateManager(false)

	models := []string{"model-a", "model-b", "model-c", "model-d", "model-e"}
	valid := map[string]bool{}
	for _, m := range models {
		valid[m] = true
	}

	var wg sync.WaitGroup
	numGoroutines := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			model := models[id%len(models)]
			sm.SetSessionModel(model)
			got := sm.GetSessionModel()
			if !valid[got] {
				t.Errorf("goroutine %d: got %q which is not in the models set", id, got)
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
