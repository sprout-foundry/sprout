package api

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Provider-agnostic field mapping.
//
// Different providers report the same logical value under different
// property names — DeepInfra puts response cost in `usage.estimated_cost`,
// OpenRouter in `usage.cost`, others use `total_cost`, `cost_usd`, a nested
// `cost.total`, etc. Rather than maintain a decode struct per provider, we
// probe a raw JSON object for the first matching candidate path. The same
// machinery powers cost extraction from chat responses and price extraction
// from /models listings (see ModelPricingFromEntry).

// costFieldPaths are candidate dotted locations for a response's monetary
// cost, probed in priority order. The provider-reported actual cost
// (`usage.cost`) is preferred over an estimate.
var costFieldPaths = []string{
	"usage.cost",
	"usage.estimated_cost",
	"usage.total_cost",
	"usage.cost_usd",
	"usage.cost.total",
	"cost",
	"estimated_cost",
	"total_cost",
}

// UsageCost returns the canonical cost from a typed ChatUsage, preferring
// the provider-reported Cost over EstimatedCost. Both are populated by the
// normal decode (and by the flexible fallback below) so callers don't need
// to know which field a given provider used.
func UsageCost(u ChatUsage) float64 {
	if u.Cost > 0 {
		return u.Cost
	}
	return u.EstimatedCost
}

// CostFromJSON probes raw response JSON for a cost value under any known
// candidate field name. Used as a fallback when the typed decode
// (estimated_cost/cost) found nothing, so a provider that names the field
// differently still surfaces a cost. Returns (0,false) when none match.
func CostFromJSON(body []byte) (float64, bool) {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0, false
	}
	return firstPositiveNumber(raw, costFieldPaths)
}

// firstPositiveNumber returns the first path whose leaf coerces to a
// positive number.
func firstPositiveNumber(raw map[string]any, paths []string) (float64, bool) {
	for _, path := range paths {
		if v, ok := numberAtPath(raw, path); ok && v > 0 {
			return v, true
		}
	}
	return 0, false
}

// numberAtPath walks a dotted path through nested JSON objects and coerces
// the leaf to float64. Accepts float64/json.Number/int/numeric-string leaves
// so providers can report numbers as JSON numbers or quoted strings.
func numberAtPath(raw map[string]any, path string) (float64, bool) {
	parts := strings.Split(path, ".")
	var cur any = raw
	for i, p := range parts {
		m, ok := cur.(map[string]any)
		if !ok {
			return 0, false
		}
		v, ok := m[p]
		if !ok {
			return 0, false
		}
		if i == len(parts)-1 {
			return toFloat(v)
		}
		cur = v
	}
	return 0, false
}

// priceCandidate is a candidate location for a model's per-token (or
// per-million) price plus the multiplier that converts the raw value to
// USD-per-million-tokens.
type priceCandidate struct {
	path string
	mult float64
}

// Most OpenAI-compatible model listings report price as USD per token, so the
// default conversion to per-million is *1e6. "cents_per_*_token" variants are
// cents-per-token (/100 then *1e6 = *1e4). "*_per_million" variants are
// already per-million (*1).
var inputPriceCandidates = []priceCandidate{
	{"pricing.prompt", 1e6},
	{"pricing.input", 1e6},
	{"pricing.input_cost_per_token", 1e6},
	{"pricing.cents_per_input_token", 1e4},
	{"input_token_cost", 1e6},
	{"pricing.input_per_million_tokens", 1},
	{"pricing.prompt_per_million", 1},
}

var outputPriceCandidates = []priceCandidate{
	{"pricing.completion", 1e6},
	{"pricing.output", 1e6},
	{"pricing.output_cost_per_token", 1e6},
	{"pricing.cents_per_output_token", 1e4},
	{"output_token_cost", 1e6},
	{"pricing.output_per_million_tokens", 1},
	{"pricing.completion_per_million", 1},
}

// cachedPriceCandidates probe for a provider's distinct cached-input rate —
// the per-token (or per-million) price charged for prompt tokens served from
// a prompt cache rather than re-processed. OpenRouter exposes this as
// pricing.input_cache_read; OpenAI-compatible listings may use other names.
// A "0" value is a valid (free) price, so toFloat's >0 guard in
// priceFromCandidates correctly treats only missing/zero as absent.
var cachedPriceCandidates = []priceCandidate{
	{"pricing.input_cache_read", 1e6},
	{"pricing.cached_input", 1e6},
	{"pricing.cached_input_cost_per_token", 1e6},
	{"pricing.cents_per_cached_input_token", 1e4},
	{"cached_input_token_cost", 1e6},
	{"pricing.cached_input_per_million_tokens", 1},
	{"pricing.input_cache_write", 1e6},
}

// ModelPricingPerMillion extracts a model's input/output price (USD per
// million tokens) from a raw /models listing entry, probing candidate field
// names/units so listings that name pricing differently still surface a
// price. Returns (0,0) when no candidate matches.
func ModelPricingPerMillion(entry map[string]any) (inputPerMillion, outputPerMillion float64) {
	return priceFromCandidates(entry, inputPriceCandidates), priceFromCandidates(entry, outputPriceCandidates)
}

// ModelCachedPricingPerMillion extracts a model's cached-input price (USD per
// million tokens) from a raw /models listing entry. Returns 0 when the listing
// does not expose a distinct cached rate (the provider either does not support
// prompt caching or folds cached tokens into the standard input price).
func ModelCachedPricingPerMillion(entry map[string]any) float64 {
	return priceFromCandidates(entry, cachedPriceCandidates)
}

func priceFromCandidates(entry map[string]any, cands []priceCandidate) float64 {
	for _, c := range cands {
		if v, ok := numberAtPath(entry, c.path); ok && v > 0 {
			return v * c.mult
		}
	}
	return 0
}

// toFloat coerces a decoded JSON scalar to float64.
func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	case string:
		if n == "" {
			return 0, false
		}
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	default:
		return 0, false
	}
}
