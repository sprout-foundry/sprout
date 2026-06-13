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

	// IsSecurityBypassApproved reports whether the user has approved
	// any external filesystem access this session. After the SP-058
	// follow-up this returns true iff at least one folder is on the
	// session allowlist — used as a coarse "user has consented to
	// external access" signal by subagent setup. Per-path decisions
	// should call IsFolderSessionAllowed instead.
	//
	// Deprecated: use IsFolderSessionAllowed(absPath) for per-path
	// checks. SetSecurityBypassApproved is gone — call
	// AddSessionAllowedFolder with the specific folder instead.
	IsSecurityBypassApproved() bool

	// IsFolderSessionAllowed reports whether absPath sits under any
	// folder the user has allowlisted for this session. Match is
	// prefix-based (path-component aware) and case-sensitive on Unix.
	IsFolderSessionAllowed(absPath string) bool

	// AddSessionAllowedFolder records that the user picked "Allow
	// this folder for the rest of the session" on the approval
	// dialog. The folder is stored after Clean()-ing and dedup'd
	// against the existing list.
	AddSessionAllowedFolder(folder string)

	// SnapshotSessionAllowedFolders returns a copy of the current
	// allowlist. Used to propagate approvals into subagents (each
	// subagent gets its own allowlist seeded from the parent's).
	SnapshotSessionAllowedFolders() []string

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
	askUserMgr              *agenttools.AskUserManager
	unsafeMode              bool
	unsafeShellMode         bool
	securityBypassMu        sync.RWMutex
	// sessionAllowedFolders holds absolute path prefixes the user
	// approved via "Allow this folder for the rest of the session"
	// on the filesystem approval dialog. Replaces the old global
	// securityBypassApproved boolean — that flag was a real safety
	// regression because approving one external path silently
	// allowed every external path for the session.
	sessionAllowedFolders   []string
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
	// Coarse signal: any folder approved this session counts.
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
