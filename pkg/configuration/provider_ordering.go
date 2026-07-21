package configuration

// Provider selection on launch used to lead with the alphabetically-first
// credentialed provider from KnownProviders(), which almost always picked
// openrouter regardless of which provider the user actually runs day-to-day.
// orderProvidersByUsage reorders a candidate list using three signals:
//
//  1. Tier 1 — has credentials AND has been used before (entry in
//     cfg.ProviderModels). This is the strongest signal: the user has
//     confirmed this provider works for them.
//  2. Tier 2 — has credentials configured (env var or file store) but
//     no recorded usage. New credentials, no history yet.
//  3. Tier 3 — no credentials configured. Kept last; preserves the
//     input order so callers retain control over display ordering
//     (static KnownProviders() order, factory order, etc.).
//
// Within each tier the input order is preserved — a stable sort by tier.
// This keeps the tier rule auditable from test fixtures and prevents the
// "alphabetical inside a tier" surprise where reordering within tier 2
// would shuffle what's effectively a feature flag.
//
// The helper is read-only: it does not call Save, mutate the config, or
// shell out to the network. Pure function over its inputs. Callers
// (selectInitialProvider, Initialize CI branch, SelectProvider) all
// apply their own selection logic on top.

// orderingTier returns 1, 2, or 3 for a given provider, used as the
// primary sort key by orderProvidersByUsage.
func orderingTier(name string, cfg *Config) int {
	if cfg != nil {
		if _, used := cfg.ProviderModels[name]; used {
			return 1
		}
	}
	if HasProviderAuth(name) {
		return 2
	}
	return 3
}

// orderProvidersByUsage reorders providers by user-history tier:
// previously-used credentialed providers first, then credentialed-but-
// unused, then the rest. Within each tier the input order is preserved.
// Duplicate names in the input collapse to one occurrence so callers
// can pass in `KnownProviderNames()` plus an `envProviders` slice
// without worrying about intersections.
func orderProvidersByUsage(providers []string, cfg *Config) []string {
	seen := make(map[string]struct{}, len(providers))
	out := make([]string, 0, len(providers))
	tiers := [3][]string{}

	for _, name := range providers {
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		tiers[orderingTier(name, cfg)-1] = append(tiers[orderingTier(name, cfg)-1], name)
	}

	for _, tier := range tiers {
		out = append(out, tier...)
	}
	return out
}
