package providers

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// assistantChatResponse is a minimal valid chat completion body the check can reuse.
const assistantChatResponse = `{"id":"resp","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"estimated_cost":0}}`

// connCheckTestServer spins up an httptest server that serves /v1/models
// (when modelsOK is true) and /v1/chat/completions (always). It records how
// many times the chat endpoint was hit, the last max_tokens seen, and
// whether the last request carried a thinking-disable knob.
type connCheckTestServer struct {
	*httptest.Server
	chatCalls      *atomic.Int32
	modelsCalls    *atomic.Int32
	maxTokens      *atomic.Int32
	sawThinkingOff *atomic.Bool
}

func newConnCheckTestServer(modelsOK *atomic.Bool) *connCheckTestServer {
	s := &connCheckTestServer{
		chatCalls:      &atomic.Int32{},
		modelsCalls:    &atomic.Int32{},
		maxTokens:      &atomic.Int32{},
		sawThinkingOff: &atomic.Bool{},
	}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			s.modelsCalls.Add(1)
			if modelsOK.Load() {
				_, _ = w.Write([]byte(`{"data":[{"id":"test-model"}]}`))
				return
			}
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"error":"no models"}`))
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			s.chatCalls.Add(1)
			body, _ := io.ReadAll(r.Body)
			var req map[string]any
			if err := json.Unmarshal(body, &req); err == nil {
				if mt, ok := req["max_tokens"]; ok {
					if f, ok := mt.(float64); ok {
						s.maxTokens.Store(int32(f))
					}
				}
				// Thinking disabled via any of the knobs the provider applies.
				if _, ok := req["thinking"]; ok {
					s.sawThinkingOff.Store(true)
				}
				if tg, ok := req["chat_template_kwargs"]; ok {
					if tgMap, ok := tg.(map[string]any); ok {
						if _, ok := tgMap["enable_thinking"]; ok {
							s.sawThinkingOff.Store(true)
						}
					}
				}
				if r, ok := req["reasoning"]; ok {
					if rMap, ok := r.(map[string]any); ok {
						if _, ok := rMap["effort"]; ok {
							s.sawThinkingOff.Store(true)
						}
					}
				}
			}
			_, _ = w.Write([]byte(assistantChatResponse))
		default:
			http.NotFound(w, r)
		}
	}))
	return s
}

func newConnCheckTestProvider(t *testing.T, server *httptest.Server) *GenericProvider {
	t.Helper()
	config := &ProviderConfig{
		Name:     "conn-check-test",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	return provider
}

func TestCheckConnectionPrefersModelsEndpoint(t *testing.T) {
	ResetConnCheckCacheForTests()
	t.Cleanup(ResetConnCheckCacheForTests)

	var modelsOK atomic.Bool
	modelsOK.Store(true)
	server := newConnCheckTestServer(&modelsOK)
	defer server.Close()

	provider := newConnCheckTestProvider(t, server.Server)
	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("expected connection check to succeed, got %v", err)
	}
	if server.chatCalls.Load() != 0 {
		t.Fatalf("expected no chat request, got %d chat calls", server.chatCalls.Load())
	}
}

func TestCheckConnectionBoundedChatWhenModelsFails(t *testing.T) {
	ResetConnCheckCacheForTests()
	t.Cleanup(ResetConnCheckCacheForTests)

	var modelsOK atomic.Bool
	modelsOK.Store(false)
	server := newConnCheckTestServer(&modelsOK)
	defer server.Close()

	provider := newConnCheckTestProvider(t, server.Server)
	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("expected connection check to succeed, got %v", err)
	}
	if server.chatCalls.Load() != 1 {
		t.Fatalf("expected exactly one chat request, got %d", server.chatCalls.Load())
	}
	if got := server.maxTokens.Load(); got != 1 {
		t.Fatalf("max_tokens = %d, want 1 (bounded check)", got)
	}
}

func TestCheckConnectionCachedAcrossAgentCreations(t *testing.T) {
	ResetConnCheckCacheForTests()
	t.Cleanup(ResetConnCheckCacheForTests)

	var modelsOK atomic.Bool
	modelsOK.Store(true)
	server := newConnCheckTestServer(&modelsOK)
	defer server.Close()

	// First agent: a fresh provider instance (as agent creation does).
	p1 := newConnCheckTestProvider(t, server.Server)
	if err := p1.CheckConnection(); err != nil {
		t.Fatalf("expected connection check to succeed, got %v", err)
	}
	// Second agent: same provider identity, different instance — must not re-probe.
	p2 := newConnCheckTestProvider(t, server.Server)
	if err := p2.CheckConnection(); err != nil {
		t.Fatalf("expected cached connection check to succeed, got %v", err)
	}
	if server.chatCalls.Load() != 0 {
		t.Fatalf("expected no chat request, got %d", server.chatCalls.Load())
	}
	// The models probe ran once — not once per agent creation.
	if got := server.modelsCalls.Load(); got != 1 {
		t.Fatalf("models probe calls = %d, want 1 (cached across agent creations)", got)
	}
}

func TestCheckConnectionBoundedChatWhenModelsEndpointNotSupported(t *testing.T) {
	ResetConnCheckCacheForTests()
	t.Cleanup(ResetConnCheckCacheForTests)

	server := newConnCheckTestServer(&atomic.Bool{})
	defer server.Close()

	// Config explicitly states no discovery — straight to the bounded chat check.
	config := &ProviderConfig{
		Name:     "conn-check-test",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000, DisableDiscovery: true},
	}
	provider, err := NewGenericProvider(config)
	if err != nil {
		t.Fatalf("failed to create provider: %v", err)
	}
	if err := provider.CheckConnection(); err != nil {
		t.Fatalf("expected connection check to succeed, got %v", err)
	}
	if server.chatCalls.Load() != 1 {
		t.Fatalf("expected exactly one chat request, got %d", server.chatCalls.Load())
	}
	if got := server.maxTokens.Load(); got != 1 {
		t.Fatalf("max_tokens = %d, want 1 (bounded check)", got)
	}
}

func TestCheckConnectionBoundedChatReportsError(t *testing.T) {
	ResetConnCheckCacheForTests()
	t.Cleanup(ResetConnCheckCacheForTests)

	// Server always returns 500 for chat.
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"boom"}`))
	}))
	defer server.Close()

	provider := newConnCheckTestProvider(t, server)
	err := provider.CheckConnection()
	if err == nil {
		t.Fatal("expected connection check to fail when the provider errors")
	}
	if !strings.Contains(err.Error(), "check connection") {
		t.Fatalf("expected a check-connection error, got: %v", err)
	}
}
