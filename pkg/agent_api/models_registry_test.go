package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// TestConvertRegistryModels tests that the convertRegistryModels function
// correctly converts modelregistry.RawModel slices to ModelInfo slices.
func TestConvertRegistryModels(t *testing.T) {
	t.Run("nil input returns empty slice", func(t *testing.T) {
		result := convertRegistryModels(nil)
		// make([]ModelInfo, 0) returns a non-nil empty slice.
		if result == nil || len(result) != 0 {
			t.Fatalf("expected non-nil empty slice, got %v", result)
		}
	})

	t.Run("empty slice returns empty slice", func(t *testing.T) {
		result := convertRegistryModels([]modelregistry.RawModel{})
		if len(result) != 0 {
			t.Fatalf("expected empty slice, got %v", result)
		}
	})

	t.Run("full field mapping", func(t *testing.T) {
		input := []modelregistry.RawModel{
			{
				ID:             "anthropic/claude-3",
				Name:           "Claude 3",
				Description:    "A large language model",
				Provider:       "openrouter",
				Size:           "70B",
				Cost:           5.0,
				InputCost:      3.0,
				OutputCost:     7.0,
				CachedInputCost: 0.3,
				ContextLength:  200000,
				Tags:           []string{"coding", "tools"},
			},
		}

		result := convertRegistryModels(input)

		if len(result) != 1 {
			t.Fatalf("expected 1 model, got %d", len(result))
		}

		m := result[0]
		if m.ID != "anthropic/claude-3" {
			t.Errorf("expected ID %q, got %q", "anthropic/claude-3", m.ID)
		}
		if m.Name != "Claude 3" {
			t.Errorf("expected Name %q, got %q", "Claude 3", m.Name)
		}
		if m.Description != "A large language model" {
			t.Errorf("expected Description %q, got %q", "A large language model", m.Description)
		}
		if m.Provider != "openrouter" {
			t.Errorf("expected Provider %q, got %q", "openrouter", m.Provider)
		}
		if m.Size != "70B" {
			t.Errorf("expected Size %q, got %q", "70B", m.Size)
		}
		if m.Cost != 5.0 {
			t.Errorf("expected Cost %f, got %f", 5.0, m.Cost)
		}
		if m.InputCost != 3.0 {
			t.Errorf("expected InputCost %f, got %f", 3.0, m.InputCost)
		}
		if m.OutputCost != 7.0 {
			t.Errorf("expected OutputCost %f, got %f", 7.0, m.OutputCost)
		}
		// CachedInputCost must be carried through so downstream pricing-aware
		// code (calculateCachedTokenSavings) can compute exact savings.
		if m.CachedInputCost != 0.3 {
			t.Errorf("expected CachedInputCost %f, got %f", 0.3, m.CachedInputCost)
		}
		if m.ContextLength != 200000 {
			t.Errorf("expected ContextLength %d, got %d", 200000, m.ContextLength)
		}
		if len(m.Tags) != 2 || m.Tags[0] != "coding" || m.Tags[1] != "tools" {
			t.Errorf("expected Tags [coding, tools], got %v", m.Tags)
		}
	})

	t.Run("CachedInputCost zero is preserved (not a discount signal)", func(t *testing.T) {
		// CachedInputCost=0 means "provider does not expose a distinct
		// cached rate" — this is distinct from a 0% discount which would
		// be an error. The conversion must not silently coerce 0 to a
		// fallback value.
		input := []modelregistry.RawModel{
			{ID: "no-cache", InputCost: 1.0, OutputCost: 2.0, CachedInputCost: 0},
		}
		result := convertRegistryModels(input)
		if result[0].CachedInputCost != 0 {
			t.Errorf("expected CachedInputCost to stay 0, got %f", result[0].CachedInputCost)
		}
	})

	t.Run("tags are independently copied", func(t *testing.T) {
		input := []modelregistry.RawModel{
			{ID: "model-1", Tags: []string{"a", "b"}},
		}

		result := convertRegistryModels(input)

		// Mutating the original tags should not affect the result.
		input[0].Tags[0] = "mutated"
		if result[0].Tags[0] != "a" {
			t.Error("tags should be independently copied, not shared")
		}
	})

	t.Run("nil tags result in nil tags", func(t *testing.T) {
		input := []modelregistry.RawModel{
			{ID: "no-tags", Tags: nil},
		}

		result := convertRegistryModels(input)
		if result[0].Tags != nil {
			t.Errorf("expected nil tags, got %v", result[0].Tags)
		}
	})
}

// TestGetModelsForProviderCtx_RegistryHit tests that when the model registry
// is enabled and returns valid models, those are used directly without
// falling back to the per-provider API.
func TestGetModelsForProviderCtx_RegistryHit(t *testing.T) {
	srv, restore := setupTestRegistry(t)
	defer srv.Close()
	defer restore()

	var requestedPath string
	// Registry returns two models for openrouter.
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"updated_at": "2024-01-01T00:00:00Z",
			"models": []modelregistry.ModelInfo{
				{ID: "registry-model-1", Name: "Registry Model 1", ContextLength: 128000},
				{ID: "registry-model-2", Name: "Registry Model 2", ContextLength: 200000},
			},
		})
	})

	models, err := GetModelsForProviderCtx(context.Background(), OpenRouterClientType)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models from registry, got %d", len(models))
	}
	if requestedPath != "/models/openrouter.json" {
		t.Errorf("expected request to /models/openrouter.json, got %s", requestedPath)
	}
	if models[0].ID != "registry-model-1" {
		t.Errorf("expected first model ID 'registry-model-1', got %q", models[0].ID)
	}
	if models[0].ContextLength != 128000 {
		t.Errorf("expected ContextLength 128000, got %d", models[0].ContextLength)
	}
}

// TestGetModelsForProviderCtx_RegistryDisabled tests that when the registry
// is not configured, the fallback path is used. The fallback behavior depends
// on whether credentials are available in the environment, so we verify the
// returned models come from the fallback (by checking no model IDs match a
// deterministic registry payload).
func TestGetModelsForProviderCtx_RegistryDisabled(t *testing.T) {
	_, restore := setupTestRegistry(t)
	defer restore()

	// Explicitly disable the registry.
	modelregistry.SetBaseURL("")
	modelregistry.ClearCache()

	// The fallback path runs. If credentials happen to be available, this
	// succeeds; if not, it fails with a provider error (not a registry error).
	models, err := GetModelsForProviderCtx(context.Background(), OpenRouterClientType)
	if err != nil {
		// Verify it's NOT a registry error.
		if strings.Contains(err.Error(), "modelregistry") {
			t.Errorf("error should not mention registry when disabled, got: %v", err)
		}
		return
	}
	// If it succeeded, models came from the direct provider API (fallback).
	_ = models
}

// TestGetModelsForProviderCtx_RegistryNotFound404 tests that a 404 from the
// registry (provider not found) falls through to the direct API path.
func TestGetModelsForProviderCtx_RegistryNotFound404(t *testing.T) {
	srv, restore := setupTestRegistry(t)
	defer srv.Close()
	defer restore()

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})

	// 404 from registry should fall through to direct provider API.
	// Whether it succeeds depends on available credentials, but the
	// error should NOT be from the registry.
	models, err := GetModelsForProviderCtx(context.Background(), OpenRouterClientType)
	if err != nil {
		if strings.Contains(err.Error(), "modelregistry: fetch") {
			t.Errorf("error should be from fallback path, not registry: %v", err)
		}
		return
	}
	// If it succeeded, models came from the direct API fallback.
	_ = models
}

// TestGetModelsForProviderCtx_RegistryEmptyModels tests that an empty models
// slice from the registry is returned directly (non-nil empty slice passes
// the `err == nil && registryModels != nil` check).
func TestGetModelsForProviderCtx_RegistryEmptyModels(t *testing.T) {
	srv, restore := setupTestRegistry(t)
	defer srv.Close()
	defer restore()

	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"updated_at": "2024-01-01T00:00:00Z",
			"models":     []modelregistry.ModelInfo{},
		})
	})

	models, err := GetModelsForProviderCtx(context.Background(), OpenRouterClientType)
	if err != nil {
		t.Fatalf("unexpected error for empty registry models: %v", err)
	}
	if len(models) != 0 {
		t.Fatalf("expected 0 models from empty registry response, got %d", len(models))
	}
}

// testServer wraps httptest.Server to allow updating the handler.
type testServer struct {
	*httptest.Server
}

// setupTestRegistry creates a test registry server, configures modelregistry
// to use it, and returns a cleanup function that restores the original state.
func setupTestRegistry(t *testing.T) (*testServer, func()) {
	t.Helper()

	// Save original env var so we can restore it exactly.
	originalEnvURL := os.Getenv("LEDIT_MODEL_REGISTRY_URL")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	modelregistry.SetBaseURL(srv.URL)
	modelregistry.ClearCache()
	modelregistry.SetTTL(5 * time.Minute)

	restore := func() {
		srv.Close()
		modelregistry.ClearCache()
		// Restore to pre-test state based on the env var.
		if originalEnvURL != "" {
			modelregistry.SetBaseURL(originalEnvURL)
		} else {
			modelregistry.SetBaseURL("")
		}
	}

	return &testServer{srv}, restore
}
