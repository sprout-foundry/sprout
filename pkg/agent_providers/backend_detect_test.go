package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBaseURLFromEndpoint(t *testing.T) {
	cases := []struct {
		endpoint string
		want     string
	}{
		{"http://localhost:8000/v1/chat/completions", "http://localhost:8000"},
		{"http://localhost:8080/chat/completions", "http://localhost:8080"},
		{"https://api.example.com/v1/chat/completions", "https://api.example.com"},
		{"http://host:1234/v1/chat/completions/", "http://host:1234/v1/chat/completions/"},
	}
	for _, tc := range cases {
		got := baseURLFromEndpoint(tc.endpoint)
		if got != tc.want {
			t.Errorf("baseURLFromEndpoint(%q) = %q, want %q", tc.endpoint, got, tc.want)
		}
	}
}

// --- detectBackend tests ---

func TestDetectBackendVLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get_model_info" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 32768,
				"model_name":    "meta-llama/Llama-3-8B",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	result := detectBackend(context.Background(), client, server.URL+"/v1/chat/completions")
	if result != BackendVLLM {
		t.Errorf("detectBackend() = %s, want %s", result, BackendVLLM)
	}
}

func TestDetectBackendLlamaCPP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"default_generation_settings": map[string]interface{}{
					"n_ctx": 4096,
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	result := detectBackend(context.Background(), client, server.URL+"/v1/chat/completions")
	if result != BackendLlamaCPP {
		t.Errorf("detectBackend() = %s, want %s", result, BackendLlamaCPP)
	}
}

func TestDetectBackendOpenAIFallback(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	result := detectBackend(context.Background(), client, server.URL+"/v1/chat/completions")
	if result != BackendOpenAI {
		t.Errorf("detectBackend() = %s, want %s", result, BackendOpenAI)
	}
}

func TestDetectBackendConnectionFailure(t *testing.T) {
	client := &http.Client{}
	// Use a port that's almost certainly not listening
	result := detectBackend(context.Background(), client, "http://127.0.0.1:1/v1/chat/completions")
	if result != BackendOpenAI {
		t.Errorf("detectBackend() on unreachable endpoint = %s, want %s", result, BackendOpenAI)
	}
}

// --- Context fetcher tests ---

func TestFetchVLLMContextLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get_model_info" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 131072,
				"model_name":    "Qwen/Qwen2.5-72B",
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchVLLMContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if !ok {
		t.Fatal("fetchVLLMContextLimit returned ok=false, want true")
	}
	if limit != 131072 {
		t.Errorf("fetchVLLMContextLimit() limit = %d, want 131072", limit)
	}
}

func TestFetchVLLMContextLimitFallbackField(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/get_model_info" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_position_embeddings": 8192,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchVLLMContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if !ok {
		t.Fatal("fetchVLLMContextLimit returned ok=false, want true")
	}
	if limit != 8192 {
		t.Errorf("fetchVLLMContextLimit() limit = %d, want 8192 (max_position_embeddings fallback)", limit)
	}
}

func TestFetchVLLMContextLimitFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchVLLMContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if ok {
		t.Errorf("fetchVLLMContextLimit returned ok=true, want false on 404")
	}
	if limit != 0 {
		t.Errorf("fetchVLLMContextLimit() limit = %d, want 0 on failure", limit)
	}
}

func TestFetchLlamaCPPContextLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"default_generation_settings": map[string]interface{}{
					"n_ctx": 8192,
					"model": "qwen2.5-coder-7b",
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchLlamaCPPContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if !ok {
		t.Fatal("fetchLlamaCPPContextLimit returned ok=false, want true")
	}
	if limit != 8192 {
		t.Errorf("fetchLlamaCPPContextLimit() limit = %d, want 8192", limit)
	}
}

func TestFetchLlamaCPPContextLimitTopLevelNCtx(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/props" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"n_ctx": 16384,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchLlamaCPPContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if !ok {
		t.Fatal("fetchLlamaCPPContextLimit returned ok=false, want true")
	}
	if limit != 16384 {
		t.Errorf("fetchLlamaCPPContextLimit() limit = %d, want 16384 (top-level n_ctx)", limit)
	}
}

func TestFetchLlamaCPPContextLimitFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	defer server.Close()

	client := &http.Client{}
	limit, ok := fetchLlamaCPPContextLimit(context.Background(), client, server.URL+"/v1/chat/completions", "")
	if ok {
		t.Errorf("fetchLlamaCPPContextLimit returned ok=true, want false on 404")
	}
	if limit != 0 {
		t.Errorf("fetchLlamaCPPContextLimit() limit = %d, want 0 on failure", limit)
	}
}

// --- Config tests ---

func TestBackendResolved(t *testing.T) {
	tests := []struct {
		name    string
		backend string
		want    BackendType
	}{
		{"empty defaults to auto", "", BackendAuto},
		{"explicit auto", "auto", BackendAuto},
		{"explicit vllm", "vllm", BackendVLLM},
		{"explicit llamacpp", "llamacpp", BackendLlamaCPP},
		{"explicit openai", "openai", BackendOpenAI},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := &ProviderConfig{Backend: tc.backend}
			if got := c.BackendResolved(); got != tc.want {
				t.Errorf("BackendResolved() = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestValidateModelConfigAllowsZeroContextWithBackend(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-vllm",
		Endpoint: "http://localhost:8000/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "vllm",
		Models:   ModelConfig{}, // both context limits are zero
	}
	if err := config.Validate(); err != nil {
		t.Errorf("Validate() with backend set should allow zero context limits, got error: %v", err)
	}
}

func TestValidateModelConfigRequiresContextWithoutBackend(t *testing.T) {
	config := &ProviderConfig{
		Name:     "test-no-backend",
		Endpoint: "http://localhost:8000/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Models:   ModelConfig{},
	}
	if err := config.Validate(); err == nil {
		t.Error("Validate() without backend should require context limit, got nil")
	}
}

// --- ListModels integration tests ---

func TestListModelsVLLMIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 65536,
				"model_name":    "meta-llama/Llama-3-70B",
			})
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "meta-llama/Llama-3-70B", "object": "model"},
					{"id": "meta-llama/Llama-3-8B", "object": "model"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-vllm",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "vllm",
		Defaults: RequestDefaults{Model: "meta-llama/Llama-3-70B"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}

	for _, m := range models {
		if m.ContextLength != 65536 {
			t.Errorf("model %q context_length = %d, want 65536 (from get_model_info)", m.ID, m.ContextLength)
		}
	}
}

func TestListModelsLlamaCPPIntegration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"default_generation_settings": map[string]interface{}{
					"n_ctx": 4096,
				},
			})
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "qwen2.5-coder-7b"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-llamacpp",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "llamacpp",
		Defaults: RequestDefaults{Model: "qwen2.5-coder-7b"},
		Models:   ModelConfig{DefaultContextLimit: 2048},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}

	if models[0].ContextLength != 4096 {
		t.Errorf("context_length = %d, want 4096 (from /props n_ctx)", models[0].ContextLength)
	}
}

func TestListModelsVLLMFallbackToCurrentModel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 32768,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-vllm-no-models-endpoint",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "vllm",
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model (current model fallback), got %d", len(models))
	}

	if models[0].ID != "test-model" {
		t.Errorf("model ID = %q, want 'test-model'", models[0].ID)
	}
	if models[0].ContextLength != 32768 {
		t.Errorf("context_length = %d, want 32768 (from get_model_info)", models[0].ContextLength)
	}
}

func TestListModelsAutoDetectsVLLM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 131072,
				"model_name":    "Qwen/Qwen2.5-72B",
			})
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "Qwen/Qwen2.5-72B"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-auto-vllm",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "auto",
		Defaults: RequestDefaults{Model: "Qwen/Qwen2.5-72B"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != 131072 {
		t.Errorf("context_length = %d, want 131072 (auto-detected vLLM)", models[0].ContextLength)
	}

	// Verify backend was cached
	provider.mu.RLock()
	detected := provider.detectedBackend
	provider.mu.RUnlock()
	if detected != BackendVLLM {
		t.Errorf("detectedBackend = %s, want %s", detected, BackendVLLM)
	}
}

func TestListModelsAutoDetectsLlamaCPP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/props":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"default_generation_settings": map[string]interface{}{
					"n_ctx": 8192,
				},
			})
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "llama-model"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-auto-llamacpp",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "auto",
		Defaults: RequestDefaults{Model: "llama-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != 8192 {
		t.Errorf("context_length = %d, want 8192 (auto-detected llama.cpp)", models[0].ContextLength)
	}
}

func TestListModelsOpenAIUnchanged(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "gpt-4", "context_length": 128000},
				},
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-openai-compat",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "openai",
		Defaults: RequestDefaults{Model: "gpt-4"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	models, err := provider.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels failed: %v", err)
	}

	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	if models[0].ContextLength != 128000 {
		t.Errorf("context_length = %d, want 128000 (standard OpenAI-compat)", models[0].ContextLength)
	}
}

// TestGetModelContextLimitWithVLLMBackend verifies the full chain from
// ListModels through GetModelContextLimit for a vLLM backend.
func TestGetModelContextLimitWithVLLMBackend(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/get_model_info":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"max_model_len": 262144,
			})
		case "/v1/models":
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(map[string]interface{}{
				"data": []map[string]interface{}{
					{"id": "big-model"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	config := &ProviderConfig{
		Name:     "test-vllm-ctx",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Backend:  "vllm",
		Defaults: RequestDefaults{Model: "big-model"},
		Models:   ModelConfig{DefaultContextLimit: 4096},
	}

	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("NewGenericProvider failed: %v", err)
	}

	// Warm the cache
	_, _ = provider.ListModels(context.Background())

	limit, err := provider.GetModelContextLimit()
	if err != nil {
		t.Fatalf("GetModelContextLimit failed: %v", err)
	}
	if limit != 262144 {
		t.Errorf("GetModelContextLimit() = %d, want 262144 (from vLLM get_model_info)", limit)
	}
}
