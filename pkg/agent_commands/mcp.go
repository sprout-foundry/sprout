package commands

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/sprout-foundry/sprout/pkg/agent"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/credentials"
	"github.com/sprout-foundry/sprout/pkg/mcp"
	"github.com/sprout-foundry/sprout/pkg/secretdetect"
	"github.com/sprout-foundry/sprout/pkg/utils"
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

// addServer handles MCP server addition
func (m *MCPCommand) addServer(chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	m.println("[>>] MCP Server Setup")
	m.println("==================")
	m.println()

	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
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
	m.println("Select MCP server type:")
	m.println()

	for i, template := range templates {
		m.printf("%d. %s\n", i+1, template.Name)
		m.printf("   %s\n", template.Description)
		if len(template.Features) > 0 {
			m.printf("   Features: %s\n", strings.Join(template.Features, ", "))
		}
		m.println()
	}

	m.print("Choice (1-" + strconv.Itoa(len(templates)) + "): ")
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
	m.println()
	console.GlyphInfo.Fprintf(m.out(), "%s Setup", template.Name)
	m.println(strings.Repeat("=", len(template.Name)+7))
	m.println()

	if template.Docs != "" {
		m.printf("  Documentation: %s\n", template.Docs)
		m.println()
	}

	// Get server name
	m.printf("Enter server name (default: %s): ", strings.ToLower(strings.ReplaceAll(template.Name, " ", "-")))
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
		m.printf("Server '%s' already exists. Reconfigure? %s: ", serverName, utils.DefaultChoiceHint(false))
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			m.println("Setup cancelled.")
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
				m.printf("Using existing %s from environment\n", envVar.Name)
			} else {
				m.printf("Using existing %s from environment: %s\n", envVar.Name, existingValue)
			}
			value = existingValue
		} else {
			// Prompt user for value
			m.printf("%s:\n", envVar.Description)
			if envVar.Required {
				m.print("Enter " + envVar.Name + ": ")
			} else {
				defaultText := ""
				if envVar.Default != "" {
					defaultText = fmt.Sprintf(" (default: %s)", envVar.Default)
				}
				m.printf("Enter %s%s: ", envVar.Name, defaultText)
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
		m.print("Enter MCP server URL: ")
		urlInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read URL: %w", err)
		}
		customURL = strings.TrimSpace(urlInput)
		if customURL == "" {
			return errors.New("URL is required for HTTP servers")
		}
	}

	if template.ID == "stdio-generic" {
		m.print("Enter command: ")
		cmdInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read command: %w", err)
		}
		customCommand = strings.TrimSpace(cmdInput)
		if customCommand == "" {
			return errors.New("command is required for stdio servers")
		}

		m.print("Enter arguments (space-separated, or press Enter for none): ")
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
	if err := mcp.SaveMCPConfig(mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	m.println()
	console.GlyphSuccess.Fprintf(m.out(), "%s configured successfully!", template.Name)
	if serverConfig.Type == "http" {
		m.printf("Type: Remote HTTP server\n")
		m.printf("URL: %s\n", secretdetect.RedactOpaque(serverConfig.URL))
	} else {
		m.printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	}
	m.println()
	m.printf("To test the configuration, run: /mcp test %s\n", serverName)

	if len(template.Features) > 0 {
		m.println()
		console.GlyphInfo.Fprintln(m.out(), "Features available:")
		for _, feature := range template.Features {
			m.printf("• %s\n", feature)
		}
	}

	return nil
}

// removeServer handles MCP server removal
func (m *MCPCommand) removeServer(serverName string, chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// If no server name provided, list available servers
	if serverName == "" {
		if len(mcpConfig.Servers) == 0 {
			m.println("No MCP servers configured.")
			return nil
		}

		m.println("Available servers:")
		i := 1
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			m.printf("%d. %s\n", i, name)
			serverNames = append(serverNames, name)
			i++
		}

		m.print("Select server to remove (1-" + strconv.Itoa(len(serverNames)) + "): ")
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
	m.printf("Are you sure you want to remove server '%s'? %s: ", serverName, utils.DefaultChoiceHint(false))
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		m.println("Removal cancelled.")
		return nil
	}

	// Clean up stored credentials before removing
	if server, exists := mcpConfig.Servers[serverName]; exists && server.Env != nil {
		for envVarName, value := range server.Env {
			if mcp.IsSecretRef(value) {
				key := mcp.CredentialKey(serverName, envVarName)
				if err := credentials.DeleteFromActiveBackend(key); err != nil {
					log.Printf("[mcp] Failed to delete credential %s: %v", key, err)
				}
			}
		}
	}

	// Remove server
	delete(mcpConfig.Servers, serverName)

	// Disable MCP if no servers remain
	if len(mcpConfig.Servers) == 0 {
		mcpConfig.Enabled = false
	}

	// Save config
	config := &mcpConfig
	if err := mcp.SaveMCPConfig(config); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}
	console.GlyphSuccess.Fprintf(m.out(), "Server '%s' removed successfully!", serverName)

	if len(mcpConfig.Servers) == 0 {
		m.println("MCP disabled (no servers remain).")
	}

	return nil
}

// listServers displays all configured MCP servers
func (m *MCPCommand) listServers() error {
	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Redact MCP config to remove sensitive data before displaying
	redactedConfig := mcp.RedactMCPConfig(mcpConfig)

	m.println("MCP Configuration")
	m.println("==================")
	m.printf("Enabled: %t\n", redactedConfig.Enabled)
	m.printf("Auto-start: %t\n", redactedConfig.AutoStart)
	m.printf("Auto-discover: %t\n", redactedConfig.AutoDiscover)
	m.printf("Default timeout: %v\n", redactedConfig.Timeout)
	m.printf("Total servers: %d\n", len(redactedConfig.Servers))
	m.println()

	if len(redactedConfig.Servers) == 0 {
		m.println("No MCP servers configured.")
		m.println("Run '/mcp add' to add a server.")
		return nil
	}

	console.Heading(m.out(), "Configured servers")

	for name, server := range redactedConfig.Servers {
		m.printf("%s%s\n", console.GlyphAction.Prefix(), name)
		if server.Type == "http" {
			m.printf("   Type: HTTP Remote Server\n")
			m.printf("   URL: %s\n", secretdetect.RedactOpaque(server.URL))
		} else {
			m.printf("   Command: %s %v\n", server.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", server.Args)))
		}
		m.printf("   Auto-start: %t\n", server.AutoStart)
		m.printf("   Max restarts: %d\n", server.MaxRestarts)
		m.printf("   Timeout: %v\n", server.Timeout)

		if server.WorkingDir != "" {
			m.printf("   Working dir: %s\n", server.WorkingDir)
		}

		if len(server.Env) > 0 {
			m.printf("   Environment vars: ")
			envEntries := make([]string, 0, len(server.Env))
			for key, value := range server.Env {
				envEntries = append(envEntries, key+"="+value)
			}
			m.printf("%s\n", strings.Join(envEntries, ", "))
		}

		// Show credentials if present (placeholder references are safe; actual secrets are masked)
		if len(server.Credentials) > 0 {
			m.printf("   Credentials: ")
			credEntries := make([]string, 0, len(server.Credentials))
			for key, value := range server.Credentials {
				credEntries = append(credEntries, key+"="+value)
			}
			m.printf("%s\n", strings.Join(credEntries, ", "))
		}

		m.println()
	}

	m.println("Commands:")
	m.println("  /mcp test [server] - Test server connection")
	m.println("  /mcp add           - Add new server")
	m.println("  /mcp remove        - Remove server")

	return nil
}

// testServer tests an MCP server connection
func (m *MCPCommand) testServer(serverName string, chatAgent *agent.Agent) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// If no server name provided, list available servers
	if serverName == "" {
		if len(mcpConfig.Servers) == 0 {
			m.println("No MCP servers configured.")
			m.println("Run '/mcp add' to add a server.")
			return nil
		}

		m.println("Available servers:")
		i := 1
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			m.printf("%d. %s\n", i, name)
			serverNames = append(serverNames, name)
			i++
		}

		m.print("Select server to test (1-" + strconv.Itoa(len(serverNames)) + "): ")
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

	console.Heading(m.out(), "Testing MCP server: "+serverName)
	if serverConfig.Type == "http" {
		m.printf("Type: HTTP Remote Server\n")
		m.printf("URL: %s\n", secretdetect.RedactOpaque(serverConfig.URL))
	} else {
		m.printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
	}
	m.println()

	// Create manager and client
	manager := mcp.NewMCPManager(nil)
	if err := manager.AddServer(serverConfig); err != nil {
		return fmt.Errorf("failed to add server to manager: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), serverConfig.Timeout+10*time.Second)
	defer cancel()

	server, exists := manager.GetServer(serverName)
	if !exists {
		return errors.New("failed to get server from manager")
	}

	console.GlyphAction.Fprintln(m.out(), "Starting server…")
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	defer func() {
		console.GlyphStopped.Fprintln(m.out(), "Stopping server...")
		server.Stop(context.Background())
	}()

	console.GlyphSuccess.Fprintln(m.out(), "Server started successfully!")

	console.GlyphAction.Fprintln(m.out(), "Initializing server…")
	if err := server.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}
	console.GlyphSuccess.Fprintln(m.out(), "Server initialized successfully!")

	console.GlyphAction.Fprintln(m.out(), "Listing available tools…")
	tools, err := server.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if len(tools) == 0 {
		console.GlyphWarning.Fprintln(m.out(), "No tools available from this server.")
		return nil
	}

	console.GlyphSuccess.Fprintf(m.out(), "Found %d tools:", len(tools))
	m.println()

	for i, tool := range tools {
		m.printf("%d. %s\n", i+1, tool.Name)
		if tool.Description != "" {
			m.printf("   Description: %s\n", tool.Description)
		}
		m.println()
	}

	console.GlyphSuccess.Fprintf(m.out(), "Server '%s' is working.", serverName)

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
