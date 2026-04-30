package agent

import (
	"sync"

	"github.com/sprout-foundry/sprout/pkg/security"
)

// SecurityManager provides an interface for managing all security-related state.
type SecurityManager interface {
	GetSecurityApprovalMgr() *security.ApprovalManager
	SetUnsafeMode(unsafe bool)
	GetUnsafeMode() bool
	IsSecurityBypassApproved() bool
	SetSecurityBypassApproved()
	IsConcernIgnored(filePath, concern string) bool
	SetConcernIgnored(filePath, concern string)
	GetOutputRedactor() *security.OutputRedactor
	GetElevationGate() *security.ElevationGate
	SetElevationGate(gate *security.ElevationGate)
	SetHasActiveWebUIClients(fn func() bool)
	HasActiveWebUIClients() bool
}

// AgentSecurityManager implements SecurityManager, holding all security-related state
// previously managed directly by the Agent struct.
type AgentSecurityManager struct {
	securityApprovalMgr     *security.ApprovalManager
	unsafeMode              bool
	securityBypassApproved  bool
	securityBypassMu        sync.RWMutex
	ignoredSecurityConcerns map[string]map[string]bool
	ignoredSecurityMu       sync.RWMutex
	outputRedactor          *security.OutputRedactor
	elevationGate           *security.ElevationGate
	hasActiveWebUIClients   func() bool
}

// NewAgentSecurityManager creates a new AgentSecurityManager with all fields initialized.
func NewAgentSecurityManager() *AgentSecurityManager {
	return &AgentSecurityManager{
		securityApprovalMgr:     security.NewApprovalManager(),
		outputRedactor:          security.NewOutputRedactor(),
		ignoredSecurityConcerns: make(map[string]map[string]bool),
		elevationGate:           security.NewElevationGate(nil),
	}
}

func (m *AgentSecurityManager) GetSecurityApprovalMgr() *security.ApprovalManager {
	return m.securityApprovalMgr
}

func (m *AgentSecurityManager) SetUnsafeMode(unsafe bool) {
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	m.unsafeMode = unsafe
}

func (m *AgentSecurityManager) GetUnsafeMode() bool {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return m.unsafeMode
}

func (m *AgentSecurityManager) IsSecurityBypassApproved() bool {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return m.securityBypassApproved
}

func (m *AgentSecurityManager) SetSecurityBypassApproved() {
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	m.securityBypassApproved = true
}

func (m *AgentSecurityManager) IsConcernIgnored(filePath, concern string) bool {
	m.ignoredSecurityMu.RLock()
	defer m.ignoredSecurityMu.RUnlock()
	if concerns, ok := m.ignoredSecurityConcerns[filePath]; ok {
		return concerns[concern]
	}
	return false
}

func (m *AgentSecurityManager) SetConcernIgnored(filePath, concern string) {
	m.ignoredSecurityMu.Lock()
	defer m.ignoredSecurityMu.Unlock()
	if m.ignoredSecurityConcerns[filePath] == nil {
		m.ignoredSecurityConcerns[filePath] = make(map[string]bool)
	}
	m.ignoredSecurityConcerns[filePath][concern] = true
}

func (m *AgentSecurityManager) GetOutputRedactor() *security.OutputRedactor {
	return m.outputRedactor
}

func (m *AgentSecurityManager) GetElevationGate() *security.ElevationGate {
	return m.elevationGate
}

func (m *AgentSecurityManager) SetElevationGate(gate *security.ElevationGate) {
	m.elevationGate = gate
}

func (m *AgentSecurityManager) SetHasActiveWebUIClients(fn func() bool) {
	m.hasActiveWebUIClients = fn
}

func (m *AgentSecurityManager) HasActiveWebUIClients() bool {
	if m.hasActiveWebUIClients != nil {
		return m.hasActiveWebUIClients()
	}
	return false
}
