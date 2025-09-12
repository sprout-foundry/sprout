package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	agent_config "github.com/alantheprice/ledit/pkg/agent_config"
	"github.com/alantheprice/ledit/pkg/config"
)

// MCPConfig represents the MCP configuration
type MCPConfig struct {
	Enabled      bool                       `json:"enabled"`
	Servers      map[string]MCPServerConfig `json:"servers"`
	AutoStart    bool                       `json:"auto_start"`
	AutoDiscover bool                       `json:"auto_discover"`
	Timeout      time.Duration              `json:"timeout"`
}

// DefaultMCPConfig returns the default MCP configuration
func DefaultMCPConfig() MCPConfig {
	return MCPConfig{
		Enabled:      false, // Disabled by default until user configures it
		Servers:      make(map[string]MCPServerConfig),
		AutoStart:    true,
		AutoDiscover: true,
		Timeout:      30 * time.Second,
	}
}

// GetGitHubServerConfig returns a default GitHub MCP server configuration
func GetGitHubServerConfig() MCPServerConfig {
	return MCPServerConfig{
		Name:        "github",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "", // User needs to set this
		},
	}
}

// GetGitHubServerConfigUvx returns a GitHub MCP server configuration using uvx
func GetGitHubServerConfigUvx() MCPServerConfig {
	return MCPServerConfig{
		Name:        "github-uvx",
		Command:     "uvx",
		Args:        []string{"mcp-server-github"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": "", // User needs to set this
		},
	}
}

// LoadMCPConfig loads MCP configuration from the main config
func LoadMCPConfig(cfg *config.Config) (MCPConfig, error) {
	mcpConfig := DefaultMCPConfig()

	// Try to load from config file if it exists
	configDir, err := agent_config.GetConfigDir()
	if err != nil {
		return mcpConfig, fmt.Errorf("failed to get config directory: %w", err)
	}
	configPath := filepath.Join(configDir, "mcp_config.json")
	if _, err := os.Stat(configPath); err == nil {
		data, err := os.ReadFile(configPath)
		if err != nil {
			return mcpConfig, fmt.Errorf("failed to read MCP config file: %w", err)
		}

		if err := json.Unmarshal(data, &mcpConfig); err != nil {
			return mcpConfig, fmt.Errorf("failed to parse MCP config file: %w", err)
		}
	}

	// Override with environment variables if present
	if enabled := os.Getenv("LEDIT_MCP_ENABLED"); enabled != "" {
		mcpConfig.Enabled = enabled == "true" || enabled == "1"
	}

	if autoStart := os.Getenv("LEDIT_MCP_AUTO_START"); autoStart != "" {
		mcpConfig.AutoStart = autoStart == "true" || autoStart == "1"
	}

	if autoDiscover := os.Getenv("LEDIT_MCP_AUTO_DISCOVER"); autoDiscover != "" {
		mcpConfig.AutoDiscover = autoDiscover == "true" || autoDiscover == "1"
	}

	// Check for GitHub token and enable GitHub server if available
	if githubToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); githubToken != "" && mcpConfig.AutoDiscover {
		if _, exists := mcpConfig.Servers["github"]; !exists {
			githubConfig := GetGitHubServerConfig()
			githubConfig.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = githubToken
			mcpConfig.Servers["github"] = githubConfig
			mcpConfig.Enabled = true // Auto-enable if GitHub token is available
		}
	}

	return mcpConfig, nil
}

// SaveMCPConfig saves MCP configuration to file
func SaveMCPConfig(cfg *config.Config, mcpConfig MCPConfig) error {
	configDir, err := agent_config.GetConfigDir()
	if err != nil {
		return fmt.Errorf("failed to get config directory: %w", err)
	}
	configPath := filepath.Join(configDir, "mcp_config.json")

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(configPath), 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	data, err := json.MarshalIndent(mcpConfig, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal MCP config: %w", err)
	}

	if err := os.WriteFile(configPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write MCP config file: %w", err)
	}

	return nil
}

// AddGitHubServer adds a GitHub MCP server to the configuration
func (c *MCPConfig) AddGitHubServer(githubToken string) {
	githubConfig := GetGitHubServerConfig()
	if githubToken != "" {
		githubConfig.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] = githubToken
	}
	c.Servers["github"] = githubConfig
	c.Enabled = true
}

// AddServer adds a custom MCP server to the configuration
func (c *MCPConfig) AddServer(serverConfig MCPServerConfig) error {
	if serverConfig.Name == "" {
		return fmt.Errorf("server name cannot be empty")
	}

	if serverConfig.Command == "" {
		return fmt.Errorf("server command cannot be empty")
	}

	// Set defaults
	if serverConfig.MaxRestarts == 0 {
		serverConfig.MaxRestarts = 3
	}
	if serverConfig.Timeout == 0 {
		serverConfig.Timeout = 30 * time.Second
	}

	c.Servers[serverConfig.Name] = serverConfig
	c.Enabled = true
	return nil
}

// RemoveServer removes an MCP server from the configuration
func (c *MCPConfig) RemoveServer(name string) {
	delete(c.Servers, name)

	// Disable MCP if no servers remain
	if len(c.Servers) == 0 {
		c.Enabled = false
	}
}

// ValidateConfig validates the MCP configuration
func (c *MCPConfig) ValidateConfig() error {
	if !c.Enabled {
		return nil // Nothing to validate if disabled
	}

	if len(c.Servers) == 0 {
		return fmt.Errorf("no MCP servers configured")
	}

	for name, server := range c.Servers {
		if server.Name != name {
			return fmt.Errorf("server name mismatch: key=%s, config.name=%s", name, server.Name)
		}

		if server.Command == "" {
			return fmt.Errorf("server %s: command cannot be empty", name)
		}

		if server.MaxRestarts < 0 {
			return fmt.Errorf("server %s: max_restarts cannot be negative", name)
		}

		if server.Timeout < 0 {
			return fmt.Errorf("server %s: timeout cannot be negative", name)
		}
	}

	return nil
}

// GetEnabledServers returns a list of servers that should be started
func (c *MCPConfig) GetEnabledServers() []MCPServerConfig {
	if !c.Enabled {
		return nil
	}

	var enabled []MCPServerConfig
	for _, server := range c.Servers {
		if server.AutoStart {
			enabled = append(enabled, server)
		}
	}

	return enabled
}

// HasGitHubToken checks if a GitHub token is configured for any server
func (c *MCPConfig) HasGitHubToken() bool {
	for _, server := range c.Servers {
		if server.Env != nil && server.Env["GITHUB_PERSONAL_ACCESS_TOKEN"] != "" {
			return true
		}
	}
	return false
}

// GetConfigSummary returns a summary of the current MCP configuration
func (c *MCPConfig) GetConfigSummary() map[string]interface{} {
	summary := map[string]interface{}{
		"enabled":       c.Enabled,
		"auto_start":    c.AutoStart,
		"auto_discover": c.AutoDiscover,
		"timeout":       c.Timeout.String(),
		"server_count":  len(c.Servers),
		"servers":       make(map[string]interface{}),
	}

	for name, server := range c.Servers {
		serverSummary := map[string]interface{}{
			"command":      server.Command,
			"args":         server.Args,
			"auto_start":   server.AutoStart,
			"max_restarts": server.MaxRestarts,
			"timeout":      server.Timeout.String(),
		}

		// Don't expose sensitive environment variables
		if server.Env != nil && len(server.Env) > 0 {
			envKeys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				envKeys = append(envKeys, key)
			}
			serverSummary["env_vars"] = envKeys
		}

		summary["servers"].(map[string]interface{})[name] = serverSummary
	}

	return summary
}
