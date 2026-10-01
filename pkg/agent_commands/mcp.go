package commands

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

// MCPCommand implements the /mcp slash command
type MCPCommand struct {
	outputSink
}

// Name returns the command name
func (m *MCPCommand) Name() string {
	return "mcp"
}

// SafeDuringSteer returns true - /mcp list is read-only (add/remove handled in command itself)
func (m *MCPCommand) SafeDuringSteer() bool {
	return true
}

// Description returns the command description
func (m *MCPCommand) Description() string {
	return "Manage MCP (Model Context Protocol) servers - add, remove, list, test"
}

// Usage returns the detailed help text shown by `/help mcp`.
func (m *MCPCommand) Usage() string {
	return strings.Join([]string{
		"/mcp <subcommand>   Manage MCP (Model Context Protocol) servers.",
		"",
		"Subcommands:",
		"  add              Add a new MCP server interactively",
		"  remove [name]    Remove an MCP server",
		"  list             List all configured MCP servers",
		"  test [name]      Test an MCP server connection",
		"  help             Show this usage message",
		"",
		"Examples:",
		"  /mcp add",
		"  /mcp list",
		"  /mcp test git",
		"  /mcp remove github",
	}, "\n")
}

// Execute runs the MCP command
func (m *MCPCommand) Execute(args []string, chatAgent *agent.Agent) error {
	if len(args) == 0 {
		return m.showHelp()
	}

	subcommand := args[0]
	subArgs := args[1:]

	switch subcommand {
	case "add":
		return m.addServer(chatAgent)
	case "remove":
		var serverName string
		if len(subArgs) > 0 {
			serverName = subArgs[0]
		}
		return m.removeServer(serverName, chatAgent)
	case "list":
		return m.listServers()
	case "test":
		var serverName string
		if len(subArgs) > 0 {
			serverName = subArgs[0]
		}
		return m.testServer(serverName, chatAgent)
	case "help", "-h", "--help":
		return m.showHelp()
	default:
		return fmt.Errorf("unknown subcommand: %s. Use '/mcp help' for usage", subcommand)
	}
}

// showHelp displays usage information
func (m *MCPCommand) showHelp() error {
	m.println("MCP (Model Context Protocol) Server Management")
	m.println("==============================================")
	m.println()
	m.println("Available subcommands:")
	m.println("  /mcp add              - Add a new MCP server interactively")
	m.println("  /mcp remove [name]   - Remove an MCP server")
	m.println("  /mcp list             - List all configured MCP servers")
	m.println("  /mcp test [name]     - Test MCP server connection")
	m.println("  /mcp help             - Show this help")
	m.println()
	m.println("Examples:")
	m.println("  /mcp add              - Start interactive setup for MCP servers")
	m.println("  /mcp list             - See all configured servers")
	m.println("  /mcp test git         - Test Git MCP server")
	m.println("  /mcp test github      - Test GitHub MCP server")
	m.println("  /mcp remove git       - Remove Git MCP server")

	return nil
}

// Complete returns completions for the /mcp command.
func (m *MCPCommand) Complete(args []string, chatAgent *agent.Agent) []string {
	subcommands := []string{"add", "help", "list", "remove", "test"}
	if len(args) == 0 {
		return subcommands
	}

	switch args[0] {
	case "add":
		// Suggest MCP server types: stdio (local process) and http (remote).
		return []string{"stdio", "http"}
	case "remove", "test":
		// List configured MCP server names from the config file.
		mcpConfig, err := mcp.LoadMCPConfig()
		if err != nil {
			return nil
		}
		prefix := ""
		if len(args) > 1 {
			prefix = args[len(args)-1]
		}
		var names []string
		for name := range mcpConfig.Servers {
			if prefix == "" || strings.HasPrefix(strings.ToLower(name), strings.ToLower(prefix)) {
				names = append(names, name)
			}
		}
		sort.Strings(names)
		return names
	default:
		// Prefix-match against known subcommands.
		prefix := args[0]
		var matches []string
		for _, sub := range subcommands {
			if strings.HasPrefix(strings.ToLower(sub), strings.ToLower(prefix)) {
				matches = append(matches, sub)
			}
		}
		return matches
	}
}
