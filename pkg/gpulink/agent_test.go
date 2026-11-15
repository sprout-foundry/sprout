package gpulink

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/sprout-foundry/sprout/pkg/runner/relay"
)

// fakeEngine records what reached it and answers like an OpenAI-compatible
// engine.
func fakeEngine(t *testing.T) (*httptest.Server, *[]string, *string) {
	t.Helper()
	var paths []string
	var auth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		auth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"data":[{"id":"llama3.1:8b"}]}`)
	}))
	t.Cleanup(srv.Close)
	return srv, &paths, &auth
}

func TestHandler_ServesModelPathsOnlyAndSwapsTheKey(t *testing.T) {
	engine, paths, auth := fakeEngine(t)
	a := &Agent{State: &State{Engine: strings.TrimPrefix(engine.URL, "http://")}, EngineKey: "engine-secret"}
	h := a.Handler()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/models", nil)
	req.Header.Set("Authorization", "Bearer platform-key")
	h("", rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "llama3.1:8b") {
		t.Fatalf("models = %d %s", rec.Code, rec.Body.String())
	}
	if *auth != "Bearer engine-secret" {
		t.Errorf("engine saw Authorization %q, want the engine's own key", *auth)
	}

	for _, p := range []string{"/api/pull", "/api/delete", "/", "/v1/../api/tags"} {
		rec = httptest.NewRecorder()
		h("", rec, httptest.NewRequest(http.MethodPost, p, nil))
		if rec.Code != http.StatusNotFound {
			t.Errorf("%s: %d, want 404 (not a model-serving path)", p, rec.Code)
		}
	}
	if len(*paths) != 1 {
		t.Errorf("engine reached %v, want only /v1/models", *paths)
	}
}

func TestHandler_EngineDownIsABadGateway(t *testing.T) {
	a := &Agent{State: &State{Engine: "127.0.0.1:1"}}
	rec := httptest.NewRecorder()
	a.Handler()("", rec, httptest.NewRequest(http.MethodGet, "/v1/models", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}

// fakePlatform accepts the agent's tunnel when the key matches and hands the
// platform side of the relay to the test.
func fakePlatform(t *testing.T, key string) (*httptest.Server, <-chan *relay.Mux) {
	t.Helper()
	muxes := make(chan *relay.Mux, 1)
	up := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/internal/gpu/tunnel" || r.URL.Query().Get("link_id") != "link-1" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("X-GPU-Key") != key {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		conn, err := up.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		m := relay.NewMux(conn, nil)
		muxes <- m
		_ = m.Serve(r.Context())
	}))
	t.Cleanup(srv.Close)
	return srv, muxes
}

func TestRun_RelaysThePlatformsRequestToTheEngine(t *testing.T) {
	engine, _, _ := fakeEngine(t)
	platform, muxes := fakePlatform(t, "sfg_key")
	a := &Agent{State: &State{PlatformURL: platform.URL, LinkID: "link-1", Engine: strings.TrimPrefix(engine.URL, "http://")}, APIKey: "sfg_key"}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- a.Run(ctx) }()

	var m *relay.Mux
	select {
	case m = <-muxes:
	case <-ctx.Done():
		t.Fatal("the agent never connected")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://gpu-link/v1/models", nil)
	resp, err := m.RoundTripper("link-1").RoundTrip(req)
	if err != nil {
		t.Fatalf("round trip: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK || !strings.Contains(string(body), "llama3.1:8b") {
		t.Fatalf("relayed = %d %s", resp.StatusCode, body)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v after cancel, want nil", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after cancel")
	}
}

func TestRun_StopsWhenTheKeyIsRefused(t *testing.T) {
	platform, _ := fakePlatform(t, "sfg_right")
	a := &Agent{State: &State{PlatformURL: platform.URL, LinkID: "link-1", Engine: DefaultEngine}, APIKey: "sfg_wrong"}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Run(ctx); !errors.Is(err, ErrLinkRemoved) {
		t.Fatalf("Run = %v, want ErrLinkRemoved", err)
	}
}

func TestTunnelURL(t *testing.T) {
	got, err := tunnelURL("https://app.example.com/", "abc")
	if err != nil || got != "wss://app.example.com/internal/gpu/tunnel?link_id=abc" {
		t.Fatalf("tunnelURL = %q, %v", got, err)
	}
}
