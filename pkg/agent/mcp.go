package agent

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	api "github.com/sprout-foundry/sprout/pkg/agent_api"
	agenterrors "github.com/sprout-foundry/sprout/pkg/errors"
	"github.com/sprout-foundry/sprout/pkg/injection"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// mcpMaxResultChars bounds an MCP tool result before it enters the transcript.
const mcpMaxResultChars = 100_000

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

	// First, load servers from the standalone mcp_config.json file (if it
	// exists). This ensures servers added via CLI commands are available.
	// Load BEFORE checking if MCP is enabled - the file may have servers.
	//
	// The load is scoped to the workspace this agent serves, not to the
	// process's ambient config root: a daemon's config root was fixed when
	// the daemon started, so reading it would hide a repo-local config
	// written afterwards. The workspace layer wins over the global one.
	workspaceRoot := a.GetWorkspaceRoot()
	var legacyEnabled bool
	if legacyMCPConfig, err := mcp.LoadMCPConfigForWorkspace(workspaceRoot); err == nil {
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
					// SP-169: the framing setting is carried through the
					// legacy-config merge below via the setter, not this
					// literal — see the comment after the closing brace.
				}
				// SP-169: carry the per-server framing setting through the
				// legacy-config merge, or a server added via `mcp add` would
				// arrive at the manager with framing silently off. The setter
				// (not the literal) also records the value as authoritative: a
				// bare field assignment leaves the provenance unset, and
				// TrustUntrustedContentOrDefault then returns true even for a
				// server the user explicitly opted out of.
				mcpServer.SetTrustUntrustedContent(serverConfig.TrustUntrustedContentOrDefault())
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
			// Name the config consulted: "no MCP servers" is otherwise
			// indistinguishable from "read the wrong config root".
			a.Logger().Debug("MCP is disabled in configuration (config: %s)",
				mcp.MCPConfigSourcePath(workspaceRoot))
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
			// SP-169: the framing setting travels with the server definition.
		}
		// SP-169: the setter records the value as authoritative. A bare field
		// assignment would leave the provenance unset, and
		// TrustUntrustedContentOrDefault would then return true even for a
		// server the user explicitly opted out of.
		mcpServer.SetTrustUntrustedContent(serverConfig.TrustUntrustedContentOrDefault())

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
			return agenterrors.NewTool("mcp", "failed to start MCP servers", err)
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

// mcpCallContext returns the context an MCP call runs under. The agent's
// interrupt context is what a turn is cancelled through, so an in-flight server
// call stops on interrupt instead of waiting out the RPC timeout (30s by
// default). Background is the fallback for an agent that was never armed.
func (a *Agent) mcpCallContext() context.Context {
	a.interruptMu.Lock()
	defer a.interruptMu.Unlock()
	if a.interruptCtx != nil {
		return a.interruptCtx
	}
	return context.Background()
}

// resolveMCPToolIdentity splits an advertised MCP tool name into the server that
// owns it and the tool's own name.
//
// The advertised form is "mcp_<server>_<tool>", which is ambiguous when a server
// name contains an underscore: splitting at the first underscore would read
// "mcp_my_server_read" as server "my" + tool "server_read". That is not cosmetic
// once an allowlist exists — the gate would authorize the wrong server, and the
// denial would name an entry that permits a different one. So the split is made
// against the configured server names, longest first, and a name that matches no
// configured server falls back to the historical split (the server may simply
// not be running yet, and CallTool reports that).
func (a *Agent) resolveMCPToolIdentity(toolName string) (server, tool string, err error) {
	rest := strings.TrimPrefix(toolName, "mcp_")
	if rest == "" {
		return "", "", agenterrors.NewInvalidInputError("empty MCP tool name", nil)
	}

	var names []string
	if manager := a.mcpSub.GetManager(); manager != nil {
		for _, srv := range manager.ListServers() {
			if srv != nil {
				names = append(names, srv.GetName())
			}
		}
	}
	if server, tool, ok := resolveMCPToolIdentityFromNames(rest, names); ok {
		return server, tool, nil
	}

	parts := strings.SplitN(rest, "_", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", agenterrors.NewInvalidInputError(fmt.Sprintf("invalid MCP tool name format: %s", toolName), nil)
	}
	return parts[0], parts[1], nil
}

// resolveMCPToolIdentityFromNames is the pure half of the split: the longest
// configured server name that prefixes the advertised remainder on a
// whole-segment boundary wins, so "my_server" is preferred over "my". ok is
// false when no configured server matches, leaving the caller to fall back.
func resolveMCPToolIdentityFromNames(rest string, names []string) (server, tool string, ok bool) {
	best := ""
	for _, name := range names {
		if name == "" || !strings.HasPrefix(rest, name+"_") {
			continue
		}
		if len(name) > len(best) {
			best = name
		}
	}
	if best == "" {
		return "", "", false
	}
	return best, strings.TrimPrefix(rest, best+"_"), true
}

// executeMCPTool executes an MCP tool
func (a *Agent) executeMCPTool(toolName string, args map[string]interface{}) (string, error) {
	serverName, actualToolName, err := a.resolveMCPToolIdentity(toolName)
	if err != nil {
		return "", err
	}

	// SP-170: authorize before any server-side call. The gate is the only
	// thing standing between a steered model and a destructive third-party
	// action, since MCP tools never reach the registry's security gate.
	if err := a.authorizeMCPTool(serverName, actualToolName); err != nil {
		return "", err
	}

	ctx := a.mcpCallContext()
	result, err := a.mcpSub.GetManager().CallTool(ctx, serverName, actualToolName, args)
	if err != nil {
		return "", agenterrors.Wrapf(err, "failed to call MCP tool %s/%s", serverName, actualToolName)
	}

	// SP-169 layer 5: the server's payload is third-party text, so it is framed
	// when the server's setting says so. Errors we generated ourselves are
	// returned above, outside the block — only the server's own content is
	// labelled as data.
	return a.frameMCPResult(serverName, actualToolName, formatMCPResult(result)), nil
}

// frameMCPResult wraps an MCP tool's result text in the untrusted-data block
// (SP-169 layer 5) when the owning server's TrustUntrustedContent setting is on.
//
// The setting is read from the live server config rather than the static
// config file, so a change lands without a restart. An empty result is
// returned unchanged: there is nothing to frame, and an empty block would only
// add noise. Content is never dropped — framing is a label, not a filter.
func (a *Agent) frameMCPResult(serverName, toolName, text string) string {
	if text == "" {
		return text
	}
	if !a.mcpServerTrustsUntrustedContent(serverName) {
		return text
	}
	return injection.WrapUntrusted("mcp: "+serverName+"/"+toolName, text)
}

// mcpServerTrustsUntrustedContent reports whether the named server's output
// should be framed. A server we cannot resolve (manager absent, server
// unregistered) defaults to framing: the conservative direction is to label
// text we cannot attribute, and the label costs nothing.
func (a *Agent) mcpServerTrustsUntrustedContent(serverName string) bool {
	if a.mcpSub == nil || a.mcpSub.GetManager() == nil {
		return true
	}
	server, exists := a.mcpSub.GetManager().GetServer(serverName)
	if !exists {
		return true
	}
	return server.GetConfig().TrustUntrustedContentOrDefault()
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

	out := strings.TrimSpace(output.String())
	// A server that returns a whole file or a log dump would otherwise enter
	// the transcript unbounded — the context blowup the built-in tools guard
	// against with MaxResultSize. The cut is reported so the model can narrow
	// the request instead of assuming it saw everything.
	if len(out) > mcpMaxResultChars {
		cut := mcpMaxResultChars
		for cut > 0 && !utf8.RuneStart(out[cut]) {
			cut--
		}
		out = out[:cut] + fmt.Sprintf("\n\n[truncated %d characters; request a narrower range or a specific field]",
			len(out)-cut)
	}
	return out
}

// handleMCPToolsCommand handles the mcp_tools meta command
func (a *Agent) handleMCPToolsCommand(args map[string]interface{}) (string, error) {
	action, ok := args["action"].(string)
	if !ok {
		return "", agenterrors.NewInvalidInputError("mcp_tools command requires 'action' parameter", nil)
	}

	ctx := a.mcpCallContext()

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

	case "call":
		// Invoke one MCP tool: server+tool+arguments, mirroring the schema the
		// synthetic mcp_tools entry advertises. Routes through the manager so
		// a not-running or unknown server surfaces as a typed error.
		server, _ := args["server"].(string)
		tool, _ := args["tool"].(string)
		if server == "" || tool == "" {
			return "", agenterrors.NewInvalidInputError("mcp_tools call requires 'server' and 'tool' parameters", nil)
		}
		callArgs, _ := args["arguments"].(map[string]interface{})
		// SP-170: the meta-command's call action is the second route to the
		// same server-side invocation, so it is gated identically. Discovery
		// actions (list/describe/status/refresh) run no server-side tool and
		// stay ungated.
		if err := a.authorizeMCPTool(server, tool); err != nil {
			return "", err
		}
		result, err := a.mcpSub.GetManager().CallTool(ctx, server, tool, callArgs)
		if err != nil {
			return "", agenterrors.Wrapf(err, "mcp_tools call %s/%s failed", server, tool)
		}
		// Same framing as a direct mcp_<server>_<tool> call: this is the same
		// third-party payload arriving by a second route.
		return a.frameMCPResult(server, tool, formatMCPResult(result)), nil

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
			return "", agenterrors.NewTool("mcp", "failed to start servers", err)
		}
		return "Started all MCP servers", nil

	case "stop":
		// For now, stop all servers
		if err := a.mcpSub.GetManager().StopAll(ctx); err != nil {
			return "", agenterrors.NewTool("mcp", "failed to stop servers", err)
		}
		return "Stopped all MCP servers", nil

	default:
		return "", agenterrors.NewInvalidInputError(fmt.Sprintf("unknown action: %s", action), nil)
	}
}
