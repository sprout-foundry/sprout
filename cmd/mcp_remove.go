//go:build !js

package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"sort"
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

		// Build a stable, alphabetically-ordered list so the picker is
		// deterministic across runs (the map iteration order is not).
		serverNames := make([]string, 0, len(mcpConfig.Servers))
		for name := range mcpConfig.Servers {
			serverNames = append(serverNames, name)
		}
		sort.Strings(serverNames)

		items := make([]console.SelectItem, 0, len(serverNames))
		for _, name := range serverNames {
			srv := mcpConfig.Servers[name]
			items = append(items, console.SelectItem{
				Label:  name,
				Detail: srv.Type,
				Value:  name,
			})
		}

		sl := console.NewSelectList(console.SelectListOptions{
			Title:      "Pick a server to remove",
			Items:      items,
			Searchable: true,
			PageSize:   10,
		})

		ctx := context.Background()
		value, ok, err := sl.Run(ctx)
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println()
			console.GlyphInfo.Print("Removal cancelled.")
			return nil
		}
		serverName = value
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
