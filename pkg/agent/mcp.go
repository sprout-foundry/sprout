package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	api "github.com/alantheprice/ledit/pkg/agent_api"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// initializeMCP initializes MCP configuration and starts servers if needed
func (a *Agent) initializeMCP() error {
	if a.configManager == nil {
		if a.debug {
			fmt.Println("🔧 Config manager is nil, skipping MCP initialization")
		}
		return nil
	}

	config := a.configManager.GetConfig()
	if config == nil {
		if a.debug {
			fmt.Println("🔧 Config is nil, skipping MCP initialization")
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
					WorkingDir:  serverConfig.WorkingDir,
					Timeout:     serverConfig.Timeout,
					AutoStart:   serverConfig.AutoStart,
					MaxRestarts: serverConfig.MaxRestarts,
				}
				if err := a.mcpManager.AddServer(mcpServer); err != nil {
					if a.debug {
						fmt.Printf("⚠️  Warning: Failed to add legacy MCP server %s: %v\n", name, err)
					}
				} else if a.debug {
					fmt.Printf("🔧 Added legacy MCP server: %s\n", name)
				}
			}
		}
	}

	// Check if MCP should be enabled - either from main config or legacy config
	mcpEnabled := config.MCP.Enabled || legacyEnabled
	if !mcpEnabled {
		if a.debug {
			fmt.Println("🔧 MCP is disabled in configuration")
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
			WorkingDir:  serverConfig.WorkingDir,
			Timeout:     serverConfig.Timeout,
			AutoStart:   serverConfig.AutoStart,
			MaxRestarts: serverConfig.MaxRestarts,
		}

		if err := a.mcpManager.AddServer(mcpServer); err != nil {
			if a.debug {
				fmt.Printf("⚠️  Warning: Failed to add MCP server %s: %v\n", name, err)
			}
			continue
		}
	}

	// Auto-start servers if configured (either from main config or legacy config)
	// If legacy config has enabled servers, start them regardless of main config settings
	shouldAutoStart := config.MCP.AutoStart || legacyEnabled
	if shouldAutoStart {
		if err := a.mcpManager.StartAll(ctx); err != nil {
			return fmt.Errorf("failed to start MCP servers: %w", err)
		}

		if a.debug {
			tools, _ := a.mcpManager.GetAllTools(ctx)
			fmt.Printf("✅ MCP initialized with %d tools available\n", len(tools))
		}
	}

	// Auto-discover GitHub server if token is available
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

				if err := a.mcpManager.AddServer(githubServer); err == nil {
					if config.MCP.AutoStart {
						if err := a.mcpManager.StartAll(ctx); err != nil {
							if a.debug {
								fmt.Printf("⚠️  Failed to start GitHub MCP server (npx): %v\n", err)
							}
						} else if a.debug {
							fmt.Println("✅ GitHub MCP server auto-discovered and started (npx)")
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
	a.mcpInitMu.Lock()
	a.mcpToolsCache = nil    // Clear cache to force reload
	a.mcpInitialized = false // Mark as needing reinitialization
	a.mcpInitMu.Unlock()

	tools := a.getMCPTools()
	if a.debug {
		fmt.Printf("🔧 Refreshed MCP tools: %d available\n", len(tools))
	}
	return nil
}

// getMCPTools retrieves all available MCP tools and converts them to agent tool format (with caching)
func (a *Agent) getMCPTools() []api.Tool {
	if a.mcpManager == nil {
		if a.debug {
			a.debugLog("⚠️  Warning: MCP manager is nil\n")
		}
		return nil
	}

	// Initialize MCP on first use (lazy loading for better startup performance)
	a.mcpInitMu.Lock()
	defer a.mcpInitMu.Unlock()

	if !a.mcpInitialized {
		if a.debug {
			a.debugLog("⚙️  Initializing MCP (first use)...\n")
		}
		if err := a.initializeMCP(); err != nil {
			// Non-fatal - MCP is optional
			a.mcpInitErr = err
			if a.debug {
				a.debugLog("⚠️  MCP initialization failed: %v\n", err)
			}
			// Don't set mcpInitialized to allow retry
			a.mcpInitialized = false
		} else {
			// Success - mark as initialized
			a.mcpInitialized = true
			a.mcpInitErr = nil
			if a.debug {
				a.debugLog("✅ MCP initialized\n")
			}
		}
	}

	// Return nil if not initialized
	if !a.mcpInitialized {
		return nil
	}

	// Return cached tools if available
	if a.mcpToolsCache != nil {
		if a.debug {
			a.debugLog("🔧 Using cached MCP tools: %d\n", len(a.mcpToolsCache))
		}
		return a.mcpToolsCache
	}

	ctx := context.Background()
	mcpTools, err := a.mcpManager.GetAllTools(ctx)
	if err != nil {
		if a.debug {
			a.debugLog("⚠️  Warning: Failed to get MCP tools: %v\n", err)
		}
		return nil
	}

	if a.debug {
		a.debugLog("🔧 Loading %d MCP tools from manager (first time)\n", len(mcpTools))
	}

	var agentTools []api.Tool
	for _, mcpTool := range mcpTools {
		// Create wrapper and convert to agent tool format
		wrapper := mcp.NewMCPToolWrapper(mcpTool, a.mcpManager)
		agentTool := wrapper.ToAgentTool()

		// Convert to api.Tool format
		apiTool := api.Tool{
			Type:     agentTool.Type,
			Function: agentTool.Function,
		}
		agentTools = append(agentTools, apiTool)
	}

	// Cache the tools
	a.mcpToolsCache = agentTools

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
	result, err := a.mcpManager.CallTool(ctx, serverName, actualToolName, args)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("action parameter required")
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
		a.mcpToolsCache = nil
		tools := a.getMCPTools()
		return fmt.Sprintf("Refreshed MCP tools. %d tools available.", len(tools)), nil

	case "status":
		servers := a.mcpManager.ListServers()
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
		if err := a.mcpManager.StartAll(ctx); err != nil {
			return "", fmt.Errorf("failed to start servers: %w", err)
		}
		return "Started all MCP servers", nil

	case "stop":
		// For now, stop all servers
		if err := a.mcpManager.StopAll(ctx); err != nil {
			return "", fmt.Errorf("failed to stop servers: %w", err)
		}
		return "Stopped all MCP servers", nil

	default:
		return "", fmt.Errorf("unknown action: %s", action)
	}
}
