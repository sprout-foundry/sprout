package agent

// submanager_session_tail.go — trailing scalar stores of AgentSessionManager,
// split out of submanager_session.go: config-override accessors, the current
// iteration counter, and the session-intent embedding (with defensive copies).

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

// --- Session intent embedding ---

func (m *AgentSessionManager) GetSessionIntentEmbedding() []float32 {
	if m == nil {
		return nil
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.sessionIntentEmbedding == nil {
		return nil
	}
	// Return a defensive copy
	result := make([]float32, len(m.sessionIntentEmbedding))
	copy(result, m.sessionIntentEmbedding)
	return result
}

func (m *AgentSessionManager) SetSessionIntentEmbedding(emb []float32) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if emb == nil || len(emb) == 0 {
		m.sessionIntentEmbedding = nil
		return
	}
	// Store a defensive copy
	m.sessionIntentEmbedding = make([]float32, len(emb))
	copy(m.sessionIntentEmbedding, emb)
}

func (m *AgentSessionManager) SetSessionIntentEmbeddingIfNil(emb []float32) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.sessionIntentEmbedding != nil {
		return false
	}
	if emb == nil || len(emb) == 0 {
		return false
	}
	m.sessionIntentEmbedding = make([]float32, len(emb))
	copy(m.sessionIntentEmbedding, emb)
	return true
}
