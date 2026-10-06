package agent

import (
	"fmt"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent/subagents"
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
	return a.state.GetCurrentContextTokens()
}

// GetMaxContextTokens returns the maximum context tokens for the current model
func (a *Agent) GetMaxContextTokens() int {
	return a.getModelContextLimit()
}

// GetMaxContextTokensCached returns the state-cached context limit.
// Unlike GetMaxContextTokens it never resolves the limit from the
// provider, so it is safe to call from hot poll paths (WebUI /api/stats)
// that run under the server's exclusive mutex — GetModelContextLimit on
// local providers can block for seconds on a network fetch.
func (a *Agent) GetMaxContextTokensCached() int {
	return a.state.GetMaxContextTokens()
}

// GetEffectiveContextCap returns the user-facing effective context cap — min of native window and user's MaxContextTokens setting.
func (a *Agent) GetEffectiveContextCap() int {
	if cap := a.effectiveCapSnapshot(); cap > 0 {
		return cap
	}
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
// cacheWriteTokens: prompt tokens written to provider cache. imageTokens: tokens from image inputs (display only, not for budget).
// actualCost is the provider-reported cost for this request (OpenRouter
// `usage.cost`); 0 when the provider does not report one.
func (a *Agent) TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens, cacheWriteTokens, imageTokens int, actualCost float64) {
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
		Role:             a.GetRole(),
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
		if newTotal >= a.fleetBudgetLimit && !a.fleetBudgetTrunc.Load() {
			a.fleetBudgetTrunc.Store(true)
		}
	}

	// Fleet USD budget NOT debited here — subagents already debit via accumulateResponseCost.

	// Calculate cost savings from cached tokens. Prefer the provider-reported
	// actual cost; when neither it nor the catalog rates can determine
	// savings, record "unknown" instead of a misleading $0.
	if savings, known := a.calculateCachedTokenSavings(cachedTokens, promptTokens, actualCost); known {
		a.state.SetCachedCostSavings(a.state.GetCachedCostSavings() + savings)
	} else {
		a.markCacheSavingsUnknown()
	}

	// Trigger stats update callback if registered
	if callback, ok := a.statsUpdateCallback.Load().(func(int, float64)); ok && callback != nil {
		callback(a.state.GetTotalTokens(), a.state.GetTotalCost())
	}
}

// RollupSubagentUsage folds a completed subagent/reviewer's usage into this
// agent's totals, attributed to the role that drove the subagent's model
// choice (SP-150 §150c, item 150.5) with the subagent's actual
// prompt/completion token split. It records a cost entry under the
// subagent's role (feeding both the per-role bucket and the overall cost
// totals) and advances the overall prompt/completion/total token counters —
// exactly as a first-party LLM call would — so the per-role totals keep
// summing to the overall totals. The subagent's own metrics manager is left
// untouched; this is the parent-side attribution.
func (a *Agent) RollupSubagentUsage(r *subagents.SubagentResult) {
	if a == nil || a.state == nil || r == nil {
		return
	}
	if r.TokensUsed == 0 && r.Cost <= 0 && r.PromptTokens == 0 && r.CompletionTokens == 0 {
		return
	}
	role := r.Role
	if strings.TrimSpace(role) == "" {
		role = configuration.RoleCoder
	}
	a.state.AddCostEntry(CostEntry{
		Role:             role,
		BillingType:      BillingPayPerToken,
		ChargedCost:      r.Cost,
		PromptTokens:     r.PromptTokens,
		CompletionTokens: r.CompletionTokens,
	})
	a.state.SetPromptTokens(a.state.GetPromptTokens() + r.PromptTokens)
	a.state.SetCompletionTokens(a.state.GetCompletionTokens() + r.CompletionTokens)
	a.state.SetTotalTokens(a.state.GetTotalTokens() + r.TokensUsed)
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

// Security telemetry: lightweight counters tracking LLM behavior after SECURITY_CAUTION_REQUIRED signals.

// GetSecurityCautionsIssued returns the number of SECURITY_CAUTION_REQUIRED errors produced this session.
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

// GetContinuationNudges returns how many seed transient continuation
// nudges ("Please continue…") were observed at the provider seam. These
// messages never enter conversation state, so this count explains
// consecutive assistant messages in transcripts.
func (a *Agent) GetContinuationNudges() int {
	return a.state.GetContinuationNudges()
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

// GetCacheSavingsUnknown reports whether any cached response in this session
// had no determinable savings. When true and no savings were determined, the
// cost views render "unknown" rather than a misleading $0.
func (a *Agent) GetCacheSavingsUnknown() bool {
	return a.state.GetCacheSavingsUnknown()
}

// FormatCacheSavings renders the session's cache savings for display. When
// savings are known it is the USD amount; when any cached response had no
// determinable savings (no actual cost, no catalog rate) and nothing was
// determined, it is "unknown" — never a misleading "$0.000000".
func (a *Agent) FormatCacheSavings() string {
	savings := a.GetCachedCostSavings()
	if savings > 0 {
		return fmt.Sprintf("$%.6f", savings)
	}
	if a.GetCacheSavingsUnknown() {
		return "unknown"
	}
	return "$0.000000"
}

// markCacheSavingsUnknown records that a cached response had no determinable
// savings (no actual cost and no usable catalog rate).
func (a *Agent) markCacheSavingsUnknown() {
	a.state.SetCacheSavingsUnknown(true)
}

// calculateCachedTokenSavings estimates the cost saved by prompt-cache hits.
//
// Two paths, in priority order:
//
//  1. actual-cost: when the provider reports the request's real cost (OpenRouter
//     `usage.cost`), savings = the uncached cost of the prompt minus that actual
//     cost. The uncached cost is what the full prompt would have cost at the
//     standard input rate, reconstructed by inverting the cache discount on the
//     reported cost (see uncachedPromptCost). This needs both catalog rates;
//     when they're unknown it falls through to path 2.
//
//  2. catalog-rate: with a known cached rate strictly below the input rate,
//     savings = cachedTokens × (inputRate − cachedRate) / 1M.
//
// Returns (0, true) when a path determined there was no saving, and (0, false)
// when neither path can determine savings — the caller renders that as
// "unknown" rather than a fabricated $0.
func (a *Agent) calculateCachedTokenSavings(cachedTokens, promptTokens int, actualCost float64) (float64, bool) {
	if cachedTokens <= 0 || promptTokens <= 0 {
		return 0, true
	}
	// Providers occasionally report cachedTokens > promptTokens on inconsistent
	// usage; clamp so the reconstruction can't go degenerate.
	if cachedTokens > promptTokens {
		cachedTokens = promptTokens
	}

	inputPerM, _, cachedPerM, pricingKnown := api.ResolveModelPricing(a.GetProvider(), a.GetModel())

	// Path 1: the provider reported the request's actual cost.
	if actualCost > 0 {
		uncachedCost, ok := uncachedPromptCost(actualCost, cachedTokens, promptTokens, inputPerM, cachedPerM, pricingKnown)
		if ok {
			savings := uncachedCost - actualCost
			if savings < 0 {
				// The actual cost exceeded the uncached-cost estimate (rate
				// drift, a cache-write premium, or a bad report). Never show a
				// negative "saving"; report zero rather than a bogus value.
				return 0, true
			}
			return savings, true
		}
		// No usable rates to reconstruct the uncached cost — fall through to
		// the catalog-rate path, which may still yield exact savings.
	}

	// Path 2: exact savings from per-model catalog rates. The rate delta alone
	// determines the saving, so no reported cost is required.
	if pricingKnown && inputPerM > 0 {
		if cachedPerM > 0 && cachedPerM < inputPerM {
			return float64(cachedTokens) * (inputPerM - cachedPerM) / 1e6, true
		}
		if cachedPerM >= inputPerM {
			// Provider reports cache hits but bills at (or above) the standard
			// rate — no discount to count.
			return 0, true
		}
	}

	// Neither an actual cost nor a usable catalog rate: savings are unknown.
	return 0, false
}

// uncachedPromptCost reconstructs what the prompt would have cost with no cache
// hits, from the provider-reported actual cost. It needs the input and cached
// rates: the actual cost is a weighted sum of the two, so inverting it
// recovers the uncached total. Returns ok=false when the rates are unusable
// (unknown, or the cached rate is not strictly below the input rate).
func uncachedPromptCost(actualCost float64, cachedTokens, promptTokens int, inputPerM, cachedPerM float64, pricingKnown bool) (float64, bool) {
	if !pricingKnown || inputPerM <= 0 || cachedPerM < 0 || cachedPerM >= inputPerM {
		return 0, false
	}
	// Cost = inputRate/1M × (promptTokens − cachedTokens) + cachedRate/1M × cachedTokens
	// effectiveTokens = promptTokens − cachedTokens × (1 − ratio)
	ratio := cachedPerM / inputPerM
	effectiveTokens := float64(promptTokens) - float64(cachedTokens)*(1-ratio)
	if effectiveTokens <= 0 {
		return 0, false
	}
	// Solves Cost = inputRate/1M × effectiveTokens for inputRate/1M.
	inputRatePerToken := actualCost / effectiveTokens
	return inputRatePerToken * float64(promptTokens), true
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
			a.GetProvider(),
			a.GetModel(),
			a.state.GetTotalTokens(),
			a.state.GetCurrentContextTokens(),
			a.getModelContextLimit(),
			a.state.GetCurrentIteration(),
			a.state.GetTotalCost(),
			category,
		),
	)
}
