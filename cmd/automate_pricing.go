//go:build !js

package cmd

import (
	"context"
	"strings"
	"sync"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

// pricingCache memoizes model-list lookups during a single CLI invocation so
// rendering a workflow overview that names 5+ models against the same
// provider only hits the registry/network once per provider.
var (
	pricingCacheMu sync.Mutex
	pricingCache   = map[string][]api.ModelInfo{}
)

// PricingRow is a single model's per-million-token rates.
type PricingRow struct {
	Provider      string
	Model         string
	InputUsdPerM  float64
	OutputUsdPerM float64
	HasPricing    bool
}

// lookupModelPricing returns the input/output USD-per-million-token rates for
// a (provider, model) pair. HasPricing=false means we couldn't determine
// pricing (unknown provider, model not in the listing, or no pricing data
// surfaced for that model). The caller should display "unknown" rather than
// fabricate a value.
//
// Network/registry calls are timeboxed to keep the overview snappy. Failures
// are silent — overview rendering must not break for a pricing miss.
func lookupModelPricing(providerName, modelID string) PricingRow {
	row := PricingRow{Provider: providerName, Model: modelID}
	providerName = strings.TrimSpace(providerName)
	modelID = strings.TrimSpace(modelID)
	if providerName == "" || modelID == "" {
		return row
	}

	models, ok := fetchModelsForProvider(providerName)
	if !ok || len(models) == 0 {
		return row
	}

	wantLower := strings.ToLower(modelID)
	for i := range models {
		if strings.ToLower(models[i].ID) == wantLower || strings.ToLower(models[i].Name) == wantLower {
			if models[i].InputCost > 0 || models[i].OutputCost > 0 {
				row.InputUsdPerM = models[i].InputCost
				row.OutputUsdPerM = models[i].OutputCost
				row.HasPricing = true
			}
			return row
		}
	}
	return row
}

// fetchModelsForProvider returns the cached model list for a provider name.
// First call per provider per CLI run triggers a lookup; subsequent calls
// return the cached slice. The boolean is false only when the lookup itself
// errored — an empty slice is still a valid result we cache.
func fetchModelsForProvider(providerName string) ([]api.ModelInfo, bool) {
	cacheKey := strings.ToLower(providerName)

	pricingCacheMu.Lock()
	if cached, hit := pricingCache[cacheKey]; hit {
		pricingCacheMu.Unlock()
		return cached, true
	}
	pricingCacheMu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	cfg, cfgErr := configuration.Load()
	if cfgErr != nil {
		pricingCacheMu.Lock()
		pricingCache[cacheKey] = nil
		pricingCacheMu.Unlock()
		return nil, false
	}
	clientType, err := configuration.MapProviderStringToClientType(cfg, providerName)
	if err != nil {
		// Unknown provider — cache an empty slice so we don't retry.
		pricingCacheMu.Lock()
		pricingCache[cacheKey] = nil
		pricingCacheMu.Unlock()
		return nil, false
	}

	models, err := api.GetModelsForProviderCtx(ctx, clientType)
	if err != nil {
		pricingCacheMu.Lock()
		pricingCache[cacheKey] = nil
		pricingCacheMu.Unlock()
		return nil, false
	}

	pricingCacheMu.Lock()
	pricingCache[cacheKey] = models
	pricingCacheMu.Unlock()
	return models, true
}
