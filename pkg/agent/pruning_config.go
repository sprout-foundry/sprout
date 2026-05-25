package agent

// SetPruningStrategy sets the conversation pruning strategy
func (a *Agent) SetPruningStrategy(strategy PruningStrategy) {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetStrategy(strategy)
		if a.debug {
			a.Logger().Debug("[~] Pruning strategy set to: %s\n", strategy)
		}
	}
}

// SetPruningThreshold sets the context usage threshold for triggering automatic pruning
// threshold should be between 0 and 1 (e.g., 0.7 = 70%)
func (a *Agent) SetPruningThreshold(threshold float64) {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetThreshold(threshold)
		if a.debug {
			a.Logger().Debug("[~] Pruning threshold set to: %.1f%%\n", threshold*100)
		}
	}
}

// SetRecentMessagesToKeep sets how many recent messages to always preserve
func (a *Agent) SetRecentMessagesToKeep(count int) {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetRecentMessagesToKeep(count)
		if a.debug {
			a.Logger().Debug("[~] Recent messages to keep set to: %d\n", count)
		}
	}
}

// SetPruningSlidingWindowSize sets the sliding window size for the sliding window strategy
func (a *Agent) SetPruningSlidingWindowSize(size int) {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetSlidingWindowSize(size)
		if a.debug {
			a.Logger().Debug("[~] Sliding window size set to: %d\n", size)
		}
	}
}

// GetPruningStats returns information about the current pruning configuration
func (a *Agent) GetPruningStats() map[string]interface{} {
	if a.state.GetConversationPruner() == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	pruner := a.state.GetConversationPruner()
	return map[string]interface{}{
		"enabled":               true,
		"strategy":              pruner.Strategy(),
		"threshold":             pruner.Threshold(),
		"recent_messages_kept":  pruner.RecentMessagesToKeep(),
		"sliding_window_size":   pruner.SlidingWindowSize(),
		"current_message_count": len(a.state.GetMessages()),
		"current_context_usage": float64(a.state.GetCurrentContextTokens()) / float64(a.state.GetMaxContextTokens()),
	}
}

// DisableAutoPruning disables automatic conversation pruning
func (a *Agent) DisableAutoPruning() {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetStrategy(PruneStrategyNone)
		if a.debug {
			a.Logger().Debug("[~] Automatic pruning disabled\n")
		}
	}
}

// EnableAutoPruning enables automatic conversation pruning with default adaptive strategy
func (a *Agent) EnableAutoPruning() {
	if a.state.GetConversationPruner() != nil {
		a.state.GetConversationPruner().SetStrategy(PruneStrategyAdaptive)
		if a.debug {
			a.Logger().Debug("[~] Automatic pruning enabled (adaptive strategy)\n")
		}
	}
}
