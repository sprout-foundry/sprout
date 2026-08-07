package agent

import (
	"sync"

	agenttools "github.com/sprout-foundry/sprout/pkg/agent_tools"
	"github.com/sprout-foundry/sprout/pkg/security"
)

// SecurityManager provides an interface for managing all security-related state.
type SecurityManager interface {
	GetSecurityApprovalMgr() *security.ApprovalManager
	SetApprovalMgr(mgr *security.ApprovalManager)
	SetAskUserMgr(mgr *agenttools.AskUserManager)
	GetAskUserMgr() *agenttools.AskUserManager
	SetUnsafeMode(unsafe bool)
	GetUnsafeMode() bool
	SetUnsafeShellMode(unsafe bool)
	GetUnsafeShellMode() bool
	IsSecurityBypassApproved() bool
	IsFolderSessionAllowed(absPath string) bool
	IsFolderSessionWriteAllowed(absPath string) bool
	AddSessionAllowedFolder(folder string)
	SetSessionAllowedFolderMode(folder, mode string)
	SnapshotSessionAllowedFolders() []string
	SnapshotSessionAllowedFolderModes() map[string]string
	RemoveSessionAllowedFolder(folder string) error
	IsConcernIgnored(filePath, concern string) bool
	SetConcernIgnored(filePath, concern string)
	GetOutputRedactor() *security.OutputRedactor
	GetElevationGate() *security.ElevationGate
	SetElevationGate(gate *security.ElevationGate)
	SetHasActiveWebUIClients(fn func() bool)
	HasActiveWebUIClients() bool
}

// AgentSecurityManager implements SecurityManager, holding all security-related state.
type AgentSecurityManager struct {
	securityApprovalMgr *security.ApprovalManager
	askUserMgr          *agenttools.AskUserManager
	unsafeMode          bool
	unsafeShellMode     bool
	securityBypassMu    sync.RWMutex
	sessionAllowedFolders []string
	sessionPathModes        map[string]string
	ignoredSecurityConcerns map[string]map[string]bool
	ignoredSecurityMu       sync.RWMutex
	outputRedactor          *security.OutputRedactor
	elevationGate           *security.ElevationGate
	webuiClientsMu        sync.RWMutex
	hasActiveWebUIClients func() bool
}

// NewAgentSecurityManager creates a new AgentSecurityManager with all fields initialized.
func NewAgentSecurityManager() *AgentSecurityManager {
	return &AgentSecurityManager{
		securityApprovalMgr:     security.NewApprovalManager(),
		outputRedactor:          security.NewOutputRedactor(),
		ignoredSecurityConcerns: make(map[string]map[string]bool),
		elevationGate:           security.NewElevationGate(nil),
		sessionPathModes:        make(map[string]string),
	}
}

func (m *AgentSecurityManager) GetSecurityApprovalMgr() *security.ApprovalManager {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return m.securityApprovalMgr
}

func (m *AgentSecurityManager) SetApprovalMgr(mgr *security.ApprovalManager) {
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	m.securityApprovalMgr = mgr
}

func (m *AgentSecurityManager) SetAskUserMgr(mgr *agenttools.AskUserManager) {
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	m.askUserMgr = mgr
}

func (m *AgentSecurityManager) GetAskUserMgr() *agenttools.AskUserManager {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return m.askUserMgr
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

func (m *AgentSecurityManager) SetUnsafeShellMode(unsafe bool) {
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	m.unsafeShellMode = unsafe
}

func (m *AgentSecurityManager) GetUnsafeShellMode() bool {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return m.unsafeShellMode
}

func (m *AgentSecurityManager) IsSecurityBypassApproved() bool {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	return len(m.sessionAllowedFolders) > 0
}

func (m *AgentSecurityManager) IsFolderSessionAllowed(absPath string) bool {
	if absPath == "" {
		return false
	}
	target := normalizePath(absPath)
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	for _, f := range m.sessionAllowedFolders {
		if isUnderPrefix(target, f) {
			return true
		}
	}
	return false
}

func (m *AgentSecurityManager) AddSessionAllowedFolder(folder string) {
	if folder == "" {
		return
	}
	normalized := normalizePath(folder)
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	for _, existing := range m.sessionAllowedFolders {
		if existing == normalized {
			return
		}
	}
	m.sessionAllowedFolders = append(m.sessionAllowedFolders, normalized)
}

func (m *AgentSecurityManager) SnapshotSessionAllowedFolders() []string {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	if len(m.sessionAllowedFolders) == 0 {
		return nil
	}
	out := make([]string, len(m.sessionAllowedFolders))
	copy(out, m.sessionAllowedFolders)
	return out
}

// IsFolderSessionWriteAllowed reports whether absPath sits under an
// allowlisted folder whose mode permits writes.
func (m *AgentSecurityManager) IsFolderSessionWriteAllowed(absPath string) bool {
	if absPath == "" {
		return false
	}
	target := normalizePath(absPath)
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	for _, f := range m.sessionAllowedFolders {
		if !isUnderPrefix(target, f) {
			continue
		}
		mode := m.sessionPathModes[f]
		if mode == "" || mode == "read_write" {
			return true
		}
		return false
	}
	return false
}

// SetSessionAllowedFolderMode records the declared mode for an
// already-allowlisted folder. Idempotent.
func (m *AgentSecurityManager) SetSessionAllowedFolderMode(folder, mode string) {
	normalized := normalizePath(folder)
	if normalized == "" {
		return
	}
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	found := false
	for _, f := range m.sessionAllowedFolders {
		if f == normalized {
			found = true
			break
		}
	}
	if !found {
		return
	}
	if mode == "" {
		delete(m.sessionPathModes, normalized)
		return
	}
	m.sessionPathModes[normalized] = mode
}

// SnapshotSessionAllowedFolderModes returns a copy of the folder-mode map.
func (m *AgentSecurityManager) SnapshotSessionAllowedFolderModes() map[string]string {
	m.securityBypassMu.RLock()
	defer m.securityBypassMu.RUnlock()
	out := make(map[string]string, len(m.sessionPathModes))
	for k, v := range m.sessionPathModes {
		out[k] = v
	}
	return out
}

// RemoveSessionAllowedFolder removes folder from the session allowlist.
// Returns nil (not an error) when the folder was not present — this makes
// the restore path idempotent regardless of whether the step actually
// added anything. Also removes any mode entry for the folder from
// sessionPathModes so a subsequent SetSessionAllowedFolderMode call
// can't re-establish a mode for a folder that's no longer on the allowlist.
func (m *AgentSecurityManager) RemoveSessionAllowedFolder(folder string) error {
	if folder == "" {
		return nil
	}
	normalized := normalizePath(folder)
	m.securityBypassMu.Lock()
	defer m.securityBypassMu.Unlock()
	newList := make([]string, 0, len(m.sessionAllowedFolders))
	for _, f := range m.sessionAllowedFolders {
		if f != normalized {
			newList = append(newList, f)
		}
	}
	m.sessionAllowedFolders = newList
	delete(m.sessionPathModes, normalized)
	return nil
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
	m.webuiClientsMu.Lock()
	defer m.webuiClientsMu.Unlock()
	m.hasActiveWebUIClients = fn
}

func (m *AgentSecurityManager) HasActiveWebUIClients() bool {
	m.webuiClientsMu.RLock()
	fn := m.hasActiveWebUIClients
	m.webuiClientsMu.RUnlock()
	if fn != nil {
		return fn()
	}
	return false
}
