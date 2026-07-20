package agent

// AgentStateManager embeds all four sub-managers. Method promotion makes every
// method on each sub-manager automatically visible on the facade, satisfying all
// 28 sub-interfaces without explicit delegation.
type AgentStateManager struct {
	*AgentSessionManager
	*AgentMetricsManager
	*AgentPersonaManager
	*AgentSecurityStateManager
}

// NewAgentStateManager creates a new AgentStateManager with sensible defaults.
func NewAgentStateManager(debug bool) *AgentStateManager {
	return &AgentStateManager{
		AgentSessionManager:        NewAgentSessionManager(debug),
		AgentMetricsManager:        NewAgentMetricsManager(),
		AgentPersonaManager:       NewAgentPersonaManager(),
		AgentSecurityStateManager: NewAgentSecurityStateManager(),
	}
}
