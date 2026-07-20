// Package agent: StateManager facade and its focused sub-managers.
//
// AgentStateManager is a thin facade composed via Go struct embedding.
// It holds 4 focused sub-managers, each owning a logical domain:
//
//   - *AgentSessionManager      — MessageStore, SessionStore,
//     CheckpointStore, SummaryStore, OptimizerStore, ContextBudgetStore,
//     ConversationPrunerStore, CommandHistoryStore, PauseStore,
//     SessionConfigStore, ConfigOverrideStore, IterationStore,
//     SessionIntentStore. (13 sub-interfaces)
//
//   - *AgentMetricsManager      — TaskActionStore, CostTracker,
//     TokenCounter, LLMCallTracker, ToolCallTracker, CacheStats,
//     EstimatedTokenStore. (7 sub-interfaces)
//
//   - *AgentPersonaManager      — PersonaStore, ToolGuidanceStore,
//     FalseStopStore. (3 sub-interfaces)
//
//   - *AgentSecurityStateManager — CircuitBreakerStore, PendingStateStore,
//     TerminationStore, ProviderErrorStore, TraceStore. (5 sub-interfaces)
//
// Method promotion makes every method on each sub-manager automatically
// visible on the AgentStateManager, satisfying the full StateManager
// interface (all 28 sub-interfaces composed) without explicit delegation.
//
// **PREFER THE FOCUSED SUB-MANAGER** in code that only needs one domain.
// Holding a *AgentSessionManager (for example) instead of a *StateManager
// makes the dependency narrower and reduces the temptation to reach
// across domains.
package agent

// AgentStateManager embeds all four focused sub-managers. Method promotion
// makes every method on each sub-manager automatically visible on the
// facade, satisfying all 28 sub-interfaces without explicit delegation.
//
// Prefer using a focused sub-manager type (e.g. *AgentSessionManager) in
// new code that only needs one domain. The facade exists for backward
// compatibility with callers that already hold *StateManager.
type AgentStateManager struct {
	*AgentSessionManager
	*AgentMetricsManager
	*AgentPersonaManager
	*AgentSecurityStateManager
}

// NewAgentStateManager creates a new AgentStateManager with sensible
// defaults. Each sub-manager is initialized with its own defaults:
// the Session sub-manager creates a fresh ConversationOptimizer and
// ConversationPruner (both needed before the first message); the
// Security sub-manager creates a CircuitBreakerState with an empty
// Actions map.
func NewAgentStateManager(debug bool) *AgentStateManager {
	return &AgentStateManager{
		AgentSessionManager:        NewAgentSessionManager(debug),
		AgentMetricsManager:        NewAgentMetricsManager(),
		AgentPersonaManager:       NewAgentPersonaManager(),
		AgentSecurityStateManager: NewAgentSecurityStateManager(),
	}
}
