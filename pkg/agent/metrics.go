package agent

import (
	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/configuration"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/events"
)

const (
	RunTerminationCompleted           = "completed"
	RunTerminationMaxIterations       = "max_iterations"
	RunTerminationInterrupted         = "interrupted"
	RunTerminationFleetBudgetExceeded = "fleet_budget_exceeded"
)

// GetTotalTokens returns the total tokens used across all requests
func (a *Agent) GetTotalTokens() int {
	return a.state.GetTotalTokens()
}

// GetCurrentIteration returns the current iteration number
func (a *Agent) GetCurrentIteration() int {
	return a.state.GetCurrentIteration()
}

// GetCurrentContextTokens returns the current context token count
func (a *Agent) GetCurrentContextTokens() int {
	// Return the current request context tokens, not cumulative
	return a.state.GetCurrentContextTokens()
}

// GetMaxContextTokens returns the maximum context tokens for the current model
func (a *Agent) GetMaxContextTokens() int {
	// Get context limit from the model
	return a.getModelContextLimit()
}

// GetConfigManager returns the configuration manager
func (a *Agent) GetConfigManager() *configuration.Manager {
	return a.configManager
}

// SetMaxIterations sets the maximum number of iterations for the agent.
// A value of 0 means unlimited (no iteration cap per prompt).
// Negative values are clamped to 0 (unlimited).
func (a *Agent) SetMaxIterations(max int) {
	if max < 0 {
		max = 0
	}
	a.maxIterations = max
}

// GetLastTPS returns the most recent TPS value from the provider
func (a *Agent) GetLastTPS() float64 {
	c := a.getClient()
	if c != nil {
		return c.GetLastTPS()
	}
	return 0.0
}

// GetPromptTokens returns the total prompt tokens used
func (a *Agent) GetPromptTokens() int {
	return a.state.GetPromptTokens()
}

// TrackMetricsFromResponse updates agent metrics from API response usage data.
// cacheWriteTokens is the number of prompt tokens written to the provider cache
// on this request (Anthropic/OpenRouter cache_creation_input_tokens). Pass 0
// when the provider does not report write tokens.
// imageTokens is the number of tokens consumed by image inputs (vision models).
// These are already included in PromptTokens/TotalTokens, so they are tracked
// separately for display only — NOT for budget deduction (avoids double-counting).
func (a *Agent) TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens, cacheWriteTokens, imageTokens int) {
	a.state.IncrementLLMCallCount()
	a.state.SetTotalTokens(a.state.GetTotalTokens() + totalTokens)
	a.state.SetPromptTokens(a.state.GetPromptTokens() + promptTokens)
	a.state.SetCompletionTokens(a.state.GetCompletionTokens() + completionTokens)
	a.state.SetCachedTokens(a.state.GetCachedTokens() + cachedTokens)
	a.state.SetCacheWriteTokens(a.state.GetCacheWriteTokens() + cacheWriteTokens)
	// Track image tokens separately for display (already included in totals).
	a.state.SetImageTokens(a.state.GetImageTokens() + imageTokens)

	// Resolve billing type and compute dual costs (ChargedCost / TokenCost)
	// using the same logic as seed_provider.go and agent_runtime.go.
	billingType := a.resolveBillingType()
	chargedCost := estimatedCost
	if chargedCost == 0 && billingType == BillingPayPerToken && totalTokens > 0 {
		chargedCost = a.estimateCostFromPricing(promptTokens, completionTokens)
	}
	var tokenCost float64
	if billingType != BillingPayPerToken {
		tokenCost = a.estimateCostFromPricing(promptTokens, completionTokens)
	}

	// AddCostEntry updates totalCost internally (for backward compat when
	// ChargedCost > 0), so we must NOT also call AddCost — that would
	// double-count.
	a.state.AddCostEntry(CostEntry{
		BillingType:      billingType,
		Provider:         a.GetProvider(),
		Model:            a.GetModel(),
		ChargedCost:      chargedCost,
		TokenCost:        tokenCost,
		PromptTokens:     promptTokens,
		CompletionTokens: completionTokens,
		CachedTokens:     cachedTokens,
		ImageTokens:      imageTokens,
	})

	// Fleet budget tracking: debit tokens to the shared fleet tracker.
	if a.fleetBudgetTracker != nil && a.fleetBudgetLimit > 0 {
		newTotal := a.fleetBudgetTracker.Add(int64(totalTokens))
		// Budget is exceeded when cumulative tokens reach or exceed the limit.
		if newTotal >= a.fleetBudgetLimit && !a.fleetBudgetTrunc.Load() {
			a.fleetBudgetTrunc.Store(true)
		}
	}

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

	// Calculate cost savings from cached tokens using the per-model pricing
	// resolver. Returns 0 when the cached rate is unknown — no fabrication.
	a.state.SetCachedCostSavings(a.state.GetCachedCostSavings() + a.calculateCachedTokenSavings(cachedTokens, totalTokens, estimatedCost))

	// Trigger stats update callback if registered
	if callback, ok := a.statsUpdateCallback.Load().(func(int, float64)); ok && callback != nil {
		callback(a.state.GetTotalTokens(), a.state.GetTotalCost())
	}
}

// GetCompletionTokens returns the total completion tokens used
func (a *Agent) GetCompletionTokens() int {
	return a.state.GetCompletionTokens()
}

// GetImageTokens returns the total image tokens used (vision model inputs).
// These are already included in PromptTokens/TotalTokens; this is for display only.
func (a *Agent) GetImageTokens() int {
	return a.state.GetImageTokens()
}

// GetLLMCallCount returns the total number of LLM API calls made
func (a *Agent) GetLLMCallCount() int {
	return a.state.GetLLMCallCount()
}

// ---------------------------------------------------------------------------
// Security telemetry (Task 3)
//
// Lightweight counters that track what the LLM does after receiving a
// SECURITY_CAUTION_REQUIRED signal. Exposed via the --output-json metrics
// object so external tools can measure caution-signal effectiveness.
// ---------------------------------------------------------------------------

// GetSecurityCautionsIssued returns the number of SECURITY_CAUTION_REQUIRED
// errors produced this session.
func (a *Agent) GetSecurityCautionsIssued() int64 {
	if a == nil {
		return 0
	}
	return a.secCautionsIssued.Load()
}

// GetSecurityRetriesAfterCaution returns the number of times the LLM retried
// the same tool+args after seeing a security caution (the count went 1→2).
func (a *Agent) GetSecurityRetriesAfterCaution() int64 {
	if a == nil {
		return 0
	}
	return a.secRetriesAfterCaution.Load()
}

// GetSecurityLoopsDetected returns the number of times loop detection fired
// (the same tool+args was blocked >= securityBlockThreshold times).
func (a *Agent) GetSecurityLoopsDetected() int64 {
	if a == nil {
		return 0
	}
	return a.secLoopsDetected.Load()
}

// incrementSecurityCautionsIssued bumps the cautions-issued counter.
func (a *Agent) incrementSecurityCautionsIssued() {
	if a == nil {
		return
	}
	a.secCautionsIssued.Add(1)
}

// incrementSecurityRetryAfterCaution bumps the retry-after-caution counter.
func (a *Agent) incrementSecurityRetryAfterCaution() {
	if a == nil {
		return
	}
	a.secRetriesAfterCaution.Add(1)
}

// incrementSecurityLoopsDetected bumps the loops-detected counter.
func (a *Agent) incrementSecurityLoopsDetected() {
	if a == nil {
		return
	}
	a.secLoopsDetected.Add(1)
}

// GetEstimatedTokenResponses returns how many responses used estimated token usage.
func (a *Agent) GetEstimatedTokenResponses() int {
	return a.state.GetEstimatedTokenResponses()
}

// MarkEstimatedTokenUsageResponse records that token usage for one response was estimated.
func (a *Agent) MarkEstimatedTokenUsageResponse() {
	a.state.SetEstimatedTokenResponses(a.state.GetEstimatedTokenResponses() + 1)
}

// GetCachedTokens returns the total cached/reused tokens
func (a *Agent) GetCachedTokens() int {
	return a.state.GetCachedTokens()
}

// GetCacheWriteTokens returns the total tokens written to the provider cache
func (a *Agent) GetCacheWriteTokens() int {
	return a.state.GetCacheWriteTokens()
}

// GetCachedCostSavings returns the cost savings from cached tokens
func (a *Agent) GetCachedCostSavings() float64 {
	return a.state.GetCachedCostSavings()
}

// calculateCachedTokenSavings estimates the cost savings from cached prompt
// tokens. Cached tokens are served from the provider's prompt cache instead
// of being re-processed, so they cost a fraction of normal input price.
//
// When the (provider, model) pair resolves to a known cached-input rate
// strictly less than the standard input rate, savings are exact:
//
//	savings = cachedTokens × (inputPrice − cachedInputPrice) / 1M
//
// The provider's reported estimatedCost already includes the discount on
// cached tokens, so this difference is the unrealized cost — i.e. the savings.
//
// When the cached rate is unknown (provider/model not in catalogue, or
// catalogue entry omits a distinct cached rate) the function returns 0
// rather than fabricating a number. The provider-reported total cost
// already reflects whatever rate was actually charged, so the ledger is
// always accurate; only the estimated savings display is suppressed.
//
// cachedTokens: number of prompt tokens served from cache.
// estimatedCost: total cost charged for this request.
// totalTokens: total tokens (prompt + completion) for this request.
func (a *Agent) calculateCachedTokenSavings(cachedTokens, totalTokens int, estimatedCost float64) float64 {
	if cachedTokens <= 0 || totalTokens <= 0 || estimatedCost <= 0 {
		return 0
	}

	// Try exact savings from per-model pricing.
	provider := a.GetProvider()
	model := a.GetModel()
	if inputPerM, _, cachedPerM, ok := api.ResolveModelPricing(provider, model); ok && inputPerM > 0 {
		switch {
		case cachedPerM > 0 && cachedPerM < inputPerM:
			// Exact: tokens that would have cost inputPrice actually cost
			// cachedPrice. Savings is the unrealized cost difference.
			return float64(cachedTokens) * (inputPerM - cachedPerM) / 1e6
		case cachedPerM >= inputPerM:
			// Cached price == input price (no discount). Provider reports
			// cache hits but bills at standard rate — no savings to claim.
			return 0
		default:
			// cachedPerM == 0: the provider/model has catalogue pricing
			// but does not expose a distinct cached rate. Caching may
			// still have happened (cachedTokens > 0) but we don't know
			// the discount. Do not fabricate a number — return 0 rather
			// than falling through to the 90% heuristic.
			return 0
		}
	}

	// Resolver miss: the (provider, model) pair is not in the catalogue at
	// all. We have no reliable pricing to compute against. Return 0 rather
	// than fabricate a 90% savings number from a cost figure we cannot
	// attribute to a known price.
	return 0
}

// GetContextWarningIssued returns whether a context warning has been issued
func (a *Agent) GetContextWarningIssued() bool {
	return a.state.IsContextWarningIssued()
}

// GetMaxIterations returns the maximum iterations allowed (0 means unlimited)
func (a *Agent) GetMaxIterations() int {
	return a.maxIterations
}

func (a *Agent) GetLastRunTerminationReason() string {
	return a.state.GetLastRunTerminationReason()
}

// IsDebugMode returns whether debug mode is enabled
func (a *Agent) IsDebugMode() bool {
	return a.debug
}

// GetCurrentTPS returns the current TPS value (alias for GetLastTPS)
func (a *Agent) GetCurrentTPS() float64 {
	return a.GetLastTPS()
}

// GetAverageTPS returns the average TPS across all requests
func (a *Agent) GetAverageTPS() float64 {
	c := a.getClient()
	if c != nil {
		return c.GetAverageTPS()
	}
	return 0.0
}

// GetTPSStats returns comprehensive TPS statistics
func (a *Agent) GetTPSStats() map[string]float64 {
	c := a.getClient()
	if c != nil {
		return c.GetTPSStats()
	}
	return map[string]float64{}
}

// RecordErrorCategory emits a metrics event with the given error's
// category label, so the cost/status footer can show "rate-limited,
// retrying…" vs "provider error" vs generic.
func (a *Agent) RecordErrorCategory(err error) {
	if err == nil || a.eventBus == nil {
		return
	}

	category := "unknown"
	if te := agenterrors.AsTypedError(err); te != nil {
		category = string(te.Code)
	} else if cat, ok := agenterrors.GetCategory(err); ok {
		category = cat.String()
	}

	a.publishEvent(
		events.EventTypeMetricsUpdate,
		events.MetricsUpdateEventWithCategory(
			a.state.GetTotalTokens(),
			a.state.GetCurrentContextTokens(),
			a.getModelContextLimit(),
			a.state.GetCurrentIteration(),
			a.state.GetTotalCost(),
			category,
		),
	)
}
