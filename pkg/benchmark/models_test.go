//go:build !js

// models_test.go — the SP-154 §154b (154.4) model-list tests: the pure
// DefaultModels derivation (synthetic catalogs, no global state), the
// embedded-catalog entry point (DefaultModelList against the shipped
// catalog), and the Runner's configurable override (SuiteModels).

package benchmark

import (
	"reflect"
	"testing"

	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// ---------------------------------------------------------------------------
// DefaultModels: the pure derivation (synthetic catalogs)
// ---------------------------------------------------------------------------

// TestDefaultModelsKeepsRecommendedInCatalogOrder pins the derivation:
// a catalog mixing a recommended provider, a provider with no
// recommended_model, and a provider with an empty recommended_model
// yields exactly the non-empty entries, in catalog provider order, each
// pairing the provider's id with its recommended model.
func TestDefaultModelsKeepsRecommendedInCatalogOrder(t *testing.T) {
	catalog := providercatalog.Catalog{
		Providers: []providercatalog.Provider{
			{ID: "alpha", RecommendedModel: "alpha-reco"},
			{ID: "beta"},
			{ID: "gamma", RecommendedModel: ""},
			{ID: "delta", RecommendedModel: "delta-reco"},
		},
	}
	got := DefaultModels(catalog)
	want := []ModelSpec{
		{Model: "alpha-reco", Provider: "alpha"},
		{Model: "delta-reco", Provider: "delta"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultModels = %+v, want %+v (non-empty recommended entries only, catalog order)", got, want)
	}
}

// TestDefaultModelsNoRecommendedEntriesYieldsNil pins the documented
// empty case: a catalog whose providers carry no recommended_model
// yields nil, and so does an empty catalog (not an empty non-nil
// slice).
func TestDefaultModelsNoRecommendedEntriesYieldsNil(t *testing.T) {
	if got := DefaultModels(providercatalog.Catalog{
		Providers: []providercatalog.Provider{{ID: "solo"}},
	}); got != nil {
		t.Fatalf("DefaultModels = %+v, want nil (no recommended entries)", got)
	}
	if got := DefaultModels(providercatalog.Catalog{}); got != nil {
		t.Fatalf("DefaultModels(empty catalog) = %+v, want nil", got)
	}
}

// TestDefaultModelsIsDeterministic pins that two calls on the same
// catalog produce equal slices (the derivation is a pure function of
// its input — the caller's catalog is never mutated).
func TestDefaultModelsIsDeterministic(t *testing.T) {
	catalog := providercatalog.Catalog{
		Providers: []providercatalog.Provider{
			{ID: "alpha", RecommendedModel: "alpha-reco"},
			{ID: "beta", RecommendedModel: "beta-reco"},
		},
	}
	original := catalog
	if !reflect.DeepEqual(DefaultModels(catalog), DefaultModels(catalog)) {
		t.Error("two DefaultModels calls on the same catalog differ (the derivation must be deterministic)")
	}
	if !reflect.DeepEqual(catalog, original) {
		t.Error("DefaultModels mutated its input catalog")
	}
}

// ---------------------------------------------------------------------------
// DefaultModelList: the production entry point (the live catalog)
// ---------------------------------------------------------------------------

// TestDefaultModelListFollowsEmbeddedCatalog pins the entry point
// against the shipped catalog: the list is non-empty, every spec pairs
// a catalog provider id with that provider's recommended_model, the
// count equals the number of embedded providers with a non-empty
// recommended_model (computed from the catalog — no hardcoded count
// that drifts when the catalog is updated), and a known spot-check
// entry (openai → gpt-5-mini, verified against providers.json) is
// present.
func TestDefaultModelListFollowsEmbeddedCatalog(t *testing.T) {
	catalog := providercatalog.Current()
	got := DefaultModelList()

	wantCount := 0
	for _, p := range catalog.Providers {
		if p.RecommendedModel != "" {
			wantCount++
		}
	}
	if len(got) != wantCount {
		t.Fatalf("len(DefaultModelList()) = %d, want %d (the embedded providers with a non-empty recommended_model)",
			len(got), wantCount)
	}
	if wantCount == 0 {
		t.Fatal("the embedded catalog declares no recommended_model entries; the test needs at least one")
	}

	byID := make(map[string]providercatalog.Provider, len(catalog.Providers))
	for _, p := range catalog.Providers {
		byID[p.ID] = p
	}
	for _, spec := range got {
		p, ok := byID[spec.Provider]
		if !ok {
			t.Errorf("spec's provider %q is not in the embedded catalog", spec.Provider)
			continue
		}
		if spec.Model != p.RecommendedModel {
			t.Errorf("provider %q: spec.Model = %q, want the catalog's recommended_model %q",
				p.ID, spec.Model, p.RecommendedModel)
		}
	}

	// Spot-check: the embedded catalog's openai entry recommends
	// gpt-5-mini (verified against providers.json).
	found := false
	for _, spec := range got {
		if spec.Provider == "openai" {
			if spec.Model != "gpt-5-mini" {
				t.Errorf("openai spec.Model = %q, want gpt-5-mini", spec.Model)
			}
			found = true
			break
		}
	}
	if !found {
		t.Error("no openai spec in the default model list (the embedded catalog recommends openai)")
	}
}

// TestDefaultModelListFollowsSetCatalog pins the §154b live-catalog
// property under a global swap: after SetCatalog the default list
// follows the swapped catalog (the benchmark follows recommendations
// without code changes), and the capture/restore cleanup brings the
// embedded list back for the rest of the suite.
func TestDefaultModelListFollowsSetCatalog(t *testing.T) {
	original := providercatalog.Current()
	t.Cleanup(func() { providercatalog.SetCatalog(original) })

	providercatalog.SetCatalog(providercatalog.Catalog{
		Providers: []providercatalog.Provider{
			{ID: "swap", RecommendedModel: "swap-reco"},
		},
	})
	want := []ModelSpec{{Model: "swap-reco", Provider: "swap"}}
	if got := DefaultModelList(); !reflect.DeepEqual(got, want) {
		t.Fatalf("DefaultModelList after SetCatalog = %+v, want the swapped catalog's entries %+v", got, want)
	}
}

// ---------------------------------------------------------------------------
// SuiteModels: the configurable override on the Runner
// ---------------------------------------------------------------------------

// TestSuiteModelsCustomListIsExact pins the override: a Runner with
// Models set resolves to exactly that list — the field's own slice, not
// a copy of the default.
func TestSuiteModelsCustomListIsExact(t *testing.T) {
	custom := []ModelSpec{
		{Model: "custom-model", Provider: "custom-provider"},
		{Model: "custom-model-2", Provider: "custom-provider"},
	}
	got := (&Runner{Models: custom}).SuiteModels()
	if !reflect.DeepEqual(got, custom) {
		t.Fatalf("SuiteModels = %+v, want exactly the runner's custom list", got)
	}
	if reflect.DeepEqual(got, DefaultModelList()) {
		t.Error("SuiteModels = the default list, want the runner's custom list (the override must win)")
	}
	if &got[0] != &custom[0] {
		t.Error("SuiteModels returned a copy of the list, want the runner's own slice")
	}
}

// TestSuiteModelsEmptyFallsBackToDefault pins the default side: a
// Runner without Models (the zero value) and one with an empty Models
// list both resolve to the default list, which is non-empty against
// the shipped catalog.
func TestSuiteModelsEmptyFallsBackToDefault(t *testing.T) {
	want := DefaultModelList()
	if len(want) == 0 {
		t.Fatal("DefaultModelList = empty; the test needs a non-empty shipped catalog")
	}
	if got := (&Runner{}).SuiteModels(); !reflect.DeepEqual(got, want) {
		t.Errorf("(&Runner{}).SuiteModels() = %+v, want the default list", got)
	}
	if got := (&Runner{Models: []ModelSpec{}}).SuiteModels(); !reflect.DeepEqual(got, want) {
		t.Errorf("Runner with an empty Models list: SuiteModels() = %+v, want the default list (empty → default)", got)
	}
}

// TestSuiteModelsNilReceiverReturnsDefault pins the nil-receiver side:
// a nil *Runner resolves to the default list.
func TestSuiteModelsNilReceiverReturnsDefault(t *testing.T) {
	var r *Runner
	if got := r.SuiteModels(); !reflect.DeepEqual(got, DefaultModelList()) {
		t.Errorf("(nil *Runner).SuiteModels() = %+v, want the default list", got)
	}
}
