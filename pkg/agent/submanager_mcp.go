package agent

import (
	"sync"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// MCPSubManager manages all MCP-related state for an Agent.
type MCPSubManager interface {
	GetManager() mcp.MCPManager
	SetManager(mgr mcp.MCPManager)
	GetToolsCache() []api.Tool
	SetToolsCache(tools []api.Tool)
	IsInitialized() bool
	SetInitialized(initialized bool)
	GetInitError() error
	SetInitError(err error)
	LockInit()
	UnlockInit()
	RLockInit()
	RUnlockInit()
}

// AgentMCPManager implements MCPSubManager.
//
// LOCK-ORDER INVARIANT
//
// AgentMCPManager uses a single internal lock (initMu, a sync.RWMutex) to
// protect the initialization state (initialized, initErr, toolsCache).
// The lock is exposed through the MCPSubManager interface as LockInit,
// UnlockInit, RLockInit, and RUnlockInit.
//
// Lock ordering:
//
//   initMu → debugLogMutex (via a.debugLog() in getMCPTools)
//   initMu → mcp.Manager.mutex (via initializeMCP → AddServer/StartAll/GetAllTools)
//
// No reverse ordering exists. debugLogMutex is never held before calling
// getMCPTools, and mcp.Manager's internal mutexes are never held before calling
// getMCPTools or RefreshMCPTools. This prevents lock-order inversions and
// deadlocks.
//
// Direct callers of getMCPTools():
//   - getOptimizedToolDefinitions()
//   - isValidMCPTool()
//   - handleMCPToolsCommand()
//
// Direct callers of RefreshMCPTools():
//   - handleMCPToolsCommand()
//   - github_setup_prompt.go
//
// Within getMCPTools(), the read lock (RLockInit) is always released
// before the write lock (LockInit) is acquired. The pattern is:
//   1. RLockInit; check cache; RUnlockInit
//   2. LockInit; double-check; operate; UnlockInit (deferred)
// The RLock and Lock are never nested, preventing self-deadlock.
//
// The initializeMCP() function is called while initMu is held (write).
// It accesses the mcp.MCPManager (manager field) and calls methods on it
// that acquire mcp.Manager's internal mutexes, but does NOT try to
// re-acquire initMu, avoiding recursive locking.
type AgentMCPManager struct {
	manager     mcp.MCPManager
	toolsCache  []api.Tool
	initialized bool
	initErr     error
	initMu      sync.RWMutex
}

// NewAgentMCPManager creates a new AgentMCPManager with default values.
func NewAgentMCPManager() *AgentMCPManager {
	return &AgentMCPManager{
		manager:    mcp.NewMCPManager(nil),
		toolsCache: nil,
	}
}

func (m *AgentMCPManager) GetManager() mcp.MCPManager {
	return m.manager
}

func (m *AgentMCPManager) SetManager(mgr mcp.MCPManager) {
	m.manager = mgr
}

func (m *AgentMCPManager) GetToolsCache() []api.Tool {
	return m.toolsCache
}

func (m *AgentMCPManager) SetToolsCache(tools []api.Tool) {
	m.toolsCache = tools
}

func (m *AgentMCPManager) IsInitialized() bool {
	return m.initialized
}

func (m *AgentMCPManager) SetInitialized(initialized bool) {
	m.initialized = initialized
}

func (m *AgentMCPManager) GetInitError() error {
	return m.initErr
}

func (m *AgentMCPManager) SetInitError(err error) {
	m.initErr = err
}

func (m *AgentMCPManager) LockInit() {
	m.initMu.Lock()
}

func (m *AgentMCPManager) UnlockInit() {
	m.initMu.Unlock()
}

func (m *AgentMCPManager) RLockInit() {
	m.initMu.RLock()
}

func (m *AgentMCPManager) RUnlockInit() {
	m.initMu.RUnlock()
}
