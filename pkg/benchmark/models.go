// models.go — the model list: the benchmark's
// default model list (the provider catalog's recommended_model entries)
// and the Runner's configurable override (Runner.Models, resolved by
// Runner.SuiteModels in runner.go).
//
// The defining property: the default follows the live provider catalog, so
// when the catalog (embedded, or remote-refreshed) changes its
// recommendations the benchmark follows without code changes. The
// suite-level report iterates SuiteModels() × tasks; RunTask's
// 3-run rule per (model, task) is what each pair consumes.
package benchmark

import "github.com/sprout-foundry/sprout/pkg/providercatalog"

// DefaultModels derives the benchmark's default model list from catalog:
// one ModelSpec per provider whose RecommendedModel is
// non-empty, in catalog provider order, pairing the provider's id with
// its recommended_model. Providers without a recommended_model are
// skipped. A catalog with no recommended entries (no providers, or
// none carrying one) yields nil.
func DefaultModels(catalog providercatalog.Catalog) []ModelSpec {
	var out []ModelSpec
	for _, p := range catalog.Providers {
		if p.RecommendedModel == "" {
			continue
		}
		out = append(out, ModelSpec{Model: p.RecommendedModel, Provider: p.ID})
	}
	return out
}

// DefaultModelList is the production entry point for the benchmark's
// default model list: the live provider catalog's
// recommended_model entries. The list follows the catalog — embedded or
// remote-refreshed — so a change of recommendations flows into the
// benchmark without code changes; that is the defining property.
func DefaultModelList() []ModelSpec {
	return DefaultModels(providercatalog.Current())
}
