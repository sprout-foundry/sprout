package providers

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"testing"
)

// maxPlausibleCostPerMTok bounds a per-million-token price. The dearest real
// model in the configs is well under it; a value above it is a unit error
// (per-token vs per-million) or a sentinel copied as a price.
const maxPlausibleCostPerMTok = 1000.0

// TestEmbeddedConfigPricesAreSane guards the provider configs, which the
// pricing-audit automation edits: no negative price (OpenRouter's "-1" means
// variable pricing and must be left unpriced, not scaled), no implausibly
// large price, and a cached-input price never above the input price.
func TestEmbeddedConfigPricesAreSane(t *testing.T) {
	files, err := fs.Glob(embeddedConfigs, "configs/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no embedded configs: %v", err)
	}
	for _, name := range files {
		data, err := embeddedConfigs.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		var doc any
		if err := json.Unmarshal(data, &doc); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		for _, problem := range priceProblems(doc) {
			t.Errorf("%s: %s", name, problem)
		}
	}
}

func priceProblems(node any) []string {
	var out []string
	switch v := node.(type) {
	case map[string]any:
		id, _ := v["id"].(string)
		for _, key := range []string{"input_cost", "output_cost", "cached_input_cost"} {
			cost, ok := v[key].(float64)
			if !ok {
				continue
			}
			if cost < 0 {
				out = append(out, fmt.Sprintf("model %q: %s %v is negative", id, key, cost))
			}
			if cost > maxPlausibleCostPerMTok {
				out = append(out, fmt.Sprintf("model %q: %s %v exceeds %v per million tokens", id, key, cost, maxPlausibleCostPerMTok))
			}
		}
		in, hasIn := v["input_cost"].(float64)
		cached, hasCached := v["cached_input_cost"].(float64)
		if hasIn && hasCached && in > 0 && cached > in {
			out = append(out, fmt.Sprintf("model %q: cached_input_cost %v exceeds input_cost %v", id, cached, in))
		}
		for _, child := range v {
			out = append(out, priceProblems(child)...)
		}
	case []any:
		for _, child := range v {
			out = append(out, priceProblems(child)...)
		}
	}
	return out
}
