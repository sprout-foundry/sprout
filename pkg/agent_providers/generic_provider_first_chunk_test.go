package providers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// newStreamingTestProvider builds a GenericProvider against a local
// httptest server with the given streaming timeouts, mirroring the
// pattern in backend_detect_test.go (local endpoint → no auth needed).
func newStreamingTestProvider(t *testing.T, handler http.Handler, firstChunkMs, chunkMs int) (*GenericProvider, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	cfg := &ProviderConfig{
		Name:     "test",
		Endpoint: server.URL + "/v1/chat/completions",
		Auth:     AuthConfig{Type: "none"},
		Defaults: RequestDefaults{Model: "test-model"},
		Models:   ModelConfig{DefaultContextLimit: 64000},
		Streaming: StreamingConfig{
			Format:              "sse",
			FirstChunkTimeoutMs: firstChunkMs,
			ChunkTimeoutMs:      chunkMs,
		},
	}
	p, err := NewGenericProvider(cfg)
	if err != nil {
		t.Fatalf("NewGenericProvider: %v", err)
	}
	return p, server
}

// TestStreamingFirstTokenTimeout verifies the pre-first-chunk window uses
// the first-chunk deadline (not the 120s inter-chunk default) and returns
// a distinguishable error message.
func TestStreamingFirstTokenTimeout(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Send nothing — model still prefilling.
		<-r.Context().Done()
	})
	p, _ := newStreamingTestProvider(t, handler, 300, 0)

	start := time.Now()
	_, err := p.SendChatRequestStream(context.Background(), nil, nil, "", false, nil)
	elapsed := time.Since(start)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "first-token timeout") {
		t.Errorf("error should mention first-token timeout, got: %v", err)
	}
	if elapsed > 5*time.Second {
		t.Errorf("first-token timeout fired too late: %v", elapsed)
	}
}

// TestStreamingIdleTimeoutAfterFirstChunk verifies the inter-chunk idle
// deadline applies after the first chunk arrives and produces the idle
// (not first-token) error message.
func TestStreamingIdleTimeoutAfterFirstChunk(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// One chunk, then stall until the request dies.
		_, _ = w.Write([]byte("data: {\"choices\":[{\"delta\":{\"content\":\"a\"}}]}\n\n"))
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	})
	// IdleChunkTimeoutMs (not ChunkTimeoutMs) so the HTTP client keeps its
	// 15m default and the idle select is what fires.
	p, _ := newStreamingTestProvider(t, handler, 5000, 0)
	p.config.Streaming.IdleChunkTimeoutMs = 300

	_, err := p.SendChatRequestStream(context.Background(), nil, nil, "", false, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "idle timeout") {
		t.Errorf("error should mention idle timeout, got: %v", err)
	}
	if strings.Contains(err.Error(), "first-token") {
		t.Errorf("post-first-chunk stall must not report first-token timeout, got: %v", err)
	}
}

// TestFirstChunkTimeoutDefaults verifies config resolution: default 10m,
// override honored, and override capped at the streaming HTTP timeout.
func TestFirstChunkTimeoutDefaults(t *testing.T) {
	c := &ProviderConfig{}
	if got := c.GetFirstChunkTimeout(); got != 10*time.Minute {
		t.Errorf("default first-chunk timeout = %v, want 10m", got)
	}
	if got := c.GetIdleChunkTimeout(); got != 120*time.Second {
		t.Errorf("default idle-chunk timeout = %v, want 120s", got)
	}

	c.Streaming.FirstChunkTimeoutMs = 30000
	if got := c.GetFirstChunkTimeout(); got != 30*time.Second {
		t.Errorf("override first-chunk timeout = %v, want 30s", got)
	}

	// Override beyond the streaming HTTP timeout is capped by it.
	c.Streaming.ChunkTimeoutMs = 20000
	if got := c.GetFirstChunkTimeout(); got != 20*time.Second {
		t.Errorf("capped first-chunk timeout = %v, want 20s (streaming cap)", got)
	}
	// ChunkTimeoutMs also overrides the idle deadline (legacy behavior)…
	if got := c.GetIdleChunkTimeout(); got != 20*time.Second {
		t.Errorf("chunk-timems idle override = %v, want 20s", got)
	}
	// …but IdleChunkTimeoutMs takes precedence over ChunkTimeoutMs.
	c.Streaming.IdleChunkTimeoutMs = 25000
	if got := c.GetIdleChunkTimeout(); got != 25*time.Second {
		t.Errorf("idle-chunk override = %v, want 25s", got)
	}
}
