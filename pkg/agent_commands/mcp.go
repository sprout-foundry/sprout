package commands

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/agent"
	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
)

// MCPCommand implements the /mcp slash command
type MCPCommand struct{}

// Name returns the command name
func (m *MCPCommand) Name() string {
	return "mcp"
}

// Description returns the command description
func (m *MCPCommand) Description() string {
	return "Manage MCP (Model Context Protocol) servers - add, remove, list, test"
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
	fmt.Println("MCP (Model Context Protocol) Server Management")
	fmt.Println("==============================================")
	fmt.Println()
	fmt.Println("Available subcommands:")
	fmt.Println("  /mcp add              - Add a new MCP server interactively")
	fmt.Println("  /mcp remove [name]    - Remove an MCP server")
	fmt.Println("  /mcp list             - List all configured MCP servers")
	fmt.Println("  /mcp test [name]      - Test MCP server connection")
	fmt.Println("  /mcp help             - Show this help")
	fmt.Println()
	fmt.Println("Examples:")
	fmt.Println("  /mcp add              - Start interactive setup for MCP servers")
	fmt.Println("  /mcp list             - See all configured servers")
	fmt.Println("  /mcp test git         - Test Git MCP server")
	fmt.Println("  /mcp test github      - Test GitHub MCP server")
	fmt.Println("  /mcp remove git       - Remove Git MCP server")

	return nil
}

// addServer handles MCP server addition
func (m *MCPCommand) addServer(chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🚀 MCP Server Setup")
	fmt.Println("==================")
	fmt.Println()

	// Disable escape monitoring during interactive input
	chatAgent.DisableEscMonitoring()
	defer chatAgent.EnableEscMonitoring()

	// Load existing config
	cfg, err := configuration.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Create server registry
	registry := mcp.NewMCPServerRegistry()

	return m.setupServerFromRegistry(&mcpConfig, registry, reader)
}

// setupServerFromRegistry sets up an MCP server using the template registry
func (m *MCPCommand) setupServerFromRegistry(mcpConfig *mcp.MCPConfig, registry *mcp.MCPServerRegistry, reader *bufio.Reader) error {
	// Show available templates
	templates := registry.ListTemplates()
	fmt.Println("Select MCP server type:")
	fmt.Println()

	for i, template := range templates {
		fmt.Printf("%d. %s\n", i+1, template.Name)
		fmt.Printf("   %s\n", template.Description)
		if len(template.Features) > 0 {
			fmt.Printf("   Features: %s\n", strings.Join(template.Features, ", "))
		}
		fmt.Println()
	}

	fmt.Print("Choice (1-" + strconv.Itoa(len(templates)) + "): ")
	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}

	choiceNum, err := strconv.Atoi(strings.TrimSpace(choice))
	if err != nil || choiceNum < 1 || choiceNum > len(templates) {
		return fmt.Errorf("invalid choice: %s", choice)
	}

	template := templates[choiceNum-1]
	return m.setupServerFromTemplate(mcpConfig, template, reader)
}

// setupServerFromTemplate sets up an MCP server from a specific template
func (m *MCPCommand) setupServerFromTemplate(mcpConfig *mcp.MCPConfig, template mcp.MCPServerTemplate, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Printf("🔧 %s Setup\n", template.Name)
	fmt.Println(strings.Repeat("=", len(template.Name)+7))
	fmt.Println()

	if template.Docs != "" {
		fmt.Printf("📚 Documentation: %s\n", template.Docs)
		fmt.Println()
	}

	// Get server name
	fmt.Printf("Enter server name (default: %s): ", strings.ToLower(strings.ReplaceAll(template.Name, " ", "-")))
	nameInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read server name: %w", err)
	}

	serverName := strings.TrimSpace(nameInput)
	if serverName == "" {
		// Generate default name from template
		serverName = strings.ToLower(strings.ReplaceAll(template.Name, " ", "-"))
		serverName = strings.ReplaceAll(serverName, "(", "")
		serverName = strings.ReplaceAll(serverName, ")", "")
		// Take first word for common cases
		if strings.Contains(serverName, "-") {
			serverName = strings.Split(serverName, "-")[0]
		}
	}

	// Check if server already exists
	if _, exists := mcpConfig.Servers[serverName]; exists {
		fmt.Printf("Server '%s' already exists. Reconfigure? (y/N): ", serverName)
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Collect environment variables
	envValues := make(map[string]string)
	for _, envVar := range template.EnvVars {
		var value string

		// Check if already set in environment
		if existingValue := os.Getenv(envVar.Name); existingValue != "" {
			if envVar.Secret {
				fmt.Printf("Using existing %s from environment\n", envVar.Name)
			} else {
				fmt.Printf("Using existing %s from environment: %s\n", envVar.Name, existingValue)
			}
			value = existingValue
		} else {
			// Prompt user for value
			fmt.Printf("%s:\n", envVar.Description)
			if envVar.Required {
				fmt.Print("Enter " + envVar.Name + ": ")
			} else {
				defaultText := ""
				if envVar.Default != "" {
					defaultText = fmt.Sprintf(" (default: %s)", envVar.Default)
				}
				fmt.Printf("Enter %s%s: ", envVar.Name, defaultText)
			}

			input, err := reader.ReadString('\n')
			if err != nil {
				return fmt.Errorf("failed to read %s: %w", envVar.Name, err)
			}
			value = strings.TrimSpace(input)

			if value == "" && envVar.Required {
				return fmt.Errorf("%s is required", envVar.Name)
			}
		}

		if value != "" {
			envValues[envVar.Name] = value
		}
	}

	// Handle custom values for generic templates
	var customURL, customCommand string
	var customArgs []string

	if template.ID == "http-generic" {
		fmt.Print("Enter MCP server URL: ")
		urlInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read URL: %w", err)
		}
		customURL = strings.TrimSpace(urlInput)
		if customURL == "" {
			return fmt.Errorf("URL is required for HTTP servers")
		}
	}

	if template.ID == "stdio-generic" {
		fmt.Print("Enter command: ")
		cmdInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read command: %w", err)
		}
		customCommand = strings.TrimSpace(cmdInput)
		if customCommand == "" {
			return fmt.Errorf("command is required for stdio servers")
		}

		fmt.Print("Enter arguments (space-separated, or press Enter for none): ")
		argsInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read arguments: %w", err)
		}
		argsStr := strings.TrimSpace(argsInput)
		if argsStr != "" {
			customArgs = strings.Fields(argsStr)
		}
	}

	// Create server config from template
	serverConfig := template.CreateServerConfig(serverName, envValues, customURL, customCommand, customArgs)

	// Add server to config
	mcpConfig.Servers[serverName] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	cfg, _ := configuration.Load()
	if err := mcp.SaveMCPConfig(cfg, *mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ %s configured successfully!\n", template.Name)
	if serverConfig.Type == "http" {
		fmt.Printf("Type: Remote HTTP server\n")
		fmt.Printf("URL: %s\n", serverConfig.URL)
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
	}
	fmt.Println()
	fmt.Printf("To test the configuration, run: /mcp test %s\n", serverName)

	if len(template.Features) > 0 {
		fmt.Println()
		fmt.Println("📦 Features available:")
		for _, feature := range template.Features {
			fmt.Printf("• %s\n", feature)
		}
	}

	return nil
}

// removeServer handles MCP server removal
func (m *MCPCommand) removeServer(serverName string, chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	// Disable escape monitoring during interactive input
	chatAgent.DisableEscMonitoring()
	defer chatAgent.EnableEscMonitoring()

	// Load existing config
	cfg, err := configuration.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// If no server name provided, list available servers
	if serverName == "" {
		if len(mcpConfig.Servers) == 0 {
			fmt.Println("No MCP servers configured.")
			return nil
		}

		fmt.Println("Available servers:")
		i := 1
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			fmt.Printf("%d. %s\n", i, name)
			serverNames = append(serverNames, name)
			i++
		}

		fmt.Print("Select server to remove (1-" + strconv.Itoa(len(serverNames)) + "): ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		choiceNum, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || choiceNum < 1 || choiceNum > len(serverNames) {
			return fmt.Errorf("invalid choice: %s", choice)
		}

		serverName = serverNames[choiceNum-1]
	}

	// Check if server exists
	if _, exists := mcpConfig.Servers[serverName]; !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	// Confirm removal
	fmt.Printf("Are you sure you want to remove server '%s'? (y/N): ", serverName)
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("Removal cancelled.")
		return nil
	}

	// Remove server
	delete(mcpConfig.Servers, serverName)

	// Disable MCP if no servers remain
	if len(mcpConfig.Servers) == 0 {
		mcpConfig.Enabled = false
	}

	// Save config
	if err := mcp.SaveMCPConfig(cfg, mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Printf("✅ Server '%s' removed successfully!\n", serverName)

	if len(mcpConfig.Servers) == 0 {
		fmt.Println("MCP disabled (no servers remain).")
	}

	return nil
}

// listServers displays all configured MCP servers
func (m *MCPCommand) listServers() error {
	// Load existing config
	cfg, err := configuration.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	fmt.Println("MCP Configuration")
	fmt.Println("==================")
	fmt.Printf("Enabled: %t\n", mcpConfig.Enabled)
	fmt.Printf("Auto-start: %t\n", mcpConfig.AutoStart)
	fmt.Printf("Auto-discover: %t\n", mcpConfig.AutoDiscover)
	fmt.Printf("Default timeout: %v\n", mcpConfig.Timeout)
	fmt.Printf("Total servers: %d\n", len(mcpConfig.Servers))
	fmt.Println()

	if len(mcpConfig.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println("Run '/mcp add' to add a server.")
		return nil
	}

	fmt.Println("Configured Servers:")
	fmt.Println("-------------------")

	for name, server := range mcpConfig.Servers {
		fmt.Printf("📡 %s\n", name)
		if server.Type == "http" {
			fmt.Printf("   Type: HTTP Remote Server\n")
			fmt.Printf("   URL: %s\n", server.URL)
		} else {
			fmt.Printf("   Command: %s %v\n", server.Command, server.Args)
		}
		fmt.Printf("   Auto-start: %t\n", server.AutoStart)
		fmt.Printf("   Max restarts: %d\n", server.MaxRestarts)
		fmt.Printf("   Timeout: %v\n", server.Timeout)

		if server.WorkingDir != "" {
			fmt.Printf("   Working dir: %s\n", server.WorkingDir)
		}

		if len(server.Env) > 0 {
			fmt.Printf("   Environment vars: ")
			envKeys := make([]string, 0, len(server.Env))
			for key := range server.Env {
				// Don't expose sensitive values
				if strings.Contains(strings.ToLower(key), "token") ||
					strings.Contains(strings.ToLower(key), "key") ||
					strings.Contains(strings.ToLower(key), "secret") {
					envKeys = append(envKeys, key+"=***")
				} else {
					envKeys = append(envKeys, key+"="+server.Env[key])
				}
			}
			fmt.Printf("%s\n", strings.Join(envKeys, ", "))
		}

		fmt.Println()
	}

	fmt.Println("Commands:")
	fmt.Println("  /mcp test [server] - Test server connection")
	fmt.Println("  /mcp add           - Add new server")
	fmt.Println("  /mcp remove        - Remove server")

	return nil
}

// testServer tests an MCP server connection
func (m *MCPCommand) testServer(serverName string, chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	// Disable escape monitoring during interactive input
	chatAgent.DisableEscMonitoring()
	defer chatAgent.EnableEscMonitoring()

	// Load existing config
	cfg, err := configuration.Load()
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// If no server name provided, list available servers
	if serverName == "" {
		if len(mcpConfig.Servers) == 0 {
			fmt.Println("No MCP servers configured.")
			fmt.Println("Run '/mcp add' to add a server.")
			return nil
		}

		fmt.Println("Available servers:")
		i := 1
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			fmt.Printf("%d. %s\n", i, name)
			serverNames = append(serverNames, name)
			i++
		}

		fmt.Print("Select server to test (1-" + strconv.Itoa(len(serverNames)) + "): ")
		choice, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read input: %w", err)
		}

		choiceNum, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || choiceNum < 1 || choiceNum > len(serverNames) {
			return fmt.Errorf("invalid choice: %s", choice)
		}

		serverName = serverNames[choiceNum-1]
	}

	// Check if server exists
	serverConfig, exists := mcpConfig.Servers[serverName]
	if !exists {
		return fmt.Errorf("server '%s' not found", serverName)
	}

	fmt.Printf("🧪 Testing MCP Server: %s\n", serverName)
	fmt.Println("========================")
	if serverConfig.Type == "http" {
		fmt.Printf("Type: HTTP Remote Server\n")
		fmt.Printf("URL: %s\n", serverConfig.URL)
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
	}
	fmt.Println()

	// Create manager and client
	manager := mcp.NewMCPManager(nil)
	if err := manager.AddServer(serverConfig); err != nil {
		return fmt.Errorf("failed to add server to manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), serverConfig.Timeout+10*time.Second)
	defer cancel()

	server, exists := manager.GetServer(serverName)
	if !exists {
		return fmt.Errorf("failed to get server from manager")
	}

	fmt.Println("⏳ Starting server...")
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	defer func() {
		fmt.Println("🛑 Stopping server...")
		server.Stop(context.Background())
	}()

	fmt.Println("✅ Server started successfully!")

	fmt.Println("🔄 Initializing server...")
	if err := server.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}
	fmt.Println("✅ Server initialized successfully!")

	fmt.Println("🔍 Listing available tools...")
	tools, err := server.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if len(tools) == 0 {
		fmt.Println("⚠️  No tools available from this server.")
		return nil
	}

	fmt.Printf("✅ Found %d tools:\n", len(tools))
	fmt.Println()

	for i, tool := range tools {
		fmt.Printf("%d. %s\n", i+1, tool.Name)
		if tool.Description != "" {
			fmt.Printf("   Description: %s\n", tool.Description)
		}
		fmt.Println()
	}

	fmt.Printf("🎉 Test completed successfully! Server '%s' is working properly.\n", serverName)

	return nil
}
