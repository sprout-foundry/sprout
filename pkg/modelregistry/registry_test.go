package modelregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestIsEnabled_NotSet(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("")
	if IsEnabled() {
		t.Error("expected IsEnabled() = false when URL is empty")
	}
}

func TestIsEnabled_Set(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("http://localhost:8080")
	if !IsEnabled() {
		t.Error("expected IsEnabled() = true when URL is set")
	}
}

func TestFetchModels_Disabled(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("")
	ClearCache()

	models, err := FetchModels(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("expected nil error when disabled, got: %v", err)
	}
	if models != nil {
		t.Fatalf("expected nil models when disabled, got: %v", models)
	}
}

func TestFetchModels_Success(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/models/openrouter.json" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models: []ModelInfo{
				{ID: "anthropic/claude-3", Name: "Claude 3", ContextLength: 200000},
				{ID: "openai/gpt-4o", Name: "GPT-4o", ContextLength: 128000},
			},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(5 * time.Minute)

	models, err := FetchModels(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("expected 2 models, got %d", len(models))
	}
	if models[0].ID != "anthropic/claude-3" {
		t.Errorf("expected first model ID 'anthropic/claude-3', got %q", models[0].ID)
	}
	if models[1].ContextLength != 128000 {
		t.Errorf("expected context length 128000, got %d", models[1].ContextLength)
	}
}

func TestFetchModels_CacheHit(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "test-model"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(1 * time.Hour)

	models1, err := FetchModels(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount.Load())
	}

	models2, err := FetchModels(context.Background(), "test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected cache hit (still 1 fetch), got %d", fetchCount.Load())
	}
	if len(models2) != 1 || models2[0].ID != "test-model" {
		t.Errorf("cache returned wrong data: %v", models2)
	}
	_ = models1
}

func TestFetchModels_CacheExpiry(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "fresh-model"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(50 * time.Millisecond)

	FetchModels(context.Background(), "expiring")
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 initial fetch, got %d", fetchCount.Load())
	}

	// Wait for cache to expire, then fetch once more.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && fetchCount.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
		FetchModels(context.Background(), "expiring")
	}

	if fetchCount.Load() < 2 {
		t.Fatalf("expected at least 2 fetches after cache expiry, got %d", fetchCount.Load())
	}
}

func TestFetchModels_NotFound(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "nonexistent")
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if models != nil {
		t.Fatalf("expected nil models for 404, got: %v", models)
	}
}

func TestFetchModels_ServerError(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if models != nil {
		t.Fatalf("expected nil models for 500, got: %v", models)
	}
}

func TestFetchModels_InvalidJSON(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json"))
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "badjson")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if models != nil {
		t.Fatalf("expected nil models for invalid JSON, got: %v", models)
	}
}

func TestFetchModels_UnsupportedSchemaVersion(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Schema version 3 is newer than this client supports → rejected, so the
		// caller gracefully falls back to the live provider API.
		w.Write([]byte(`{"schema_version": 3, "models": [{"id": "test"}]}`))
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "badversion")
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
	if !strings.Contains(err.Error(), "unsupported schema version") {
		t.Errorf("expected error containing 'unsupported schema version', got: %v", err)
	}
	if models != nil {
		t.Fatalf("expected nil models for unsupported schema version, got: %v", models)
	}
}

func TestFetchModels_CanonicalSchemaV2(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"schema_version":2,"provider":"openrouter","generated_at":"2024-01-01T00:00:00Z","models":[
			{"id":"openai/gpt-x","provider":"openrouter","display_name":"GPT-X","context_window":200000,
			 "pricing":{"input_per_mtok":2,"output_per_mtok":8,"currency":"USD"},
			 "capabilities":{"tools":true,"vision":true},
			 "eligible_roles":["primary","subagent"]}
		]}`))
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected 1 model, got %d", len(models))
	}
	m := models[0]
	if m.ID != "openai/gpt-x" || m.ContextLength != 200000 || m.InputCost != 2 {
		t.Errorf("flattened fields wrong: %+v", m)
	}
	hasTools := false
	for _, tag := range m.Tags {
		if tag == "tools" {
			hasTools = true
		}
	}
	if !hasTools {
		t.Errorf("expected capabilities flattened to tags, got %v", m.Tags)
	}
	if len(m.EligibleRoles) != 2 {
		t.Errorf("expected eligible_roles carried through, got %v", m.EligibleRoles)
	}
}

func TestFetchModels_LegacySchemaVersion(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// No schema_version field - defaults to 0 (legacy/unset), which should be accepted
		w.Write([]byte(`{"updated_at": "2024-01-01T00:00:00Z", "models": [{"id": "legacy-model"}]}`))
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	models, err := FetchModels(context.Background(), "legacy")
	if err != nil {
		t.Fatalf("unexpected error for legacy schema version: %v", err)
	}
	if models == nil {
		t.Fatal("expected non-nil models for legacy schema version")
	}
	if len(models) != 1 || models[0].ID != "legacy-model" {
		t.Errorf("expected models with ID 'legacy-model', got: %v", models)
	}
}

func TestFetchModels_SentinelDisabledEnv(t *testing.T) {
	// Verify that loadConfig recognizes "disabled", "off", "none" sentinels.
	// We test by calling SetBaseURL (which loadConfig would set to ""),
	// then verifying IsEnabled returns false and FetchModels returns nil.
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	for _, sentinel := range []string{"disabled", "Disabled", "OFF", "None"} {
		t.Run(sentinel, func(t *testing.T) {
			// Simulate what loadConfig does when it sees the sentinel.
			lower := strings.ToLower(strings.TrimSpace(sentinel))
			if lower == "off" || lower == "none" || lower == "disabled" {
				SetBaseURL("")
			}
			if IsEnabled() {
				t.Errorf("expected IsEnabled() = false for sentinel %q", sentinel)
			}
			ClearCache()
			models, err := FetchModels(context.Background(), "openrouter")
			if err != nil {
				t.Fatalf("expected nil error when disabled, got: %v", err)
			}
			if models != nil {
				t.Fatalf("expected nil models when disabled, got: %v", models)
			}
		})
	}
}

func TestSetTTL(t *testing.T) {
	originalTTL := ttlCopy()
	defer func() { SetTTL(originalTTL) }()

	SetTTL(10 * time.Second)
	if ttlCopy() != 10*time.Second {
		t.Errorf("expected TTL 10s, got %v", ttlCopy())
	}
}

func TestClearCache(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "cached"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(1 * time.Hour)

	FetchModels(context.Background(), "cleared")
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount.Load())
	}

	ClearCache()

	FetchModels(context.Background(), "cleared")
	if fetchCount.Load() != 2 {
		t.Fatalf("expected 2 fetches after clear, got %d", fetchCount.Load())
	}
}

func TestFetchModels_ProviderIDNormalized(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var lastPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "model"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	FetchModels(context.Background(), "OpenRouter")

	path := lastPath.Load().(string)
	if path != "/models/openrouter.json" {
		t.Errorf("expected normalized path /models/openrouter.json, got %s", path)
	}
}

func TestFetchModels_NegativeCache(t *testing.T) {
	originalURL := baseURLCopy()
	originalNegTTL := negativeTTLCopy()
	defer func() {
		SetBaseURL(originalURL)
		SetNegativeTTL(originalNegTTL)
	}()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetNegativeTTL(200 * time.Millisecond)

	// First request: 404 → stored in negative cache
	models, err := FetchModels(context.Background(), "negcached")
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if models != nil {
		t.Fatalf("expected nil models for 404, got: %v", models)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount.Load())
	}

	// Second request: should hit negative cache, NOT make a network call
	models, err = FetchModels(context.Background(), "negcached")
	if err != nil {
		t.Fatalf("expected nil error from negative cache, got: %v", err)
	}
	if models != nil {
		t.Fatalf("expected nil models from negative cache, got: %v", models)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected negative cache hit (still 1 fetch), got %d", fetchCount.Load())
	}

	// Wait for negative cache to expire, then fetch again
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && fetchCount.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
		FetchModels(context.Background(), "negcached")
	}

	if fetchCount.Load() < 2 {
		t.Fatalf("expected at least 2 fetches after negative cache expiry, got %d", fetchCount.Load())
	}
}

func TestFetchModels_ContextCancelled(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client disconnects (context cancellation).
		<-r.Context().Done()
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	_, err := FetchModels(ctx, "slow")
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
}

func TestFetchModels_HTTPTimeout(t *testing.T) {
	originalURL := baseURLCopy()
	originalTimeout := httpTimeoutCopy()
	defer func() {
		SetBaseURL(originalURL)
		SetHTTPTimeout(originalTimeout)
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "slow-model"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetHTTPTimeout(50 * time.Millisecond)

	start := time.Now()
	_, err := FetchModels(context.Background(), "slow-timeout")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to HTTP timeout")
	}
	if elapsed > time.Second {
		t.Errorf("timeout should have fired within 50ms, but took %v", elapsed)
	}
}

// --- NEW TESTS from code review ---

func TestFetchModels_InvalidProviderID(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	// Use a mock server that returns 404 for valid IDs, so validation errors
	// are distinguishable from HTTP errors. A 404 for a valid provider ID should
	// return (nil, nil), but validation should return an error BEFORE the request.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	testCases := []struct {
		name string
		id   string
	}{
		{"empty", ""},
		{"spaces only", "   "},
		{"with slash", "open/router"},
		{"with dot", "open.router"},
		{"with special chars", "openrouter!@#"},
		{"too long", strings.Repeat("a", 129)},
		{"with spaces", "open router"},
		{"with mixed invalid chars", "model-1!invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			models, err := FetchModels(context.Background(), tc.id)
			if err == nil {
				t.Fatalf("expected error for invalid provider ID %q", tc.id)
			}
			if models != nil {
				t.Fatalf("expected nil models for invalid ID, got: %v", models)
			}
			// Verify the error is a validation error, not an HTTP error
			if !strings.Contains(err.Error(), "invalid provider ID") {
				t.Errorf("expected validation error for %q, got: %v", tc.id, err)
			}
		})
	}
}

// TestFetchModels_ValidIDAfterNormalization tests that IDs with uppercase chars
// are normalized to lowercase and treated as valid (if they contain only valid chars).
func TestFetchModels_ValidIDAfterNormalization(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var lastPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.Path)
		// Return 404 for all valid IDs - normalization should work, then 404 returns nil, nil
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()

	testCases := []struct {
		name         string
		id           string
		expectedPath string
	}{
		{"uppercase", "OpenRouter", "/models/openrouter.json"},
		{"mixed case", "MyProvider-V2", "/models/myprovider-v2.json"},
		{"all caps", "OPENAI", "/models/openai.json"},
		{"camelCase", "deepInfra", "/models/deepinfra.json"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			models, err := FetchModels(context.Background(), tc.id)
			// Valid ID after normalization: should succeed but return nil (404).
			if err != nil {
				t.Fatalf("unexpected error for valid ID %q: %v", tc.id, err)
			}
			if models != nil {
				t.Fatalf("expected nil models for 404 response, got: %v", models)
			}
			// Verify normalization worked.
			path := lastPath.Load().(string)
			if path != tc.expectedPath {
				t.Errorf("expected path %s, got %s", tc.expectedPath, path)
			}
		})
	}
}

func TestFetchModels_ConcurrentRequests(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		// Small delay to make concurrent requests more likely
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(providerResponse{
			UpdatedAt: "2024-01-01T00:00:00Z",
			Models:    []ModelInfo{{ID: "shared-model"}},
		})
	}))
	defer srv.Close()

	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(1 * time.Hour)

	var wg sync.WaitGroup
	concurrency := 10
	errors := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			models, err := FetchModels(context.Background(), "shared")
			if err != nil {
				errors[idx] = err
				return
			}
			if len(models) != 1 || models[0].ID != "shared-model" {
				errors[idx] = fmt.Errorf("wrong model data")
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errors {
		if err != nil {
			t.Errorf("goroutine %d failed: %v", i, err)
		}
	}

	// singleflight should deduplicate: only 1 network call despite 10 goroutines
	if fetchCount.Load() != 1 {
		t.Errorf("expected 1 fetch (singleflight dedup), got %d", fetchCount.Load())
	}
}

func TestIsValidProviderID(t *testing.T) {
	valid := []string{"openrouter", "openai", "zai", "ollama-local", "deepinfra", "a1", "model-v2"}
	invalid := []string{"", "OpenRouter", "open/router", "open.router", "open router", "has!special", strings.Repeat("a", 129)}

	for _, id := range valid {
		if !isValidProviderID(id) {
			t.Errorf("expected %q to be valid", id)
		}
	}
	for _, id := range invalid {
		if isValidProviderID(id) {
			t.Errorf("expected %q to be invalid", id)
		}
	}
}
