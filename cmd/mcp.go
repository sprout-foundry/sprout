package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/alantheprice/ledit/pkg/configuration"
	"github.com/alantheprice/ledit/pkg/mcp"
	"github.com/spf13/cobra"
)

var mcpCmd = &cobra.Command{
	Use:   "mcp",
	Short: "Manage MCP (Model Context Protocol) servers",
	Long: `Manage MCP servers that extend ledit with external tools and services.
Use subcommands to add, remove, or list configured MCP servers.`,
	Run: func(cmd *cobra.Command, args []string) {
		cmd.Help()
	},
}

var mcpAddCmd = &cobra.Command{
	Use:   "add",
	Short: "Add a new MCP server interactively",
	Long: `Interactively add a new MCP server configuration.
This will guide you through setting up popular MCP servers or custom configurations.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runMCPAdd(); err != nil {
			fmt.Fprintf(os.Stderr, "Error adding MCP server: %v\n", err)
			os.Exit(1)
		}
	},
}

var mcpRemoveCmd = &cobra.Command{
	Use:   "remove [server-name]",
	Short: "Remove an MCP server",
	Long:  `Remove an MCP server from the configuration.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var serverName string
		if len(args) > 0 {
			serverName = args[0]
		}
		if err := runMCPRemove(serverName); err != nil {
			fmt.Fprintf(os.Stderr, "Error removing MCP server: %v\n", err)
			os.Exit(1)
		}
	},
}

var mcpListCmd = &cobra.Command{
	Use:   "list",
	Short: "List configured MCP servers",
	Long:  `Display all configured MCP servers and their status.`,
	Run: func(cmd *cobra.Command, args []string) {
		if err := runMCPList(); err != nil {
			fmt.Fprintf(os.Stderr, "Error listing MCP servers: %v\n", err)
			os.Exit(1)
		}
	},
}

var mcpTestCmd = &cobra.Command{
	Use:   "test [server-name]",
	Short: "Test MCP server connection",
	Long:  `Test connection to an MCP server and list its available tools.`,
	Args:  cobra.MaximumNArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		var serverName string
		if len(args) > 0 {
			serverName = args[0]
		}
		if err := runMCPTest(serverName); err != nil {
			fmt.Fprintf(os.Stderr, "Error testing MCP server: %v\n", err)
			os.Exit(1)
		}
	},
}

func runMCPAdd() error {
	reader := bufio.NewReader(os.Stdin)

	fmt.Println("🚀 MCP Server Setup")
	fmt.Println("==================")
	fmt.Println()

	// Load existing config
	cfg, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	mcpConfig, err := mcp.LoadMCPConfig(cfg)
	if err != nil {
		return fmt.Errorf("failed to load MCP config: %w", err)
	}

	// Server type selection
	fmt.Println("Select server type:")
	fmt.Println("1. Git MCP Server (local Git operations)")
	fmt.Println("2. GitHub MCP Server (GitHub API, issues, PRs)")
	fmt.Println("3. Custom MCP Server")
	fmt.Print("Choice (1-3): ")

	choice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	choice = strings.TrimSpace(choice)

	switch choice {
	case "1":
		return setupGitMCPServer(&mcpConfig, reader)
	case "2":
		return setupGitHubMCPServer(&mcpConfig, reader)
	case "3":
		return setupCustomMCPServer(&mcpConfig, reader)
	default:
		return fmt.Errorf("invalid choice: %s", choice)
	}
}

func setupGitMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("🔧 Git MCP Server Setup")
	fmt.Println("========================")
	fmt.Println()

	// Check if Git server already exists
	if _, exists := mcpConfig.Servers["git"]; exists {
		fmt.Print("Git MCP server is already configured. Reconfigure? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Get repository path (optional)
	fmt.Print("Enter repository path (optional, leave empty to use current directory): ")
	repoInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read repository path: %w", err)
	}
	repoPath := strings.TrimSpace(repoInput)

	// Installation method selection
	fmt.Println("Select installation method:")
	fmt.Println("1. uvx (recommended)")
	fmt.Println("2. pip/pipx")
	fmt.Print("Choice (1-2): ")

	installChoice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	installChoice = strings.TrimSpace(installChoice)

	var serverConfig mcp.MCPServerConfig

	switch installChoice {
	case "1", "":
		args := []string{"mcp-server-git"}
		if repoPath != "" {
			args = append(args, "--repository", repoPath)
		}
		serverConfig = mcp.MCPServerConfig{
			Name:        "git",
			Command:     "uvx",
			Args:        args,
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
		}
	case "2":
		args := []string{"-m", "mcp_server_git"}
		if repoPath != "" {
			args = append(args, "--repository", repoPath)
		}
		serverConfig = mcp.MCPServerConfig{
			Name:        "git",
			Command:     "python",
			Args:        args,
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
		}
	default:
		return fmt.Errorf("invalid choice: %s", installChoice)
	}

	// Add server to config
	mcpConfig.Servers["git"] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	cfg, _ := configuration.LoadOrInitConfig(false)
	if err := mcp.SaveMCPConfig(cfg, *mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ Git MCP Server configured successfully!")
	fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
	fmt.Println()
	fmt.Println("To test the configuration, run: ledit mcp test git")
	fmt.Println()

	// Installation instructions
	if installChoice == "1" || installChoice == "" {
		fmt.Println("📦 Installation (if not already installed):")
		fmt.Println("No installation needed - uvx will install automatically")
	} else {
		fmt.Println("📦 Installation (if not already installed):")
		fmt.Println("pip install mcp-server-git")
	}

	return nil
}

func setupGitHubMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("🐙 GitHub MCP Server Setup")
	fmt.Println("==========================")
	fmt.Println()

	// Check if GitHub server already exists
	if _, exists := mcpConfig.Servers["github"]; exists {
		fmt.Print("GitHub MCP server is already configured. Reconfigure? (y/N): ")
		confirm, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
			fmt.Println("Setup cancelled.")
			return nil
		}
	}

	// Check for existing GitHub token
	githubToken := os.Getenv("GITHUB_PERSONAL_ACCESS_TOKEN")

	if githubToken == "" {
		fmt.Println("GitHub Personal Access Token is required.")
		fmt.Println("You can create one at: https://github.com/settings/tokens")
		fmt.Println("Required permissions: repo, read:user, read:org, issues")
		fmt.Println()
		fmt.Print("Enter your GitHub Personal Access Token: ")

		tokenInput, err := reader.ReadString('\n')
		if err != nil {
			return fmt.Errorf("failed to read token: %w", err)
		}
		githubToken = strings.TrimSpace(tokenInput)

		if githubToken == "" {
			return fmt.Errorf("GitHub token is required")
		}

		// Offer to set environment variable
		fmt.Print("Would you like to set GITHUB_PERSONAL_ACCESS_TOKEN environment variable? (y/N): ")
		setEnv, _ := reader.ReadString('\n')
		if strings.ToLower(strings.TrimSpace(setEnv)) == "y" {
			fmt.Println()
			fmt.Printf("Add this to your shell profile (~/.bashrc, ~/.zshrc, etc.):")
			fmt.Printf("\nexport GITHUB_PERSONAL_ACCESS_TOKEN=\"%s\"\n", githubToken)
			fmt.Println()
		}
	}

	// Installation method selection  
	fmt.Println("Select installation method:")
	fmt.Println("1. Remote server (recommended - no Docker needed)")
	fmt.Println("2. Local Docker")
	fmt.Print("Choice (1-2): ")

	installChoice, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read input: %w", err)
	}
	installChoice = strings.TrimSpace(installChoice)

	var serverConfig mcp.MCPServerConfig

	switch installChoice {
	case "1", "":
		// Remote HTTP server configuration (recommended)
		serverConfig = mcp.MCPServerConfig{
			Name:      "github",
			Type:      "http",
			URL:       "https://api.githubcopilot.com/mcp/",
			AutoStart: true,
			Timeout:   30 * time.Second,
			Env: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": githubToken,
			},
		}
	case "2":
		// Docker configuration
		serverConfig = mcp.MCPServerConfig{
			Name:        "github",
			Command:     "docker",
			Args:        []string{"run", "-i", "--rm", "-e", "GITHUB_PERSONAL_ACCESS_TOKEN", "ghcr.io/github/github-mcp-server"},
			AutoStart:   true,
			MaxRestarts: 3,
			Timeout:     30 * time.Second,
			Env: map[string]string{
				"GITHUB_PERSONAL_ACCESS_TOKEN": githubToken,
			},
		}
	default:
		return fmt.Errorf("invalid choice: %s", installChoice)
	}

	// Add server to config
	mcpConfig.Servers["github"] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	cfg, _ := configuration.LoadOrInitConfig(false)
	if err := mcp.SaveMCPConfig(cfg, *mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	fmt.Println("✅ GitHub MCP Server configured successfully!")
	if serverConfig.Type == "http" {
		fmt.Printf("Type: Remote HTTP server\n")
		fmt.Printf("URL: %s\n", serverConfig.URL)
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
	}
	fmt.Println()
	fmt.Println("To test the configuration, run: ledit mcp test github")
	fmt.Println()
	fmt.Println("📦 Features available:")
	fmt.Println("• Repository management and file operations")
	fmt.Println("• Issues and pull request automation")
	fmt.Println("• GitHub Actions workflow monitoring")
	fmt.Println("• Code analysis and security findings")

	return nil
}

func setupCustomMCPServer(mcpConfig *mcp.MCPConfig, reader *bufio.Reader) error {
	fmt.Println()
	fmt.Println("🔧 Custom MCP Server Setup")
	fmt.Println("==========================")
	fmt.Println()

	// Server name
	fmt.Print("Enter server name: ")
	nameInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read server name: %w", err)
	}
	serverName := strings.TrimSpace(nameInput)

	if serverName == "" {
		return fmt.Errorf("server name is required")
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

	// Command
	fmt.Print("Enter command to run the server: ")
	commandInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read command: %w", err)
	}
	command := strings.TrimSpace(commandInput)

	if command == "" {
		return fmt.Errorf("command is required")
	}

	// Arguments
	fmt.Print("Enter command arguments (space-separated, or press Enter for none): ")
	argsInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read arguments: %w", err)
	}
	argsStr := strings.TrimSpace(argsInput)

	var args []string
	if argsStr != "" {
		args = strings.Fields(argsStr)
	}

	// Environment variables
	fmt.Print("Enter environment variables (KEY=VALUE format, comma-separated, or press Enter for none): ")
	envInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read environment variables: %w", err)
	}
	envStr := strings.TrimSpace(envInput)

	env := make(map[string]string)
	if envStr != "" {
		envPairs := strings.Split(envStr, ",")
		for _, pair := range envPairs {
			pair = strings.TrimSpace(pair)
			if parts := strings.SplitN(pair, "=", 2); len(parts) == 2 {
				env[strings.TrimSpace(parts[0])] = strings.TrimSpace(parts[1])
			}
		}
	}

	// Working directory
	fmt.Print("Enter working directory (or press Enter for default): ")
	workDirInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read working directory: %w", err)
	}
	workDir := strings.TrimSpace(workDirInput)

	// Auto-start
	fmt.Print("Auto-start this server? (Y/n): ")
	autoStartInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read auto-start preference: %w", err)
	}
	autoStart := strings.ToLower(strings.TrimSpace(autoStartInput)) != "n"

	// Timeout
	fmt.Print("Server timeout in seconds (default: 30): ")
	timeoutInput, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read timeout: %w", err)
	}
	timeoutStr := strings.TrimSpace(timeoutInput)

	timeout := 30 * time.Second
	if timeoutStr != "" {
		if timeoutSecs, err := strconv.Atoi(timeoutStr); err == nil && timeoutSecs > 0 {
			timeout = time.Duration(timeoutSecs) * time.Second
		}
	}

	// Create server config
	serverConfig := mcp.MCPServerConfig{
		Name:        serverName,
		Command:     command,
		Args:        args,
		Env:         env,
		WorkingDir:  workDir,
		AutoStart:   autoStart,
		MaxRestarts: 3,
		Timeout:     timeout,
	}

	// Add server to config
	mcpConfig.Servers[serverName] = serverConfig
	mcpConfig.Enabled = true

	// Save config
	cfg, _ := configuration.LoadOrInitConfig(false)
	if err := mcp.SaveMCPConfig(cfg, *mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	fmt.Println()
	fmt.Printf("✅ Custom MCP Server '%s' configured successfully!\n", serverName)
	fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
	fmt.Println()
	fmt.Printf("To test the configuration, run: ledit mcp test %s\n", serverName)

	return nil
}

func runMCPRemove(serverName string) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config
	cfg, err := configuration.LoadOrInitConfig(false)
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

func runMCPList() error {
	// Load existing config
	cfg, err := configuration.LoadOrInitConfig(false)
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
		fmt.Println("Run 'ledit mcp add' to add a server.")
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
	fmt.Println("  ledit mcp test [server] - Test server connection")
	fmt.Println("  ledit mcp add          - Add new server")
	fmt.Println("  ledit mcp remove       - Remove server")

	return nil
}

func runMCPTest(serverName string) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config
	cfg, err := configuration.LoadOrInitConfig(false)
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
			fmt.Println("Run 'ledit mcp add' to add a server.")
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
	fmt.Printf("Command: %s %v\n", serverConfig.Command, serverConfig.Args)
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

func init() {
	mcpCmd.AddCommand(mcpAddCmd)
	mcpCmd.AddCommand(mcpRemoveCmd)
	mcpCmd.AddCommand(mcpListCmd)
	mcpCmd.AddCommand(mcpTestCmd)
}
