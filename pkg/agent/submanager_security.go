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

	// IsFolderSessionWriteAllowed reports whether absPath sits under
	// an allowlisted folder whose declared mode permits writes.
	// Returns true when the folder is allowlisted AND its mode is
	// empty (legacy default = read_write) OR explicitly
	// "read_write". Returns false for "read_only" entries. Used by
	// the filesystem gate to block write tools against folders the
	// workflow declared as read_only (SP-128 / B6).
	IsFolderSessionWriteAllowed(absPath string) bool

	// AddSessionAllowedFolder records that the user picked "Allow
	// this folder for the rest of the session" on the approval
	// dialog. The folder is stored after Clean()-ing and dedup'd
	// against the existing list.
	AddSessionAllowedFolder(folder string)

	// SetSessionAllowedFolderMode records the declared mode for an
	// already-allowlisted folder. Calling with mode=="" clears the
	// entry so the folder reverts to the default read_write
	// semantics. Idempotent.
	SetSessionAllowedFolderMode(folder, mode string)

	// SnapshotSessionAllowedFolders returns a copy of the current
	// allowlist. Used to propagate approvals into subagents (each
	// subagent gets its own allowlist seeded from the parent's).
	SnapshotSessionAllowedFolders() []string

	// SnapshotSessionAllowedFolderModes returns a copy of the
	// current folder-mode map. Used alongside
	// SnapshotSessionAllowedFolders when seeding subagents so the
	// declared mode survives delegation. Paths with no entry in the
	// map default to read_write (legacy semantics).
	SnapshotSessionAllowedFolderModes() map[string]string

	// RemoveSessionAllowedFolder removes the given folder from the session
	// allowlist. Returns nil if the folder was not on the list (idempotent).
	// Also removes any associated mode entry from the folder-mode map.
	// Used by the workflow step restore logic to undo per-step path grants
	// without disturbing paths that were already present before the step
	// started (SP-127 Phase 2.3).
	RemoveSessionAllowedFolder(folder string) error

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
	securityApprovalMgr *security.ApprovalManager
	askUserMgr          *agenttools.AskUserManager
	unsafeMode          bool
	unsafeShellMode     bool
	securityBypassMu    sync.RWMutex
	// sessionAllowedFolders holds absolute path prefixes the user
	// approved via "Allow this folder for the rest of the session"
	// on the filesystem approval dialog. Replaces the old global
	// securityBypassApproved boolean — that flag was a real safety
	// regression because approving one external path silently
	// allowed every external path for the session.
	sessionAllowedFolders []string
	// sessionPathModes stores the declared mode ("read_only" or
	// "read_write") for each session-allowlisted folder. An entry
	// without a mode (e.g. added by the legacy AddSessionAllowedFolder
	// call site) is treated as "read_write" — the most permissive
	// reading. Workflow-declared paths (SP-128 allowed_paths) always
	// carry a mode. Read tools don't consult this map; only write
	// tools do (via IsFolderSessionWriteAllowed).
	sessionPathModes        map[string]string
	ignoredSecurityConcerns map[string]map[string]bool
	ignoredSecurityMu       sync.RWMutex
	outputRedactor          *security.OutputRedactor
	elevationGate           *security.ElevationGate
	// webuiClientsMu protects hasActiveWebUIClients, which is written by
	// every getChatAgent call (potentially concurrently across chat
	// sessions) and read by security prompt routing during tool execution.
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

// IsFolderSessionWriteAllowed reports whether absPath sits under an
// allowlisted folder whose mode permits writes. Matches the same
// prefix-based semantics as IsFolderSessionAllowed: a folder is
// "matched" if absPath == folder or sits strictly inside it
// (component-aware). When the folder is allowlisted but has no entry
// in sessionPathModes (legacy default), writes are permitted — the
// pre-SP-128 allowlist was binary (allowed or not), and treating
// entries added by the legacy path as read_write preserves the
// existing behavior. Entries explicitly marked "read_only" block
// writes here but do NOT change IsFolderSessionAllowed — reads
// continue to succeed.
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
// already-allowlisted folder. The folder itself must already be in
// sessionAllowedFolders (call AddSessionAllowedFolder first); calling
// this for a folder not on the allowlist is a no-op so the mode
// can't widen access to a folder the user never approved. Passing
// mode=="" clears the entry and reverts the folder to default
// read_write semantics. Idempotent.
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

// SnapshotSessionAllowedFolderModes returns a copy of the
// folder-mode map. Used by the subagent creation path to propagate
// declared modes alongside the folder allowlist so a subagent
// inherits the workflow's read_only constraints. Returns an empty
// map (not nil) so callers can mutate the result without affecting
// the manager.
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
