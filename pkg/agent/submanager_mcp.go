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
}

// AgentMCPManager implements MCPSubManager.
type AgentMCPManager struct {
	manager      mcp.MCPManager
	toolsCacheMu sync.RWMutex // protects toolsCache
	toolsCache   []api.Tool
	stateMu      sync.RWMutex // protects initialized + initErr
	initialized  bool
	initErr      error
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
	return m.manager
}

func (m *AgentMCPManager) SetManager(mgr mcp.MCPManager) {
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
