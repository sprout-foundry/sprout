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
