package mcp

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sprout-foundry/sprout/pkg/utils"
)

// DefaultMCPManager implements the MCPManager interface
type DefaultMCPManager struct {
	servers map[string]MCPServer
	mutex   sync.RWMutex
	logger  *utils.Logger
	// serverFactory is used for testing to inject mock servers
	serverFactory func(config MCPServerConfig, logger *utils.Logger) MCPServer
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(logger *utils.Logger) *DefaultMCPManager {
	m := &DefaultMCPManager{
		servers: make(map[string]MCPServer),
		logger:  logger,
	}
	// Set default server factory
	m.serverFactory = m.defaultServerFactory
			return m
}

// addServerUnlocked is a helper method that adds a server assuming the caller already holds the lock
// This is used internally to avoid lock/unlock cycles
func (m *DefaultMCPManager) addServerUnlocked(config MCPServerConfig) error {
	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("MCP server %s already exists", config.Name)
	}
	m.servers[config.Name] = m.serverFactory(config, m.logger)
	return nil
}

// defaultServerFactory creates a server instance based on config type
func (m *DefaultMCPManager) defaultServerFactory(config MCPServerConfig, logger *utils.Logger) MCPServer {
	if config.Type == "http" {
		return NewMCPHTTPClient(config, logger)
	}
	// Default to stdio client for backwards compatibility
	return NewMCPClient(config, logger)
}

// AddServer adds a new MCP server
func (m *DefaultMCPManager) AddServer(config MCPServerConfig) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()

	if _, exists := m.servers[config.Name]; exists {
		return fmt.Errorf("MCP server %s already exists", config.Name)
	}

	// Use helper method that assumes caller holds lock
	m.addServerUnlocked(config)

	if m.logger != nil {
		serverType := config.Type
		if serverType == "" {
			serverType = "stdio"
		}
		m.logger.LogProcessStep(fmt.Sprintf("[list] Added MCP server: %s (%s)", config.Name, serverType))
	}

	return nil
}

// RemoveServer removes an MCP server
func (m *DefaultMCPManager) RemoveServer(name string) error {
	// Extract and deregister the server under the lock, then stop it
	// outside the lock. server.Stop() can block up to 5s waiting for
	// graceful shutdown; holding m.mutex across that freezes all
	// ListServers/GetServer calls, causing UI hangs.
	m.mutex.Lock()
	server, exists := m.servers[name]
	if !exists {
		m.mutex.Unlock()
		return fmt.Errorf("MCP server %s not found", name)
	}
	delete(m.servers, name)
	m.mutex.Unlock()

	// Stop the server if it's running — no longer holds the global lock.
	if server.IsRunning() {
		ctx := context.Background()
		if err := server.Stop(ctx); err != nil {
			if m.logger != nil {
				m.logger.LogProcessStep(fmt.Sprintf("[WARN] Failed to stop MCP server %s: %v", name, err))
			}
		}
	}

	if m.logger != nil {
		m.logger.LogProcessStep(fmt.Sprintf("[x] Removed MCP server: %s", name))
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

	var (
		errs  []error
		errMu sync.Mutex
		wg    sync.WaitGroup
	)

	for _, server := range servers {
		if !server.IsRunning() && server.GetConfig().AutoStart {
			wg.Add(1)
			go func(s MCPServer) {
				defer wg.Done()
				if err := s.Start(ctx); err != nil {
					errMu.Lock()
					errs = append(errs, fmt.Errorf("failed to start %s: %w", s.GetName(), err))
					errMu.Unlock()
				}
			}(server)
		}
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("failed to start some MCP servers: %w", errors.Join(errs...))
	}

	if m.logger != nil {
		runningCount := 0
		for _, server := range servers {
			if server.IsRunning() {
				runningCount++
			}
		}
		m.logger.LogProcessStep(fmt.Sprintf("[>>] Started %d MCP servers", runningCount))
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

	var (
		errs  []error
		errMu sync.Mutex
		wg    sync.WaitGroup
	)

	for _, server := range servers {
		wg.Add(1)
		go func(s MCPServer) {
			defer wg.Done()
			if err := s.Stop(ctx); err != nil {
				errMu.Lock()
				errs = append(errs, fmt.Errorf("failed to stop %s: %w", s.GetName(), err))
				errMu.Unlock()
			}
		}(server)
	}

	wg.Wait()

	if len(errs) > 0 {
		return fmt.Errorf("failed to stop some MCP servers: %w", errors.Join(errs...))
	}

	if m.logger != nil && len(servers) > 0 {
		m.logger.LogProcessStep(fmt.Sprintf("[STOP] Stopped %d MCP servers", len(servers)))
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
	var errs []error
	var errorsMutex sync.Mutex
	var wg sync.WaitGroup

	for _, server := range servers {
		wg.Add(1)
		go func(s MCPServer) {
			defer wg.Done()

			tools, err := s.ListTools(ctx)
			if err != nil {
				errorsMutex.Lock()
				errs = append(errs, fmt.Errorf("failed to list tools from %s: %w", s.GetName(), err))
				errorsMutex.Unlock()
				return
			}

			toolsMutex.Lock()
			allTools = append(allTools, tools...)
			toolsMutex.Unlock()
		}(server)
	}

	wg.Wait()

	if len(errs) > 0 {
		// Log errors but don't fail completely - return partial results
		if m.logger != nil {
			for _, err := range errs {
				m.logger.LogProcessStep(fmt.Sprintf("[WARN] %v", err))
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
			Timeout:   30 * time.Second, // Fixed: was 30 (nanoseconds), now 30 seconds
		},
		{
			Name:      "github",
			Command:   "uvx",
			Args:      []string{"mcp-server-github"},
			AutoStart: true,
			Timeout:   30 * time.Second, // Fixed: was 30 (nanoseconds), now 30 seconds
		},
	}

	for _, config := range githubConfigs {
		if err := m.AddServer(config); err != nil {
			continue // Try next config
		}

		// Get server reference atomically after AddServer returns
		server, exists := m.GetServer(config.Name)

		// Try to start the server
		if exists {
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
				m.logger.LogProcessStep(fmt.Sprintf("[OK] Auto-discovered GitHub MCP server using: %s %v", config.Command, config.Args))
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
