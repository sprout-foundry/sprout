package providers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

// countingChatServer serves an httptest server that records every request,
// so a test can assert what CheckConnection actually sent. /models is
// handled only when `serveModels` is true; otherwise the path 404s.
type countingChatServer struct {
	server        *httptest.Server
	models        atomic.Int32
	chats         atomic.Int32
	lastMaxTokens atomic.Int32 // set to -1 when no chat request happened
	lastThinking  atomic.Value // stores a string (sub-key presence)
}

func newCountingChatServer(t *testing.T, serveModels bool) *countingChatServer {
	t.Helper()
	s := &countingChatServer{}
	s.lastMaxTokens.Store(-1)

	s.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/models":
			s.models.Add(1)
			if !serveModels {
				http.Error(w, "not found", http.StatusNotFound)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"data":[{"id":"qwen3-coder:30b","object":"model","created":1234567890,"owned_by":"test"}]}`)
		case r.Method == http.MethodPost && r.URL.Path == "/":
			s.chats.Add(1)
			var body map[string]interface{}
			if err := json.NewDecoder(r.Body).Decode(&body); err == nil {
				if v, ok := body["max_tokens"].(float64); ok {
					s.lastMaxTokens.Store(int32(v))
				} else {
					s.lastMaxTokens.Store(-2)
				}
				if thinking, ok := body["thinking"].(map[string]interface{}); ok {
					if typ, _ := thinking["type"].(string); typ != "" {
						s.lastThinking.Store(typ)
					}
				}
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1234567890,"model":"qwen3-coder:30b","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
		default:
			// /get_model_info, /props, /v1/props and anything else:
			// no model list endpoint exists for this backend.
			http.Error(w, "not found", http.StatusNotFound)
		}
	}))
	t.Cleanup(s.server.Close)
	return s
}

// newConnectionTestProvider builds a provider whose endpoint is the given
// httptest server (loopback), with a model that is not in the model catalog
// so the request body is not rewritten by model-specific settings.
func newConnectionTestProvider(t *testing.T, endpoint, model string) *GenericProvider {
	t.Helper()
	config := &ProviderConfig{
		Name:     "test-local",
		Endpoint: endpoint,
		Defaults: RequestDefaults{
			Model: model,
		},
		Models: ModelConfig{
			DefaultModel:        model,
			DefaultContextLimit: 4096,
			AvailableModels:     []string{model},
		},
		Auth: AuthConfig{
			Type:   "bearer",
			EnvVar: "",
		},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return provider
}

func TestCheckConnectionPrefersModelList(t *testing.T) {
	server := newCountingChatServer(t, true)
	provider := newConnectionTestProvider(t, server.server.URL, "qwen3-coder:30b")

	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("CheckConnection failed: %v", err)
	}
	if got := server.chats.Load(); got != 0 {
		t.Fatalf("expected no chat generation, got %d requests", got)
	}
	if got := server.models.Load(); got < 1 {
		t.Fatalf("expected the /models endpoint to be queried, got %d", got)
	}

	// A repeated check inside the TTL must reuse the cached success and not
	// query the server again.
	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("second CheckConnection failed: %v", err)
	}
	if got := server.models.Load(); got > 1 {
		t.Fatalf("expected at most one /models query inside the TTL, got %d", got)
	}
	if got := server.chats.Load(); got != 0 {
		t.Fatalf("expected no chat generation, got %d requests", got)
	}
}

func TestCheckConnectionFallbackChatProbeIsBounded(t *testing.T) {
	server := newCountingChatServer(t, false)
	provider := newConnectionTestProvider(t, server.server.URL, "deepseek-chat")

	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("CheckConnection failed: %v", err)
	}
	if got := server.chats.Load(); got != 1 {
		t.Fatalf("expected exactly one bounded chat probe, got %d requests", got)
	}
	if got := server.lastMaxTokens.Load(); got > 1 {
		t.Fatalf("expected max_tokens <= 1, got %d", got)
	}
	if got := server.lastThinking.Load(); got != "disabled" {
		t.Fatalf("expected thinking disabled, got %v", got)
	}

	// A repeated check inside the TTL must not re-probe.
	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("second CheckConnection failed: %v", err)
	}
	if got := server.chats.Load(); got != 1 {
		t.Fatalf("expected the probe to be reused inside the TTL, got %d requests", got)
	}
}
