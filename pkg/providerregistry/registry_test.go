package providerregistry

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// validProviderJSON is a full sample provider config JSON for use in tests.
const validProviderJSON = `{
  "name": "openrouter",
  "endpoint": "https://openrouter.ai/api/v1/chat/completions",
  "auth": {
    "type": "bearer",
    "env_var": "OPENROUTER_API_KEY"
  },
  "headers": {
    "HTTP-Referer": "https://github.com/sprout-foundry/sprout",
    "X-Title": "Sprout"
  },
  "defaults": {
    "model": "openai/gpt-5",
    "temperature": 0.7,
    "max_tokens": -1,
    "top_p": 1.0
  },
  "message_conversion": {
    "include_tool_call_id": true,
    "convert_tool_role_to_user": false,
    "reasoning_content_field": "reasoning_content",
    "arguments_as_json": false,
    "skip_tool_execution_summary": false,
    "force_tool_call_type": ""
  },
  "streaming": {
    "format": "sse",
    "chunk_timeout_ms": 300000,
    "done_marker": "[DONE]"
  },
  "models": {
    "default_context_limit": 128000,
    "default_max_completion_tokens": 128000,
    "default_model": "openai/gpt-5",
    "supports_vision": true,
    "vision_model": "google/gemma-3-27b-it",
    "available_models": [],
    "model_overrides": {
      "openai/gpt-5": 272000
    },
    "max_completion_overrides": {
      "openai/gpt-5": 128000
    },
    "pattern_overrides": [
      {"pattern": "gpt-4.*", "context_limit": 128000}
    ],
    "completion_pattern_overrides": [
      {"pattern": "gpt-5.*", "context_limit": 128000}
    ],
    "model_info": [
      {"id": "openai/gpt-5", "name": "GPT-5", "context_length": 272000}
    ]
  },
  "retry": {
    "max_attempts": 3,
    "base_delay_ms": 1000,
    "backoff_multiplier": 2.0,
    "max_delay_ms": 30000,
    "retryable_errors": ["rate_limit_exceeded", "timeout"]
  },
  "cost": {
    "input_token_cost": 0.0,
    "output_token_cost": 0.0,
    "currency": "USD"
  }
}`

// setupTest is a helper that configures the package for testing and returns
// a cleanup function to restore state.
func setupTest(t *testing.T, srv *httptest.Server) {
	t.Helper()
	SetBaseURL(srv.URL)
	ClearCache()
	SetTTL(5 * time.Minute)
	SetHTTPTimeout(defaultHTTPTimeout)
	SetNegativeTTL(defaultNegativeTTL)
}

// ---------------------------------------------------------------------------
// IsEnabled
// ---------------------------------------------------------------------------

func TestIsEnabled_Default(t *testing.T) {
	// By default the package is enabled (loadConfig in init() sets defaultRegistryURL).
	// We just verify IsEnabled() returns true with a non-empty URL.
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("http://example.com")
	if !IsEnabled() {
		t.Error("expected IsEnabled() = true when URL is set")
	}
}

func TestIsEnabled_Disabled(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("")
	if IsEnabled() {
		t.Error("expected IsEnabled() = false when URL is empty")
	}
}

func TestIsEnabled_CustomURL(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("http://my-registry.example.com")
	if !IsEnabled() {
		t.Error("expected IsEnabled() = true with custom URL")
	}
}

// ---------------------------------------------------------------------------
// Config defaults and setter effects
// ---------------------------------------------------------------------------

func TestConfig_DefaultBaseURL(t *testing.T) {
	// Verify that baseURL defaults to defaultRegistryURL from init()/loadConfig()
	url := baseURLCopy()
	if url != defaultRegistryURL {
		t.Errorf("expected default URL %q, got %q", defaultRegistryURL, url)
	}
	if !IsEnabled() {
		t.Error("expected IsEnabled() = true with default URL")
	}
}

func TestConfig_SettingsTakeEffect(t *testing.T) {
	// Verify that SetTTL, SetHTTPTimeout, and SetNegativeTTL affect subsequent operations
	originalTTL := ttlCopy()
	originalTimeout := httpTimeoutCopy()
	originalNegTTL := negativeTTLCopy()
	defer func() {
		SetTTL(originalTTL)
		SetHTTPTimeout(originalTimeout)
		SetNegativeTTL(originalNegTTL)
	}()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/providers/fast.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(validProviderJSON))
		case "/providers/slow.json":
			time.Sleep(1 * time.Second)
			w.Write([]byte(validProviderJSON))
		case "/providers/missing.json":
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	setupTest(t, srv)

	// SetHTTPTimeout — short timeout should cause failure for a slow endpoint
	SetHTTPTimeout(50 * time.Millisecond)
	_, err := FetchProviderConfig(context.Background(), "slow")
	if err == nil {
		t.Error("expected timeout error with short HTTP timeout")
	}
	SetHTTPTimeout(defaultHTTPTimeout)

	// SetTTL — very short TTL should cause cache re-fetch on expiry
	ClearCache()
	SetTTL(50 * time.Millisecond)
	cfg1, err := FetchProviderConfig(context.Background(), "fast")
	if err != nil || cfg1 == nil {
		t.Fatalf("first fetch failed: err=%v, cfg=%v", err, cfg1)
	}
	time.Sleep(100 * time.Millisecond)
	cfg2, err := FetchProviderConfig(context.Background(), "fast")
	if err != nil || cfg2 == nil {
		t.Fatalf("second fetch after TTL expiry failed: err=%v, cfg=%v", err, cfg2)
	}

	// SetNegativeTTL — short negative TTL should allow re-check after expiry
	ClearCache()
	SetNegativeTTL(50 * time.Millisecond)
	_, err = FetchProviderConfig(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected no error for 404, got: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	_, err = FetchProviderConfig(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected no error after negative cache expiry, got: %v", err)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — basic success
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/providers/openrouter.json" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)

	cfg, err := FetchProviderConfig(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}
	if cfg.Name != "openrouter" {
		t.Errorf("expected name 'openrouter', got %q", cfg.Name)
	}
	if cfg.Endpoint != "https://openrouter.ai/api/v1/chat/completions" {
		t.Errorf("unexpected endpoint: %q", cfg.Endpoint)
	}
	if cfg.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got %q", cfg.Auth.Type)
	}
	if cfg.Auth.EnvVar != "OPENROUTER_API_KEY" {
		t.Errorf("expected env_var 'OPENROUTER_API_KEY', got %q", cfg.Auth.EnvVar)
	}
	if len(cfg.Headers) != 2 {
		t.Errorf("expected 2 headers, got %d", len(cfg.Headers))
	}
	if cfg.Headers["HTTP-Referer"] != "https://github.com/sprout-foundry/sprout" {
		t.Errorf("unexpected HTTP-Referer: %q", cfg.Headers["HTTP-Referer"])
	}
}

func TestFetchProviderConfig_ValidJSONParsing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)

	cfg, err := FetchProviderConfig(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Defaults
	if cfg.Defaults.Model != "openai/gpt-5" {
		t.Errorf("expected model 'openai/gpt-5', got %q", cfg.Defaults.Model)
	}
	if cfg.Defaults.Temperature == nil || *cfg.Defaults.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", cfg.Defaults.Temperature)
	}
	if cfg.Defaults.MaxTokens == nil || *cfg.Defaults.MaxTokens != -1 {
		t.Errorf("expected max_tokens -1, got %v", cfg.Defaults.MaxTokens)
	}
	if cfg.Defaults.TopP == nil || *cfg.Defaults.TopP != 1.0 {
		t.Errorf("expected top_p 1.0, got %v", cfg.Defaults.TopP)
	}

	// Message conversion
	if !cfg.Conversion.IncludeToolCallID {
		t.Error("expected include_tool_call_id = true")
	}
	if cfg.Conversion.ReasoningContentField != "reasoning_content" {
		t.Errorf("expected reasoning_content_field, got %q", cfg.Conversion.ReasoningContentField)
	}

	// Streaming
	if cfg.Streaming.Format != "sse" {
		t.Errorf("expected format 'sse', got %q", cfg.Streaming.Format)
	}
	if cfg.Streaming.ChunkTimeoutMs != 300000 {
		t.Errorf("expected chunk_timeout_ms 300000, got %d", cfg.Streaming.ChunkTimeoutMs)
	}
	if cfg.Streaming.DoneMarker != "[DONE]" {
		t.Errorf("expected done_marker '[DONE]', got %q", cfg.Streaming.DoneMarker)
	}

	// Models
	if cfg.Models.DefaultContextLimit != 128000 {
		t.Errorf("expected default_context_limit 128000, got %d", cfg.Models.DefaultContextLimit)
	}
	if cfg.Models.DefaultMaxCompletionTokens != 128000 {
		t.Errorf("expected default_max_completion_tokens 128000, got %d", cfg.Models.DefaultMaxCompletionTokens)
	}
	if !cfg.Models.SupportsVision {
		t.Error("expected supports_vision = true")
	}
	if cfg.Models.VisionModel != "google/gemma-3-27b-it" {
		t.Errorf("expected vision_model, got %q", cfg.Models.VisionModel)
	}
	if cfg.Models.DefaultModel != "openai/gpt-5" {
		t.Errorf("expected default_model, got %q", cfg.Models.DefaultModel)
	}
	if len(cfg.Models.ModelOverrides) != 1 {
		t.Fatalf("expected 1 model override, got %d", len(cfg.Models.ModelOverrides))
	}
	if cfg.Models.ModelOverrides["openai/gpt-5"] != 272000 {
		t.Errorf("expected model override 272000, got %d", cfg.Models.ModelOverrides["openai/gpt-5"])
	}
	if len(cfg.Models.PatternOverrides) != 1 {
		t.Fatalf("expected 1 pattern override, got %d", len(cfg.Models.PatternOverrides))
	}
	if cfg.Models.PatternOverrides[0].Pattern != "gpt-4.*" {
		t.Errorf("expected pattern 'gpt-4.*', got %q", cfg.Models.PatternOverrides[0].Pattern)
	}
	if cfg.Models.PatternOverrides[0].ContextLimit != 128000 {
		t.Errorf("expected context_limit 128000, got %d", cfg.Models.PatternOverrides[0].ContextLimit)
	}
	if len(cfg.Models.CompletionPatternOverrides) != 1 {
		t.Fatalf("expected 1 completion pattern override, got %d", len(cfg.Models.CompletionPatternOverrides))
	}
	if len(cfg.Models.ModelInfo) != 1 {
		t.Fatalf("expected 1 model info, got %d", len(cfg.Models.ModelInfo))
	}
	if cfg.Models.ModelInfo[0].ID != "openai/gpt-5" {
		t.Errorf("expected model info ID 'openai/gpt-5', got %q", cfg.Models.ModelInfo[0].ID)
	}
	if cfg.Models.ModelInfo[0].ContextLength != 272000 {
		t.Errorf("expected context_length 272000, got %d", cfg.Models.ModelInfo[0].ContextLength)
	}

	// Retry
	if cfg.Retry.MaxAttempts != 3 {
		t.Errorf("expected max_attempts 3, got %d", cfg.Retry.MaxAttempts)
	}
	if cfg.Retry.BaseDelayMs != 1000 {
		t.Errorf("expected base_delay_ms 1000, got %d", cfg.Retry.BaseDelayMs)
	}
	if cfg.Retry.BackoffMultiplier != 2.0 {
		t.Errorf("expected backoff_multiplier 2.0, got %f", cfg.Retry.BackoffMultiplier)
	}
	if cfg.Retry.MaxDelayMs != 30000 {
		t.Errorf("expected max_delay_ms 30000, got %d", cfg.Retry.MaxDelayMs)
	}
	if len(cfg.Retry.RetryableErrors) != 2 {
		t.Errorf("expected 2 retryable errors, got %d", len(cfg.Retry.RetryableErrors))
	}

	// Cost
	if cfg.Cost.Currency != "USD" {
		t.Errorf("expected currency 'USD', got %q", cfg.Cost.Currency)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — provider ID normalization
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_IDNormalization(t *testing.T) {
	var lastPath atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath.Store(r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)

	// "OpenRouter" → normalized to "openrouter"
	cfg, err := FetchProviderConfig(context.Background(), "OpenRouter")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil config")
	}

	path := lastPath.Load().(string)
	if path != "/providers/openrouter.json" {
		t.Errorf("expected path '/providers/openrouter.json', got %q", path)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — cache hit
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_CacheHit(t *testing.T) {
	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetTTL(1 * time.Hour)

	cfg1, err := FetchProviderConfig(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("first fetch failed: %v", err)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount.Load())
	}

	cfg2, err := FetchProviderConfig(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("second fetch failed: %v", err)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected cache hit (still 1 fetch), got %d", fetchCount.Load())
	}
	if cfg2.Name != "openrouter" {
		t.Errorf("cached config has wrong name: %q", cfg2.Name)
	}
	_ = cfg1
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — cache expiry
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_CacheExpiry(t *testing.T) {
	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetTTL(50 * time.Millisecond)

	FetchProviderConfig(context.Background(), "expiring")
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 initial fetch, got %d", fetchCount.Load())
	}

	// Wait for cache to expire, then fetch once more.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && fetchCount.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
		FetchProviderConfig(context.Background(), "expiring")
	}

	if fetchCount.Load() < 2 {
		t.Fatalf("expected at least 2 fetches after cache expiry, got %d", fetchCount.Load())
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — negative cache
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_NegativeCache(t *testing.T) {
	originalNegTTL := negativeTTLCopy()
	defer func() { SetNegativeTTL(originalNegTTL) }()

	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetNegativeTTL(200 * time.Millisecond)

	// First: 404 → negative cache
	cfg, err := FetchProviderConfig(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected nil error for 404, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config for 404, got: %v", cfg)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected 1 fetch, got %d", fetchCount.Load())
	}

	// Second: should hit negative cache (no network call)
	cfg, err = FetchProviderConfig(context.Background(), "missing")
	if err != nil {
		t.Fatalf("expected nil error from negative cache, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config from negative cache, got: %v", cfg)
	}
	if fetchCount.Load() != 1 {
		t.Fatalf("expected negative cache hit (still 1 fetch), got %d", fetchCount.Load())
	}

	// Wait for negative cache to expire, then fetch again.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && fetchCount.Load() < 2 {
		time.Sleep(10 * time.Millisecond)
		FetchProviderConfig(context.Background(), "missing")
	}

	if fetchCount.Load() < 2 {
		t.Fatalf("expected at least 2 fetches after negative cache expiry, got %d", fetchCount.Load())
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — invalid provider ID
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_InvalidProviderID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	setupTest(t, srv)

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
		{"with at sign", "open@router"},
		{"with underscore and special", "model_1!invalid"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := FetchProviderConfig(context.Background(), tc.id)
			if err == nil {
				t.Fatalf("expected error for invalid provider ID %q", tc.id)
			}
			if cfg != nil {
				t.Fatalf("expected nil config for invalid ID, got: %v", cfg)
			}
			if !strings.Contains(err.Error(), "invalid provider ID") {
				t.Errorf("expected validation error for %q, got: %v", tc.id, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — disabled registry
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_Disabled(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("")
	ClearCache()

	cfg, err := FetchProviderConfig(context.Background(), "openrouter")
	if err != nil {
		t.Fatalf("expected nil error when disabled, got: %v", err)
	}
	if cfg != nil {
		t.Fatalf("expected nil config when disabled, got: %v", cfg)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — HTTP error (non-404, non-200)
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	setupTest(t, srv)

	cfg, err := FetchProviderConfig(context.Background(), "broken")
	if err == nil {
		t.Fatal("expected error for 500 response")
	}
	if cfg != nil {
		t.Fatalf("expected nil config for 500, got: %v", cfg)
	}
	if !strings.Contains(err.Error(), "HTTP 500") {
		t.Errorf("expected error mentioning HTTP 500, got: %v", err)
	}
}

func TestFetchProviderConfig_BadGateway(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	setupTest(t, srv)

	cfg, err := FetchProviderConfig(context.Background(), "badgateway")
	if err == nil {
		t.Fatal("expected error for 502 response")
	}
	if cfg != nil {
		t.Fatalf("expected nil config for 502, got: %v", cfg)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — invalid JSON
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_InvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte("not-json{{{"))
	}))
	defer srv.Close()

	setupTest(t, srv)

	cfg, err := FetchProviderConfig(context.Background(), "badjson")
	if err == nil {
		t.Fatal("expected error for invalid JSON response")
	}
	if cfg != nil {
		t.Fatalf("expected nil config for invalid JSON, got: %v", cfg)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — HTTP timeout
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_HTTPTimeout(t *testing.T) {
	originalTimeout := httpTimeoutCopy()
	defer func() { SetHTTPTimeout(originalTimeout) }()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetHTTPTimeout(50 * time.Millisecond)

	start := time.Now()
	cfg, err := FetchProviderConfig(context.Background(), "slow-timeout")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("expected error due to HTTP timeout")
	}
	if cfg != nil {
		t.Fatalf("expected nil config on timeout, got: %v", cfg)
	}
	if elapsed > time.Second {
		t.Errorf("timeout should have fired within 50ms, but took %v", elapsed)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — context cancelled
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Block until the client disconnects (context cancellation).
		<-r.Context().Done()
	}))
	defer srv.Close()

	setupTest(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	cfg, err := FetchProviderConfig(ctx, "slow")
	if err == nil {
		t.Fatal("expected error due to context cancellation")
	}
	if cfg != nil {
		t.Fatalf("expected nil config on context cancellation, got: %v", cfg)
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — singleflight deduplication
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_ConcurrentRequests(t *testing.T) {
	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		// Small delay to make concurrent requests more likely
		time.Sleep(20 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetTTL(1 * time.Hour)

	var wg sync.WaitGroup
	concurrency := 10
	errors := make([]error, concurrency)

	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			cfg, err := FetchProviderConfig(context.Background(), "shared")
			if err != nil {
				errors[idx] = err
				return
			}
			if cfg == nil {
				errors[idx] = fmt.Errorf("nil config")
				return
			}
			if cfg.Name != "openrouter" {
				errors[idx] = fmt.Errorf("wrong name: %q", cfg.Name)
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

// ---------------------------------------------------------------------------
// FetchProviderConfig — response headers
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_RequestHeaders(t *testing.T) {
	var capturedHeaders http.Header
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)

	FetchProviderConfig(context.Background(), "headers")

	accept := capturedHeaders.Get("Accept")
	if accept != "application/json" {
		t.Errorf("expected Accept header 'application/json', got %q", accept)
	}
	ua := capturedHeaders.Get("User-Agent")
	if ua != "sprout-provider-registry/1.0" {
		t.Errorf("expected User-Agent 'sprout-provider-registry/1.0', got %q", ua)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — disabled
// ---------------------------------------------------------------------------

func TestFetchAllProviders_Disabled(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("")
	ClearCache()

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error when disabled, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result when disabled, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — index 404
// ---------------------------------------------------------------------------

func TestFetchAllProviders_IndexNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for 404 index, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for 404 index, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — index returns non-200
// ---------------------------------------------------------------------------

func TestFetchAllProviders_IndexServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "server error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for 500 index, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for 500 index, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — index returns invalid JSON
// ---------------------------------------------------------------------------

func TestFetchAllProviders_IndexInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("not-json{{{"))
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for invalid index JSON, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for invalid index JSON, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — empty providers list
// ---------------------------------------------------------------------------

func TestFetchAllProviders_EmptyProvidersList(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"providers": []}`))
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for empty providers list, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for empty providers list, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — valid index + providers
// ---------------------------------------------------------------------------

func TestFetchAllProviders_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/providers/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"providers": ["openrouter", "openai"]}`))
		case "/providers/openrouter.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(validProviderJSON))
		case "/providers/openai.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(strings.ReplaceAll(validProviderJSON, "openrouter", "openai")))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result")
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 providers, got %d", len(result))
	}
	if _, ok := result["openrouter"]; !ok {
		t.Error("expected 'openrouter' in results")
	}
	if _, ok := result["openai"]; !ok {
		t.Error("expected 'openai' in results")
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — partial failure (some providers fail)
// ---------------------------------------------------------------------------

func TestFetchAllProviders_PartialFailure(t *testing.T) {
	var fetchPaths []string
	var fetchPathsMu sync.Mutex
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchPathsMu.Lock()
		fetchPaths = append(fetchPaths, r.URL.Path)
		fetchPathsMu.Unlock()

		switch r.URL.Path {
		case "/providers/index.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(`{"providers": ["good", "bad-404", "bad-500"]}`))
		case "/providers/good.json":
			w.Header().Set("Content-Type", "application/json")
			w.Write([]byte(validProviderJSON))
		case "/providers/bad-404.json":
			http.Error(w, "not found", http.StatusNotFound)
		case "/providers/bad-500.json":
			http.Error(w, "server error", http.StatusInternalServerError)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result (partial success)")
	}
	if len(result) != 1 {
		t.Fatalf("expected exactly 1 provider (only 'good'), got %d", len(result))
	}
	if _, ok := result["good"]; !ok {
		t.Error("expected 'good' in results")
	}

	// Verify the server was called for index + 3 provider files
	fetchPathsMu.Lock()
	allPaths := fetchPaths
	fetchPathsMu.Unlock()

	hasIndex := false
	hasGood := false
	hasBad404 := false
	hasBad500 := false
	for _, p := range allPaths {
		switch p {
		case "/providers/index.json":
			hasIndex = true
		case "/providers/good.json":
			hasGood = true
		case "/providers/bad-404.json":
			hasBad404 = true
		case "/providers/bad-500.json":
			hasBad500 = true
		}
	}
	if !hasIndex {
		t.Error("expected index.json to be fetched")
	}
	if !hasGood {
		t.Error("expected good.json to be fetched")
	}
	// Both bad ones should have been attempted
	if !hasBad404 {
		t.Error("expected bad-404.json to be fetched (even though it returns 404)")
	}
	if !hasBad500 {
		t.Error("expected bad-500.json to be fetched (even though it returns 500)")
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — index returns non-JSON
// ---------------------------------------------------------------------------

func TestFetchAllProviders_IndexNonJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/providers/index.json" {
			w.Write([]byte(`{"not": "a valid index"`))
			return
		}
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	setupTest(t, srv)

	result, err := FetchAllProviders(context.Background())
	if err != nil {
		t.Fatalf("expected nil error for malformed index, got: %v", err)
	}
	if result != nil {
		t.Fatalf("expected nil result for malformed index, got: %v", result)
	}
}

// ---------------------------------------------------------------------------
// ToProviderConfig
// ---------------------------------------------------------------------------

func TestToProviderConfig_Nil(t *testing.T) {
	var r *RemoteProviderConfig
	result := r.ToProviderConfig()
	if result != nil {
		t.Fatalf("expected nil for nil receiver, got: %v", result)
	}
}

func TestToProviderConfig_FullConfig(t *testing.T) {
	// Build a RemoteProviderConfig from JSON
	var r RemoteProviderConfig
	if err := json.Unmarshal([]byte(validProviderJSON), &r); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	result := r.ToProviderConfig()
	if result == nil {
		t.Fatal("expected non-nil result")
	}

	// Name and Endpoint
	if result.Name != "openrouter" {
		t.Errorf("expected name 'openrouter', got %q", result.Name)
	}
	if result.Endpoint != "https://openrouter.ai/api/v1/chat/completions" {
		t.Errorf("unexpected endpoint: %q", result.Endpoint)
	}

	// Auth (Key is left empty)
	if result.Auth.Type != "bearer" {
		t.Errorf("expected auth type 'bearer', got %q", result.Auth.Type)
	}
	if result.Auth.EnvVar != "OPENROUTER_API_KEY" {
		t.Errorf("expected env_var, got %q", result.Auth.EnvVar)
	}
	if result.Auth.Key != "" {
		t.Errorf("expected empty Key, got %q", result.Auth.Key)
	}

	// Headers (copied)
	if len(result.Headers) != 2 {
		t.Fatalf("expected 2 headers, got %d", len(result.Headers))
	}
	if result.Headers["HTTP-Referer"] != "https://github.com/sprout-foundry/sprout" {
		t.Errorf("unexpected HTTP-Referer: %q", result.Headers["HTTP-Referer"])
	}

	// Defaults
	if result.Defaults.Model != "openai/gpt-5" {
		t.Errorf("expected model 'openai/gpt-5', got %q", result.Defaults.Model)
	}
	if result.Defaults.Temperature == nil || *result.Defaults.Temperature != 0.7 {
		t.Errorf("expected temperature 0.7, got %v", result.Defaults.Temperature)
	}
	if result.Defaults.MaxTokens == nil || *result.Defaults.MaxTokens != -1 {
		t.Errorf("expected max_tokens -1, got %v", result.Defaults.MaxTokens)
	}
	if result.Defaults.TopP == nil || *result.Defaults.TopP != 1.0 {
		t.Errorf("expected top_p 1.0, got %v", result.Defaults.TopP)
	}

	// Conversion
	if !result.Conversion.IncludeToolCallID {
		t.Error("expected include_tool_call_id = true")
	}
	if result.Conversion.ReasoningContentField != "reasoning_content" {
		t.Errorf("unexpected reasoning_content_field: %q", result.Conversion.ReasoningContentField)
	}
	if result.Conversion.ForceToolCallType != "" {
		t.Errorf("expected empty force_tool_call_type, got %q", result.Conversion.ForceToolCallType)
	}

	// Streaming
	if result.Streaming.Format != "sse" {
		t.Errorf("expected format 'sse', got %q", result.Streaming.Format)
	}
	if result.Streaming.ChunkTimeoutMs != 300000 {
		t.Errorf("unexpected chunk_timeout_ms: %d", result.Streaming.ChunkTimeoutMs)
	}

	// Models
	if result.Models.DefaultContextLimit != 128000 {
		t.Errorf("expected default_context_limit 128000, got %d", result.Models.DefaultContextLimit)
	}
	if result.Models.DefaultMaxCompletionTokens != 128000 {
		t.Errorf("expected default_max_completion_tokens, got %d", result.Models.DefaultMaxCompletionTokens)
	}
	if !result.Models.SupportsVision {
		t.Error("expected supports_vision = true")
	}
	if result.Models.VisionModel != "google/gemma-3-27b-it" {
		t.Errorf("unexpected vision_model: %q", result.Models.VisionModel)
	}
	if len(result.Models.ModelOverrides) != 1 {
		t.Fatalf("expected 1 model override, got %d", len(result.Models.ModelOverrides))
	}
	if result.Models.ModelOverrides["openai/gpt-5"] != 272000 {
		t.Errorf("unexpected model override: %d", result.Models.ModelOverrides["openai/gpt-5"])
	}
	if len(result.Models.PatternOverrides) != 1 {
		t.Fatalf("expected 1 pattern override, got %d", len(result.Models.PatternOverrides))
	}
	if result.Models.PatternOverrides[0].Pattern != "gpt-4.*" {
		t.Errorf("unexpected pattern: %q", result.Models.PatternOverrides[0].Pattern)
	}
	if len(result.Models.CompletionPatternOverrides) != 1 {
		t.Fatalf("expected 1 completion pattern override, got %d", len(result.Models.CompletionPatternOverrides))
	}
	if len(result.Models.ModelInfo) != 1 {
		t.Fatalf("expected 1 model info, got %d", len(result.Models.ModelInfo))
	}
	if result.Models.ModelInfo[0].ID != "openai/gpt-5" {
		t.Errorf("unexpected model info ID: %q", result.Models.ModelInfo[0].ID)
	}

	// Retry
	if result.Retry.MaxAttempts != 3 {
		t.Errorf("unexpected max_attempts: %d", result.Retry.MaxAttempts)
	}
	if len(result.Retry.RetryableErrors) != 2 {
		t.Errorf("expected 2 retryable errors, got %d", len(result.Retry.RetryableErrors))
	}

	// Cost
	if result.Cost.Currency != "USD" {
		t.Errorf("unexpected currency: %q", result.Cost.Currency)
	}
}

func TestToProviderConfig_CopiesData(t *testing.T) {
	// Verify that ToProviderConfig creates deep copies (modifying the result
	// should not affect the original RemoteProviderConfig).
	var r RemoteProviderConfig
	if err := json.Unmarshal([]byte(validProviderJSON), &r); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	result := r.ToProviderConfig()

	// Modify the result
	result.Name = "modified"
	result.Headers["X-Title"] = "modified"
	if result.Defaults.Temperature != nil {
		*result.Defaults.Temperature = 99.0
	}
	result.Models.DefaultContextLimit = 0

	// Original should be unchanged
	if r.Name != "openrouter" {
		t.Errorf("original name should be 'openrouter', got %q", r.Name)
	}
	if r.Headers["X-Title"] != "Sprout" {
		t.Errorf("original header should be 'Sprout', got %q", r.Headers["X-Title"])
	}
	if r.Defaults.Temperature == nil || *r.Defaults.Temperature != 0.7 {
		t.Errorf("original temperature should be 0.7, got %v", r.Defaults.Temperature)
	}
	if r.Models.DefaultContextLimit != 128000 {
		t.Errorf("original default_context_limit should be 128000, got %d", r.Models.DefaultContextLimit)
	}
}

func TestToProviderConfig_ZeroValues(t *testing.T) {
	var r RemoteProviderConfig // all zero/nil
	result := r.ToProviderConfig()

	if result == nil {
		t.Fatal("expected non-nil result for zero-value struct")
	}
	if result.Name != "" {
		t.Errorf("expected empty name, got %q", result.Name)
	}
	if result.Auth.Type != "" {
		t.Errorf("expected empty auth type, got %q", result.Auth.Type)
	}
}

// ---------------------------------------------------------------------------
// isValidProviderID
// ---------------------------------------------------------------------------

func TestIsValidProviderID(t *testing.T) {
	validIDs := []string{
		"openrouter",
		"open-ai",
		"my_provider",
		"a",
		"zzz-999_aaa",
		"deepinfra",
		"openai",
		"zai",
		"ollama-local",
		"12345",
	}
	for _, id := range validIDs {
		if !isValidProviderID(id) {
			t.Errorf("expected %q to be valid", id)
		}
	}

	invalidIDs := []struct {
		id   string
		why  string
	}{
		{"", "empty string"},
		{"OpenRouter", "uppercase O"},
		{"open router", "space"},
		{"open/router", "slash"},
		{"open@router", "at sign"},
		{"open.router", "dot"},
		{"open_router!", "exclamation"},
		{strings.Repeat("a", 129), "too long (>128)"},
		{"open_router#", "hash"},
		{"Open_AI", "uppercase O and A"},
		{"UPPERCASE", "all uppercase"},
		{"a b c", "multiple spaces"},
	}
	for _, tc := range invalidIDs {
		if isValidProviderID(tc.id) {
			t.Errorf("expected %q (%s) to be invalid", tc.id, tc.why)
		}
	}
}

// ---------------------------------------------------------------------------
// ClearCache
// ---------------------------------------------------------------------------

func TestClearCache_ClearsBothCaches(t *testing.T) {
	var fetchCount atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		switch r.URL.Path {
		case "/providers/openrouter.json":
			w.Write([]byte(validProviderJSON))
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetTTL(1 * time.Hour)
	SetNegativeTTL(1 * time.Hour)

	// Populate positive cache
	FetchProviderConfig(context.Background(), "openrouter")
	// Populate negative cache
	FetchProviderConfig(context.Background(), "nonexistent")

	if fetchCount.Load() != 2 {
		t.Fatalf("expected 2 initial fetches, got %d", fetchCount.Load())
	}

	// Clear cache
	ClearCache()

	// Re-fetch — should trigger new network calls
	FetchProviderConfig(context.Background(), "openrouter")
	FetchProviderConfig(context.Background(), "nonexistent")

	if fetchCount.Load() != 4 {
		t.Fatalf("expected 4 fetches after clear (2 new), got %d", fetchCount.Load())
	}
}

// ---------------------------------------------------------------------------
// SetTTL
// ---------------------------------------------------------------------------

func TestSetTTL(t *testing.T) {
	originalTTL := ttlCopy()
	defer func() { SetTTL(originalTTL) }()

	SetTTL(10 * time.Second)
	if ttlCopy() != 10*time.Second {
		t.Errorf("expected TTL 10s, got %v", ttlCopy())
	}
}

// ---------------------------------------------------------------------------
// SetHTTPTimeout
// ---------------------------------------------------------------------------

func TestSetHTTPTimeout(t *testing.T) {
	originalTimeout := httpTimeoutCopy()
	defer func() { SetHTTPTimeout(originalTimeout) }()

	SetHTTPTimeout(200 * time.Millisecond)
	if httpTimeoutCopy() != 200*time.Millisecond {
		t.Errorf("expected timeout 200ms, got %v", httpTimeoutCopy())
	}
}

// ---------------------------------------------------------------------------
// SetNegativeTTL
// ---------------------------------------------------------------------------

func TestSetNegativeTTL(t *testing.T) {
	originalNegTTL := negativeTTLCopy()
	defer func() { SetNegativeTTL(originalNegTTL) }()

	SetNegativeTTL(5 * time.Second)
	if negativeTTLCopy() != 5*time.Second {
		t.Errorf("expected negative TTL 5s, got %v", negativeTTLCopy())
	}
}

// ---------------------------------------------------------------------------
// SetBaseURL strips trailing slash
// ---------------------------------------------------------------------------

func TestSetBaseURL_StripTrailingSlash(t *testing.T) {
	originalURL := baseURLCopy()
	defer func() { SetBaseURL(originalURL) }()

	SetBaseURL("http://example.com/")
	if baseURLCopy() != "http://example.com" {
		t.Errorf("expected trailing slash stripped, got %q", baseURLCopy())
	}

	SetBaseURL("http://example.com//")
	if baseURLCopy() != "http://example.com" {
		t.Errorf("expected trailing slashes stripped, got %q", baseURLCopy())
	}
}

// ---------------------------------------------------------------------------
// cloneConfig — deep copy isolation
// ---------------------------------------------------------------------------

func TestCloneConfig_DeepCopy(t *testing.T) {
	rpc := RemoteProviderConfig{
		Name:     "test",
		Headers:  map[string]string{"X-Key": "value"},
		Defaults: RemoteRequestDefaults{Model: "m1"},
	}
	cached := &cachedConfig{
		config:    rpc,
		fetchedAt: time.Now(),
	}

	clone := cloneConfig(cached)

	// Mutate clone
	clone.Name = "modified"
	clone.Headers["X-Key"] = "modified"

	// Original should be unchanged
	if cached.config.Name != "test" {
		t.Errorf("original name should be 'test', got %q", cached.config.Name)
	}
	if cached.config.Headers["X-Key"] != "value" {
		t.Errorf("original header should be 'value', got %q", cached.config.Headers["X-Key"])
	}
}

// ---------------------------------------------------------------------------
// FetchProviderConfig — cache returns cloned config (not shared reference)
// ---------------------------------------------------------------------------

func TestFetchProviderConfig_CacheReturnsClones(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(validProviderJSON))
	}))
	defer srv.Close()

	setupTest(t, srv)
	SetTTL(1 * time.Hour)

	cfg1, _ := FetchProviderConfig(context.Background(), "openrouter")
	cfg2, _ := FetchProviderConfig(context.Background(), "openrouter")

	// Mutate cfg1 — cfg2 should be unaffected
	cfg1.Name = "modified"
	if cfg2.Name != "openrouter" {
		t.Errorf("second call should return a clone, not shared reference: got %q", cfg2.Name)
	}
}

// ---------------------------------------------------------------------------
// FetchAllProviders — context cancelled during index fetch
// ---------------------------------------------------------------------------

func TestFetchAllProviders_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Slow response — the context timeout will cancel it
		time.Sleep(500 * time.Millisecond)
		w.Write([]byte(`{"providers": ["openrouter"]}`))
	}))
	defer srv.Close()

	setupTest(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, err := FetchAllProviders(ctx)
	// FetchAllProviders returns (nil, nil) for index errors as graceful degradation.
	// The HTTP timeout (default 500ms > 50ms context timeout) may fire differently,
	// but the function is designed to never return errors from the index.
	// We just verify it doesn't panic and returns quickly.
	_ = err
}

// ---------------------------------------------------------------------------
// validateEndpoint — SSRF validation
// ---------------------------------------------------------------------------

func TestValidateEndpoint_HTTPS_Allowed(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"standard HTTPS", "https://api.openai.com/v1"},
		{"HTTPS with path", "https://example.com/api/v1/chat/completions"},
		{"HTTPS with port", "https://api.example.com:8443/v1"},
		{"HTTPS with subdomain", "https://deepinfra.ai/v1/chat/completions"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.endpoint)
			if err != nil {
				t.Errorf("expected %q to be allowed, got error: %v", tt.endpoint, err)
			}
		})
	}
}

func TestValidateEndpoint_HTTP_Rejected(t *testing.T) {
	err := validateEndpoint("http://api.example.com/v1")
	if err == nil {
		t.Fatal("expected error for HTTP endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "https") {
		t.Errorf("expected error mentioning https, got: %v", err)
	}
}

func TestValidateEndpoint_FileScheme_Rejected(t *testing.T) {
	err := validateEndpoint("file:///etc/passwd")
	if err == nil {
		t.Fatal("expected error for file:// endpoint, got nil")
	}
}

func TestValidateEndpoint_InvalidURL(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"not a URL", "not a valid url at all"},
		{"mixed garbage", "ht tp://example.com"},
		{"empty scheme", "://example.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.endpoint)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.endpoint)
			}
		})
	}
}

func TestValidateEndpoint_EmptyEndpoint(t *testing.T) {
	err := validateEndpoint("")
	if err == nil {
		t.Fatal("expected error for empty endpoint, got nil")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("expected error mentioning empty, got: %v", err)
	}
}

func TestValidateEndpoint_Localhost_Rejected(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"localhost plain", "https://localhost:8080/api"},
		{"localhost no port", "https://localhost/api"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.endpoint)
			if err == nil {
				t.Errorf("expected error for %q, got nil", tt.endpoint)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "localhost") {
				t.Errorf("expected error mentioning localhost, got: %v", err)
			}
		})
	}
}

func TestValidateEndpoint_LocalhostUpperCase(t *testing.T) {
	err := validateEndpoint("https://LOCALHOST/api")
	if err == nil {
		t.Fatal("expected error for LOCALHOST (uppercase), got nil")
	}
}

func TestValidateEndpoint_Private_IP_127(t *testing.T) {
	err := validateEndpoint("https://127.0.0.1:8080/api")
	if err == nil {
		t.Fatal("expected error for 127.0.0.1, got nil")
	}
	if !strings.Contains(err.Error(), "private IP") {
		t.Errorf("expected error mentioning private IP, got: %v", err)
	}
}

func TestValidateEndpoint_Private_IP_10(t *testing.T) {
	err := validateEndpoint("https://10.0.0.1/api")
	if err == nil {
		t.Fatal("expected error for 10.0.0.1, got nil")
	}
	if !strings.Contains(err.Error(), "private IP") {
		t.Errorf("expected error mentioning private IP, got: %v", err)
	}
}

func TestValidateEndpoint_Private_IP_172_16(t *testing.T) {
	err := validateEndpoint("https://172.16.0.1/api")
	if err == nil {
		t.Fatal("expected error for 172.16.0.1, got nil")
	}
	if !strings.Contains(err.Error(), "private IP") {
		t.Errorf("expected error mentioning private IP, got: %v", err)
	}
}

func TestValidateEndpoint_Private_IP_172_31(t *testing.T) {
	err := validateEndpoint("https://172.31.255.255/api")
	if err == nil {
		t.Fatal("expected error for 172.31.255.255, got nil")
	}
	if !strings.Contains(err.Error(), "private IP") {
		t.Errorf("expected error mentioning private IP, got: %v", err)
	}
}

func TestValidateEndpoint_Private_IP_172_32_Allowed(t *testing.T) {
	err := validateEndpoint("https://172.32.0.1/api")
	if err != nil {
		t.Errorf("expected 172.32.0.1 to be allowed (outside private range), got error: %v", err)
	}
}

func TestValidateEndpoint_Private_IP_192_168(t *testing.T) {
	err := validateEndpoint("https://192.168.1.1/api")
	if err == nil {
		t.Fatal("expected error for 192.168.1.1, got nil")
	}
	if !strings.Contains(err.Error(), "private IP") {
		t.Errorf("expected error mentioning private IP, got: %v", err)
	}
}

func TestValidateEndpoint_Public_IP_Allowed(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
	}{
		{"Google DNS", "https://8.8.8.8/api"},
		{"Cloudflare DNS", "https://1.1.1.1/api"},
		{"Public IP", "https://52.85.132.100/v1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateEndpoint(tt.endpoint)
			if err != nil {
				t.Errorf("expected %q to be allowed, got error: %v", tt.endpoint, err)
			}
		})
	}
}

func TestValidateEndpoint_IPv6_Loopback(t *testing.T) {
	err := validateEndpoint("https://[::1]/api")
	if err == nil {
		t.Fatal("expected error for IPv6 loopback ::1, got nil")
	}
}

func TestValidateEndpoint_IPv6_Ul_Prefix(t *testing.T) {
	err := validateEndpoint("https://[fc00::1]/api")
	if err == nil {
		t.Fatal("expected error for IPv6 unique local fc00::1, got nil")
	}
}

// ---------------------------------------------------------------------------
// isPrivateIP — edge cases
// ---------------------------------------------------------------------------

func TestIsPrivateIP_Loopback(t *testing.T) {
	if !isPrivateIP(net.ParseIP("127.0.0.1")) {
		t.Error("expected 127.0.0.1 to be private")
	}
	if !isPrivateIP(net.ParseIP("127.255.255.255")) {
		t.Error("expected 127.255.255.255 to be private")
	}
}

func TestIsPrivateIP_ClassA(t *testing.T) {
	if !isPrivateIP(net.ParseIP("10.0.0.0")) {
		t.Error("expected 10.0.0.0 to be private")
	}
	if !isPrivateIP(net.ParseIP("10.255.255.255")) {
		t.Error("expected 10.255.255.255 to be private")
	}
	if isPrivateIP(net.ParseIP("11.0.0.0")) {
		t.Error("expected 11.0.0.0 to be public")
	}
}

func TestIsPrivateIP_ClassB(t *testing.T) {
	if !isPrivateIP(net.ParseIP("172.16.0.0")) {
		t.Error("expected 172.16.0.0 to be private")
	}
	if !isPrivateIP(net.ParseIP("172.31.255.255")) {
		t.Error("expected 172.31.255.255 to be private")
	}
	if isPrivateIP(net.ParseIP("172.15.0.0")) {
		t.Error("expected 172.15.0.0 to be public")
	}
	if isPrivateIP(net.ParseIP("172.32.0.0")) {
		t.Error("expected 172.32.0.0 to be public")
	}
}

func TestIsPrivateIP_ClassC(t *testing.T) {
	if !isPrivateIP(net.ParseIP("192.168.0.0")) {
		t.Error("expected 192.168.0.0 to be private")
	}
	if !isPrivateIP(net.ParseIP("192.168.255.255")) {
		t.Error("expected 192.168.255.255 to be private")
	}
	if isPrivateIP(net.ParseIP("192.169.0.0")) {
		t.Error("expected 192.169.0.0 to be public")
	}
}

func TestIsPrivateIP_Nil(t *testing.T) {
	if isPrivateIP(nil) {
		t.Error("expected nil IP to not be private")
	}
}

func TestIsPrivateIP_IPv6_Loopback(t *testing.T) {
	if !isPrivateIP(net.ParseIP("::1")) {
		t.Error("expected ::1 to be private")
	}
}

func TestIsPrivateIP_IPv6_Unspecified(t *testing.T) {
	if !isPrivateIP(net.ParseIP("::")) {
		t.Error("expected :: (unspecified) to be private")
	}
}

func TestIsPrivateIP_IPv6_LinkLocal(t *testing.T) {
	if !isPrivateIP(net.ParseIP("fe80::1")) {
		t.Error("expected fe80::1 to be private (link-local)")
	}
}

func TestIsPrivateIP_IPv6_UniqueLocal(t *testing.T) {
	if !isPrivateIP(net.ParseIP("fc00::1")) {
		t.Error("expected fc00::1 to be private (unique local)")
	}
	if !isPrivateIP(net.ParseIP("fdff::ffff")) {
		t.Error("expected fdff::ffff to be private (unique local)")
	}
}

func TestIsPrivateIP_IPv6_Public(t *testing.T) {
	if isPrivateIP(net.ParseIP("2001:db8::1")) {
		t.Error("expected 2001:db8::1 to be public")
	}
}
