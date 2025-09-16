package agent

// SetPruningStrategy sets the conversation pruning strategy
func (a *Agent) SetPruningStrategy(strategy PruningStrategy) {
	if a.conversationPruner != nil {
		a.conversationPruner.SetStrategy(strategy)
		if a.debug {
			a.debugLog("🔄 Pruning strategy set to: %s\n", strategy)
		}
	}
}

// SetPruningThreshold sets the context usage threshold for triggering automatic pruning
// threshold should be between 0 and 1 (e.g., 0.7 = 70%)
func (a *Agent) SetPruningThreshold(threshold float64) {
	if a.conversationPruner != nil {
		a.conversationPruner.SetThreshold(threshold)
		if a.debug {
			a.debugLog("🔄 Pruning threshold set to: %.1f%%\n", threshold*100)
		}
	}
}

// SetRecentMessagesToKeep sets how many recent messages to always preserve
func (a *Agent) SetRecentMessagesToKeep(count int) {
	if a.conversationPruner != nil {
		a.conversationPruner.SetRecentMessagesToKeep(count)
		if a.debug {
			a.debugLog("🔄 Recent messages to keep set to: %d\n", count)
		}
	}
}

// SetPruningSlidingWindowSize sets the sliding window size for the sliding window strategy
func (a *Agent) SetPruningSlidingWindowSize(size int) {
	if a.conversationPruner != nil {
		a.conversationPruner.SetSlidingWindowSize(size)
		if a.debug {
			a.debugLog("🔄 Sliding window size set to: %d\n", size)
		}
	}
}

// GetPruningStats returns information about the current pruning configuration
func (a *Agent) GetPruningStats() map[string]interface{} {
	if a.conversationPruner == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	return map[string]interface{}{
		"enabled":               true,
		"strategy":              a.conversationPruner.strategy,
		"threshold":             a.conversationPruner.contextThreshold,
		"recent_messages_kept":  a.conversationPruner.recentMessagesToKeep,
		"sliding_window_size":   a.conversationPruner.slidingWindowSize,
		"current_message_count": len(a.messages),
		"current_context_usage": float64(a.currentContextTokens) / float64(a.maxContextTokens),
	}
}

// DisableAutoPruning disables automatic conversation pruning
func (a *Agent) DisableAutoPruning() {
	if a.conversationPruner != nil {
		a.conversationPruner.SetStrategy(PruneStrategyNone)
		if a.debug {
			a.debugLog("🔄 Automatic pruning disabled\n")
		}
	}
}

// EnableAutoPruning enables automatic conversation pruning with default adaptive strategy
func (a *Agent) EnableAutoPruning() {
	if a.conversationPruner != nil {
		a.conversationPruner.SetStrategy(PruneStrategyAdaptive)
		if a.debug {
			a.debugLog("🔄 Automatic pruning enabled (adaptive strategy)\n")
		}
	}
}
