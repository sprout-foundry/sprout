//go:build !js

package webui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/events"
)

// newSlowProviderTestServer builds a daemon-shaped server whose config
// selects an OpenAI-compatible custom provider ("slow-bridge") whose
// endpoint never answers in time, plus a second custom provider that
// disables model discovery. Every live model lookup hangs. It returns the
// server and the number of requests the provider endpoint received.
func newSlowProviderTestServer(t *testing.T) (*ReactWebServer, *atomic.Int32, *atomic.Int32) {
	t.Helper()

	var endpointHits atomic.Int32
	release := make(chan struct{})
	endpoint := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		endpointHits.Add(1)
		select {
		case <-release:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	t.Cleanup(endpoint.Close)
	t.Cleanup(func() { close(release) })

	// Isolate every config location: loading custom providers also syncs
	// them into the default (XDG / home) providers directory.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	cfgDir := t.TempDir()
	t.Setenv("SPROUT_CONFIG", cfgDir)
	cfg := fmt.Sprintf(`{
  "last_used_provider": "slow-bridge",
  "custom_providers": {
    "slow-bridge": {"name": "slow-bridge", "endpoint": %q, "model_name": "managed", "context_size": 163840},
    "pinned-bridge": {"name": "pinned-bridge", "endpoint": %q, "model_name": "pinned-model", "context_size": 163840, "disable_model_discovery": true}
  }
}`, endpoint.URL+"/v1", endpoint.URL+"/v1")
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(cfg), 0600); err != nil {
		t.Fatal(err)
	}

	ws, err := NewReactWebServer(nil, events.NewEventBus(), 0, "127.0.0.1", "", "")
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	ws.daemonRoot = root
	ws.workspaceRoot = root

	var pinnedLookups atomic.Int32
	ws.providerModels.fetch = func(ctx context.Context, pt api.ClientType) ([]api.ModelInfo, error) {
		if pt == "pinned-bridge" {
			pinnedLookups.Add(1)
		}
		select {
		case <-release:
		case <-ctx.Done():
		}
		return nil, fmt.Errorf("lookup for %s timed out", pt)
	}
	return ws, &endpointHits, &pinnedLookups
}

// TestProvidersAPI_SlowDiscoveryDoesNotBlock pins that the provider listing
// answers promptly when every live model lookup hangs: catalog providers
// are served from the embedded catalog, the custom provider falls back to
// its configured model after at most providerListWait, and a provider with
// disable_model_discovery is never looked up at all.
func TestProvidersAPI_SlowDiscoveryDoesNotBlock(t *testing.T) {
	ws, _, pinnedLookups := newSlowProviderTestServer(t)
	mux := ws.setupRoutes(context.Background())

	start := time.Now()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	elapsed := time.Since(start)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", rec.Code, rec.Body.String())
	}
	if limit := providerListWait + time.Second; elapsed > limit {
		t.Fatalf("/api/providers took %v with hanging discovery; want under %v", elapsed, limit)
	}

	var body struct {
		Providers       []providerDescriptor `json:"providers"`
		CurrentProvider string               `json:"current_provider"`
		CurrentModel    string               `json:"current_model"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	byID := map[string][]string{}
	for _, p := range body.Providers {
		byID[p.ID] = p.Models
	}
	if got := byID["pinned-bridge"]; len(got) != 1 || got[0] != "pinned-model" {
		t.Errorf("pinned-bridge models = %v, want [pinned-model]", got)
	}
	if got := byID["slow-bridge"]; len(got) != 1 || got[0] != "managed" {
		t.Errorf("slow-bridge models = %v, want the configured [managed]", got)
	}
	if got := byID["openai"]; len(got) == 0 {
		t.Errorf("openai models empty; want the embedded catalog while discovery hangs")
	}
	if n := pinnedLookups.Load(); n != 0 {
		t.Errorf("pinned-bridge was looked up %d time(s) despite disable_model_discovery", n)
	}
	if body.CurrentProvider != "slow-bridge" || body.CurrentModel != "managed" {
		t.Errorf("current = %s/%s, want slow-bridge/managed", body.CurrentProvider, body.CurrentModel)
	}

	// The second listing is served from the cache: nothing waits.
	start = time.Now()
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/providers", nil))
	if elapsed := time.Since(start); elapsed > providerListWait/2 {
		t.Fatalf("second /api/providers took %v; in-flight lookups must not be waited on again past the first budget", elapsed)
	}
}

// TestPageLoadEndpoints_DoNotBuildAgent pins that the read-only endpoints a
// page load fires together answer without building an agent. A build runs
// the provider connection check against the model endpoint, which here
// never answers in time.
func TestPageLoadEndpoints_DoNotBuildAgent(t *testing.T) {
	ws, endpointHits, _ := newSlowProviderTestServer(t)
	builds := countAgentBuilds(t, 0)
	mux := ws.setupRoutes(context.Background())

	paths := []string{"/api/stats", "/api/changes/session", "/api/changes/summary", "/api/providers"}
	var wg sync.WaitGroup
	durations := make([]time.Duration, len(paths))
	codes := make([]int, len(paths))
	for i, p := range paths {
		wg.Add(1)
		go func(i int, p string) {
			defer wg.Done()
			start := time.Now()
			rec := httptest.NewRecorder()
			mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, p, nil))
			durations[i] = time.Since(start)
			codes[i] = rec.Code
		}(i, p)
	}
	wg.Wait()

	for i, p := range paths {
		if codes[i] != http.StatusOK {
			t.Errorf("%s status = %d", p, codes[i])
		}
		if limit := providerListWait + time.Second; durations[i] > limit {
			t.Errorf("%s took %v; want under %v", p, durations[i], limit)
		}
	}
	if n := builds.Load(); n != 0 {
		t.Errorf("read-only page-load endpoints started %d agent build(s), want 0", n)
	}
	if n := endpointHits.Load(); n != 0 {
		t.Errorf("model endpoint received %d request(s); a read-only endpoint built an agent", n)
	}
}

// countAgentBuilds stubs buildWebUIAgent with a build that takes delay and
// fails, and returns the number of builds started.
func countAgentBuilds(t *testing.T, delay time.Duration) *atomic.Int32 {
	t.Helper()
	var builds atomic.Int32
	orig := buildWebUIAgent
	buildWebUIAgent = func(configBase, workspaceDir, workspaceRoot, model string) (*agent.Agent, error) {
		builds.Add(1)
		time.Sleep(delay)
		return nil, errors.New("agent builds are stubbed in this test")
	}
	t.Cleanup(func() { buildWebUIAgent = orig })
	return &builds
}

// TestGetClientAgent_ConcurrentCallersShareOneBuild pins that concurrent
// requests from one client share a single agent build instead of each
// building (and running the provider connection check for) its own.
func TestGetClientAgent_ConcurrentCallersShareOneBuild(t *testing.T) {
	ws, _, _ := newSlowProviderTestServer(t)
	builds := countAgentBuilds(t, 100*time.Millisecond)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 6; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, _ = ws.getClientAgent("tab-a")
		}()
	}
	close(start)
	wg.Wait()

	if n := builds.Load(); n != 1 {
		t.Fatalf("agent builds for 6 concurrent callers = %d, want 1", n)
	}
}
