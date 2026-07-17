// Package agent: LLM response generation and cost tracking (split from agent_getters.go)
package agent

import (
	"context"
	"fmt"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	providers "github.com/sprout-foundry/sprout/pkg/agent_providers"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/providercatalog"
)

// GenerateResponse generates a simple response using the current model without tool calls.
//
// SP-073: uses a.interruptCtx so Stop/cancel aborts the in-flight call. If
// callers need to pass their own context, they can set it via SetInterruptCtx
// before calling.
func (a *Agent) GenerateResponse(messages []api.Message) (string, error) {
	resp, err := a.getClient().SendChatRequest(a.interruptCtx, messages, nil, "", false) // No tools, no reasoning, no disableThinking
	if err != nil {
		return "", agenterrors.NewProviderError("failed to generate response", err, a.GetProvider(), a.GetModel())
	}

	if len(resp.Choices) == 0 {
		return "", agenterrors.NewProviderError(fmt.Sprintf("no response generated for %d messages", len(messages)), nil, a.GetProvider(), a.GetModel())
	}

	// Track cost for this response so gate calls in the TODO loop and
	// other GenerateResponse callers contribute to fleetUsdBudget and
	// lifetime cost counters. GenerateResponse bypasses the seed provider
	// path (which does its own accumulateResponseCost), so we handle it here.
	a.accumulateResponseCost(resp)

	return resp.Choices[0].Message.Content, nil
}

// accumulateResponseCost tracks the cost of a chat response in the agent's
// state and debits the fleet USD budget. Mirrors sproutProvider's version
// but operates directly on the Agent's state so callers like GenerateResponse
// (which bypass the seed provider) still accumulate cost.
func (a *Agent) accumulateResponseCost(resp *api.ChatResponse) {
	if a == nil || a.state == nil || resp == nil {
		return
	}
	billingType := a.resolveBillingType()
	chargedCost := api.UsageCost(resp.Usage)
	if chargedCost == 0 && billingType == BillingPayPerToken && resp.Usage.TotalTokens > 0 {
		chargedCost = a.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	var tokenCost float64
	if billingType != BillingPayPerToken {
		tokenCost = a.estimateCostFromPricing(resp.Usage.PromptTokens, resp.Usage.CompletionTokens)
	}
	entry := CostEntry{
		BillingType:      billingType,
		Provider:         a.GetProvider(),
		Model:            a.GetModel(),
		ChargedCost:      chargedCost,
		TokenCost:        tokenCost,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
		CachedTokens:     resp.Usage.CachedTokens,
	}
	a.state.AddCostEntry(entry)

	a.state.SetPromptTokens(a.state.GetPromptTokens() + resp.Usage.PromptTokens)
	a.state.SetCompletionTokens(a.state.GetCompletionTokens() + resp.Usage.CompletionTokens)
	a.state.SetLLMCallCount(a.state.GetLLMCallCount() + 1)

	// Fleet USD budget: per SP-113 Layer 4, only ChargedCost is debited.
	// Subscription and free providers (chargedCost == 0) must NOT consume
	// the fleet budget — there is no marginal spend to protect against.
	if a.fleetUsdBudget != nil && chargedCost > 0 {
		spent, crossed, justExceeded := a.fleetUsdBudget.Add(chargedCost)
		_, limit := a.fleetUsdBudget.Snapshot()
		for _, t := range crossed {
			if cb, ok := a.budgetWarningCallback.Load().(func(threshold, spent, limit float64)); ok && cb != nil {
				cb(t, spent, limit)
			}
		}
		if justExceeded {
			a.fleetBudgetTrunc.Store(true)
			if cb, ok := a.budgetExceededCallback.Load().(func(spent, limit float64)); ok && cb != nil {
				cb(spent, limit)
			}
		}
	}

	if n := resp.Usage.CachedTokens; n > 0 {
		a.state.SetCachedTokens(a.state.GetCachedTokens() + n)
	}
	if resp.Usage.CacheWriteTokens != nil {
		if n := *resp.Usage.CacheWriteTokens; n > 0 {
			a.state.SetCacheWriteTokens(a.state.GetCacheWriteTokens() + n)
		}
	}
}

// resolveBillingType returns the billing model for the current provider.
// Mirrors sproutProvider.resolveBillingType but operates on Agent directly.
func (a *Agent) resolveBillingType() string {
	if a == nil {
		return BillingPayPerToken
	}
	provider := a.GetProvider()
	cfg, err := providers.GlobalFactory().GetProviderConfig(provider)
	if err == nil && cfg != nil {
		return cfg.BillingTypeResolved()
	}
	if provider == "zai-coding" {
		return BillingSubscription
	}
	return BillingPayPerToken
}

// ResolveBillingType is the exported wrapper around resolveBillingType so
// the CLI footer (cmd package) can surface subscription/free billing
// instead of "$0.0000". SP-113 Phase 3.
func (a *Agent) ResolveBillingType() string {
	return a.resolveBillingType()
}

// estimateCostFromPricing computes a cost estimate from token counts and the
// current model's per-million pricing. Mirrors sproutProvider.estimateCostFromPricing.
func (a *Agent) estimateCostFromPricing(promptTokens, completionTokens int) float64 {
	if a == nil {
		return 0
	}
	client := a.getClient()
	if client == nil {
		return 0
	}
	model := client.GetModel()
	if model == "" {
		return 0
	}

	if models, err := api.GetModelsForProviderCtx(context.Background(), a.getClientType()); err == nil {
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

	provider := a.GetProvider()
	if inPerM, outPerM, _, ok := providercatalog.FindModelPricing(provider, model); ok {
		return float64(promptTokens)/1e6*inPerM + float64(completionTokens)/1e6*outPerM
	}

	return 0
}
