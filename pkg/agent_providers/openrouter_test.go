package providers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alantheprice/ledit/pkg/agent_types"
)

func TestListModelsParsing(t *testing.T) {
	// Mock server with sample response
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			sample := `{
				"data": [
					{
						"id": "anthropic/claude-3.5-sonnet:20240620",
						"name": "Claude 3.5 Sonnet",
						"context_length": 200000,
						"pricing": {"prompt": "0.003", "completion": "0.015"}
					},
					{
						"id": "test-model",
						"name": "Test",
						"context_length": "131072"
					}
				]
			}`
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(sample))
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	// Create provider with mock client pointing to server
	p := &OpenRouterProvider{
		httpClient: server.Client(),
		apiToken:   "test",
		model:      "claude-3.5-sonnet",
	}

	// Test parsing and fuzzy match
	models, err := p.ListModels()
	if err != nil {
		t.Fatal("ListModels failed:", err)
	}

	if len(models) != 2 {
		t.Errorf("Expected 2 models, got %d", len(models))
	}

	// Check Claude parsed correctly (fuzzy match)
	if models[0].ContextLength != 200000 {
		t.Errorf("Expected 200000 for Claude, got %d", models[0].ContextLength)
	}

	// Test string context parsed
	if models[1].ContextLength != 131072 {
		t.Errorf("Expected 131072 from string, got %d", models[1].ContextLength)
	}

	// Test GetModelContextLimit with fuzzy
	cl, err := p.GetModelContextLimit()
	if err != nil {
		t.Fatal("GetModelContextLimit failed:", err)
	}
	if cl != 200000 {
		t.Errorf("Expected 200000, got %d", cl)
	}
}

func TestGetModelContextLimitFallback(t *testing.T) {
	p := &OpenRouterProvider{
		model: "unknown-gpt-3.5",
		models: []agent_types.ModelInfo{}, // Empty cache
		modelsCached: true,
	}

	cl, err := p.GetModelContextLimit()
	if err != nil {
		t.Fatal("Unexpected error:", err)
	}
	if cl != 4096 { // Model-aware fallback for gpt-3.5
		t.Errorf("Expected 4096 fallback, got %d", cl)
	}
}