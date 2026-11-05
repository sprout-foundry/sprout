package providercatalog

import "testing"

func TestEmbeddedCatalogLoads(t *testing.T) {
	catalog := Current()
	if len(catalog.Providers) == 0 {
		t.Fatal("expected embedded provider catalog to contain providers")
	}

	if provider, ok := FindProvider("zai"); !ok || provider.ID != "zai" {
		t.Fatal("expected zai provider to be present in catalog")
	}
}

func TestEmbeddedOnboardingOrderNamesKnownProviders(t *testing.T) {
	catalog := Current()
	if len(catalog.OnboardingOrder) == 0 {
		t.Fatal("expected the embedded catalog to define onboarding_order")
	}
	known := make(map[string]bool, len(catalog.Providers))
	for _, p := range catalog.Providers {
		known[p.ID] = true
	}
	seen := make(map[string]bool, len(catalog.OnboardingOrder))
	for _, id := range catalog.OnboardingOrder {
		if !known[id] {
			t.Errorf("onboarding_order names unknown provider %q", id)
		}
		if seen[id] {
			t.Errorf("onboarding_order lists %q twice", id)
		}
		seen[id] = true
	}
	for _, p := range catalog.Providers {
		if p.Recommended && p.RecommendedModel == "" {
			t.Errorf("recommended provider %q has no recommended_model", p.ID)
		}
		if p.RecommendedModel != "" && len(p.Models) > 0 && !hasModel(p, p.RecommendedModel) {
			t.Errorf("provider %q recommends %q, which is not in its model list", p.ID, p.RecommendedModel)
		}
	}
}

// TestDeepSeekV41CatalogFreshness (rel.3) pins that the embedded catalog
// carries the current DeepSeek V4.1 line with usable pricing, so the
// DeepSeek-V4.1-Flash model resolves a nonzero cost. Without this, a
// refreshed catalog could silently drop or zero the entry and DeepSeek
// (whose /v1/models omits cost) would track cost as $0 with no fallback.
func TestDeepSeekV41CatalogFreshness(t *testing.T) {
	// The deepseek provider's flagship V4.1 line: deepseek-flash. FindModelPricing
	// reports ok only when a model carries nonzero input OR output cost, so this
	// also proves the "nonzero cost" half of the entry.
	if in, out, _, ok := FindModelPricing("deepseek", "deepseek-flash"); !ok || in <= 0 || out <= 0 {
		t.Fatalf("deepseek/deepseek-flash (V4.1 flagship) must carry nonzero cost; got in=%v out=%v ok=%v", in, out, ok)
	}
	if provider, found := FindProvider("deepseek"); !found {
		t.Fatal("deepseek provider missing from catalog")
	} else if provider.RecommendedModel == "" {
		t.Error("deepseek provider has no recommended_model")
	}

	// The canonical OpenRouter alias for the same V4.1 model must resolve too.
	if _, _, _, ok := FindModelPricing("openrouter", "deepseek/deepseek-v4.1-flash"); !ok {
		t.Error("openrouter/deepseek/deepseek-v4.1-flash not in catalog — V4.1 freshness regressed")
	}

	// DeepInfra's best DeepSeek model is the V4.1-Flash-1017 snapshot: the
	// recommendation must point at it (not the superseded plain V4.1-Flash)
	// and it must carry nonzero cost.
	if in, out, _, ok := FindModelPricing("deepinfra", "deepseek-ai/DeepSeek-V4.1-Flash-1017"); !ok || in <= 0 || out <= 0 {
		t.Fatalf("deepinfra/deepseek-ai/DeepSeek-V4.1-Flash-1017 must carry nonzero cost; got in=%v out=%v ok=%v", in, out, ok)
	}
	if provider, found := FindProvider("deepinfra"); found {
		want := "deepseek-ai/DeepSeek-V4.1-Flash-1017"
		if provider.RecommendedModel != want {
			t.Errorf("deepinfra recommended_model = %q, want %q (the -1017 snapshot is the best DeepSeek model)", provider.RecommendedModel, want)
		}
		if provider.DefaultModel != want {
			t.Errorf("deepinfra default_model = %q, want %q", provider.DefaultModel, want)
		}
	} else {
		t.Fatal("deepinfra provider missing from catalog")
	}
}

func hasModel(p Provider, id string) bool {
	for _, m := range p.Models {
		if m.ID == id {
			return true
		}
	}
	return false
}

func TestOnboardingProvidersFollowsOrder(t *testing.T) {
	catalog := Catalog{
		OnboardingOrder: []string{"c", "a", "missing"},
		Providers:       []Provider{{ID: "a"}, {ID: "b"}, {ID: "c"}, {ID: "d"}},
	}
	got := catalog.OnboardingProviders()
	want := []string{"c", "a", "b", "d"}
	for i, p := range got {
		if p.ID != want[i] {
			t.Fatalf("order = %v, want %v", ids(got), want)
		}
	}
	if catalog.Providers[0].ID != "a" {
		t.Fatal("OnboardingProviders reordered the catalog in place")
	}
}

func ids(ps []Provider) []string {
	out := make([]string, len(ps))
	for i, p := range ps {
		out[i] = p.ID
	}
	return out
}
