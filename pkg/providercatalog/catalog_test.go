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
