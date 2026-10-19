package mcp

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/envutil"
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
		return fmt.Errorf("unmarshal MCP config: %w", err)
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
		Enabled:      true, // Enabled by default
		Servers:      make(map[string]MCPServerConfig),
		AutoStart:    false, // Don't auto-start until user configures servers
		AutoDiscover: true,
		Timeout:      30 * time.Second,
	}
}

// getConfigDir returns the user's config directory for MCP configuration.
func getConfigDir() (string, error) {
	return envutil.ConfigDir()
}

// mcpConfigFileName is the standalone MCP config file, resolved per config
// layer. It is separate from the layered config.json/workspace.json that
// carries mcp.servers; both are merged at agent startup.
const mcpConfigFileName = "mcp_config.json"

// mcpConfigCandidates returns the config files to consult for a workspace,
// in precedence order: the workspace layer first, then the global config
// root. A workspace root that is empty or is the user's home directory
// contributes no workspace layer — $HOME is not a project, and its .sprout
// would alias the user-level state directory.
//
// The global root comes from envutil.ConfigDir(), which honors
// SPROUT_CONFIG. That is correct for the *global* half: the env var names
// the process's config root. The workspace half is explicit so an agent
// serving a workspace reads that workspace's file regardless of which
// process (CLI or daemon) is running the turn.
func mcpConfigCandidates(workspaceRoot string) []string {
	var paths []string
	if ws := strings.TrimSpace(workspaceRoot); ws != "" {
		if abs, err := filepath.Abs(ws); err == nil {
			ws = abs
		}
		if home, err := envutil.HomeDir(); err != nil || ws != home {
			paths = append(paths, filepath.Join(ws, ".sprout", mcpConfigFileName))
		}
	}
	if globalDir, err := getConfigDir(); err == nil {
		paths = append(paths, filepath.Join(globalDir, mcpConfigFileName))
	}
	return paths
}

// LoadMCPConfig loads MCP configuration from the process's config root.
func LoadMCPConfig() (MCPConfig, error) {
	return LoadMCPConfigForWorkspace("")
}

// LoadMCPConfigForWorkspace loads MCP configuration for a workspace,
// consulting the workspace layer first and the global config root second.
// Servers are merged with the workspace winning on a name collision, so a
// repo-local mcp_config.json is visible to an agent serving that repo no
// matter which process constructed it. An empty workspaceRoot reads only
// the global root, matching LoadMCPConfig.
func LoadMCPConfigForWorkspace(workspaceRoot string) (MCPConfig, error) {
	mcpConfig := DefaultMCPConfig()

	// Merge each layer in precedence order: the first layer that exists
	// supplies the scalar flags, and later layers only contribute servers
	// the higher-precedence layers did not define.
	flagsApplied := false
	for _, configPath := range mcpConfigCandidates(workspaceRoot) {
		layer, ok := readMCPConfigFile(configPath)
		if !ok {
			continue
		}
		mergeMCPConfig(&mcpConfig, layer, !flagsApplied)
		flagsApplied = true
	}

	// Override with environment variables if present
	if enabled := envutil.GetEnvSimple("MCP_ENABLED"); enabled != "" {
		mcpConfig.Enabled = enabled == "true" || enabled == "1"
	}

	if autoStart := envutil.GetEnvSimple("MCP_AUTO_START"); autoStart != "" {
		mcpConfig.AutoStart = autoStart == "true" || autoStart == "1"
	}

	if autoDiscover := envutil.GetEnvSimple("MCP_AUTO_DISCOVER"); autoDiscover != "" {
		mcpConfig.AutoDiscover = autoDiscover == "true" || autoDiscover == "1"
	}

	// Check for GitHub token and enable GitHub server if available
	if githubToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); githubToken != "" && mcpConfig.AutoDiscover {
		if _, exists := mcpConfig.Servers["github"]; !exists {
			mcpConfig.Servers["github"] = MCPServerConfig{
				Name:        "github",
				Command:     "npx",
				Args:        []string{"-y", "@modelcontextprotocol/server-github"},
				AutoStart:   false, // Don't auto-start auto-discovered servers to prevent blocking
				MaxRestarts: 3,
				Timeout:     30 * time.Second,
				Env: map[string]string{
					"GITHUB_PERSONAL_ACCESS_TOKEN": githubToken,
				},
			}
			mcpConfig.Enabled = true // Auto-enable if GitHub token is available
		}
	}

	// Check for Playwright server auto-discovery
	if mcpConfig.AutoDiscover {
		// Check if npx is available and playwright packages might be installed
		if _, exists := mcpConfig.Servers["playwright"]; !exists {
			// Try to detect if playwright packages are available
			if isPlaywrightAvailable() {
				mcpConfig.Servers["playwright"] = MCPServerConfig{
					Name:        "playwright",
					Command:     "npx",
					Args:        []string{"-y", "@playwright/mcp"},
					AutoStart:   false, // Don't auto-start auto-discovered servers to prevent blocking
					MaxRestarts: 3,
					Timeout:     60 * time.Second,
				}
				mcpConfig.Enabled = true
			}
		}
	}

	// Auto-migrate any secrets from auto-discovered servers
	migrateSecretsOnLoad(&mcpConfig)

	return mcpConfig, nil
}

// migrateSecretsOnLoad auto-migrates plaintext secrets in all server env blocks
// to the credential store and the Credentials field. This is a best-effort migration:
// errors are logged but do not fail the config load. If any secrets were migrated,
// the updated config is saved back to disk so the config file no longer contains
// raw token values in the Env block.
func migrateSecretsOnLoad(config *MCPConfig) {
	if config == nil || len(config.Servers) == 0 {
		return
	}
	totalMigrated := 0
	for name := range config.Servers {
		s := config.Servers[name]
		count, err := MigrateSecretsToCredentialsField(name, &s)
		if err != nil {
			log.Printf("[mcp-secrets] Warning: failed to migrate secrets for server %s: %v", name, err)
		}
		if count > 0 {
			config.Servers[name] = s
			totalMigrated += count
		}
	}
	if totalMigrated > 0 {
		if err := SaveMCPConfig(config); err != nil {
			log.Printf("[mcp-secrets] Warning: failed to persist migrated config: %v", err)
		} else {
			log.Printf("[mcp-secrets] Auto-migrated %d secret(s) from MCP config Env to Credentials field", totalMigrated)
		}
	}
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

// readMCPConfigFile reads and parses one mcp_config.json. A missing file is
// "no layer", not an error; a malformed file is reported so a typo in a
// workspace config is visible rather than silently ignored.
func readMCPConfigFile(path string) (MCPConfig, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return MCPConfig{}, false
	}
	var cfg MCPConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		log.Printf("[mcp] Warning: ignoring malformed MCP config %s: %v", path, err)
		return MCPConfig{}, false
	}
	migrateSecretsOnLoad(&cfg)
	return cfg, true
}

// mergeMCPConfig folds one layer into dst. dst is the higher-precedence
// layer, so its servers win on a name collision; the layer only contributes
// servers dst does not already define.
//
// applyFlags is true only for the highest-precedence layer that exists. The
// scalar flags take that layer's own value rather than OR-ing with dst:
// DefaultMCPConfig starts Enabled and AutoDiscover true, so OR-ing would
// let those defaults override an explicit "false" in a config file — which
// silently re-enables auto-discovery (and, via it, Enabled) for a user who
// turned it off.
func mergeMCPConfig(dst *MCPConfig, layer MCPConfig, applyFlags bool) {
	if dst.Servers == nil {
		dst.Servers = make(map[string]MCPServerConfig)
	}
	for name, server := range layer.Servers {
		if _, exists := dst.Servers[name]; !exists {
			dst.Servers[name] = server
		}
	}
	if !applyFlags {
		return
	}
	dst.Enabled = layer.Enabled
	dst.AutoStart = layer.AutoStart
	dst.AutoDiscover = layer.AutoDiscover
	if layer.Timeout > 0 {
		dst.Timeout = layer.Timeout
	}
}

// SaveMCPConfig saves MCP configuration to the process's config root.
func SaveMCPConfig(mcpConfig *MCPConfig) error {
	return SaveMCPConfigForWorkspace(mcpConfig, "")
}

// SaveMCPConfigForWorkspace saves MCP configuration for a workspace. With a
// workspace root it writes the workspace layer (so a repo-local config is
// what `mcp add` produces inside a repo); with an empty root it writes the
// global config root, matching SaveMCPConfig.
func SaveMCPConfigForWorkspace(mcpConfig *MCPConfig, workspaceRoot string) error {
	// Migrate secrets before persisting. This is a defense-in-depth measure:
	// migrateSecretsOnLoad (called on read) also migrates. Both paths are
	// intentional — if secrets are somehow injected between load and save,
	// they will still be caught here. Already-migrated refs are safely
	// skipped, so the double-check is idempotent and low-cost.
	for name := range mcpConfig.Servers {
		s := mcpConfig.Servers[name]
		count, err := MigrateSecretsToCredentialsField(name, &s)
		if err != nil {
			log.Printf("[mcp-secrets] Warning: failed to migrate secrets for server %s: %v", name, err)
		} else if count > 0 {
			mcpConfig.Servers[name] = s
		}
	}

	configDir, err := mcpConfigWriteDir(workspaceRoot)
	if err != nil {
		return err
	}
	configPath := filepath.Join(configDir, mcpConfigFileName)

	// Create config directory if it doesn't exist
	if err := os.MkdirAll(filepath.Dir(configPath), 0700); err != nil {
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

// mcpConfigWriteDir picks the directory a save targets: the workspace layer
// when there is one, else the global config root. Mirrors
// mcpConfigCandidates' workspace rule so a write lands where a read looks.
func mcpConfigWriteDir(workspaceRoot string) (string, error) {
	if ws := strings.TrimSpace(workspaceRoot); ws != "" {
		if abs, err := filepath.Abs(ws); err == nil {
			ws = abs
		}
		if home, err := envutil.HomeDir(); err != nil || ws != home {
			return filepath.Join(ws, ".sprout"), nil
		}
	}
	return getConfigDir()
}

// MCPConfigSourcePath returns the file a workspace's MCP config is read
// from — the workspace layer when it exists, else the global root — so a
// caller can tell the user which config was consulted.
func MCPConfigSourcePath(workspaceRoot string) string {
	candidates := mcpConfigCandidates(workspaceRoot)
	for _, path := range candidates {
		if _, err := os.Stat(path); err == nil {
			return path
		}
	}
	if len(candidates) > 0 {
		return candidates[0]
	}
	return ""
}

// AddGitHubServer adds a GitHub MCP server to the configuration
func (c *MCPConfig) AddGitHubServer(githubToken string) {
	if c.Servers == nil {
		c.Servers = make(map[string]MCPServerConfig)
	}
	server := MCPServerConfig{
		Name:        "github",
		Command:     "npx",
		Args:        []string{"-y", "@modelcontextprotocol/server-github"},
		AutoStart:   true,
		MaxRestarts: 3,
		Timeout:     30 * time.Second,
		Env: map[string]string{
			"GITHUB_PERSONAL_ACCESS_TOKEN": githubToken,
		},
	}
	if _, err := MigrateSecretsToCredentialsField("github", &server); err != nil {
		log.Printf("[mcp-secrets] Warning: failed to migrate GitHub token: %v", err)
	}
	c.Servers["github"] = server
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
	// Clean up stored credentials for this server from both Env and Credentials
	if server, exists := c.Servers[name]; exists {
		// Clean up from Env
		if server.Env != nil {
			for envVarName, value := range server.Env {
				if IsSecretRef(value) {
					key := CredentialKey(name, envVarName)
					if err := credentials.DeleteFromActiveBackend(key); err != nil {
						log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
					}
				}
			}
		}
		// Clean up from Credentials field
		if server.Credentials != nil {
			for envVarName, value := range server.Credentials {
				if IsSecretRef(value) {
					_, actualEnvVarName, ok := ParseSecretRef(value)
					if !ok {
						actualEnvVarName = envVarName
					}
					key := CredentialKey(name, actualEnvVarName)
					if err := credentials.DeleteFromActiveBackend(key); err != nil {
						log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
					}
				}
			}
		}
	}

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
