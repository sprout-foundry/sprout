package api

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// pricingResolverCache memoizes per-(provider,model) pricing lookups within a
// single process so the metrics tracking path never blocks on a registry/network
// lookup more than once per model. A miss populates the cache; subsequent calls
// return the cached entry until the process ends.
var (
	mu              sync.Mutex
	pricingResolver = map[string]resolvedPricing{}
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
//
// The embedded provider catalog is the fallback when the registry path is
// unavailable or resolves no cached-input rate: providers whose live model
// listing omits `cached_input_cost` (DeepInfra, DeepSeek) still get a cached
// rate from the curated catalog, so cache-savings reporting is exact rather
// than silently zero.
func ResolveModelPricing(provider, model string) (inputPerM, outputPerM, cachedPerM float64, ok bool) {
	provider = strings.ToLower(strings.TrimSpace(provider))
	model = strings.ToLower(strings.TrimSpace(model))
	if provider == "" || model == "" {
		return 0, 0, 0, false
	}

	key := provider + "/" + model
	mu.Lock()
	if cached, hit := pricingResolver[key]; hit {
		mu.Unlock()
		return cached.inputPerM, cached.outputPerM, cached.cachedPerM, cached.inputPerM > 0 || cached.outputPerM > 0
	}
	mu.Unlock()

	// Resolve on miss. Timeboxed — the metrics path must not stall.
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	clientType, err := DetermineProvider(provider, "")
	if err != nil {
		// Provider isn't configured/available: fall back to the embedded
		// catalog before giving up, so a model with curated pricing still
		// resolves.
		if inPerM, outPerM, cachedPerM, ok := catalogPricingFallback(provider, model); ok {
			storeResolvedPricing(key, inPerM, outPerM, cachedPerM)
			return inPerM, outPerM, cachedPerM, true
		}
		storeResolvedPricing(key, 0, 0, 0)
		return 0, 0, 0, false
	}

	models, err := GetModelsForProviderCtx(ctx, clientType)
	if err != nil || len(models) == 0 {
		if inPerM, outPerM, cachedPerM, ok := catalogPricingFallback(provider, model); ok {
			storeResolvedPricing(key, inPerM, outPerM, cachedPerM)
			return inPerM, outPerM, cachedPerM, true
		}
		storeResolvedPricing(key, 0, 0, 0)
		return 0, 0, 0, false
	}

	var registryPricing *resolvedPricing
	for i := range models {
		if strings.ToLower(models[i].ID) == model || strings.ToLower(models[i].Name) == model {
			registryPricing = &resolvedPricing{
				inputPerM:  models[i].InputCost,
				outputPerM: models[i].OutputCost,
				cachedPerM: models[i].CachedInputCost,
			}
			break
		}
	}

	if registryPricing != nil {
		// The registry path can supply input/output pricing while leaving the
		// cached rate zero (the live listing didn't report one). Fill only the
		// missing cached rate from the catalog so the exact-savings path can
		// run; never overwrite a registry-provided rate.
		if registryPricing.cachedPerM <= 0 {
			if _, _, catalogCached, ok := catalogPricingFallback(provider, model); ok && catalogCached > 0 {
				registryPricing.cachedPerM = catalogCached
			}
		}
		storeResolvedPricing(key, registryPricing.inputPerM, registryPricing.outputPerM, registryPricing.cachedPerM)
		return registryPricing.inputPerM, registryPricing.outputPerM, registryPricing.cachedPerM,
			registryPricing.inputPerM > 0 || registryPricing.outputPerM > 0
	}

	if inPerM, outPerM, cachedPerM, ok := catalogPricingFallback(provider, model); ok {
		storeResolvedPricing(key, inPerM, outPerM, cachedPerM)
		return inPerM, outPerM, cachedPerM, true
	}

	storeResolvedPricing(key, 0, 0, 0)
	return 0, 0, 0, false
}

// catalogPricingFallback resolves pricing from the embedded provider catalog.
// The catalog is the curated source of truth for the recommended models — its
// entries carry cached_input_cost even when a provider's live /models listing
// omits it.
func catalogPricingFallback(provider, model string) (inputPerM, outputPerM, cachedPerM float64, ok bool) {
	return providercatalog.FindModelPricing(provider, model)
}

func storeResolvedPricing(key string, inputPerM, outputPerM, cachedPerM float64) {
	mu.Lock()
	pricingResolver[key] = resolvedPricing{inputPerM: inputPerM, outputPerM: outputPerM, cachedPerM: cachedPerM}
	mu.Unlock()
}

// ResetPricingResolver clears the memoized pricing cache. For tests.
func ResetPricingResolver() {
	mu.Lock()
	pricingResolver = map[string]resolvedPricing{}
	mu.Unlock()
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
