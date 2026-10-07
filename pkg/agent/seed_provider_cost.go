// seed_provider_cost.go — the sproutProvider cost / billing layer: the
// response-cost accumulation, billing-type resolution, pricing-based cost
// estimation, and fleet-budget tracking. Split out of seed_provider.go.

package agent

import (
	"context"

	core "github.com/sprout-foundry/seed/core"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// accumulateResponseCost adds the provider-reported cost to the agent's lifetime cost counter.
// Also populates prompt/completion token breakdowns and debits the fleet USD budget.
func (sp *sproutProvider) accumulateResponseCost(resp *core.ChatResponse) {
	if sp.agent == nil || sp.agent.state == nil || resp == nil {
		return
	}
	billingType := sp.resolveBillingType()
	chargedCost := api.UsageCost(resp.Usage)
	if chargedCost == 0 && billingType == BillingPayPerToken && resp.Usage.TotalTokens > 0 {
		chargedCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	var tokenCost float64
	if billingType != BillingPayPerToken {
		tokenCost = sp.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	entry := CostEntry{
		BillingType:      billingType,
		Provider:         sp.agent.GetProvider(),
		Model:            sp.agent.GetModel(),
		Role:             sp.agent.GetRole(),
		ChargedCost:      chargedCost,
		TokenCost:        tokenCost,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CachedTokens:     resp.Usage.CachedTokens,
		ImageTokens:      resp.Usage.ImageTokens,
	}
	sp.agent.state.AddCostEntry(entry)

	sp.agent.state.SetPromptTokens(sp.agent.state.GetPromptTokens() + resp.Usage.PromptTokens)
	sp.agent.state.SetCompletionTokens(sp.agent.state.GetCompletionTokens() + resp.Usage.CompletionTokens)
	sp.agent.state.SetLLMCallCount(sp.agent.state.GetLLMCallCount() + 1)

	// Debit the fleet USD budget (only charged cost, not subscription/free).
	if sp.agent.fleetUsdBudget != nil && chargedCost > 0 {
		spent, crossed, justExceeded := sp.agent.fleetUsdBudget.Add(chargedCost)
		_, limit := sp.agent.fleetUsdBudget.Snapshot()
		for _, t := range crossed {
			if cb, ok := sp.agent.budgetWarningCallback.Load().(func(threshold, spent, limit float64)); ok && cb != nil {
				cb(t, spent, limit)
			}
		}
		if justExceeded {
			sp.agent.fleetBudgetTrunc.Store(true)
			if cb, ok := sp.agent.budgetExceededCallback.Load().(func(spent, limit float64)); ok && cb != nil {
				cb(spent, limit)
			}
		}
	}

	if n := resp.Usage.CachedTokens; n > 0 {
		sp.agent.state.SetCachedTokens(sp.agent.state.GetCachedTokens() + n)
		// Cache-savings: prefer the provider-reported actual cost (OpenRouter
		// `usage.cost`) over catalog rates, and record "unknown" when neither
		// can determine savings.
		if savings, known := sp.agent.calculateCachedTokenSavings(n, resp.Usage.PromptTokens, resp.Usage.Cost); known {
			sp.agent.state.SetCachedCostSavings(sp.agent.state.GetCachedCostSavings() + savings)
		} else {
			sp.agent.markCacheSavingsUnknown()
		}
	}
	if resp.Usage.CacheWriteTokens != nil {
		if n := *resp.Usage.CacheWriteTokens; n > 0 {
			sp.agent.state.SetCacheWriteTokens(sp.agent.state.GetCacheWriteTokens() + n)
		}
	}
	if n := resp.Usage.ImageTokens; n > 0 {
		sp.agent.state.SetImageTokens(sp.agent.state.GetImageTokens() + n)
	}
}

// resolveBillingType returns the billing model for the current provider.
func (sp *sproutProvider) resolveBillingType() string {
	if sp.agent == nil {
		return BillingPayPerToken
	}
	provider := sp.agent.GetProvider()
	// Check embedded provider configs for explicit billing_type
	cfg, err := providers.GlobalFactory().GetProviderConfig(provider)
	if err == nil && cfg != nil {
		return cfg.BillingTypeResolved()
	}
	// Fallback heuristics for custom/dynamic providers
	if provider == "zai-coding" {
		return BillingSubscription
	}
	return BillingPayPerToken
}

// estimateCostFromPricing computes a cost estimate from token counts and per-million pricing.
func (sp *sproutProvider) estimateCostFromPricing(promptTokens, completionTokens int) float64 {
	if sp.agent == nil || sp.agent.client == nil {
		return 0
	}
	model := sp.agent.client.GetModel()
	if model == "" {
		return 0
	}

	if models, err := api.GetModelsForProviderCtx(context.Background(), sp.agent.getClientType()); err == nil {
		for _, m := range models {
			if m.ID != model {
				continue
			}
			if m.InputCost > 0 || m.OutputCost > 0 {
				return float64(promptTokens)/1e6*m.InputCost + float64(completionTokens)/1e6*m.OutputCost
			}
			break
		}
	}

	provider := sp.agent.GetProvider()
	if inPerM, outPerM, _, ok := providercatalog.FindModelPricing(provider, model); ok {
		return float64(promptTokens)/1e6*inPerM + float64(completionTokens)/1e6*outPerM
	}

	return 0
}

// trackFleetBudgetForResponse debits tokens from this LLM response to the fleet budget tracker.
func (sp *sproutProvider) trackFleetBudgetForResponse(resp *api.ChatResponse) error {
	if sp.agent == nil {
		return nil
	}
	tracker := sp.agent.fleetBudgetTracker
	limit := sp.agent.fleetBudgetLimit
	if tracker == nil || limit <= 0 {
		return nil
	}
	tokens := int64(resp.Usage.TotalTokens)
	if tokens <= 0 {
		return nil
	}
	newTotal := tracker.Add(tokens)
	if newTotal >= limit && !sp.agent.fleetBudgetTrunc.Load() {
		sp.agent.fleetBudgetTrunc.Store(true)
		return FleetBudgetExceededError
	}
	return nil
}
