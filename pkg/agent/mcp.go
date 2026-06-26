package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// initializeMCP initializes MCP configuration and starts servers if needed
func (a *Agent) initializeMCP() error {
	if a.configManager == nil {
		if a.debug {
			a.Logger().Debug("Config manager is nil, skipping MCP initialization")
		}
		return nil
	}

	config := a.configManager.GetConfig()
	if config == nil {
		if a.debug {
			a.Logger().Debug("Config is nil, skipping MCP initialization")
		}
		return nil
	}

	ctx := context.Background()

	// First, load servers from the legacy mcp_config.json file (if it exists)
	// This ensures servers added via CLI commands are available
	// Load BEFORE checking if MCP is enabled - legacy config may have servers
	var legacyEnabled bool
	if legacyMCPConfig, err := mcp.LoadMCPConfig(); err == nil {
		legacyEnabled = len(legacyMCPConfig.Servers) > 0 && legacyMCPConfig.Enabled
		for name, serverConfig := range legacyMCPConfig.Servers {
			// Only add if not already in main config
			if _, exists := config.MCP.Servers[name]; !exists {
				mcpServer := mcp.MCPServerConfig{
					Name:        serverConfig.Name,
					Type:        serverConfig.Type,
					Command:     serverConfig.Command,
					Args:        serverConfig.Args,
					URL:         serverConfig.URL,
					Env:         serverConfig.Env,
					Credentials: serverConfig.Credentials,
					WorkingDir:  serverConfig.WorkingDir,
					Timeout:     serverConfig.Timeout,
					AutoStart:   serverConfig.AutoStart,
					MaxRestarts: serverConfig.MaxRestarts,
				}
				if err := a.mcpSub.GetManager().AddServer(mcpServer); err != nil {
					if a.debug {
						a.Logger().Warn("Failed to add legacy MCP server %s: %v", name, err)
					}
				} else if a.debug {
					a.Logger().Info("Added legacy MCP server: %s", name)
				}
			}
		}
	}

	// Check if MCP should be enabled - either from main config or legacy config
	mcpEnabled := config.MCP.Enabled || legacyEnabled
	if !mcpEnabled {
		if a.debug {
			a.Logger().Debug("MCP is disabled in configuration")
		}
		return nil
	}

	// Add configured servers from main config
	for name, serverConfig := range config.MCP.Servers {
		mcpServer := mcp.MCPServerConfig{
			Name:        serverConfig.Name,
			Type:        serverConfig.Type,
			Command:     serverConfig.Command,
			Args:        serverConfig.Args,
			URL:         serverConfig.URL,
			Env:         serverConfig.Env,
			Credentials: serverConfig.Credentials,
			WorkingDir:  serverConfig.WorkingDir,
			Timeout:     serverConfig.Timeout,
			AutoStart:   serverConfig.AutoStart,
			MaxRestarts: serverConfig.MaxRestarts,
		}

		if err := a.mcpSub.GetManager().AddServer(mcpServer); err != nil {
			if a.debug {
				a.Logger().Warn("Failed to add MCP server %s: %v", name, err)
			}
			continue
		}
	}

	// Auto-start servers if configured (either from main config or legacy config)
	// If legacy config has enabled servers, start them regardless of main config settings
	shouldAutoStart := config.MCP.AutoStart || legacyEnabled
	if shouldAutoStart {
		if err := a.mcpSub.GetManager().StartAll(ctx); err != nil {
			return fmt.Errorf("failed to start MCP servers: %w", err)
		}

		if a.debug {
			tools, _ := a.mcpSub.GetManager().GetAllTools(ctx)
			a.Logger().Info("MCP initialized with %d tools available", len(tools))
		}
	}

	// Auto-discover GitHub server if PAT token is available.
	// Note: This only applies to PAT-based (local) GitHub MCP servers.
	// The remote OAuth server (https://api.githubcopilot.com/mcp/) is configured
	// explicitly via `sprout mcp add` and is NOT auto-discovered here, since it
	// requires a Copilot seat and uses OAuth rather than a PAT.
	if config.MCP.AutoDiscover {
		if githubToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN"); githubToken != "" {
			if _, exists := config.MCP.Servers["github"]; !exists {
				// Try npx version first
				githubServer := mcp.MCPServerConfig{
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

				// Migrate secrets BEFORE adding to manager
				if _, err := mcp.MigrateEnvSecretsFromServer("github", &githubServer); err != nil {
					a.Logger().Warn("[mcp-secrets] failed to migrate secrets for auto-discovered GitHub server: %v", err)
				}

				if err := a.mcpSub.GetManager().AddServer(githubServer); err == nil {
					if config.MCP.AutoStart {
						if err := a.mcpSub.GetManager().StartAll(ctx); err != nil {
							if a.debug {
								a.Logger().Warn("Failed to start GitHub MCP server (npx): %v", err)
							}
						} else if a.debug {
							a.Logger().Info("GitHub MCP server auto-discovered and started (npx)")
						}
					}
				}
			}
		}
	}

	return nil
}

// RefreshMCPTools refreshes the MCP tools cache
func (a *Agent) RefreshMCPTools() error {
	// Clear cache with mutex protection to avoid race conditions
	a.mcpSub.LockInit()
	a.mcpSub.SetToolsCache(nil)    // Clear cache to force reload
	a.mcpSub.SetInitialized(false) // Mark as needing reinitialization
	a.mcpSub.UnlockInit()

	tools := a.getMCPTools()
	if a.debug {
		a.Logger().Info("Refreshed MCP tools: %d available", len(tools))
	}
	return nil
}

// getMCPTools retrieves all available MCP tools and converts them to agent tool format (with caching)
func (a *Agent) getMCPTools() []api.Tool {
	if a.mcpSub == nil || a.mcpSub.GetManager() == nil {
		if a.debug {
			a.Logger().Debug("[WARN] Warning: MCP manager is nil\n")
		}
		return nil
	}

	// Initialize MCP on first use (lazy loading for better startup performance)
	a.mcpSub.LockInit()
	defer a.mcpSub.UnlockInit()

	if !a.mcpSub.IsInitialized() {
		if a.debug {
			a.Logger().Debug("[cfg] Initializing MCP (first use)...\n")
		}
		if err := a.initializeMCP(); err != nil {
			// Non-fatal - MCP is optional
			a.mcpSub.SetInitError(err)
			if a.debug {
				a.Logger().Debug("[WARN] MCP initialization failed: %v\n", err)
			}
			// Don't set mcpInitialized to allow retry
			a.mcpSub.SetInitialized(false)
		} else {
			// Success - mark as initialized
			a.mcpSub.SetInitialized(true)
			a.mcpSub.SetInitError(nil)
			if a.debug {
				a.Logger().Debug("[OK] MCP initialized\n")
			}
		}
	}

	// Return nil if not initialized
	if !a.mcpSub.IsInitialized() {
		return nil
	}

	// Return cached tools if available
	if a.mcpSub.GetToolsCache() != nil {
		if a.debug {
			a.Logger().Debug("[tool] Using cached MCP tools: %d\n", len(a.mcpSub.GetToolsCache()))
		}
		return a.mcpSub.GetToolsCache()
	}

	ctx := context.Background()
	mcpTools, err := a.mcpSub.GetManager().GetAllTools(ctx)
	if err != nil {
		if a.debug {
			a.Logger().Debug("[WARN] Warning: Failed to get MCP tools: %v\n", err)
		}
		return nil
	}

	if a.debug {
		a.Logger().Debug("[tool] Loading %d MCP tools from manager (first time)\n", len(mcpTools))
	}

	var agentTools []api.Tool
	for _, mcpTool := range mcpTools {
		// Create wrapper and convert to agent tool format
		wrapper := mcp.NewMCPToolWrapper(mcpTool, a.mcpSub.GetManager())
		agentTool := wrapper.ToAgentTool()

		// Convert to api.Tool format
		apiTool := api.Tool{
			Type:     agentTool.Type,
			Function: agentTool.Function,
		}
		agentTools = append(agentTools, apiTool)
	}

	// Cache the tools
	a.mcpSub.SetToolsCache(agentTools)

	return agentTools
}

// isValidMCPTool checks if the tool name is a valid MCP tool
func (a *Agent) isValidMCPTool(toolName string) bool {
	if !strings.HasPrefix(toolName, "mcp_") {
		return false
	}

	// Get MCP tools
	mcpTools := a.getMCPTools()
	for _, tool := range mcpTools {
		if tool.Function.Name == toolName {
			return true
		}
	}

	return false
}

// executeMCPTool executes an MCP tool
func (a *Agent) executeMCPTool(toolName string, args map[string]interface{}) (string, error) {
	// Remove mcp_ prefix and parse server:tool format
	toolName = strings.TrimPrefix(toolName, "mcp_")
	parts := strings.SplitN(toolName, "_", 2)
	if len(parts) != 2 {
		return "", fmt.Errorf("invalid MCP tool name format: %s", toolName)
	}

	serverName := parts[0]
	actualToolName := parts[1]

	ctx := context.Background()
	result, err := a.mcpSub.GetManager().CallTool(ctx, serverName, actualToolName, args)
	if err != nil {
		return "", fmt.Errorf("failed to call MCP tool %s/%s: %w", serverName, actualToolName, err)
	}

	// Convert result to string
	return formatMCPResult(result), nil
}

// formatMCPResult formats an MCP result for display
func formatMCPResult(result *mcp.MCPToolCallResult) string {
	if result == nil {
		return "No result"
	}

	var output strings.Builder
	for _, content := range result.Content {
		switch content.Type {
		case "text":
			output.WriteString(content.Text)
			output.WriteString("\n")
		case "resource":
			output.WriteString(fmt.Sprintf("[Resource: %s]\n", content.Data))
		default:
			output.WriteString(fmt.Sprintf("[%s: %s]\n", content.Type, content.Text))
		}
	}

	return strings.TrimSpace(output.String())
}

// handleMCPToolsCommand handles the mcp_tools meta command
func (a *Agent) handleMCPToolsCommand(args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok {
		return "", agenterrors.NewInvalidInputError("mcp_tools command requires 'action' parameter", nil)
	}

	ctx := context.Background()

	switch action {
	case "list":
		tools := a.getMCPTools()
		if len(tools) == 0 {
			return "No MCP tools available", nil
		}

		var output strings.Builder
		output.WriteString(fmt.Sprintf("Available MCP tools (%d):\n", len(tools)))
		for _, tool := range tools {
			output.WriteString(fmt.Sprintf("- %s: %s\n", tool.Function.Name, tool.Function.Description))
		}
		return output.String(), nil

	case "refresh":
		a.mcpSub.SetToolsCache(nil)
		tools := a.getMCPTools()
		return fmt.Sprintf("Refreshed MCP tools. %d tools available.", len(tools)), nil

	case "status":
		servers := a.mcpSub.GetManager().ListServers()
		var output strings.Builder
		output.WriteString("MCP Server Status:\n")
		for _, server := range servers {
			status := "stopped"
			if server.IsRunning() {
				status = "running"
			}
			output.WriteString(fmt.Sprintf("- %s: %s\n", server.GetName(), status))
		}
		return output.String(), nil

	case "start":
		// For now, start all servers
		if err := a.mcpSub.GetManager().StartAll(ctx); err != nil {
			return "", fmt.Errorf("failed to start servers: %w", err)
		}
		return "Started all MCP servers", nil

	case "stop":
		// For now, stop all servers
		if err := a.mcpSub.GetManager().StopAll(ctx); err != nil {
			return "", fmt.Errorf("failed to stop servers: %w", err)
		}
		return "Stopped all MCP servers", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}
