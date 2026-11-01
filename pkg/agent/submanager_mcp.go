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
	// IsInherited reports whether this sub-manager shares another agent's
	// MCP manager instead of owning one. An inherited sub-manager must not
	// stop servers on shutdown: the servers belong to the parent, which is
	// still using them.
	IsInherited() bool
	SetInherited(inherited bool)
}

// AgentMCPManager implements MCPSubManager.
type AgentMCPManager struct {
	// managerMu guards manager. An inherited sub-manager shares its pointer
	// with the parent agent, so a read here can race with a write on another
	// agent — the pointer decides which server set a tool call reaches.
	managerMu    sync.RWMutex
	manager      mcp.MCPManager
	toolsCacheMu sync.RWMutex // protects toolsCache
	toolsCache   []api.Tool
	stateMu      sync.RWMutex // protects initialized + initErr + inherited
	initialized  bool
	initErr      error
	inherited    bool
	initMu       sync.Mutex
}

// NewAgentMCPManager creates a new AgentMCPManager with default values.
func NewAgentMCPManager() *AgentMCPManager {
	return &AgentMCPManager{
		manager:    mcp.NewMCPManager(nil),
		toolsCache: nil,
	}
}

func (m *AgentMCPManager) GetManager() mcp.MCPManager {
	m.managerMu.RLock()
	defer m.managerMu.RUnlock()
	return m.manager
}

func (m *AgentMCPManager) SetManager(mgr mcp.MCPManager) {
	m.managerMu.Lock()
	defer m.managerMu.Unlock()
	m.manager = mgr
}

func (m *AgentMCPManager) GetToolsCache() []api.Tool {
	m.toolsCacheMu.RLock()
	defer m.toolsCacheMu.RUnlock()
	return m.toolsCache
}

func (m *AgentMCPManager) SetToolsCache(tools []api.Tool) {
	m.toolsCacheMu.Lock()
	defer m.toolsCacheMu.Unlock()
	m.toolsCache = tools
}

func (m *AgentMCPManager) IsInitialized() bool {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.initialized
}

func (m *AgentMCPManager) SetInitialized(initialized bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.initialized = initialized
}

func (m *AgentMCPManager) GetInitError() error {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.initErr
}

func (m *AgentMCPManager) SetInitError(err error) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.initErr = err
}

func (m *AgentMCPManager) LockInit() {
	m.initMu.Lock()
}

func (m *AgentMCPManager) UnlockInit() {
	m.initMu.Unlock()
}

// IsInherited reports whether the manager is shared with another agent.
func (m *AgentMCPManager) IsInherited() bool {
	m.stateMu.RLock()
	defer m.stateMu.RUnlock()
	return m.inherited
}

// SetInherited records that the manager is shared with another agent.
func (m *AgentMCPManager) SetInherited(inherited bool) {
	m.stateMu.Lock()
	defer m.stateMu.Unlock()
	m.inherited = inherited
}
