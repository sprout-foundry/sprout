package api

import (
	"context"
	"strings"
	"sync"
	"time"
)

// pricingResolverCache memoizes per-(provider,model) pricing lookups within a
// single process so the metrics tracking path never blocks on a registry/network
// lookup more than once per model. A miss populates the cache; subsequent calls
// return the cached entry until the process ends.
var (
	pricingResolverMu sync.Mutex
	pricingResolver   = map[string]resolvedPricing{}
)

type resolvedPricing struct {
	inputPerM  float64
	outputPerM float64
	cachedPerM float64
}

// ResolveModelPricing returns the input/output/cached input/output prices (USD
// per million tokens) for a (provider, model) pair, resolved from the model
// registry / canonical adapter path and memoized for the process lifetime.
// cachedPerM is 0 when the provider/model does not expose a distinct cached
// rate. The boolean reports whether any pricing was found at all.
//
// Network lookups are timeboxed so the caller (the metrics path) never blocks
// for long. A lookup failure populates a zero entry so we don't retry every
// response — the model's pricing won't change mid-session.
func ResolveModelPricing(provider, model string) (inputPerM, outputPerM, cachedPerM float64, ok bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return 0, 0, 0, false
	}

	key := provider + "/" + model
	pricingResolverMu.Lock()
	if cached, hit := pricingResolver[key]; hit {
		pricingResolverMu.Unlock()
		return cached.inputPerM, cached.outputPerM, cached.cachedPerM, cached.inputPerM > 0 || cached.outputPerM > 0
	}
	pricingResolverMu.Unlock()

	// Resolve on miss. Timeboxed — the metrics path must not stall.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	clientType, err := DetermineProvider(provider, "")
	if err != nil {
		storeResolvedPricing(key, 0, 0, 0)
		return 0, 0, 0, false
	}

	models, err := GetModelsForProviderCtx(ctx, clientType)
	if err != nil || len(models) == 0 {
		storeResolvedPricing(key, 0, 0, 0)
		return 0, 0, 0, false
	}

	for i := range models {
		if strings.ToLower(models[i].ID) == model || strings.ToLower(models[i].Name) == model {
			storeResolvedPricing(key, models[i].InputCost, models[i].OutputCost, models[i].CachedInputCost)
			return models[i].InputCost, models[i].OutputCost, models[i].CachedInputCost, models[i].InputCost > 0 || models[i].OutputCost > 0
		}
	}

	storeResolvedPricing(key, 0, 0, 0)
	return 0, 0, 0, false
}

func storeResolvedPricing(key string, inputPerM, outputPerM, cachedPerM float64) {
	pricingResolverMu.Lock()
	pricingResolver[key] = resolvedPricing{inputPerM: inputPerM, outputPerM: outputPerM, cachedPerM: cachedPerM}
	pricingResolverMu.Unlock()
}

// ResetPricingResolver clears the memoized pricing cache. For tests.
func ResetPricingResolver() {
	pricingResolverMu.Lock()
	pricingResolver = map[string]resolvedPricing{}
	pricingResolverMu.Unlock()
}

// SeedPricingForTest populates the resolver cache for a specific (provider,
// model) pair without hitting the registry. For tests that need a known
// pricing rate to exercise the exact-savings branch in
// Agent.calculateCachedTokenSavings.
func SeedPricingForTest(provider, model string, inputPerM, outputPerM, cachedPerM float64) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return
	}
	storeResolvedPricing(provider+"/"+model, inputPerM, outputPerM, cachedPerM)
}
