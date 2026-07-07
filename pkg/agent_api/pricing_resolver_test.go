package api

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/modelregistry"
)

// TestResolveModelPricing_EmptyInputs verifies that empty provider or model
// strings short-circuit and return (0,0,0,false) without touching the cache.
func TestResolveModelPricing_EmptyInputs(t *testing.T) {
	ResetPricingResolver()

	cases := []struct {
		name   string
		prov   string
		model  string
		wantOK bool
	}{
		{"empty provider", "", "gpt-4o", false},
		{"whitespace provider", "  ", "gpt-4o", false},
		{"empty model", "openrouter", "", false},
		{"whitespace model", "openrouter", "  ", false},
		{"both empty", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, out, cached, ok := ResolveModelPricing(tc.prov, tc.model)
			if ok != tc.wantOK {
				t.Errorf("ok: got %v, want %v (in=%v out=%v cached=%v)", ok, tc.wantOK, in, out, cached)
			}
			if in != 0 || out != 0 || cached != 0 {
				t.Errorf("expected all-zero for empty input, got in=%v out=%v cached=%v", in, out, cached)
			}
		})
	}

	t.Cleanup(ResetPricingResolver)
}

// TestResolveModelPricing_Registry resolves pricing from a test registry
// server that returns a model with input/output/cached costs, verifying the
// full registry → ResolveModelPricing path carries cached_input_cost through.
func TestResolveModelPricing_Registry(t *testing.T) {

	// Make OpenRouter "available" so DetermineProvider succeeds without network.
	t.Setenv("OPENROUTER_API_KEY", "test-key-for-resolver")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Legacy flat schema (no schema_version): cached_input_cost maps
		// directly to ModelInfo.CachedInputCost.
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
		  "updated_at": "2024-01-01T00:00:00Z",
		  "models": [
		    {"id": "anthropic/claude-3.5", "input_cost": 3.0, "output_cost": 15.0, "cached_input_cost": 0.3},
		    {"id": "openai/gpt-4o", "input_cost": 5.0, "output_cost": 15.0}
		  ]
		}`))
	}))
	t.Cleanup(srv.Close)

	modelregistry.SetBaseURL(srv.URL)
	modelregistry.ClearCache()
	modelregistry.SetTTL(1)
	t.Cleanup(func() {
		modelregistry.SetBaseURL("")
		modelregistry.ClearCache()
	})
	t.Cleanup(ResetPricingResolver)

	// Model with a cached-input rate.
	in, out, cached, ok := ResolveModelPricing("openrouter", "anthropic/claude-3.5")
	if !ok {
		t.Fatalf("expected ok=true, got ok=false (provider availability issue?)")
	}
	if !approxEqual(in, 3.0) || !approxEqual(out, 15.0) {
		t.Errorf("input/output: got in=%v out=%v, want 3.0/15.0", in, out)
	}
	if !approxEqual(cached, 0.3) {
		t.Errorf("cached: got %v, want 0.3", cached)
	}

	// Model without a cached-input rate → cached is 0 but ok is still true
	// (input/output costs are present).
	in2, out2, cached2, ok2 := ResolveModelPricing("openrouter", "openai/gpt-4o")
	if !ok2 {
		t.Fatalf("expected ok=true for gpt-4o")
	}
	if !approxEqual(in2, 5.0) || !approxEqual(out2, 15.0) {
		t.Errorf("gpt-4o input/output: got in=%v out=%v", in2, out2)
	}
	if cached2 != 0 {
		t.Errorf("gpt-4o cached should be 0, got %v", cached2)
	}
}

// TestResolveModelPricing_Memoization verifies that the second call for the
// same (provider, model) pair hits the in-process cache and does not re-fetch.
func TestResolveModelPricing_Memoization(t *testing.T) {

	t.Setenv("OPENROUTER_API_KEY", "test-key-for-memo")

	var fetchCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fetchCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"updated_at":"2024-01-01T00:00:00Z","models":[{"id":"memo-model","input_cost":2,"output_cost":4,"cached_input_cost":0.2}]}`))
	}))
	t.Cleanup(srv.Close)

	modelregistry.SetBaseURL(srv.URL)
	modelregistry.ClearCache()
	modelregistry.SetTTL(0) // registry cache disabled so fetches always hit the server
	t.Cleanup(func() {
		modelregistry.SetBaseURL("")
		modelregistry.ClearCache()
	})
	t.Cleanup(ResetPricingResolver)

	// First call resolves from registry.
	in1, _, cached1, ok1 := ResolveModelPricing("openrouter", "memo-model")
	if !ok1 || !approxEqual(in1, 2) || !approxEqual(cached1, 0.2) {
		t.Fatalf("first call: ok=%v in=%v cached=%v", ok1, in1, cached1)
	}

	// Second call must return the same values from the pricing resolver cache
	// (not from the registry again).
	in2, _, cached2, ok2 := ResolveModelPricing("openrouter", "memo-model")
	if !ok2 || !approxEqual(in2, 2) || !approxEqual(cached2, 0.2) {
		t.Fatalf("second call: ok=%v in=%v cached=%v", ok2, in2, cached2)
	}

	// Verify the server was only hit once — second call came from in-process cache.
	if fetchCount != 1 {
		t.Errorf("expected exactly 1 server fetch (memoization), got %d", fetchCount)
	}
}

// TestResolveModelPricing_NotFoundInRegistry verifies that a model not present
// in the registry returns ok=false and caches that negative result.
func TestResolveModelPricing_NotFoundInRegistry(t *testing.T) {

	t.Setenv("OPENROUTER_API_KEY", "test-key-for-notfound")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"updated_at":"2024-01-01T00:00:00Z","models":[{"id":"real-model","input_cost":1,"output_cost":2}]}`))
	}))
	t.Cleanup(srv.Close)

	modelregistry.SetBaseURL(srv.URL)
	modelregistry.ClearCache()
	t.Cleanup(func() {
		modelregistry.SetBaseURL("")
		modelregistry.ClearCache()
	})
	t.Cleanup(ResetPricingResolver)

	in, out, cached, ok := ResolveModelPricing("openrouter", "nonexistent-model")
	if ok {
		t.Errorf("expected ok=false for missing model, got ok=true (in=%v out=%v cached=%v)", in, out, cached)
	}
	if in != 0 || out != 0 || cached != 0 {
		t.Errorf("expected all-zero for missing model, got in=%v out=%v cached=%v", in, out, cached)
	}
}
