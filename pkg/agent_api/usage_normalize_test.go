package api

import "testing"

func TestUsageCostPrefersActualOverEstimate(t *testing.T) {
	if got := UsageCost(ChatUsage{EstimatedCost: 0.5}); got != 0.5 {
		t.Errorf("estimate-only: got %v, want 0.5", got)
	}
	if got := UsageCost(ChatUsage{Cost: 0.3, EstimatedCost: 0.5}); got != 0.3 {
		t.Errorf("actual over estimate: got %v, want 0.3", got)
	}
	if got := UsageCost(ChatUsage{}); got != 0 {
		t.Errorf("empty: got %v, want 0", got)
	}
}

func TestCostFromJSON_DifferingPropertyNames(t *testing.T) {
	cases := []struct {
		name string
		body string
		want float64
	}{
		{"deepinfra estimated_cost", `{"usage":{"prompt_tokens":10,"estimated_cost":0.000123}}`, 0.000123},
		{"openrouter cost", `{"usage":{"cost":0.0042}}`, 0.0042},
		{"total_cost", `{"usage":{"total_cost":0.01}}`, 0.01},
		{"cost_usd", `{"usage":{"cost_usd":0.02}}`, 0.02},
		{"nested cost.total", `{"usage":{"cost":{"total":0.05}}}`, 0.05},
		{"top-level cost", `{"cost":0.07}`, 0.07},
		{"string value", `{"usage":{"estimated_cost":"0.0009"}}`, 0.0009},
		{"no cost field", `{"usage":{"prompt_tokens":10,"completion_tokens":5}}`, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := CostFromJSON([]byte(tc.body))
			if tc.want == 0 {
				if ok {
					t.Errorf("expected no cost, got %v", got)
				}
				return
			}
			if !ok {
				t.Fatalf("expected cost %v, got none", tc.want)
			}
			if got != tc.want {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestModelPricingPerMillion_Variants(t *testing.T) {
	cases := []struct {
		name            string
		entry           map[string]any
		wantIn, wantOut float64
	}{
		{
			name:    "per-token strings (openrouter/deepinfra)",
			entry:   map[string]any{"pricing": map[string]any{"prompt": "0.0000002", "completion": "0.0000006"}},
			wantIn:  0.2,
			wantOut: 0.6,
		},
		{
			name:    "cents per token",
			entry:   map[string]any{"pricing": map[string]any{"cents_per_input_token": 0.00002, "cents_per_output_token": 0.00006}},
			wantIn:  0.2,
			wantOut: 0.6,
		},
		{
			name:    "input/output numeric per-token",
			entry:   map[string]any{"pricing": map[string]any{"input": 0.0000003, "output": 0.0000009}},
			wantIn:  0.3,
			wantOut: 0.9,
		},
		{
			name:    "no pricing",
			entry:   map[string]any{"id": "some-model"},
			wantIn:  0,
			wantOut: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in, out := ModelPricingPerMillion(tc.entry)
			if !approxEqual(in, tc.wantIn) || !approxEqual(out, tc.wantOut) {
				t.Errorf("got in=%v out=%v, want in=%v out=%v", in, out, tc.wantIn, tc.wantOut)
			}
		})
	}
}

func TestModelCachedPricingPerMillion(t *testing.T) {
	cases := []struct {
		name       string
		entry      map[string]any
		wantCached float64
	}{
		{
			// OpenRouter-style: pricing.input_cache_read as a per-token USD
			// string. 0.0000003/token * 1e6 = 0.30/M.
			name:       "input_cache_read per-token string (openrouter)",
			entry:      map[string]any{"pricing": map[string]any{"input_cache_read": "0.0000003"}},
			wantCached: 0.3,
		},
		{
			// pricing.cached_input as a numeric per-token USD value.
			name:       "cached_input numeric per-token",
			entry:      map[string]any{"pricing": map[string]any{"cached_input": 0.0000003}},
			wantCached: 0.3,
		},
		{
			// cents_per_cached_input_token: 0.03 cents/token.
			// /100 then *1e6 = *1e4 → 0.03 * 1e4 = 300.0
			name:       "cents per cached token",
			entry:      map[string]any{"pricing": map[string]any{"cents_per_cached_input_token": 0.03}},
			wantCached: 300.0,
		},
		{
			// cached_input_per_million_tokens: already per-million.
			name:       "per-million already",
			entry:      map[string]any{"pricing": map[string]any{"cached_input_per_million_tokens": 0.3}},
			wantCached: 0.3,
		},
		{
			name:       "no cached field present",
			entry:      map[string]any{"pricing": map[string]any{"prompt": "0.000002", "completion": "0.000006"}},
			wantCached: 0,
		},
		{
			name:       "empty entry",
			entry:      map[string]any{},
			wantCached: 0,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ModelCachedPricingPerMillion(tc.entry)
			if !approxEqual(got, tc.wantCached) {
				t.Errorf("got %v, want %v", got, tc.wantCached)
			}
		})
	}
}

func approxEqual(a, b float64) bool {
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < 1e-9
}

// TestParseSSEData_FlexibleCost verifies the streaming path captures cost
// even when the provider reports it under a non-standard property name.
func TestParseSSEData_FlexibleCost(t *testing.T) {
	// estimated_cost: captured by the typed decode.
	chunk, err := ParseSSEData(`{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"estimated_cost":0.0001}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if chunk.Usage == nil || chunk.Usage.EstimatedCost != 0.0001 {
		t.Errorf("estimated_cost not captured: %+v", chunk.Usage)
	}

	// total_cost: not a typed field — must be picked up by the flexible fallback.
	chunk, err = ParseSSEData(`{"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2,"total_cost":0.0005}}`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if chunk.Usage == nil || chunk.Usage.EstimatedCost != 0.0005 {
		t.Errorf("flexible total_cost fallback failed: %+v", chunk.Usage)
	}
}
