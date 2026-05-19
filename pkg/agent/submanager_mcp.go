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
	DoInit(f func())
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
//
// No reverse ordering exists. debugLogMutex is never held before calling
// getMCPTools. initMu is never held while calling into mcp.Manager (the
// Manager's internal mutexes are acquired outside of initMu). This prevents
// lock-order inversions and deadlocks.
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
// Within getMCPTools(), the initialization is performed using sync.Once
// (via DoInit) WITHOUT holding initMu. The slow initializeMCP() call,
// which can take seconds when starting subprocesses, runs outside of
// any lock. Only the final cache store requires a brief write lock.
// Reads use RLock for fast-path cache access. This pattern eliminates
// deadlocks under high concurrency.
//
// The initializeMCP() function accesses the mcp.MCPManager (manager field)
// and calls methods on it that acquire mcp.Manager's internal mutexes, but
// does NOT try to re-acquire initMu, avoiding recursive locking.
type AgentMCPManager struct {
	manager     mcp.MCPManager
	toolsCache  []api.Tool
	initialized bool
	initErr     error
	initMu      sync.RWMutex
	initOnce    sync.Once
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

// DoInit executes the given function exactly once using sync.Once.
// This is used for MCP initialization without holding initMu during
// the slow initializeMCP() call.
func (m *AgentMCPManager) DoInit(f func()) {
	m.initOnce.Do(f)
}
