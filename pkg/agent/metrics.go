package agent

import (
	"github.com/sprout-foundry/sprout/pkg/configuration"
)

const (
	RunTerminationCompleted            = "completed"
	RunTerminationMaxIterations        = "max_iterations"
	RunTerminationInterrupted          = "interrupted"
	RunTerminationFleetBudgetExceeded  = "fleet_budget_exceeded"
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
	if a.client != nil {
		return a.client.GetLastTPS()
	}
	return 0.0
}

// GetPromptTokens returns the total prompt tokens used
func (a *Agent) GetPromptTokens() int {
	return a.state.GetPromptTokens()
}

// TrackMetricsFromResponse updates agent metrics from API response usage data
func (a *Agent) TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens int) {
	a.state.IncrementLLMCallCount()
	a.state.SetTotalTokens(a.state.GetTotalTokens() + totalTokens)
	a.state.SetPromptTokens(a.state.GetPromptTokens() + promptTokens)
	a.state.SetCompletionTokens(a.state.GetCompletionTokens() + completionTokens)
	a.state.AddCost(estimatedCost)
	a.state.SetCachedTokens(a.state.GetCachedTokens() + cachedTokens)

	// Fleet budget tracking: debit tokens to the shared fleet tracker.
	if a.fleetBudgetTracker != nil && a.fleetBudgetLimit > 0 {
		newTotal := a.fleetBudgetTracker.Add(int64(totalTokens))
		// Budget is exceeded when cumulative tokens reach or exceed the limit.
		if newTotal >= a.fleetBudgetLimit && !a.fleetBudgetTrunc.Load() {
			a.fleetBudgetTrunc.Store(true)
		}
	}

	// Calculate cost savings from cached tokens
	// Assuming cached tokens save approximately 90% of the cost (since they're reused)
	if cachedTokens > 0 {
		// Rough estimate: cached token value = tokens * average cost per token
		avgCostPerToken := 0.0
		if totalTokens > 0 && estimatedCost > 0 {
			avgCostPerToken = estimatedCost / float64(totalTokens)
		}
		a.state.SetCachedCostSavings(a.state.GetCachedCostSavings() + float64(cachedTokens)*avgCostPerToken*0.9)
	}

	// Trigger stats update callback if registered
	if callback, ok := a.statsUpdateCallback.Load().(func(int, float64)); ok && callback != nil {
		callback(a.state.GetTotalTokens(), a.state.GetTotalCost())
	}
}

// GetCompletionTokens returns the total completion tokens used
func (a *Agent) GetCompletionTokens() int {
	return a.state.GetCompletionTokens()
}

// GetLLMCallCount returns the total number of LLM API calls made
func (a *Agent) GetLLMCallCount() int {
	return a.state.GetLLMCallCount()
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

// GetCachedCostSavings returns the cost savings from cached tokens
func (a *Agent) GetCachedCostSavings() float64 {
	return a.state.GetCachedCostSavings()
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
	if a.client != nil {
		return a.client.GetAverageTPS()
	}
	return 0.0
}

// GetTPSStats returns comprehensive TPS statistics
func (a *Agent) GetTPSStats() map[string]float64 {
	if a.client != nil {
		return a.client.GetTPSStats()
	}
	return map[string]float64{}
}
