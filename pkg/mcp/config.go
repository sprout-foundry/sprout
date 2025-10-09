package mcp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// MCPConfig represents the MCP configuration
type MCPConfig struct {
	Enabled      bool                       `json:"enabled"`
	Servers      map[string]MCPServerConfig `json:"servers"`
	AutoStart    bool                       `json:"auto_start"`
	AutoDiscover bool                       `json:"auto_discover"`
	Timeout      time.Duration              `json:"timeout"`
}

// UnmarshalJSON implements custom JSON unmarshaling for MCPConfig to handle timeout as string or duration
func (c *MCPConfig) UnmarshalJSON(data []byte) error {
	// Create an alias to avoid infinite recursion
	type MCPConfigAlias MCPConfig

	// First try to unmarshal as the normal struct
	aux := &struct {
		Timeout interface{} `json:"timeout"`
		*MCPConfigAlias
	}{
		MCPConfigAlias: (*MCPConfigAlias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	// Handle timeout field conversion
	if aux.Timeout != nil {
		switch v := aux.Timeout.(type) {
		case string:
			// Parse string duration (backward compatibility)
			if v != "" {
				duration, err := time.ParseDuration(v)
				if err != nil {
					return fmt.Errorf("invalid timeout duration: %w", err)
				}
				c.Timeout = duration
			} else {
				c.Timeout = 30 * time.Second // default
			}
		case float64:
			// Handle JSON number (nanoseconds)
			c.Timeout = time.Duration(v)
		default:
			c.Timeout = 30 * time.Second // default fallback
		}
	} else {
		c.Timeout = 30 * time.Second // default if not present
	}

	return nil
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

// GetPlaywrightServerConfig returns a default Playwright MCP server configuration
func GetPlaywrightServerConfig() MCPServerConfig {
	return MCPServerConfig{
		Name:        "playwright",
		Command:     "npx",
		Args:        []string{"-y", "@playwright/mcp"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     60 * time.Second, // Longer timeout for browser operations
	}
}

// GetPlaywrightServerConfigUvx returns a Playwright MCP server configuration using uvx
func GetPlaywrightServerConfigUvx() MCPServerConfig {
	return MCPServerConfig{
		Name:        "playwright-uvx",
		Command:     "uvx",
		Args:        []string{"playwright-mcp-server"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     60 * time.Second, // Longer timeout for browser operations
	}
}

// getConfigDir returns the user's config directory
func getConfigDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	return filepath.Join(homeDir, ".ledit"), nil
}

// LoadMCPConfig loads MCP configuration from file
func LoadMCPConfig() (MCPConfig, error) {
	mcpConfig := DefaultMCPConfig()

	// Try to load from config file if it exists
	configDir, err := getConfigDir()
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

	// Check for Playwright server auto-discovery
	if mcpConfig.AutoDiscover {
		// Check if npx is available and playwright packages might be installed
		if _, exists := mcpConfig.Servers["playwright"]; !exists {
			// Try to detect if playwright packages are available
			if isPlaywrightAvailable() {
				mcpConfig.Servers["playwright"] = GetPlaywrightServerConfig()
				mcpConfig.Enabled = true
			}
		}
	}

	return mcpConfig, nil
}

// isPlaywrightAvailable checks if Playwright MCP packages are likely available
func isPlaywrightAvailable() bool {
	// Simple check: see if npx is available and can run playwright commands
	// This is a basic detection - in practice we'd want more robust checks
	if _, err := os.Stat("/usr/bin/npx"); err == nil {
		return true
	}
	if _, err := os.Stat("/usr/local/bin/npx"); err == nil {
		return true
	}
	
	// Check PATH
	if path := os.Getenv("PATH"); path != "" {
		paths := filepath.SplitList(path)
		for _, p := range paths {
			if _, err := os.Stat(filepath.Join(p, "npx")); err == nil {
				return true
			}
		}
	}
	
	return false
}

// SaveMCPConfig saves MCP configuration to file
func SaveMCPConfig(mcpConfig MCPConfig) error {
	configDir, err := getConfigDir()
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

	// Validate command for stdio servers, URL for HTTP servers
	if serverConfig.Type == "http" {
		if serverConfig.URL == "" {
			return fmt.Errorf("server URL cannot be empty for HTTP servers")
		}
	} else {
		// stdio server (default)
		if serverConfig.Command == "" {
			return fmt.Errorf("server command cannot be empty for stdio servers")
		}
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

		// Validate command for stdio servers, URL for HTTP servers
		if server.Type == "http" {
			if server.URL == "" {
				return fmt.Errorf("server %s: URL cannot be empty for HTTP servers", name)
			}
		} else {
			// stdio server (default)
			if server.Command == "" {
				return fmt.Errorf("server %s: command cannot be empty for stdio servers", name)
			}
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
