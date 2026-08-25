package providers

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
)

// TestWarmModelsCachePopulatesInBackground verifies that the background
// warm-up populates the cache with the endpoint's declared context_length,
// all without blocking startup. The provider is constructed manually
// (bypassing NewGenericProvider's localhost guard) and warmModelsCache is
// called explicitly.
func TestWarmModelsCachePopulatesInBackground(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/models" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{
				"data": [
					{"id": "test-model", "context_length": 65536}
				]
			}`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "warmup-test",
		Endpoint: server.URL + "/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models: ModelConfig{
			DefaultContextLimit: 4096,
		},
	}

	// Construct directly (bypasses NewGenericProvider's 127.0.0.1 guard).
	provider := &GenericProvider{
		config:          config,
		httpClient:      &http.Client{},
		streamingClient: &http.Client{},
		model:           config.Defaults.Model,
	}

	// Immediately after construction, the cache should be cold.
	provider.mu.RLock()
	cold := !provider.modelsCached
	provider.mu.RUnlock()
	if !cold {
		t.Fatal("expected modelsCached to be false before warm-up")
	}

	// Fire the background warm-up.
	provider.warmModelsCache()

	// The first GetModelContextLimit call returns the fast fallback (4096)
	// because the background fetch hasn't completed yet.
	limit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed: %v", err)
	}
	if limit != 4096 {
		t.Logf("first call returned %d (expected fallback 4096 if goroutine hasn't finished)", limit)
	}

	// Wait for the background warm-up to complete (bounded retry).
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		provider.mu.RLock()
		warm := provider.modelsCached
		provider.mu.RUnlock()
		if warm {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	provider.mu.RLock()
	warm := provider.modelsCached
	provider.mu.RUnlock()
	if !warm {
		t.Fatal("expected modelsCache to be warmed within 3s")
	}

	// Now GetModelContextLimit should return the endpoint-declared value.
	limit, err = provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed after warm-up: %v", err)
	}
	if limit != 65536 {
		t.Errorf("expected endpoint context_length 65536 after warm-up, got %d", limit)
	}
}

// TestWarmModelsCacheSkippedForLocalEndpoints verifies that the background
// warm-up is NOT fired for localhost endpoints (the server may not be up yet).
func TestWarmModelsCacheSkippedForLocalEndpoints(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer server.Close()

	// Use 127.0.0.1 in the endpoint to simulate a local instance.
	config := &ProviderConfig{
		Name:     "local-test",
		Endpoint: "http://127.0.0.1:9999/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "local-model"},
		Models:   ModelConfig{DefaultContextLimit: 8192},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Give any potential goroutine time to fire.
	time.Sleep(100 * time.Millisecond)

	provider.mu.RLock()
	cold := !provider.modelsCached
	provider.mu.RUnlock()
	if !cold {
		t.Error("expected modelsCached to be false — local endpoints should skip warm-up")
	}

	// The test server should never be hit (different port).
	if atomic.LoadInt32(&hits) > 0 {
		t.Error("ListModels should not have been called for a local endpoint")
	}
}

// TestWarmModelsCacheSkippedWhenNoModel verifies the warm-up is skipped
// when no model is configured (nothing to look up).
func TestWarmModelsCacheSkippedWhenNoModel(t *testing.T) {
	var hits int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "no-model-test",
		Endpoint: server.URL + "/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		// No Defaults.Model
		Models: ModelConfig{DefaultContextLimit: 4096},
	}

	_, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	time.Sleep(100 * time.Millisecond)
	if atomic.LoadInt32(&hits) > 0 {
		t.Error("ListModels should not have been called when no model is configured")
	}
}

// TestGetModelContextLimitUsesCachedModelAfterWarmup is a regression guard:
// once the cache is warm (simulated manually), GetModelContextLimit returns
// the cached value without calling ListModels again.
func TestGetModelContextLimitUsesCachedModelAfterWarmup(t *testing.T) {
	called := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "cached-test",
		Endpoint: server.URL + "/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "cached-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}

	// Pre-populate the cache to simulate a completed warm-up.
	provider.mu.Lock()
	provider.model = "cached-model"
	provider.mu.Unlock()
	provider.setCachedModels([]api.ModelInfo{
		{ID: "cached-model", ContextLength: 98304, Provider: "cached-test"},
	})

	limit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed: %v", err)
	}

	if limit != 98304 {
		t.Errorf("expected cached 98304, got %d", limit)
	}
	if called {
		t.Error("ListModels should not be called when cache is already warm")
	}
}
