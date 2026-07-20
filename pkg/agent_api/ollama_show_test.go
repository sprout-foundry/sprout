// Package agent_api: regression tests for OllamaShow, OllamaLocalClient.ListModels
// context-population, and OllamaLocalClient.GetModelContextLimit cache behavior.
//
// What these tests pin down:
//   - httpOllamaClient.Show returns the right shape and surfaces 4xx/5xx/decode
//     failures as errors (Tests 1-4).
//   - OllamaLocalClient.ListModels calls /api/show per listed model so
//     ModelInfo.ContextLength is populated end-to-end (Test 5).
//   - /api/show failures during ListModels are best-effort: the model is still
//     listed, just without context_length (Test 6).
//   - GetModelContextLimit reads from cache when fresh, from /api/show when not,
//     and falls back to the configured default (Tests 7-9).
//   - After the 5-minute cache TTL elapses, a fresh /api/show is consulted
//     (Test 10 — exercises the same package's direct field access).
//
// We deliberately use a fresh, minimal httptest fixture rather than the
// existing ollamaTestServer from ollama_local_test.go so that adding tests
// here can never disturb the file the task scope forbids us from touching.
package api

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// ollamaShowFixture is a single httptest.Server that mocks the subset of
// Ollama endpoints these tests need: /api/tags, /api/show. Callers wire
// handlers per test via set*(); defaults are honest 200 / empty responses.
type ollamaShowFixture struct {
	srv         *httptest.Server
	tagsCalls   atomic.Int32
	showCalls   atomic.Int32
	tagsHandler http.HandlerFunc
	showHandler http.HandlerFunc
}

func newOllamaShowFixture(t *testing.T) *ollamaShowFixture {
	f := &ollamaShowFixture{}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		f.tagsCalls.Add(1)
		if f.tagsHandler != nil {
			f.tagsHandler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"models":[]}`)
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		f.showCalls.Add(1)
		if f.showHandler != nil {
			f.showHandler(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"model_info":{"context_length":0}}`)
	})
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// factory returns a closure suitable for newOllamaLocalClientWithFactory
// that pins the underlying httpOllamaClient at the fixture's URL.
func (f *ollamaShowFixture) factory() ollamaClientFactory {
	return func() (ollamaClient, error) {
		return newHTTPClientAt(f.srv.URL), nil
	}
}

// setShowContext serves /api/show with a fixed context_length for any
// incoming model name. Captures the show call count via the atomic on f.
func (f *ollamaShowFixture) setShowContext(ctx int) {
	f.showHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]any{
			"model_info": map[string]any{
				"context_length": ctx,
			},
		}
		_ = json.NewEncoder(w).Encode(body)
	}
}

// setTags mocks /api/tags with the given list of model names. This is
// what feeds OllamaLocalClient.ListModels with names to populate via
// /api/show.
func (f *ollamaShowFixture) setTags(names ...string) {
	f.tagsHandler = func(w http.ResponseWriter, r *http.Request) {
		type entry struct {
			Name string `json:"name"`
		}
		out := struct {
			Models []entry `json:"models"`
		}{}
		for _, n := range names {
			out.Models = append(out.Models, entry{Name: n})
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// setShowNotFound responds 404 to every /api/show call.
func (f *ollamaShowFixture) setShowNotFound() {
	f.showHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"model not found"}`, http.StatusNotFound)
	}
}

// setShowServerError responds 500 to every /api/show call.
func (f *ollamaShowFixture) setShowServerError() {
	f.showHandler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}
}

// setShowGarbage responds 200 with a body that won't decode as
// localOllamaShowResponse.
func (f *ollamaShowFixture) setShowGarbage() {
	f.showHandler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, "<not-json>")
	}
}

// --- Test 1: httpOllamaClient.Show happy path ---

// TestOllamaShowHappy verifies the happy-path wire/decode of Show: a 200
// response with a model_info.context_length yields the typed result.
func TestOllamaShowHappy(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setShowContext(8192)

	c, err := f.factory()()
	require.NoError(t, err)
	require.NotNil(t, c)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Show(ctx, "test-model")
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.Equal(t, 8192, resp.ModelInfo.ContextLength,
		"context_length must round-trip through the show endpoint")
	require.Equal(t, int32(1), f.showCalls.Load(),
		"exactly one /api/show call expected")
}

// --- Test 2: /api/show returns 404 ---

// TestOllamaShowNotFound verifies a non-2xx response becomes an error that
// carries the upstream status code, so callers can distinguish "not found
// on disk" from a transport failure.
func TestOllamaShowNotFound(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setShowNotFound()

	c, err := f.factory()()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Show(ctx, "missing")
	require.Error(t, err)
	require.Nil(t, resp,
		"Show must return a nil response when upstream errors")
	require.Contains(t, err.Error(), "404",
		"the error must surface the upstream status code so callers can distinguish not-found from network errors")
	require.Equal(t, int32(1), f.showCalls.Load())
}

// --- Test 3: /api/show returns 500 ---

// TestOllamaShowServerError verifies that an HTTP 500 surfaces as an error
// with the status code embedded.
func TestOllamaShowServerError(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setShowServerError()

	c, err := f.factory()()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Show(ctx, "any-model")
	require.Error(t, err)
	require.Nil(t, resp)
	require.Contains(t, err.Error(), "500")
	require.Equal(t, int32(1), f.showCalls.Load())
}

// --- Test 4: /api/show returns 200 with invalid JSON ---

// TestOllamaShowInvalidJSON verifies a 200 with a non-JSON body surfaces
// as a decode error rather than silently returning a zero-value response.
func TestOllamaShowInvalidJSON(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setShowGarbage()

	c, err := f.factory()()
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Show(ctx, "any-model")
	require.Error(t, err,
		"a 200 response body that won't decode must surface as an error")
	require.Nil(t, resp,
		"no partial result should be returned on a decode failure")
	require.Contains(t, strings.ToLower(err.Error()), "decode")
	require.Equal(t, int32(1), f.showCalls.Load())
}

// --- Test 5: ListModels populates ContextLength via /api/show ---

// TestOllamaListModelsPopulatesContextLength verifies the full
// ListModels → /api/show → ModelInfo.ContextLength flow that the auto-
// discovery implementation promises. The model returned in /api/tags
// must come back with the context_length the /api/show handler reports.
func TestOllamaListModelsPopulatesContextLength(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setTags("llama3:8b")
	f.setShowContext(8192)

	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)
	require.NotNil(t, client)

	// Construct a fresh context (the client itself only stores ctx on
	// its methods) and call ListModels.
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	models, err := client.ListModels(ctx)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Len(t, models, 1)
	require.Equal(t, "llama3:8b", models[0].ID)
	require.Equal(t, 8192, models[0].ContextLength,
		"ContextLength must be sourced from /api/show, not left at zero")
	require.Equal(t, "ollama-local", models[0].Provider)
	require.Equal(t, int32(1), f.showCalls.Load(),
		"ListModels must call /api/show exactly once per listed model")
	require.GreaterOrEqual(t, f.tagsCalls.Load(), int32(1),
		"ListModels must call /api/tags at least once (constructor also calls it once)")
}

// --- Test 6: Show failure during ListModels is best-effort ---

// TestOllamaListModelsShowFailureIsBestEffort verifies that a /api/show
// failure during ListModels does not abort the listing: the model is
// included with ContextLength=0 and no error is propagated.
//
// The implementation emits "[~] Failed to fetch Ollama model details…"
// lines to stderr in this path. We redirect stderr to /dev/null during
// the call so test output stays clean — the warning is correct to emit
// in production but is noise here.
func TestOllamaListModelsShowFailureIsBestEffort(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setTags("llama3:8b", "qwen2.5:7b")
	f.setShowNotFound()

	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Suppress the production stderr "Failed to fetch …" lines for
	// this best-effort path: they're correct behavior, not a sign of
	// test failure.
	origStderr := os.Stderr
	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	require.NoError(t, err)
	os.Stderr = devNull
	defer func() {
		os.Stderr = origStderr
		_ = devNull.Close()
	}()

	models, err := client.ListModels(ctx)
	require.NoError(t, err,
		"Show failures must be swallowed during ListModels (best-effort)")
	require.NotNil(t, models)
	require.Len(t, models, 2,
		"both models must still appear in the list despite Show failures")
	// Order is preserved in the spec; model-a (the constructor's model)
	// is listed first by the implementation, but we accept any order.
	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	require.Equal(t, 0, byID["llama3:8b"].ContextLength,
		"context must remain 0 when Show failed (best-effort)")
	require.Equal(t, 0, byID["qwen2.5:7b"].ContextLength)
	require.Equal(t, int32(2), f.showCalls.Load(),
		"ListModels still attempts Show on every model, even when they error")
}

// --- Test 7: GetModelContextLimit uses cache without re-fetching ---

// TestOllamaGetModelContextLimitCached verifies the cache fast-path:
// after ListModels has populated the cache, GetModelContextLimit reads
// from the cache and does NOT call /api/show again.
func TestOllamaGetModelContextLimitCached(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setTags("llama3:8b")
	f.setShowContext(8192)

	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// Populate the cache via ListModels.
	_, err = client.ListModels(ctx)
	require.NoError(t, err)
	require.Equal(t, int32(1), f.showCalls.Load(),
		"ListModels should have done exactly one Show")
	require.False(t, client.cachedAt.IsZero(),
		"ListModels must have stamped cachedAt")

	// Now hit the cache.
	got, err := client.GetModelContextLimit()
	require.NoError(t, err)
	require.Equal(t, 8192, got)
	require.Equal(t, int32(1), f.showCalls.Load(),
		"GetModelContextLimit must NOT re-call /api/show while cache is fresh")
}

// --- Test 8: GetModelContextLimit does a fresh Show when no cache exists ---

// TestOllamaGetModelContextLimitFreshShow verifies that without a prior
// ListModels call (cache cold), GetModelContextLimit does a per-call
// /api/show and returns the discovered context.
func TestOllamaGetModelContextLimitFreshShow(t *testing.T) {
	f := newOllamaShowFixture(t)
	// /api/show returns 4096 for any name.
	f.setShowContext(4096)
	// /api/tags so the constructor can pick up the requested model.
	f.setTags("llama3:8b")

	// Use the direct factory build so we know exactly what state the
	// client is in (cachedAt must be the zero value here).
	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)
	require.True(t, client.cachedAt.IsZero(),
		"a freshly built client must have cachedAt==zero (cold cache)")

	got, err := client.GetModelContextLimit()
	require.NoError(t, err)
	require.Equal(t, 4096, got,
		"fresh Show path must surface the upstream context_length")
	require.Equal(t, int32(1), f.showCalls.Load(),
		"fresh Show path must hit /api/show exactly once")
}

// --- Test 9: GetModelContextLimit falls back to DefaultContextLimit ---

// TestOllamaGetModelContextLimitDefaultFallback verifies that when /api/show
// returns a 4xx the function falls through to c.config.DefaultContextLimit
// (which the constructor seeds to 32000). The 0 sentinel the source code
// guards against is implicit: both branches produce 0 from a 404 response,
// and the function MUST NOT propagate 0 here in the production path.
func TestOllamaGetModelContextLimitDefaultFallback(t *testing.T) {
	f := newOllamaShowFixture(t)
	// Cache cold so /api/show is consulted; the Show call then 404s.
	f.setTags("llama3:8b")
	f.setShowNotFound()

	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)
	// Confirm the constructor seeded the default.
	require.Equal(t, defaultOllamaContextLimit, client.config.DefaultContextLimit,
		"constructor must seed DefaultContextLimit to %d (got %d)",
		defaultOllamaContextLimit, client.config.DefaultContextLimit)

	got, err := client.GetModelContextLimit()
	require.NoError(t, err)
	require.Equal(t, defaultOllamaContextLimit, got,
		"Show failure with a configured default must fall back to DefaultContextLimit")
	require.Equal(t, int32(1), f.showCalls.Load())
}

// --- Test 10: cache expiry triggers a fresh Show ---

// TestOllamaGetModelContextLimitCacheExpiredRefreshes verifies that once
// the cache's cachedAt is older than ollamaModelsCacheTTL (5 min),
// GetModelContextLimit disregards the stale entries and re-queries
// /api/show. We exercise the TTL boundary by backdating cachedAt past
// the TTL from inside this same package (the field is unexported, so
// peer tests must live in package api — which is where this file lives).
func TestOllamaGetModelContextLimitCacheExpiredRefreshes(t *testing.T) {
	f := newOllamaShowFixture(t)
	f.setTags("llama3:8b")
	f.setShowContext(2048) // fresh Show value

	client, err := newOllamaLocalClientWithFactory("llama3:8b", f.factory())
	require.NoError(t, err)

	// Seed a stale cache entry: cachedAt well in the past, plus a
	// cached Models entry whose ContextLength deliberately disagrees
	// with /api/show so we can observe the refresh.
	now := time.Now()
	client.cacheMu.Lock()
	client.cachedModels = []ModelInfo{{
		ID:            "llama3:8b",
		Name:          "llama3:8b",
		Provider:      "ollama-local",
		ContextLength: 999999, // stale value: must be ignored after TTL
	}}
	// Backdate past the 5-minute TTL so the read-side considers the
	// cache expired.
	client.cachedAt = now.Add(-2 * ollamaModelsCacheTTL)
	client.cacheMu.Unlock()

	got, err := client.GetModelContextLimit()
	require.NoError(t, err)
	require.Equal(t, 2048, got,
		"after cache TTL expiry, the function must consult /api/show, not the stale entry")
	require.Equal(t, int32(1), f.showCalls.Load(),
		"expired cache must trigger exactly one fresh /api/show call")
}
