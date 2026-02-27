package agent

import (
	"github.com/alantheprice/ledit/pkg/configuration"
)

// GetTotalTokens returns the total tokens used across all requests
func (a *Agent) GetTotalTokens() int {
	return a.totalTokens
}

// GetCurrentIteration returns the current iteration number
func (a *Agent) GetCurrentIteration() int {
	return a.currentIteration
}

// GetCurrentContextTokens returns the current context token count
func (a *Agent) GetCurrentContextTokens() int {
	// Return the current request context tokens, not cumulative
	return a.currentContextTokens
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

// SetMaxIterations sets the maximum number of iterations for the agent
func (a *Agent) SetMaxIterations(max int) {
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
	return a.promptTokens
}

// TrackMetricsFromResponse updates agent metrics from API response usage data
func (a *Agent) TrackMetricsFromResponse(promptTokens, completionTokens, totalTokens int, estimatedCost float64, cachedTokens int) {
	a.totalTokens += totalTokens
	a.promptTokens += promptTokens
	a.completionTokens += completionTokens
	a.totalCost += estimatedCost
	a.cachedTokens += cachedTokens

	// Calculate cost savings from cached tokens
	// Assuming cached tokens save approximately 90% of the cost (since they're reused)
	if cachedTokens > 0 {
		// Rough estimate: cached token value = tokens * average cost per token
		avgCostPerToken := 0.0
		if totalTokens > 0 && estimatedCost > 0 {
			avgCostPerToken = estimatedCost / float64(totalTokens)
		}
		a.cachedCostSavings += float64(cachedTokens) * avgCostPerToken * 0.9
	}

	// Trigger stats update callback if registered
	if a.statsUpdateCallback != nil {
		a.statsUpdateCallback(a.totalTokens, a.totalCost)
	}
}

// GetCompletionTokens returns the total completion tokens used
func (a *Agent) GetCompletionTokens() int {
	return a.completionTokens
}

// GetCachedTokens returns the total cached/reused tokens
func (a *Agent) GetCachedTokens() int {
	return a.cachedTokens
}

// GetCachedCostSavings returns the cost savings from cached tokens
func (a *Agent) GetCachedCostSavings() float64 {
	return a.cachedCostSavings
}

// GetContextWarningIssued returns whether a context warning has been issued
func (a *Agent) GetContextWarningIssued() bool {
	return a.contextWarningIssued
}

// GetMaxIterations returns the maximum iterations allowed
func (a *Agent) GetMaxIterations() int {
	return a.maxIterations
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
