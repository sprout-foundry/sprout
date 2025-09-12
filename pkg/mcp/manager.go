package mcp

import (
	"context"
	"fmt"
	"sync"

	"github.com/alantheprice/ledit/pkg/utils"
)

// DefaultMCPManager implements the MCPManager interface
type DefaultMCPManager struct {
	servers map[string]MCPServer
	mutex   sync.RWMutex
	logger  *utils.Logger
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(logger *utils.Logger) *DefaultMCPManager {
	return &DefaultMCPManager{
		servers: make(map[string]MCPServer),
		logger:  logger,
	}
}

// AddServer adds a new MCP server
func (m *DefaultMCPManager) AddServer(config MCPServerConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("MCP server %s already exists", config.Name)
	}

	client := NewMCPClient(config, m.logger)
	m.servers[config.Name] = client

	if m.logger != nil {
		m.logger.LogProcessStep(fmt.Sprintf("📋 Added MCP server: %s", config.Name))
	}

	return nil
}

// RemoveServer removes an MCP server
func (m *DefaultMCPManager) RemoveServer(name string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	server, exists := m.servers[name]
	if !exists {
		return fmt.Errorf("MCP server %s not found", name)
	}

	// Stop the server if it's running
	if server.IsRunning() {
		ctx := context.Background()
		if err := server.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.LogProcessStep(fmt.Sprintf("⚠️ Failed to stop MCP server %s: %v", name, err))
			}
		}
	}

	delete(m.servers, name)

	if m.logger != nil {
		m.logger.LogProcessStep(fmt.Sprintf("🗑️ Removed MCP server: %s", name))
	}

	return nil
}

// GetServer gets an MCP server by name
func (m *DefaultMCPManager) GetServer(name string) (MCPServer, bool) {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	server, exists := m.servers[name]
	return server, exists
}

// ListServers lists all registered servers
func (m *DefaultMCPManager) ListServers() []MCPServer {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	servers := make([]MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		servers = append(servers, server)
	}

	return servers
}

// StartAll starts all configured servers
func (m *DefaultMCPManager) StartAll(ctx context.Context) error {
	m.mutex.RLock()
	servers := make([]MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		servers = append(servers, server)
	}
	m.mutex.RUnlock()

	var errors []error
	var wg sync.WaitGroup

	for _, server := range servers {
		if !server.IsRunning() && server.GetConfig().AutoStart {
			wg.Add(1)
			go func(s MCPServer) {
				defer wg.Done()
				if err := s.Start(ctx); err != nil {
					errors = append(errors, fmt.Errorf("failed to start %s: %w", s.GetName(), err))
				}
			}(server)
		}
	}

	wg.Wait()

	if len(errors) > 0 {
		errorMsg := "Failed to start some MCP servers:"
		for _, err := range errors {
			errorMsg += "\n  - " + err.Error()
		}
		return fmt.Errorf(errorMsg)
	}

	if m.logger != nil {
		runningCount := 0
		for _, server := range servers {
			if server.IsRunning() {
				runningCount++
			}
		}
		m.logger.LogProcessStep(fmt.Sprintf("🚀 Started %d MCP servers", runningCount))
	}

	return nil
}

// StopAll stops all running servers
func (m *DefaultMCPManager) StopAll(ctx context.Context) error {
	m.mutex.RLock()
	servers := make([]MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		if server.IsRunning() {
			servers = append(servers, server)
		}
	}
	m.mutex.RUnlock()

	var errors []error
	var wg sync.WaitGroup

	for _, server := range servers {
		wg.Add(1)
		go func(s MCPServer) {
			defer wg.Done()
			if err := s.Stop(ctx); err != nil {
				errors = append(errors, fmt.Errorf("failed to stop %s: %w", s.GetName(), err))
			}
		}(server)
	}

	wg.Wait()

	if len(errors) > 0 {
		errorMsg := "Failed to stop some MCP servers:"
		for _, err := range errors {
			errorMsg += "\n  - " + err.Error()
		}
		return fmt.Errorf(errorMsg)
	}

	if m.logger != nil && len(servers) > 0 {
		m.logger.LogProcessStep(fmt.Sprintf("🛑 Stopped %d MCP servers", len(servers)))
	}

	return nil
}

// GetAllTools gets all tools from all running servers
func (m *DefaultMCPManager) GetAllTools(ctx context.Context) ([]MCPTool, error) {
	m.mutex.RLock()
	servers := make([]MCPServer, 0, len(m.servers))
	for _, server := range m.servers {
		if server.IsRunning() {
			servers = append(servers, server)
		}
	}
	m.mutex.RUnlock()

	var allTools []MCPTool
	var toolsMutex sync.Mutex
	var errors []error
	var errorsMutex sync.Mutex
	var wg sync.WaitGroup

	for _, server := range servers {
		wg.Add(1)
		go func(s MCPServer) {
			defer wg.Done()

			tools, err := s.ListTools(ctx)
			if err != nil {
				errorsMutex.Lock()
				errors = append(errors, fmt.Errorf("failed to list tools from %s: %w", s.GetName(), err))
				errorsMutex.Unlock()
				return
			}

			toolsMutex.Lock()
			allTools = append(allTools, tools...)
			toolsMutex.Unlock()
		}(server)
	}

	wg.Wait()

	if len(errors) > 0 {
		// Log errors but don't fail completely - return partial results
		if m.logger != nil {
			for _, err := range errors {
				m.logger.LogProcessStep(fmt.Sprintf("⚠️ %v", err))
			}
		}
	}

	return allTools, nil
}

// CallTool calls a tool on the appropriate server
func (m *DefaultMCPManager) CallTool(ctx context.Context, serverName, toolName string, args map[string]interface{}) (*MCPToolCallResult, error) {
	server, exists := m.GetServer(serverName)
	if !exists {
		return nil, fmt.Errorf("MCP server %s not found", serverName)
	}

	if !server.IsRunning() {
		return nil, fmt.Errorf("MCP server %s is not running", serverName)
	}

	request := MCPToolCallRequest{
		Name:      toolName,
		Arguments: args,
	}

	return server.CallTool(ctx, request)
}

// AutoDiscoverTools attempts to discover and start GitHub MCP server automatically
func (m *DefaultMCPManager) AutoDiscoverGitHubServer(ctx context.Context) error {
	// Try common GitHub MCP server configurations
	githubConfigs := []MCPServerConfig{
		{
			Name:      "github",
			Command:   "npx",
			Args:      []string{"-y", "@modelcontextprotocol/server-github"},
			AutoStart: true,
			Timeout:   30,
		},
		{
			Name:      "github",
			Command:   "uvx",
			Args:      []string{"mcp-server-github"},
			AutoStart: true,
			Timeout:   30,
		},
	}

	for _, config := range githubConfigs {
		if err := m.AddServer(config); err != nil {
			continue // Try next config
		}

		// Try to start the server
		if server, exists := m.GetServer(config.Name); exists {
			if err := server.Start(ctx); err != nil {
				// Remove failed server and try next config
				m.RemoveServer(config.Name)
				continue
			}

			// Test if server is working by listing tools
			if _, err := server.ListTools(ctx); err != nil {
				// Server started but not working properly
				m.RemoveServer(config.Name)
				continue
			}

			if m.logger != nil {
				m.logger.LogProcessStep(fmt.Sprintf("✅ Auto-discovered GitHub MCP server using: %s %v", config.Command, config.Args))
			}
			return nil
		}
	}

	return fmt.Errorf("failed to auto-discover GitHub MCP server - please install @modelcontextprotocol/server-github or mcp-server-github")
}

// GetServerStats returns statistics about all servers
func (m *DefaultMCPManager) GetServerStats() map[string]interface{} {
	m.mutex.RLock()
	defer m.mutex.RUnlock()

	stats := map[string]interface{}{
		"total_servers":   len(m.servers),
		"running_servers": 0,
		"servers":         make(map[string]interface{}),
	}

	for name, server := range m.servers {
		running := server.IsRunning()
		if running {
			stats["running_servers"] = stats["running_servers"].(int) + 1
		}

		stats["servers"].(map[string]interface{})[name] = map[string]interface{}{
			"running": running,
			"config":  server.GetConfig(),
		}
	}

	return stats
}
