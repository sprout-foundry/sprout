//go:build !js

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/sprout-foundry/sprout/pkg/configuration"
	"github.com/sprout-foundry/sprout/pkg/console"
	"github.com/sprout-foundry/sprout/pkg/mcp"
)

func runMCPRemove(serverName string) error {
	reader := bufio.NewReader(os.Stdin)

	// Load existing config
	_, err := configuration.LoadOrInitConfig(false)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

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
	if err := mcp.SaveMCPConfig(&mcpConfig); err != nil {
		return fmt.Errorf("failed to save MCP config: %w", err)
	}

	console.GlyphSuccess.Fprintf(os.Stdout, "Server '%s' removed successfully!", serverName)

	if len(mcpConfig.Servers) == 0 {
		fmt.Println("MCP disabled (no servers remain).")
	}

	return nil
}
