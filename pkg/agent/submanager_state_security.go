package agent

import "sync"

// AgentSecurityStateManager owns 5 sub-interfaces: CircuitBreakerStore,
// PendingStateStore, TerminationStore, ProviderErrorStore, and TraceStore.
// All fields are protected by a single RWMutex.
type AgentSecurityStateManager struct {
	mu sync.RWMutex

	// CircuitBreakerStore
	circuitBreaker *CircuitBreakerState

	// PendingStateStore
	pendingSwitchContextRefresh string
	pendingStrictSwitchNotice   string
	pendingSystemSupplement     string

	// TerminationStore
	lastRunTerminationReason string

	// ProviderErrorStore
	lastProviderError *ProviderErrorInfo

	// TraceStore
	traceSession interface{}
}

// NewAgentSecurityStateManager creates a new AgentSecurityStateManager with a
// default CircuitBreakerState.
func NewAgentSecurityStateManager() *AgentSecurityStateManager {
	return &AgentSecurityStateManager{
		circuitBreaker: &CircuitBreakerState{Actions: make(map[string]*CircuitBreakerAction)},
	}
}

// CircuitBreakerStore
func (s *AgentSecurityStateManager) GetCircuitBreaker() *CircuitBreakerState {
	if s == nil {
		return nil
	}
	return s.circuitBreaker
}
func (s *AgentSecurityStateManager) SetCircuitBreaker(cb *CircuitBreakerState) {
	if s == nil {
		return
	}
	s.circuitBreaker = cb
}

// PendingStateStore
func (s *AgentSecurityStateManager) GetPendingSwitchContextRefresh() string {
	if s == nil {
		return ""
	}
	return s.pendingSwitchContextRefresh
}
func (s *AgentSecurityStateManager) SetPendingSwitchContextRefresh(v string) {
	if s == nil {
		return
	}
	s.pendingSwitchContextRefresh = v
}
func (s *AgentSecurityStateManager) GetPendingStrictSwitchNotice() string {
	if s == nil {
		return ""
	}
	return s.pendingStrictSwitchNotice
}
func (s *AgentSecurityStateManager) SetPendingStrictSwitchNotice(v string) {
	if s == nil {
		return
	}
	s.pendingStrictSwitchNotice = v
}
func (s *AgentSecurityStateManager) GetPendingSystemSupplement() string {
	if s == nil {
		return ""
	}
	return s.pendingSystemSupplement
}
func (s *AgentSecurityStateManager) SetPendingSystemSupplement(v string) {
	if s == nil {
		return
	}
	s.pendingSystemSupplement = v
}

// TerminationStore
func (s *AgentSecurityStateManager) GetLastRunTerminationReason() string {
	if s == nil {
		return ""
	}
	return s.lastRunTerminationReason
}
func (s *AgentSecurityStateManager) SetLastRunTerminationReason(reason string) {
	if s == nil {
		return
	}
	s.lastRunTerminationReason = reason
}

// ProviderErrorStore
func (s *AgentSecurityStateManager) GetLastProviderError() *ProviderErrorInfo {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastProviderError
}
func (s *AgentSecurityStateManager) SetLastProviderError(err *ProviderErrorInfo) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastProviderError = err
}

// TraceStore
func (s *AgentSecurityStateManager) GetTraceSession() interface{} {
	if s == nil {
		return nil
	}
	return s.traceSession
}
func (s *AgentSecurityStateManager) SetTraceSession(ts interface{}) {
	if s == nil {
		return
	}
	s.traceSession = ts
}
