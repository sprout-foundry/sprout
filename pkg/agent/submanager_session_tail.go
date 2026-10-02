package agent

// submanager_session_tail.go — trailing scalar stores of AgentSessionManager,
// split out of submanager_session.go: config-override accessors and the
// current iteration counter.

// --- Config overrides ---

func (m *AgentSessionManager) GetConfigOverrides() map[string]interface{} {
	if m == nil {
		return nil
	}
	return m.configOverrides
}

func (m *AgentSessionManager) SetConfigOverrides(overrides map[string]interface{}) {
	if m == nil {
		return
	}
	m.configOverrides = overrides
}

// --- Current iteration ---

func (m *AgentSessionManager) GetCurrentIteration() int {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.currentIteration
}

func (m *AgentSessionManager) SetCurrentIteration(iter int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.currentIteration = iter
}
