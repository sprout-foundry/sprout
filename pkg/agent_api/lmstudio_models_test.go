// Package agent_api: regression tests for lmStudioListModelsWrapper.ListModels.
//
// The wrapper has a layered fallback strategy:
//  1. Read LMSTUDIO_BASE_URL (default http://127.0.0.1:1234/v1).
//  2. Strip trailing /v1 and try the LM Studio-native /api/v0/models endpoint,
//     which returns per-model state + max_context_length.
//  3. Skip entries with state != "loaded" entirely.
//  4. If every kept loaded model has a positive max_context_length, return
//     them directly without ever touching /v1/models.
//  5. Otherwise merge by ID with the OpenAI-compatible /v1/models fallback
//     (which hardcodes ContextLength=32768 — that endpoint doesn't expose
//     context size).
//  6. If the native call itself fails (non-200, network error, decode error)
//     fall through to /v1/models.
//
// Each test pins down exactly one branch of that decision tree so a future
// refactor that swaps the merge rule, breaks state filtering, or accidentally
// hits /v1/models when it shouldn't will fail loudly here.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// lmStudioTestFixture spins up an httptest.Server that plays the role of an
// LM Studio instance. The wrapper splits requests across two distinct paths:
//
//   - baseURL + "/v1/models"      (OpenAI-compat fallback)
//   - baseURL-v1  + "/api/v0/models" (native LM Studio API, where baseURL's
//     trailing /v1 is stripped before the path is appended)
//
// To exercise both with one server we pin LMSTUDIO_BASE_URL to
// "<srv.URL>/v1" and register both endpoint paths on the test mux.
//
// Each test wires up whichever sub-handlers it cares about; the default 404
// keeps the test honest about unexpected requests.
type lmStudioTestFixture struct {
	srv *httptest.Server

	v0Calls   atomic.Int32
	v1Calls   atomic.Int32
	v0Handler http.HandlerFunc
	v1Handler http.HandlerFunc
}

func newLMStudioTestFixture(t *testing.T) *lmStudioTestFixture {
	f := &lmStudioTestFixture{}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v0/models", func(w http.ResponseWriter, r *http.Request) {
		f.v0Calls.Add(1)
		if f.v0Handler != nil {
			f.v0Handler(w, r)
			return
		}
		http.NotFound(w, r)
	})
	// Two v1 paths are registered because the wrapper strips the suffix
	// differently depending on which path it takes. listLMStudioOpenAIModels
	// uses baseURL+"/models" where baseURL already includes /v1, so the
	// real wire path is /v1/models.
	mux.HandleFunc("/v1/models", func(w http.ResponseWriter, r *http.Request) {
		f.v1Calls.Add(1)
		if f.v1Handler != nil {
			f.v1Handler(w, r)
			return
		}
		http.NotFound(w, r)
	})
	// Fallback for fixtures that point LMSTUDIO_BASE_URL somewhere
	// without /v1 (older environments). Hit only if a test author opts in.
	mux.HandleFunc("/models", func(w http.ResponseWriter, r *http.Request) {
		f.v1Calls.Add(1)
		if f.v1Handler != nil {
			f.v1Handler(w, r)
			return
		}
		http.NotFound(w, r)
	})

	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

// baseURL returns what we feed LMSTUDIO_BASE_URL so that the wrapper's
// "<base>/api/v0/models" lands on our /api/v0/models handler and the v1
// fallback's "<base>/models" lands on /v1/models.
func (f *lmStudioTestFixture) baseURL() string {
	return f.srv.URL + "/v1"
}

// modelIDs extracts and sorts the IDs so assertions are order-stable even
// if the upstream encoder ever reorders maps. The wrapper currently iterates
// a decoded slice (so order IS stable), but sorted assertions make
// regressions obvious too.
func modelIDs(models []ModelInfo) []string {
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ID)
	}
	sort.Strings(ids)
	return ids
}

// --- Test 1: v0 happy path, /v1/models must not be hit ---

// TestLMStudioListModelsV0AllLoadedWithContext verifies the wrapper returns
// the LM Studio-native v0 payload untouched when every loaded model reports
// a positive max_context_length. Most importantly, it must NOT touch the
// OpenAI-compat fallback — that path hardcodes ContextLength=32768 and
// would silently overwrite real values.
func TestLMStudioListModelsV0AllLoadedWithContext(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "qwen2.5-coder:7b", "state": "loaded", "max_context_length": 32768},
				{"id": "llama3.1:8b", "state": "loaded", "max_context_length": 131072},
			},
		})
	}
	// Catch an accidental fallback trip: /v1/models is destructive to
	// max_context_length semantics, so it must be inert on this branch.
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("/v1/models was unexpectedly hit: wrapper should trust v0 when context is populated")
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Len(t, models, 2)
	require.Equal(t, []string{"llama3.1:8b", "qwen2.5-coder:7b"}, modelIDs(models))

	byID := map[string]ModelInfo{}
	for _, m := range models {
		byID[m.ID] = m
	}
	require.Equal(t, 32768, byID["qwen2.5-coder:7b"].ContextLength,
		"v0 max_context_length must be preserved on the happy path")
	require.Equal(t, 131072, byID["llama3.1:8b"].ContextLength)
	require.Equal(t, "lmstudio", byID["llama3.1:8b"].Provider,
		"Provider must be tagged for downstream eligibility classification")
	require.Equal(t, int32(1), f.v0Calls.Load(),
		"v0 endpoint must be hit exactly once")
	require.Equal(t, int32(0), f.v1Calls.Load(),
		"/v1/models fallback must not be touched when v0 fully populates context")
}

// --- Test 2: state filter drops not-loaded entries ---

// TestLMStudioListModelsV0SkipsNotLoaded verifies that v0 entries with
// state != "loaded" are silently dropped, not surfaced as
// ContextLength=0 fallbacks.
func TestLMStudioListModelsV0SkipsNotLoaded(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "loaded-one", "state": "loaded", "max_context_length": 16384},
				{"id": "loaded-two", "state": "loaded", "max_context_length": 65536},
				// Not loaded: must be excluded.
				{"id": "not-loaded", "state": "not-loaded", "max_context_length": 131072},
			},
		})
	}
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		t.Fatalf("/v1/models should not be hit when loaded models carry context")
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Equal(t, []string{"loaded-one", "loaded-two"}, modelIDs(models),
		"only state=loaded entries must appear in the result")
	require.Equal(t, int32(1), f.v0Calls.Load())
	require.Equal(t, int32(0), f.v1Calls.Load())
}

// --- Test 3: per-row v0/v1 merge when context is missing on a loaded row ---

// TestLMStudioListModelsV0MissingContextMergesWithV1 verifies the merge
// branch: a loaded v0 entry whose max_context_length is 0 must be replaced
// by the matching /v1/models entry (which hardcodes 32768). This is the
// "fallback for context only, not for missing models" path — the v1 result
// is consulted, but only as a context-length source for IDs v0 already
// confirmed.
//
// The "v1-only" entry in the fixture is intentional: if a future refactor
// accidentally drops the merge step and substitutes the whole v1 list,
// this test fails because the v1-only entry would appear in the result.
func TestLMStudioListModelsV0MissingContextMergesWithV1(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []map[string]any{
				{"id": "mystery-model", "state": "loaded", "max_context_length": 0},
			},
		})
	}
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "mystery-model", "object": "model", "owned_by": "lmstudio"},
				// This entry is only in v1; if the merge degenerates into
				// "just use v1 list", it will leak into the result and
				// fail the count assertion below.
				{"id": "v1-only", "object": "model", "owned_by": "lmstudio"},
			},
		})
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Len(t, models, 1,
		"only v0-confirmed IDs must appear; a v1-only entry would mean merge degenerated into v1 replacement")
	require.Equal(t, "mystery-model", models[0].ID)
	require.Equal(t, 32768, models[0].ContextLength,
		"a loaded model with max_context_length=0 must inherit the OpenAI-compat hardcode via v1 merge")
	require.Equal(t, "lmstudio", models[0].Provider)
	require.Equal(t, int32(1), f.v0Calls.Load())
	require.Equal(t, int32(1), f.v1Calls.Load(),
		"v1 fallback must be consulted exactly once when v0 has at least one missing-context loaded model")
}

// --- Test 4: v0 returns non-200, fall through to v1 ---

// TestLMStudioListModelsV0FailsFallsBackToV1 verifies the wrapper swallows a
// v0 non-2xx response and falls back to the OpenAI-compat path. The v1
// result is taken as-is (all entries get ContextLength=32768).
func TestLMStudioListModelsV0FailsFallsBackToV1(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "v0 not enabled here", http.StatusNotFound)
	}
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "compat-model-a", "object": "model", "owned_by": "lmstudio"},
				{"id": "compat-model-b", "object": "model", "owned_by": "lmstudio"},
			},
		})
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err)
	require.NotNil(t, models)
	require.Equal(t, []string{"compat-model-a", "compat-model-b"}, modelIDs(models))
	require.Equal(t, 32768, models[0].ContextLength,
		"OpenAI-compat fallback hardcodes 32768")
	require.Equal(t, int32(1), f.v0Calls.Load())
	require.Equal(t, int32(1), f.v1Calls.Load())
}

// --- Test 5: v0 transport-level error, fall through to v1 ---

// TestLMStudioListModelsV0NetworkErrorFallsBackToV1 verifies the wrapper
// cleanly handles a v0 transport-level failure (connection torn down before
// a status line is sent) and falls through to the v1 path. The v0 handler
// hijacks the underlying connection and closes it without writing a status
// line or body, so the client's client.Do returns an EOF. The wrapper's
// subsequent v1 call opens a fresh connection against the same test
// server, which serves the recovery payload normally.
//
// A handler-level panic would also produce this behavior, but it spams the
// server log with panic recovery stack traces. Hijack-and-close is silent.
func TestLMStudioListModelsV0NetworkErrorFallsBackToV1(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatalf("ResponseWriter is not a Hijacker; cannot simulate transport failure")
		}
		conn, buf, err := hj.Hijack()
		if err != nil {
			t.Fatalf("hijack failed: %v", err)
		}
		// Flush any unread request body, then close with no reply.
		_ = buf.Flush()
		_ = conn.Close()
	}
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "after-network-fail", "object": "model", "owned_by": "lmstudio"},
			},
		})
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err,
		"transport-level v0 failure must not leak as an error to callers")
	require.NotNil(t, models)
	require.Equal(t, []string{"after-network-fail"}, modelIDs(models),
		"after a v0 transport failure the wrapper must surface the v1 result")
	require.Equal(t, 32768, models[0].ContextLength,
		"v1 fallback hardcodes context at 32768")
	require.Equal(t, int32(1), f.v1Calls.Load(),
		"v1 must be consulted exactly once after a v0 transport failure")
}

// --- Test 6: v0 returns HTTP 200 with garbage body, fall through to v1 ---

// TestLMStudioListModelsV0DecodeErrorFallsBackToV1 verifies that an HTTP 200
// from /api/v0/models with a body that won't decode as the v0 shape causes
// the wrapper to fall through to the OpenAI-compat path rather than return
// an empty (and misleading) list. The decode is done at the wrapper layer
// and its result is currently treated exactly like a transport failure:
// drop v0, call v1.
func TestLMStudioListModelsV0DecodeErrorFallsBackToV1(t *testing.T) {
	f := newLMStudioTestFixture(t)
	f.v0Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("<not-json>"))
	}
	f.v1Handler = func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"object": "list",
			"data": []map[string]any{
				{"id": "compat-only", "object": "model", "owned_by": "lmstudio"},
			},
		})
	}

	t.Setenv("LMSTUDIO_BASE_URL", f.baseURL())

	wrapper := lmStudioListModelsWrapper{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	models, err := wrapper.ListModels(ctx)
	require.NoError(t, err,
		"a decode failure on v0 must not leak a json error to callers")
	require.Equal(t, int32(1), f.v0Calls.Load())
	require.Equal(t, int32(1), f.v1Calls.Load(),
		"v1 must be consulted as part of the v0-failure fallback")
	require.Equal(t, []string{"compat-only"}, modelIDs(models),
		"on v0 decode failure the wrapper must surface the v1 fallback list")
	require.Equal(t, 32768, models[0].ContextLength,
		"v1 fallback hardcodes context at 32768")
}
