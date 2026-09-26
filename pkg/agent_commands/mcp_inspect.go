package commands

// mcp_inspect.go — the /mcp inspect path: removeServer, listServers,
// and testServer (connection probe + credential validation). Split out
// of mcp.go.
import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log"
	"os"
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
	fmt.Printf("Are you sure you want to remove server '%s'? %s: ", serverName, utils.DefaultChoiceHint(false))
	confirm, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("failed to read confirmation: %w", err)
	}

	if strings.ToLower(strings.TrimSpace(confirm)) != "y" {
		fmt.Println("Removal cancelled.")
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
	console.GlyphSuccess.Printf("Server '%s' removed successfully!", serverName)

	if len(mcpConfig.Servers) == 0 {
		fmt.Println("MCP disabled (no servers remain).")
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

	fmt.Println("MCP Configuration")
	fmt.Println("==================")
	fmt.Printf("Enabled: %t\n", redactedConfig.Enabled)
	fmt.Printf("Auto-start: %t\n", redactedConfig.AutoStart)
	fmt.Printf("Auto-discover: %t\n", redactedConfig.AutoDiscover)
	fmt.Printf("Default timeout: %v\n", redactedConfig.Timeout)
	fmt.Printf("Total servers: %d\n", len(redactedConfig.Servers))
	fmt.Println()

	if len(redactedConfig.Servers) == 0 {
		fmt.Println("No MCP servers configured.")
		fmt.Println("Run '/mcp add' to add a server.")
		return nil
	}

	fmt.Println("Configured Servers:")
	fmt.Println("-------------------")

	for name, server := range redactedConfig.Servers {
		fmt.Printf("[signal] %s\n", name)
		if server.Type == "http" {
			fmt.Printf("   Type: HTTP Remote Server\n")
			fmt.Printf("   URL: %s\n", secretdetect.RedactOpaque(server.URL))
		} else {
			fmt.Printf("   Command: %s %v\n", server.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", server.Args)))
		}
		fmt.Printf("   Auto-start: %t\n", server.AutoStart)
		fmt.Printf("   Max restarts: %d\n", server.MaxRestarts)
		fmt.Printf("   Timeout: %v\n", server.Timeout)

		if server.WorkingDir != "" {
			fmt.Printf("   Working dir: %s\n", server.WorkingDir)
		}

		if len(server.Env) > 0 {
			fmt.Printf("   Environment vars: ")
			envEntries := make([]string, 0, len(server.Env))
			for key, value := range server.Env {
				envEntries = append(envEntries, key+"="+value)
			}
			fmt.Printf("%s\n", strings.Join(envEntries, ", "))
		}

		// Show credentials if present (placeholder references are safe; actual secrets are masked)
		if len(server.Credentials) > 0 {
			fmt.Printf("   Credentials: ")
			credEntries := make([]string, 0, len(server.Credentials))
			for key, value := range server.Credentials {
				credEntries = append(credEntries, key+"="+value)
			}
			fmt.Printf("%s\n", strings.Join(credEntries, ", "))
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

	// Load existing config (no longer needed for MCP)

	mcpConfig, err := mcp.LoadMCPConfig()
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

	fmt.Printf("[test] Testing MCP Server: %s\n", serverName)
	fmt.Println("========================")
	if serverConfig.Type == "http" {
		fmt.Printf("Type: HTTP Remote Server\n")
		fmt.Printf("URL: %s\n", secretdetect.RedactOpaque(serverConfig.URL))
	} else {
		fmt.Printf("Command: %s %v\n", serverConfig.Command, secretdetect.RedactOpaque(fmt.Sprintf("%v", serverConfig.Args)))
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
		return errors.New("failed to get server from manager")
	}

	fmt.Println("[...] Starting server...")
	if err := server.Start(ctx); err != nil {
		return fmt.Errorf("failed to start server: %w", err)
	}

	defer func() {
		console.GlyphStopped.Print("Stopping server...")
		server.Stop(context.Background())
	}()

	console.GlyphSuccess.Print("Server started successfully!")

	fmt.Println("[~] Initializing server...")
	if err := server.Initialize(ctx); err != nil {
		return fmt.Errorf("failed to initialize server: %w", err)
	}
	console.GlyphSuccess.Print("Server initialized successfully!")

	fmt.Println("[search] Listing available tools...")
	tools, err := server.ListTools(ctx)
	if err != nil {
		return fmt.Errorf("failed to list tools: %w", err)
	}

	if len(tools) == 0 {
		console.GlyphWarning.Print("No tools available from this server.")
		return nil
	}

	console.GlyphSuccess.Printf("Found %d tools:", len(tools))
	fmt.Println()

	for i, tool := range tools {
		fmt.Printf("%d. %s\n", i+1, tool.Name)
		if tool.Description != "" {
			fmt.Printf("   Description: %s\n", tool.Description)
		}
		fmt.Println()
	}

	fmt.Printf("[done] Test completed successfully! Server '%s' is working properly.\n", serverName)

	return nil
}
